package app

import (
	"fmt"

	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"github.com/ararat-network/ark/app/upgrade"
)

// Upgrades lists the coordinated upgrade builders registered by this binary. Builders receive live
// app state for their handlers; see upgrade/README.md for the registration workflow.
var Upgrades []func(*ArkApp) upgrade.Upgrade

// setupUpgrades registers Upgrades handlers and installs the matching store loader
// for a scheduled restart.
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
