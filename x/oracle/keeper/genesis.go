package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/x/oracle/types"
)

// InitGenesis imports oracle genesis state.
func (k Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {
	if data == nil {
		return fmt.Errorf("oracle genesis state is nil")
	}
	if err := data.Validate(); err != nil {
		return fmt.Errorf("invalid oracle genesis state: %w", err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	genesisTime := sdkCtx.BlockTime()
	for _, er := range data.ExchangeRates {
		if er.BlockTimestamp.After(genesisTime) {
			return fmt.Errorf(
				"genesis exchange rate for denom %s has timestamp %s after genesis block time %s",
				er.Denom,
				er.BlockTimestamp,
				genesisTime,
			)
		}
	}

	// Accounting anchors are block heights, so an anchor above the genesis
	// height is unreachable rather than merely early: EndBlocker settles when
	// the height is a whole number of windows past the anchor, and heights
	// below it are outside the period entirely. Importing one — the shape a
	// zero-height export produces if it carries old-chain heights over —
	// silently disables reward settlement and attendance jailing until the
	// chain climbs to a height it was never meant to see. Scheduled feed
	// transitions are deliberately not checked here: an activation height
	// above the genesis height is exactly what a legitimately pending
	// transition looks like.
	genesisHeight := sdkCtx.BlockHeight()
	if genesisHeight >= 0 {
		if data.Accounting.RewardWindowStartHeight > uint64(genesisHeight) {
			return fmt.Errorf(
				"genesis accounting reward window start height %d is after genesis block height %d",
				data.Accounting.RewardWindowStartHeight,
				genesisHeight,
			)
		}
		if data.Accounting.AttendanceWindowStartHeight > uint64(genesisHeight) {
			return fmt.Errorf(
				"genesis accounting attendance window start height %d is after genesis block height %d",
				data.Accounting.AttendanceWindowStartHeight,
				genesisHeight,
			)
		}
	}

	// Check that the module account exists before writing module state.
	moduleAcc := k.accountKeeper.GetModuleAccount(ctx, types.ModuleName)
	if moduleAcc == nil {
		return fmt.Errorf("%s module account has not been set", types.ModuleName)
	}

	if err := k.Accounting.Set(ctx, data.Accounting); err != nil {
		return fmt.Errorf("setting accounting state: %w", err)
	}

	for _, er := range data.ExchangeRates {
		if err := k.ExchangeRate.Set(ctx, er.Denom, er); err != nil {
			return fmt.Errorf("setting genesis exchange rate for denom %s: %w", er.Denom, err)
		}
	}

	for _, sw := range data.RewardWeights {
		operator, err := sdk.ValAddressFromBech32(sw.ValidatorAddress)
		if err != nil {
			return fmt.Errorf("parsing reward weight validator address %q: %w", sw.ValidatorAddress, err)
		}

		if err := k.RewardWeight.Set(ctx, operator, sw.RewardWeight); err != nil {
			return fmt.Errorf("setting genesis reward weight for validator %s: %w", operator, err)
		}
	}

	for _, record := range data.AttendanceRecords {
		operator, err := sdk.ValAddressFromBech32(record.ValidatorAddress)
		if err != nil {
			return fmt.Errorf("parsing attendance validator address %q: %w", record.ValidatorAddress, err)
		}
		if err := k.Attendance.Set(ctx, operator, record.Attendance); err != nil {
			return fmt.Errorf("setting genesis attendance for validator %s: %w", operator, err)
		}
	}

	if err := k.Feeds.Set(ctx, data.Feeds); err != nil {
		return fmt.Errorf("setting feeds: %w", err)
	}

	if err := k.Params.Set(ctx, data.Params); err != nil {
		return fmt.Errorf("setting params: %w", err)
	}

	return nil
}

// ExportGenesis exports oracle store state.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting params: %w", err)
	}
	accounting, err := k.Accounting.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting accounting state: %w", err)
	}

	exchangeRates := []types.ExchangeRate{}
	if err := k.ExchangeRate.Walk(ctx, nil, func(denom string, exchangeRate types.ExchangeRate) (bool, error) {
		exchangeRates = append(exchangeRates, types.ExchangeRate{
			Denom:          denom,
			Rate:           exchangeRate.Rate,
			BlockTimestamp: exchangeRate.BlockTimestamp,
			BlockHeight:    exchangeRate.BlockHeight,
		})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating exchange rates: %w", err)
	}

	rewardWeights := []types.RewardWeight{}
	if err := k.RewardWeight.Walk(ctx, nil, func(operator sdk.ValAddress, rewardWeight math.Int) (bool, error) {
		rewardWeights = append(rewardWeights, types.RewardWeight{
			ValidatorAddress: operator.String(),
			RewardWeight:     rewardWeight,
		})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating reward weights: %w", err)
	}

	attendanceRecords := []types.AttendanceRecord{}
	if err := k.Attendance.Walk(ctx, nil, func(operator sdk.ValAddress, attendance types.Attendance) (bool, error) {
		attendanceRecords = append(attendanceRecords, types.AttendanceRecord{
			ValidatorAddress: operator.String(),
			Attendance:       attendance,
		})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating attendance records: %w", err)
	}

	feeds, err := k.Feeds.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting feeds: %w", err)
	}

	return types.NewGenesisState(
		params,
		exchangeRates,
		rewardWeights,
		attendanceRecords,
		accounting,
		feeds,
	), nil
}
