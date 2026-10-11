package app_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/app/upgrade"
)

// TestUpgradesAreNamedForTheirPackage checks that registered plans match their app/upgrade/v<N>
// directory names, which rehearsal and nightly e2e tooling schedule.
func TestUpgradesAreNamedForTheirPackage(t *testing.T) {
	if len(app.Upgrades) == 0 {
		t.Skip("no upgrade is registered")
	}
	arkApp := apptestutil.Setup(t, false)
	for _, build := range app.Upgrades {
		u := build(arkApp)
		handler := runtime.FuncForPC(reflect.ValueOf(u.Handler).Pointer()).Name()
		require.Contains(t, handler, "/app/upgrade/"+u.PlanName+".",
			"plan %q is built by %s, not by app/upgrade/%s", u.PlanName, handler, u.PlanName)
	}
}

// TestRegisteredUpgradeRestart drives a registered upgrade across the restart a halt forces: the
// halted binary left upgrade-info.json, and the next one installs the plan's store loader before
// loading state, then runs the handler at the plan height. The plan deletes a mounted store, so
// whether the loader ran shows in that store's contents.
func TestRegisteredUpgradeRestart(t *testing.T) {
	const (
		planName    = "synthetic"
		planHeight  = int64(3)
		markerValue = "kept"
	)
	marker := []byte("upgrade-test-marker")

	tests := []struct {
		name     string
		schedule bool   // the plan is in state at planHeight
		infoName string // the plan upgrade-info.json names
		skip     bool   // the operator skips planHeight
		loaded   bool   // the store loader applied the plan's store upgrades
		ran      []int64
	}{
		{name: "matching plan loads its stores and runs at its height", schedule: true, infoName: planName, loaded: true, ran: []int64{planHeight}},
		{name: "a stale plan name loads nothing", infoName: "earlier"},
		{name: "a skipped height loads nothing and runs nothing", schedule: true, infoName: planName, skip: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := dbm.NewMemDB()
			// The Wasm VM may hold its home's cache lock past Close, so the restarted binary gets a
			// home of its own, holding the halted one's upgrade-info.json.
			opts := simtestutil.AppOptionsMap{flags.FlagHome: t.TempDir()}
			restarted := simtestutil.AppOptionsMap{flags.FlagHome: t.TempDir()}
			if tc.skip {
				restarted[server.FlagUnsafeSkipUpgrades] = []int{int(planHeight)}
			}

			// The binary before the upgrade commits two blocks, writing the marker and scheduling
			// the plan in the second, and halts with upgrade-info.json on disk.
			before := apptestutil.NewArkappWithCustomOptions(t, false, apptestutil.SetupOptions{Logger: log.NewNopLogger(), DB: db, AppOpts: opts})
			_, err := before.FinalizeBlock(&cmtabci.RequestFinalizeBlock{Height: 1})
			require.NoError(t, err)
			_, err = before.Commit()
			require.NoError(t, err)
			ctx := before.NewNextBlockContext(cmtproto.Header{Height: 2})
			ctx.KVStore(before.GetKey(feegrant.StoreKey)).Set(marker, []byte(markerValue))
			plan := upgradetypes.Plan{Name: tc.infoName, Height: planHeight}
			if tc.schedule {
				require.NoError(t, before.UpgradeKeeper.ScheduleUpgrade(ctx, plan))
			}
			_, err = before.FinalizeBlock(&cmtabci.RequestFinalizeBlock{Height: 2})
			require.NoError(t, err)
			_, err = before.Commit()
			require.NoError(t, err)
			require.NoError(t, before.UpgradeKeeper.DumpUpgradeInfoToDisk(planHeight, plan))
			written, err := before.UpgradeKeeper.GetUpgradeInfoPath()
			require.NoError(t, err)
			info, err := os.ReadFile(written)
			require.NoError(t, err)
			require.NoError(t, before.Close())
			dataDir := filepath.Join(restarted[flags.FlagHome].(string), "data")
			require.NoError(t, os.MkdirAll(dataDir, 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(dataDir, upgradetypes.UpgradeInfoFilename), info, 0o600)) //nolint:gosec // under t.TempDir()

			var ran []int64
			registered := app.Upgrades
			app.Upgrades = append(slices.Clip(registered), func(*app.ArkApp) upgrade.Upgrade {
				return upgrade.Upgrade{
					PlanName:      planName,
					StoreUpgrades: storetypes.StoreUpgrades{Deleted: []string{feegrant.StoreKey}},
					Handler: func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
						ran = append(ran, sdk.UnwrapSDKContext(ctx).BlockHeight())
						return fromVM, nil
					},
				}
			})
			t.Cleanup(func() { app.Upgrades = registered })

			after := app.NewArkApp(log.NewNopLogger(), db, true, restarted)
			t.Cleanup(func() { require.NoError(t, after.Close()) })
			stored := after.NewUncachedContext(false, cmtproto.Header{Height: planHeight}).KVStore(after.GetKey(feegrant.StoreKey)).Get(marker) //nolint:staticcheck // reads the loaded root store
			if tc.loaded {
				require.Nil(t, stored, "the loader deleted the store")
			} else {
				require.Equal(t, []byte(markerValue), stored, "no loader touched the store")
			}

			_, err = after.FinalizeBlock(&cmtabci.RequestFinalizeBlock{Height: planHeight})
			require.NoError(t, err)
			require.Equal(t, tc.ran, ran)
		})
	}
}
