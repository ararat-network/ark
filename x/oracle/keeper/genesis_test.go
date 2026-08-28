package keeper_test

import (
	"time"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestInitGenesis() {
	blockTime := time.Unix(1_700_000_000, 0).UTC()

	tests := []struct {
		name                string
		genesis             func() *types.GenesisState
		expected            func() *types.GenesisState
		setup               func()
		expectErr           string
		expectNoRate        string
		expectAccountingOld bool
	}{
		{
			name: "full genesis stores all collections",
			genesis: func() *types.GenesisState {
				params := types.DefaultParams()
				return &types.GenesisState{
					Params:     params,
					Accounting: types.NewAccounting(params),
					ExchangeRates: []types.ExchangeRate{
						{Denom: chain.KRWBaseDenom, Rate: math.LegacyNewDec(1000), BlockTimestamp: blockTime, BlockHeight: 10},
						{Denom: chain.USDBaseDenom, Rate: math.LegacyNewDecWithPrec(123, 2), BlockTimestamp: blockTime, BlockHeight: 11},
					},
					RewardWeights: []types.RewardWeight{
						{ValidatorAddress: valAddr1.String(), RewardWeight: math.NewInt(5)},
						{ValidatorAddress: valAddr2.String(), RewardWeight: math.ZeroInt()},
					},
					AttendanceRecords: []types.AttendanceRecord{
						{ValidatorAddress: valAddr1.String(), Attendance: types.Attendance{EligibleBlocks: 5, AttendedBlocks: 3}},
						{ValidatorAddress: valAddr2.String(), Attendance: types.Attendance{EligibleBlocks: 0, AttendedBlocks: 0}},
					},
					Feeds: types.Feeds{
						Denoms: []string{
							chain.KRWBaseDenom,
							chain.USDBaseDenom,
						},
						Version: types.InitialFeedVersion,
					},
				}
			},
			setup: func() {
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(
					authtypes.NewEmptyModuleAccount(types.ModuleName),
				)
			},
		},
		{
			name: "invalid validator address in reward weight",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.Accounting.RewardWindow = 100
				gs.RewardWeights = []types.RewardWeight{
					{ValidatorAddress: "invalid", RewardWeight: math.NewInt(5)},
				}
				return gs
			},
			expectErr:           "invalid oracle genesis state: reward weight validator address is invalid",
			expectAccountingOld: true,
		},
		{
			name: "invalid validator address in attendance record",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.AttendanceRecords = []types.AttendanceRecord{
					{ValidatorAddress: "invalid", Attendance: types.Attendance{EligibleBlocks: 5}},
				}
				return gs
			},
			expectErr: "invalid oracle genesis state: attendance record validator address is invalid",
		},
		{
			name:      "nil genesis returns error",
			genesis:   func() *types.GenesisState { return nil },
			expectErr: "oracle genesis state is nil",
		},
		{
			// The shape a zero-height export produces if it carries old-chain
			// heights over: the anchor is unreachable, so reward settlement
			// would never fire on the new chain.
			name: "reward window anchor after genesis height returns error",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.Accounting.RewardWindowStartHeight = uint64(oracleTestGenesisHeight) + 1
				return gs
			},
			expectErr:           "reward window start height 21 is after genesis block height 20",
			expectAccountingOld: true,
		},
		{
			name: "attendance window anchor after genesis height returns error",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.Accounting.AttendanceWindowStartHeight = uint64(oracleTestGenesisHeight) + 1
				return gs
			},
			expectErr:           "attendance window start height 21 is after genesis block height 20",
			expectAccountingOld: true,
		},
		{
			// The boundary is inclusive: an anchor at the genesis height itself
			// is the window opening exactly now, not a stale import.
			name: "anchors at the genesis height are accepted",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.Accounting.RewardWindowStartHeight = uint64(oracleTestGenesisHeight)
				gs.Accounting.AttendanceWindowStartHeight = uint64(oracleTestGenesisHeight)
				return gs
			},
		},
		{
			name: "unsorted feeds return error",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.Feeds.Denoms = []string{chain.USDBaseDenom, chain.KRWBaseDenom}
				return gs
			},
			expectErr: "must be sorted by unique denom",
		},
		{
			name: "future exchange rate timestamp returns error",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.ExchangeRates = []types.ExchangeRate{
					{
						Denom:          chain.USDBaseDenom,
						Rate:           math.LegacyOneDec(),
						BlockTimestamp: oracleTestBlockTime.Add(time.Second),
					},
				}
				return gs
			},
			expectErr:    "timestamp 2026-07-12 12:00:01 +0000 UTC after genesis block time",
			expectNoRate: chain.USDBaseDenom,
		},
		{
			name: "nil module account returns error",
			genesis: func() *types.GenesisState {
				gs := types.DefaultGenesisState()
				gs.ExchangeRates = []types.ExchangeRate{
					{
						Denom:          chain.USDBaseDenom,
						Rate:           math.LegacyOneDec(),
						BlockTimestamp: oracleTestBlockTime,
					},
				}
				return gs
			},
			setup: func() {
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(nil)
			},
			expectErr:    "module account has not been set",
			expectNoRate: chain.USDBaseDenom,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			} else if tc.expectErr == "" {
				s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(
					authtypes.NewEmptyModuleAccount(types.ModuleName),
				)
			}

			genesis := tc.genesis()
			expected := genesis
			if tc.expected != nil {
				expected = tc.expected()
			}

			err := s.keeper.InitGenesis(s.ctx, genesis)
			if tc.expectErr != "" {
				s.Require().Error(err)
				s.Require().ErrorContains(err, tc.expectErr)
				if tc.expectNoRate != "" {
					hasRate, hasErr := s.keeper.ExchangeRate.Has(s.ctx, tc.expectNoRate)
					s.Require().NoError(hasErr)
					s.Require().False(hasRate)
				}
				if tc.expectAccountingOld {
					accounting, getErr := s.keeper.Accounting.Get(s.ctx)
					s.Require().NoError(getErr)
					s.Require().Equal(types.NewAccounting(types.DefaultParams()), accounting)
				}
			} else {
				s.Require().NoError(err)
				s.requireGenesisState(expected)
			}
		})
	}
}

func (s *KeeperTestSuite) requireGenesisState(expected *types.GenesisState) {
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(expected.Params.VoteThreshold.Equal(params.VoteThreshold))
	s.Require().True(expected.Params.RewardBand.Equal(params.RewardBand))
	s.Require().Equal(expected.Params.RewardWindow, params.RewardWindow)
	s.Require().Equal(expected.Params.RewardDistributionWindow, params.RewardDistributionWindow)
	s.Require().Equal(expected.Params.AttendanceWindow, params.AttendanceWindow)
	s.Require().True(expected.Params.MinAttendancePerWindow.Equal(params.MinAttendancePerWindow))
	s.Require().Equal(expected.Params.MaxExchangeRateAge, params.MaxExchangeRateAge)
	s.Require().True(expected.Params.FunctioningBlockThreshold.Equal(params.FunctioningBlockThreshold))
	s.Require().True(expected.Params.ParticipationThreshold.Equal(params.ParticipationThreshold))
	accounting, err := s.keeper.Accounting.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(expected.Accounting, accounting)

	// Exchange rates are keyed by denom.
	exchangeRateCount := 0
	err = s.keeper.ExchangeRate.Walk(s.ctx, nil, func(_ string, _ types.ExchangeRate) (bool, error) {
		exchangeRateCount++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Len(expected.ExchangeRates, exchangeRateCount)

	for _, item := range expected.ExchangeRates {
		exchangeRate, err := s.keeper.ExchangeRate.Get(s.ctx, item.Denom)
		s.Require().NoError(err)
		s.Require().Equal(item.Denom, exchangeRate.Denom)
		s.Require().True(item.Rate.Equal(exchangeRate.Rate), "expected %s for %s, got %s", item.Rate, item.Denom, exchangeRate.Rate)
		s.Require().True(item.BlockTimestamp.Equal(exchangeRate.BlockTimestamp))
		s.Require().Equal(item.BlockHeight, exchangeRate.BlockHeight)
	}

	// Reward weights are keyed by validator address.
	rewardWeightCount := 0
	err = s.keeper.RewardWeight.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ math.Int) (bool, error) {
		rewardWeightCount++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Len(expected.RewardWeights, rewardWeightCount)

	for _, item := range expected.RewardWeights {
		valAddr, err := sdk.ValAddressFromBech32(item.ValidatorAddress)
		s.Require().NoError(err)

		rewardWeight, err := s.keeper.RewardWeight.Get(s.ctx, valAddr)
		s.Require().NoError(err)
		s.Require().True(item.RewardWeight.Equal(rewardWeight))
	}

	// Attendance records are keyed by validator address.
	attendanceCount := 0
	err = s.keeper.Attendance.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ types.Attendance) (bool, error) {
		attendanceCount++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Len(expected.AttendanceRecords, attendanceCount)

	for _, item := range expected.AttendanceRecords {
		valAddr, err := sdk.ValAddressFromBech32(item.ValidatorAddress)
		s.Require().NoError(err)

		attendance, err := s.keeper.Attendance.Get(s.ctx, valAddr)
		s.Require().NoError(err)
		s.Require().Equal(item.Attendance, attendance)
	}

	voteTargets, err := s.keeper.Feeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(expected.Feeds, voteTargets)
}

func (s *KeeperTestSuite) TestExportGenesis() {
	blockTime := time.Unix(1_700_000_000, 0).UTC()

	params := types.DefaultParams()
	expected := &types.GenesisState{
		Params: params,
		Accounting: types.Accounting{
			RewardWindow:                10,
			RewardDistributionWindow:    100,
			RewardWindowStartHeight:     7,
			AttendanceWindow:            20,
			AttendanceWindowStartHeight: 11,
		},
		ExchangeRates: []types.ExchangeRate{
			{Denom: chain.KRWBaseDenom, Rate: math.LegacyNewDec(1000), BlockTimestamp: blockTime, BlockHeight: 10},
			{Denom: chain.USDBaseDenom, Rate: math.LegacyNewDecWithPrec(123, 2), BlockTimestamp: blockTime, BlockHeight: 11},
		},
		RewardWeights: []types.RewardWeight{
			{ValidatorAddress: valAddr1.String(), RewardWeight: math.NewInt(5)},
			{ValidatorAddress: valAddr2.String(), RewardWeight: math.ZeroInt()},
		},
		AttendanceRecords: []types.AttendanceRecord{
			{ValidatorAddress: valAddr1.String(), Attendance: types.Attendance{EligibleBlocks: 5, AttendedBlocks: 3}},
			{ValidatorAddress: valAddr2.String(), Attendance: types.Attendance{EligibleBlocks: 0, AttendedBlocks: 0}},
		},
		Feeds: types.Feeds{
			Denoms: []string{
				chain.KRWBaseDenom,
				chain.USDBaseDenom,
			}, Version: types.InitialFeedVersion,
		},
	}
	expected.Params.RewardWindow = 10
	expected.Params.VoteThreshold = math.LegacyNewDecWithPrec(6, 1)

	s.Require().NoError(s.keeper.Params.Set(s.ctx, expected.Params))
	s.Require().NoError(s.keeper.Accounting.Set(s.ctx, expected.Accounting))
	for _, item := range expected.ExchangeRates {
		s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, item.Denom, item))
	}
	for _, item := range expected.RewardWeights {
		valAddr, err := sdk.ValAddressFromBech32(item.ValidatorAddress)
		s.Require().NoError(err)

		s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, valAddr, item.RewardWeight))
	}
	for _, item := range expected.AttendanceRecords {
		valAddr, err := sdk.ValAddressFromBech32(item.ValidatorAddress)
		s.Require().NoError(err)

		s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr, item.Attendance))
	}
	s.Require().NoError(s.keeper.Feeds.Set(s.ctx, expected.Feeds))

	gs, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().NotNil(gs)

	// Params are exported from the Params collection.
	s.Require().True(expected.Params.VoteThreshold.Equal(gs.Params.VoteThreshold))
	s.Require().True(expected.Params.RewardBand.Equal(gs.Params.RewardBand))
	s.Require().Equal(expected.Params.RewardWindow, gs.Params.RewardWindow)
	s.Require().Equal(expected.Params.RewardDistributionWindow, gs.Params.RewardDistributionWindow)
	s.Require().Equal(expected.Params.AttendanceWindow, gs.Params.AttendanceWindow)
	s.Require().True(expected.Params.MinAttendancePerWindow.Equal(gs.Params.MinAttendancePerWindow))
	s.Require().Equal(expected.Params.MaxExchangeRateAge, gs.Params.MaxExchangeRateAge)
	s.Require().True(expected.Params.FunctioningBlockThreshold.Equal(gs.Params.FunctioningBlockThreshold))
	s.Require().True(expected.Params.ParticipationThreshold.Equal(gs.Params.ParticipationThreshold))
	s.Require().Equal(expected.Feeds, gs.Feeds)
	s.Require().Equal(expected.Accounting, gs.Accounting)

	// Exchange rates are exported by denom.
	s.Require().Len(gs.ExchangeRates, len(expected.ExchangeRates))
	exchangeRates := make(map[string]types.ExchangeRate)
	for _, item := range gs.ExchangeRates {
		exchangeRates[item.Denom] = item
	}
	for _, item := range expected.ExchangeRates {
		exchangeRate := exchangeRates[item.Denom]
		s.Require().True(item.Rate.Equal(exchangeRate.Rate))
		s.Require().True(item.BlockTimestamp.Equal(exchangeRate.BlockTimestamp))
		s.Require().Equal(item.BlockHeight, exchangeRate.BlockHeight)
	}

	// Reward weights are exported by validator address.
	s.Require().Len(gs.RewardWeights, len(expected.RewardWeights))
	rewardWeights := make(map[string]math.Int)
	for _, item := range gs.RewardWeights {
		rewardWeights[item.ValidatorAddress] = item.RewardWeight
	}
	for _, item := range expected.RewardWeights {
		s.Require().True(item.RewardWeight.Equal(rewardWeights[item.ValidatorAddress]))
	}

	// Attendance records are exported by validator address.
	s.Require().Len(gs.AttendanceRecords, len(expected.AttendanceRecords))
	attendanceRecords := make(map[string]types.Attendance)
	for _, item := range gs.AttendanceRecords {
		attendanceRecords[item.ValidatorAddress] = item.Attendance
	}
	for _, item := range expected.AttendanceRecords {
		s.Require().Equal(item.Attendance, attendanceRecords[item.ValidatorAddress])
	}
}
