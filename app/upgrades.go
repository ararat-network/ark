package app

import "fmt"

// setupUpgrades wires the pending coordinated upgrade, if any: the handler run
// at the upgrade height, and the store loader applied on the restart into it.
// Each upgrade registers under its plan name; the release after it executes
// deletes the registration. Empty until the first post-genesis upgrade.
func (app *ArkApp) setupUpgrades() error {
	// app.UpgradeKeeper.SetUpgradeHandler(name, handler) per pending upgrade.

	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		return fmt.Errorf("read upgrade info from disk: %w", err)
	}
	if upgradeInfo.Name == "" || app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		return nil
	}
	// Store-loader registration per pending upgrade with store changes goes
	// here, matching on upgradeInfo.Name. upgrade-info.json outlives the
	// upgrade that wrote it, so an unmatched name is normal, never an error.
	return nil
}
