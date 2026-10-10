package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	sdk "github.com/cosmos/cosmos-sdk/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/pkg/chain"
)

// TestDryRunAssembly runs launch assembly as the validators run it, each from
// its own home: seven seats on the launch artefact, one gentx per seat at the
// gentx default of 10%, collection, and the dry run, which lists every seat
// bonded at one seat. A seated file without gentxs has no validators. A gentx
// under the 5% staking floor passes collection and genesis validate, which
// check each gentx alone, and fails the dry run instead of genesis_time.
func TestDryRunAssembly(t *testing.T) {
	run := func(home string, args ...string) (string, error) {
		var out bytes.Buffer
		rootCmd := NewRootCmd()
		rootCmd.SetOut(&out)
		rootCmd.SetArgs(append(args, fmt.Sprintf("--%s=%s", flags.FlagHome, home)))
		err := svrcmd.Execute(rootCmd, "", app.DefaultNodeHome)
		return out.String(), err
	}

	coordinator := t.TempDir()
	genesisFile := filepath.Join(coordinator, "config", "genesis.json")
	gentxDir := filepath.Join(coordinator, "config", "gentx")
	require.NoError(t, os.MkdirAll(gentxDir, 0o755))
	appGenesis, err := genutiltypes.AppGenesisFromFile(filepath.Join("..", "..", "..", "app", "genesis", "genesis.json"))
	require.NoError(t, err)
	appGenesis.GenesisTime = time.Date(2027, time.January, 4, 12, 0, 0, 0, time.UTC)
	require.NoError(t, appGenesis.SaveAs(genesisFile))

	cdc := clientCodec(t)
	homes := make([]string, chain.MinLaunchSeats)
	seatArgs := []string{"genesis", "add-validator-seats"}
	for i := range homes {
		homes[i] = t.TempDir()
		kr, err := keyring.New(sdk.KeyringServiceName(), keyring.BackendTest, homes[i], nil, cdc)
		require.NoError(t, err)
		record, _, err := kr.NewMnemonic("seat", keyring.English, sdk.FullFundraiserPath, keyring.DefaultBIP39Passphrase, hd.Secp256k1)
		require.NoError(t, err)
		operator, err := record.GetAddress()
		require.NoError(t, err)
		if i == 0 {
			seatArgs = append(seatArgs, fmt.Sprintf("--%s=%s", flagFounder, operator))
		}
		seatArgs = append(seatArgs, operator.String())
	}
	_, err = run(coordinator, seatArgs...)
	require.NoError(t, err)
	_, err = run(coordinator, "genesis", "dry-run")
	require.ErrorContains(t, err, "validator set is empty after InitGenesis", "a seated file without gentxs has no validators")

	// Every validator signs against the seated file in its own home.
	seated, err := os.ReadFile(genesisFile)
	require.NoError(t, err)
	seat := sdk.NewCoin(chain.NoahBaseDenom, chain.SeatGrant).String()
	gentx := func(i int, commission string) {
		t.Helper()
		_, err := run(homes[i], "genesis", "gentx", "seat", seat,
			"--chain-id="+appGenesis.ChainID,
			"--keyring-backend=test",
			fmt.Sprintf("--moniker=seat%d", i),
			"--commission-rate="+commission,
			"--output-document="+filepath.Join(gentxDir, fmt.Sprintf("seat%d.json", i)),
		)
		require.NoError(t, err)
	}
	for i, home := range homes {
		require.NoError(t, os.MkdirAll(filepath.Join(home, "config"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(home, "config", "genesis.json"), seated, 0o600)) //nolint:gosec // a copy of the seated file under t.TempDir()
		gentx(i, "0.1")
	}
	_, err = run(coordinator, "genesis", "collect-gentxs")
	require.NoError(t, err)

	out, err := run(coordinator, "genesis", "dry-run", genesisFile)
	require.NoError(t, err)
	require.Contains(t, out, fmt.Sprintf("%s boots at height 1, 2027-01-04T12:00:00Z; bonded validators: %d", appGenesis.ChainID, len(homes)))
	for i := range homes {
		require.Regexp(t, fmt.Sprintf(`(?m)^seat%d +arkvaloper1\w+ +%d +0\.1$`, i, chain.SeatGrantNoah), out, "every seat bonds one seat, the founder's too")
	}

	last := len(homes) - 1
	require.NoError(t, os.Remove(filepath.Join(gentxDir, fmt.Sprintf("seat%d.json", last))))
	gentx(last, "0.01")
	_, err = run(coordinator, "genesis", "collect-gentxs")
	require.NoError(t, err)
	_, err = run(coordinator, "genesis", "validate")
	require.NoError(t, err, "validate checks each gentx alone, never against the staking floor")
	_, err = run(coordinator, "genesis", "dry-run")
	require.ErrorContains(t, err, "commission cannot be less than min rate")
}
