package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	arkmetrics "ark/pkg/metrics"
	"ark/x/oracle/types"
)

// EndBlocker settles periodic oracle rewards and attendance.
func (k Keeper) EndBlocker(ctx context.Context) error {
	defer arkmetrics.RecordModuleMethodLatency(ctx, types.ModuleName, arkmetrics.EndBlock)()

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}
	accounting, err := k.Accounting.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting accounting state: %w", err)
	}
	accountingChanged := false

	if chain.IsPeriodLastBlockFrom(ctx, accounting.RewardWindowStartHeight, accounting.RewardWindow) {
		if err := k.SettleRewards(ctx, accounting.RewardWindow, accounting.RewardDistributionWindow); err != nil {
			return err
		}

		// Clear reward weights after reward settlement.
		if err := k.RewardWeight.Clear(ctx, nil); err != nil {
			return fmt.Errorf("clearing reward weights: %w", err)
		}

		if accounting.RewardWindow != params.RewardWindow ||
			accounting.RewardDistributionWindow != params.RewardDistributionWindow {
			accounting.RewardWindow = params.RewardWindow
			accounting.RewardDistributionWindow = params.RewardDistributionWindow
			accounting.RewardWindowStartHeight = uint64(sdkCtx.BlockHeight()) + 1
			accountingChanged = true
		}
	}

	if chain.IsPeriodLastBlockFrom(ctx, accounting.AttendanceWindowStartHeight, accounting.AttendanceWindow) {
		if err := k.SettleAttendance(ctx, accounting.AttendanceWindow); err != nil {
			return err
		}

		// Clear attendance records after settlement.
		if err := k.Attendance.Clear(ctx, nil); err != nil {
			return fmt.Errorf("clearing attendance records: %w", err)
		}

		if accounting.AttendanceWindow != params.AttendanceWindow {
			accounting.AttendanceWindow = params.AttendanceWindow
			accounting.AttendanceWindowStartHeight = uint64(sdkCtx.BlockHeight()) + 1
			accountingChanged = true
		}
	}

	if accountingChanged {
		if err := k.Accounting.Set(ctx, accounting); err != nil {
			return fmt.Errorf("setting accounting state: %w", err)
		}
	}

	return nil
}
