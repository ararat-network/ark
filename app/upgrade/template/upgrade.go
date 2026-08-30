// Package template is the scaffold for a coordinated upgrade, compiled but
// never imported, so the shape stays checked between upgrades. To ship
// upgrade v<N>: copy this directory to app/upgrade/v<N>, rename the package
// and plan name to the release tag, fill in the migration, and list a
// builder closure calling Build in app/upgrades.go's pendingUpgrades var.
// The release after the upgrade executes deletes the package and the list
// entry; git history is the archive.
package template

import (
	"context"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"github.com/ararat-network/ark/app/upgrade"
)

// PlanName is exported for the migration test; everything else leaves through
// Build.
const PlanName = "template"

// Build couples the plan name, store changes, and handler as the one value
// the registry lists. It takes exactly the keepers the migration touches —
// the blast radius is the signature — never the whole app, which the import
// cycle forbids anyway.
func Build(
	mm *module.Manager,
	cfg module.Configurator,
) upgrade.Upgrade {
	return upgrade.Upgrade{
		PlanName:      PlanName,
		StoreUpgrades: storetypes.StoreUpgrades{},
		Handler: func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			// One-shot state surgery goes before RunMigrations if it must
			// see pre-migration state, after it otherwise.
			return mm.RunMigrations(ctx, cfg, fromVM)
		},
	}
}
