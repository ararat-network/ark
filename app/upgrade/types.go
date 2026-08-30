// Package upgrade holds the value a coordinated upgrade hands the app: plan
// name, store changes, and handler coupled as one value, so registering one
// half without the other is impossible. Version packages under
// app/upgrade/v<N> build it; app/upgrades.go's pendingUpgrades var lists the
// builders. This file is fixed: an upgrade's dependencies travel through its
// own Build signature, never through a shared type here.
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
