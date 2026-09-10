// Package upgrade couples plan name, store changes, and handler in one descriptor. Versioned
// builders receive their own dependencies and are registered in app.Upgrades; see README.md.
package upgrade

import (
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

// Upgrade is one coordinated upgrade this binary can execute.
type Upgrade struct {
	// PlanName is the name a governance MsgSoftwareUpgrade schedules.
	// Directory, package, and release tag all match it.
	PlanName string

	// StoreUpgrades declares the stores the upgrade adds, renames, or
	// deletes. The store loader applies it on the restart into the upgrade
	// height, before Handler runs, so a store added here is usable there.
	StoreUpgrades storetypes.StoreUpgrades

	// Handler runs once at the upgrade height.
	Handler upgradetypes.UpgradeHandler
}
