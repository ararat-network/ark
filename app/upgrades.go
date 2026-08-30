package app

import (
	"fmt"

	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"github.com/ararat-network/ark/app/upgrade"
)

// Upgrades lists the coordinated upgrades this binary can execute —
// normally one or none — as builders, because a handler closes over live app
// state no package-level literal can reach. The only registration a release
// edits:
//
//	func(app *ArkApp) upgrade.Upgrade {
//		return v2.Build(app.ModuleManager, app.Configurator())
//	},
//
// with the keepers the migration touches appended to Build's signature.
var Upgrades []func(*ArkApp) upgrade.Upgrade

// setupUpgrades wires every upgrade pendingUpgrades lists: the handler run at
// the upgrade height, and the store loader applied on the restart into it.
// Fixed machinery: a release edits only the list above and its own
// app/upgrade/v<N> package.
func (app *ArkApp) setupUpgrades() error {
	pending := make([]upgrade.Upgrade, len(Upgrades))
	for i, build := range Upgrades {
		pending[i] = build(app)
	}
	for _, u := range pending {
		app.UpgradeKeeper.SetUpgradeHandler(u.PlanName, u.Handler)
	}

	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		return fmt.Errorf("read upgrade info from disk: %w", err)
	}
	if upgradeInfo.Name == "" || app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		return nil
	}
	// upgrade-info.json outlives the upgrade that wrote it, so an unmatched
	// name is normal, never an error.
	for _, u := range pending {
		if upgradeInfo.Name == u.PlanName {
			app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &u.StoreUpgrades))
		}
	}
	return nil
}
