// SPDX-License-Identifier: Apache-2.0
// Originates from Ark's Terra Classic port of x/oracle/genesis.go.
// Modified for Ark: genesis construction, validation, and state integration.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/oracle/types"
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

	genesisHeight := sdkCtx.BlockHeight()
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

	if data.ReferenceDenom != "" {
		if err := k.requireFeedActive(ctx, data.ReferenceDenom); err != nil {
			return fmt.Errorf("invalid genesis reference denom: %w", err)
		}
	}
	if err := k.ReferenceDenom.Set(ctx, data.ReferenceDenom); err != nil {
		return fmt.Errorf("setting genesis protocol reference: %w", err)
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

	referenceDenom, err := k.GetReferenceDenom(ctx)
	if err != nil {
		return nil, err
	}

	return types.NewGenesisState(
		params,
		exchangeRates,
		rewardWeights,
		attendanceRecords,
		accounting,
		feeds,
		referenceDenom,
	), nil
}
