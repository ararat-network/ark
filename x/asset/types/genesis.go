package types

import (
	"fmt"

	chain "github.com/ararat-network/ark/pkg/chain"
)

// DefaultGenesisState returns the launch asset registry.
func DefaultGenesisState() *GenesisState {
	denoms := []string{
		chain.CNYBaseDenom,
		chain.EURBaseDenom,
		chain.GBPBaseDenom,
		chain.JPYBaseDenom,
		chain.KRWBaseDenom,
		chain.MNTBaseDenom,
		chain.USDBaseDenom,
	}
	assets := make([]Asset, len(denoms))
	for i, denom := range denoms {
		assets[i] = Asset{
			Denom:    denom,
			Metadata: chain.NativeAssetMetadata(denom),
			Status:   AssetStatus_ASSET_STATUS_ACTIVE,
			Version:  1,
		}
	}

	return &GenesisState{
		Params:           DefaultParams(),
		Assets:           assets,
		EmergencyMandate: DefaultEmergencyMandate(),
	}
}

// Validate checks asset identities and their internal consistency. Feed
// existence is a cross-module rule the keeper checks at InitGenesis, because
// the feed registry lives in x/oracle.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}

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

	settlementPlans := make(map[string]SettlementPlan, len(gs.SettlementPlans))
	for i, plan := range gs.SettlementPlans {
		if err := plan.Validate(); err != nil {
			return err
		}
		if i > 0 && plan.Denom <= gs.SettlementPlans[i-1].Denom {
			return fmt.Errorf("genesis settlement plans must be sorted by unique denom")
		}
		if _, exists := assets[plan.Denom]; !exists {
			return fmt.Errorf("settlement plan %s has no registered asset", plan.Denom)
		}
		settlementPlans[plan.Denom] = plan
	}

	latestWriteOffVersion := make(map[string]uint64)
	for i, record := range gs.ResolutionRecords {
		if err := record.Validate(); err != nil {
			return err
		}
		if i > 0 {
			previous := gs.ResolutionRecords[i-1]
			if record.Denom < previous.Denom ||
				(record.Denom == previous.Denom && record.Version <= previous.Version) {
				return fmt.Errorf(
					"genesis resolution records must be sorted by unique denom and version",
				)
			}
		}
		asset, exists := assets[record.Denom]
		if !exists {
			return fmt.Errorf("resolution record %s has no registered asset", record.Denom)
		}
		if record.Version > asset.Version {
			return fmt.Errorf(
				"resolution record %s version %d exceeds asset version %d",
				record.Denom,
				record.Version,
				asset.Version,
			)
		}
		if record.Kind == ResolutionKind_RESOLUTION_KIND_WRITE_OFF {
			latestWriteOffVersion[record.Denom] = record.Version
		}
	}

	if err := gs.EmergencyMandate.Validate(); err != nil {
		return err
	}
	// A suspension is recorded only by a live committee and cleared by every
	// replacement, disablement included, so no running chain can hold usage
	// under a disabled mandate. Importing that pair would seed a per-term bound
	// belonging to nobody, which the next appointment silently clears — the
	// same reason the mandate itself must carry no delegated power once
	// disabled.
	if gs.EmergencyMandate.IsDisabled() && len(gs.EmergencySuspensions) > 0 {
		return fmt.Errorf("disabled emergency mandate must record no suspensions")
	}
	for i, denom := range gs.EmergencySuspensions {
		if i > 0 && denom <= gs.EmergencySuspensions[i-1] {
			return fmt.Errorf("genesis emergency suspensions must be sorted by unique denom")
		}
		if _, exists := assets[denom]; !exists {
			return fmt.Errorf("emergency suspension %s has no registered asset", denom)
		}
	}

	for _, asset := range gs.Assets {
		_, hasSettlement := settlementPlans[asset.Denom]
		if asset.Status != AssetStatus_ASSET_STATUS_SUSPENDED && hasSettlement {
			return fmt.Errorf(
				"%s asset %s must not have a settlement plan",
				asset.Status,
				asset.Denom,
			)
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
