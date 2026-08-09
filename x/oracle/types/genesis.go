package types

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
)

// NewGenesisState creates a new GenesisState object
func NewGenesisState(
	params Params,
	exchangeRates []ExchangeRate,
	rewardWeights []RewardWeight,
	attendanceRecords []AttendanceRecord,
	accounting Accounting,
	feeds Feeds,
	referenceDenom string,
) *GenesisState {
	return &GenesisState{
		Params:            params,
		ExchangeRates:     exchangeRates,
		RewardWeights:     rewardWeights,
		AttendanceRecords: attendanceRecords,
		Accounting:        accounting,
		Feeds:             feeds,
		ReferenceDenom:    referenceDenom,
	}
}

// NewAccounting starts reward and attendance accounting from genesis using
// the supplied active parameter windows.
func NewAccounting(params Params) Accounting {
	return Accounting{
		RewardWindow:             params.RewardWindow,
		RewardDistributionWindow: params.RewardDistributionWindow,
		AttendanceWindow:         params.AttendanceWindow,
	}
}

// DefaultGenesisState - default GenesisState
func DefaultGenesisState() *GenesisState {
	params := DefaultParams()
	return NewGenesisState(
		params,
		[]ExchangeRate{},
		[]RewardWeight{},
		[]AttendanceRecord{},
		NewAccounting(params),
		DefaultFeeds(),
		chain.SDRBaseDenom,
	)
}

// DefaultFeedDenoms is the launch feed set, sorted unique as Feeds requires. A
// feed is keyed by the denomination it prices, so naming the launch feeds after
// their denominations keeps rate-store keys stable. This list was derived from
// the oracle Tobin-tax parameter until that parameter was deleted; membership
// itself has lived behind the asset registry since the feed decoupling.
var DefaultFeedDenoms = []string{
	chain.CNYBaseDenom,
	chain.EURBaseDenom,
	chain.GBPBaseDenom,
	chain.JPYBaseDenom,
	chain.KRWBaseDenom,
	chain.MNTBaseDenom,
	chain.SDRBaseDenom,
	chain.USDBaseDenom,
}

// DefaultFeeds seeds the launch feed set.
func DefaultFeeds() Feeds {
	return NewFeeds(DefaultFeedDenoms)
}

// Validate validates the oracle genesis state
func (gs GenesisState) Validate() error {
	if gs.Accounting.RewardWindow == 0 {
		return errors.New("accounting reward window must be greater than zero")
	}
	if gs.Accounting.RewardDistributionWindow < gs.Accounting.RewardWindow {
		return errors.New("accounting reward distribution window must be greater than or equal to reward window")
	}
	if gs.Accounting.AttendanceWindow == 0 {
		return errors.New("accounting attendance window must be greater than zero")
	}

	// ExchangeRates: ordered unique feed keys and positive rates. A rate may
	// exist for a denomination no asset is listed under, because a feed can be
	// priced ahead of listing.
	for i, er := range gs.ExchangeRates {
		if err := chain.ValidatePricedDenom(er.Denom); err != nil {
			return fmt.Errorf("exchange rate %w", err)
		}
		if er.Rate.IsNil() {
			return fmt.Errorf("exchange rate for %s must be set", er.Denom)
		}
		if !er.Rate.IsInValidRange() {
			return fmt.Errorf("exchange rate for %s must be representable", er.Denom)
		}
		if !er.Rate.IsPositive() {
			return fmt.Errorf("exchange rate for %s must be positive: %s", er.Denom, er.Rate)
		}
		if i > 0 && er.Denom <= gs.ExchangeRates[i-1].Denom {
			return errors.New("genesis exchange rates must be sorted by unique denom")
		}
	}

	// RewardWeights: ordered unique validator-address storage keys
	var previousValidatorAddress sdk.ValAddress
	for i, rewardWeight := range gs.RewardWeights {
		if rewardWeight.RewardWeight.IsNil() {
			return errors.New("reward weight must be set")
		}
		if rewardWeight.RewardWeight.IsNegative() {
			return fmt.Errorf("reward weight must not be negative for validator %s", rewardWeight.ValidatorAddress)
		}
		if len(rewardWeight.ValidatorAddress) == 0 {
			return errors.New("reward weight validator address must not be empty")
		}
		validatorAddress, err := chain.ParseCanonicalValidatorAddress(
			"reward weight validator address",
			rewardWeight.ValidatorAddress,
		)
		if err != nil {
			return err
		}
		if i > 0 && bytes.Compare(validatorAddress, previousValidatorAddress) <= 0 {
			return errors.New("genesis reward weights must be sorted by unique validator address")
		}
		previousValidatorAddress = validatorAddress
	}

	// AttendanceRecords: ordered unique validator-address storage keys and
	// internally consistent counters.
	previousValidatorAddress = nil
	for i, record := range gs.AttendanceRecords {
		if len(record.ValidatorAddress) == 0 {
			return errors.New("attendance record validator address must not be empty")
		}
		validatorAddress, err := chain.ParseCanonicalValidatorAddress(
			"attendance record validator address",
			record.ValidatorAddress,
		)
		if err != nil {
			return err
		}
		if i > 0 && bytes.Compare(validatorAddress, previousValidatorAddress) <= 0 {
			return errors.New("genesis attendance records must be sorted by unique validator address")
		}
		previousValidatorAddress = validatorAddress

		if record.Attendance.AttendedBlocks > record.Attendance.EligibleBlocks {
			return fmt.Errorf(
				"attendance record attended blocks %d exceed eligible blocks %d",
				record.Attendance.AttendedBlocks,
				record.Attendance.EligibleBlocks,
			)
		}
		if record.Attendance.EligibleBlocks > gs.Accounting.AttendanceWindow {
			return fmt.Errorf(
				"attendance record eligible blocks %d exceed attendance window %d",
				record.Attendance.EligibleBlocks,
				gs.Accounting.AttendanceWindow,
			)
		}
	}

	if err := gs.Feeds.Validate(); err != nil {
		return err
	}
	for _, er := range gs.ExchangeRates {
		if _, found := slices.BinarySearch(gs.Feeds.Denoms, er.Denom); !found {
			return fmt.Errorf("exchange rate %s is not an active feed", er.Denom)
		}
	}

	// The reference identity is a feed key; that it is an Active feed is a
	// keeper-level check against the imported registry. Empty is legitimate
	// before governance has ever configured one.
	if gs.ReferenceDenom != "" {
		if err := chain.ValidatePricedDenom(gs.ReferenceDenom); err != nil {
			return fmt.Errorf("reference %w", err)
		}
	}

	if err := gs.Params.Validate(); err != nil {
		return err
	}

	return nil
}
