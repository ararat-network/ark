package types

import (
	"encoding/json"
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"
)

// DefaultGenesisState returns the launch asset registry and target epoch.
func DefaultGenesisState() *GenesisState {
	assets := DefaultAssets()
	denoms := make([]string, len(assets))
	for i, asset := range assets {
		denoms[i] = asset.Denom
	}

	return &GenesisState{
		Assets:        assets,
		OracleTargets: NewOracleTargets(denoms),
	}
}

// Validate checks asset identities and their consistency with active and
// pending target epochs.
func (gs GenesisState) Validate() error {
	assets := make(map[string]Asset, len(gs.Assets))
	for i, asset := range gs.Assets {
		if err := asset.Validate(); err != nil {
			return err
		}
		if i > 0 && asset.Denom <= gs.Assets[i-1].Denom {
			return fmt.Errorf("genesis assets must be sorted by unique denom")
		}
		assets[asset.Denom] = asset
	}

	if err := gs.OracleTargets.Validate(); err != nil {
		return err
	}

	settlementPlans := make(map[string]SettlementPlan, len(gs.SettlementPlans))
	for i, plan := range gs.SettlementPlans {
		if err := plan.Validate(); err != nil {
			return err
		}
		if i > 0 && plan.Denom <= gs.SettlementPlans[i-1].Denom {
			return fmt.Errorf("genesis settlement plans must be sorted by unique denom")
		}
		asset, exists := assets[plan.Denom]
		if !exists {
			return fmt.Errorf("settlement plan %s has no registered asset", plan.Denom)
		}
		if plan.Version != asset.Version {
			return fmt.Errorf(
				"settlement plan %s version %d must match asset version %d",
				plan.Denom,
				plan.Version,
				asset.Version,
			)
		}
		settlementPlans[plan.Denom] = plan
	}

	latestWriteOffVersion := make(map[string]uint64)
	for i, record := range gs.WriteOffRecords {
		if err := record.Validate(); err != nil {
			return err
		}
		if i > 0 {
			previous := gs.WriteOffRecords[i-1]
			if record.Denom < previous.Denom ||
				(record.Denom == previous.Denom && record.Version <= previous.Version) {
				return fmt.Errorf(
					"genesis write-off records must be sorted by unique denom and version",
				)
			}
		}
		asset, exists := assets[record.Denom]
		if !exists {
			return fmt.Errorf("write-off record %s has no registered asset", record.Denom)
		}
		if record.Version > asset.Version {
			return fmt.Errorf(
				"write-off record %s version %d exceeds asset version %d",
				record.Denom,
				record.Version,
				asset.Version,
			)
		}
		latestWriteOffVersion[record.Denom] = record.Version
	}

	active := denomSet(gs.OracleTargets.Denoms)
	staged := active
	if gs.OracleTargets.Pending != nil {
		staged = denomSet(gs.OracleTargets.Pending.Denoms)
	}

	for denom := range active {
		if _, exists := assets[denom]; !exists {
			return fmt.Errorf("active vote target %s has no registered asset", denom)
		}
	}
	for denom := range staged {
		if _, exists := assets[denom]; !exists {
			return fmt.Errorf("pending vote target %s has no registered asset", denom)
		}
	}
	for _, asset := range gs.Assets {
		if err := validateAssetTargetState(
			asset,
			active[asset.Denom],
			staged[asset.Denom],
			gs.OracleTargets.Pending != nil,
		); err != nil {
			return err
		}
		_, hasSettlement := settlementPlans[asset.Denom]
		switch asset.Status {
		case AssetStatus_ASSET_STATUS_SETTLING:
			if !hasSettlement {
				return fmt.Errorf("settling asset %s must have a settlement plan", asset.Denom)
			}
		case AssetStatus_ASSET_STATUS_RELISTING:
			if hasSettlement &&
				settlementPlans[asset.Denom].EarliestClosingHeight == 0 {
				return fmt.Errorf(
					"relisting asset %s settlement plan must have an earliest closing height",
					asset.Denom,
				)
			}
		default:
			if hasSettlement {
				return fmt.Errorf(
					"%s asset %s must not have a settlement plan",
					asset.Status,
					asset.Denom,
				)
			}
		}
		if asset.Status == AssetStatus_ASSET_STATUS_WRITTEN_OFF &&
			latestWriteOffVersion[asset.Denom] != asset.Version {
			return fmt.Errorf(
				"written-off asset %s version %d must have a matching write-off record",
				asset.Denom,
				asset.Version,
			)
		}
	}

	return nil
}

// GetGenesisStateFromAppState returns x/asset GenesisState from application
// genesis state.
func GetGenesisStateFromAppState(
	cdc codec.JSONCodec,
	appState map[string]json.RawMessage,
) *GenesisState {
	var genesisState GenesisState
	if appState[ModuleName] != nil {
		cdc.MustUnmarshalJSON(appState[ModuleName], &genesisState)
	}

	return &genesisState
}

func denomSet(denoms []string) map[string]bool {
	set := make(map[string]bool, len(denoms))
	for _, denom := range denoms {
		set[denom] = true
	}

	return set
}

func validateAssetTargetState(asset Asset, active, staged, hasPending bool) error {
	if !asset.OracleRequired && (active || staged) {
		return fmt.Errorf(
			"asset %s without an Oracle requirement must not be a vote target",
			asset.Denom,
		)
	}

	switch asset.Status {
	case AssetStatus_ASSET_STATUS_PENDING:
		if active {
			return fmt.Errorf("pending asset %s must not be an active vote target", asset.Denom)
		}
		if staged && !hasPending {
			return fmt.Errorf("pending asset %s target addition must be scheduled", asset.Denom)
		}
	case AssetStatus_ASSET_STATUS_ACTIVE, AssetStatus_ASSET_STATUS_RETIRING:
		if asset.OracleRequired && (!active || !staged) {
			return fmt.Errorf(
				"priced %s asset %s must remain in active and staged vote targets",
				asset.Status.String(),
				asset.Denom,
			)
		}
	case AssetStatus_ASSET_STATUS_REMOVAL_PENDING:
		if !hasPending || !active || staged {
			return fmt.Errorf(
				"removal-pending asset %s must be active and absent from pending vote targets",
				asset.Denom,
			)
		}
	case AssetStatus_ASSET_STATUS_DELISTING:
		if !hasPending || !active || staged {
			return fmt.Errorf(
				"delisting asset %s must be active and absent from pending vote targets",
				asset.Denom,
			)
		}
	case AssetStatus_ASSET_STATUS_RETIRED:
		if active || staged {
			return fmt.Errorf("retired asset %s must not be a vote target", asset.Denom)
		}
	case AssetStatus_ASSET_STATUS_DELISTED:
		if active || staged {
			return fmt.Errorf("delisted asset %s must not be a vote target", asset.Denom)
		}
	case AssetStatus_ASSET_STATUS_SETTLING:
		if active || staged {
			return fmt.Errorf("settling asset %s must not be a vote target", asset.Denom)
		}
	case AssetStatus_ASSET_STATUS_WRITTEN_OFF:
		if active || staged {
			return fmt.Errorf("written-off asset %s must not be a vote target", asset.Denom)
		}
	case AssetStatus_ASSET_STATUS_RELISTING:
		if !staged || (!active && !hasPending) {
			return fmt.Errorf(
				"relisting asset %s must be active or scheduled for target addition",
				asset.Denom,
			)
		}
	}

	return nil
}
