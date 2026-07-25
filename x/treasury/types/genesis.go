package types

import (
	"encoding/json"
	"fmt"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
)

// NewGenesisState creates a Treasury genesis state.
func NewGenesisState(
	params Params,
	monetaryPolicy MonetaryPolicy,
	taxCaps []TaxCap,
	claimsMandate ClaimsMandate,
	claimsAllowanceUsed math.Int,
	insuranceReserved math.Int,
	nextClaimID uint64,
	claims []Claim,
	rewardFunding RewardFundingState,
	monetaryMandate MonetaryMandate,
) *GenesisState {
	return &GenesisState{
		Params:              params,
		MonetaryPolicy:      monetaryPolicy,
		TaxCaps:             append([]TaxCap(nil), taxCaps...),
		ClaimsMandate:       claimsMandate,
		ClaimsAllowanceUsed: claimsAllowanceUsed,
		InsuranceReserved:   insuranceReserved,
		NextClaimId:         nextClaimID,
		Claims:              append([]Claim(nil), claims...),
		RewardFunding:       rewardFunding,
		MonetaryMandate:     monetaryMandate,
	}
}

// DefaultGenesisState returns the safe, unconfigured Treasury genesis state.
func DefaultGenesisState() *GenesisState {
	return NewGenesisState(
		DefaultParams(),
		DefaultMonetaryPolicy(),
		[]TaxCap{},
		DefaultClaimsMandate(),
		math.ZeroInt(),
		math.ZeroInt(),
		1,
		[]Claim{},
		DefaultRewardFundingState(),
		DefaultMonetaryMandate(),
	)
}

// Validate checks the context-free Treasury genesis invariants. Fund balances
// and Oracle-dependent tax-cap coverage are validated by the keeper.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	if err := gs.MonetaryPolicy.Validate(); err != nil {
		return err
	}

	for i, taxCap := range gs.TaxCaps {
		if err := chain.ValidateNativeBaseDenom(taxCap.Denom); err != nil {
			return fmt.Errorf("tax cap denom %q is invalid: %w", taxCap.Denom, err)
		}
		if taxCap.TaxCap.IsNil() {
			return fmt.Errorf("tax cap for %s must be set", taxCap.Denom)
		}
		if taxCap.TaxCap.IsNegative() {
			return fmt.Errorf("tax cap for %s must be zero or positive", taxCap.Denom)
		}
		if gs.Params.ReferenceTaxCap.IsZero() && !taxCap.TaxCap.IsZero() {
			return fmt.Errorf("tax cap for %s must be zero when the reference tax cap is zero", taxCap.Denom)
		}
		if gs.Params.ReferenceTaxCap.IsPositive() && taxCap.TaxCap.IsZero() {
			return fmt.Errorf("tax cap for %s must be positive when the reference tax cap is positive", taxCap.Denom)
		}
		if i > 0 && taxCap.Denom <= gs.TaxCaps[i-1].Denom {
			return fmt.Errorf("genesis tax caps must be sorted by unique denom")
		}
	}

	if err := gs.validateClaims(); err != nil {
		return err
	}
	if err := gs.validateRewardFunding(); err != nil {
		return err
	}
	if err := gs.MonetaryMandate.Validate(); err != nil {
		return fmt.Errorf("invalid monetary mandate: %w", err)
	}
	if gs.MonetaryMandate.Committee != "" && gs.MonetaryMandate.Committee == gs.ClaimsMandate.Committee {
		return fmt.Errorf("monetary-policy committee must be distinct from Claims committee")
	}

	return nil
}

// ParseCanonicalAccountAddress parses one canonical bech32 account address.
func ParseCanonicalAccountAddress(field, value string) (sdk.AccAddress, error) {
	address, err := sdk.AccAddressFromBech32(value)
	if err != nil {
		return nil, fmt.Errorf("%s is invalid: %w", field, err)
	}
	if address.String() != value {
		return nil, fmt.Errorf("%s must be a canonical account address", field)
	}
	return address, nil
}

// GetGenesisStateFromAppState returns the Treasury genesis state from raw app
// genesis state.
func GetGenesisStateFromAppState(cdc codec.JSONCodec, appState map[string]json.RawMessage) *GenesisState {
	var genesisState GenesisState
	if appState[ModuleName] != nil {
		cdc.MustUnmarshalJSON(appState[ModuleName], &genesisState)
	}
	return &genesisState
}
