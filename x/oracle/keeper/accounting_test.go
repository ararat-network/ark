package keeper_test

import (
	"errors"

	cmttypes "github.com/cometbft/cometbft/types"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"

	chain "ark/pkg/chain"
	"ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestRecordVoteAccountingAccumulatesAttendance() {
	consAddr, validator := s.newBondedValidator(valAddr1)
	s.stakingKeeper.EXPECT().ValidatorByConsAddr(s.ctx, consAddr).Return(validator, nil).Times(2)

	// Eligible + participated increments both counters.
	s.Require().NoError(s.keeper.RecordVoteAccounting(s.ctx, consAddr, math.ZeroInt(), true, true))
	// Eligible only increments eligible.
	s.Require().NoError(s.keeper.RecordVoteAccounting(s.ctx, consAddr, math.ZeroInt(), true, false))
	// Ineligible zero-weight call is a no-op and skips the validator lookup
	// entirely, so no third ValidatorByConsAddr call is expected above.
	s.Require().NoError(s.keeper.RecordVoteAccounting(s.ctx, consAddr, math.ZeroInt(), false, false))

	attendance, err := s.keeper.Attendance.Get(s.ctx, valAddr1)
	s.Require().NoError(err)
	s.Require().Equal(types.Attendance{EligibleBlocks: 2, AttendedBlocks: 1}, attendance)
}

// TestRecordVoteAccountingIneligibleZeroWeightIsNoOp pins the early-return
// guard: no staking mock expectation is armed, so gomock fails the test if
// the guard ever falls through to a validator lookup.
func (s *KeeperTestSuite) TestRecordVoteAccountingIneligibleZeroWeightIsNoOp() {
	consAddr := sdk.ConsAddress([]byte("missing_validator___"))

	s.Require().NoError(s.keeper.RecordVoteAccounting(s.ctx, consAddr, math.ZeroInt(), false, false))

	rewardWeightEntries := 0
	err := s.keeper.RewardWeight.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ math.Int) (bool, error) {
		rewardWeightEntries++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Zero(rewardWeightEntries)

	attendanceEntries := 0
	err = s.keeper.Attendance.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ types.Attendance) (bool, error) {
		attendanceEntries++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Zero(attendanceEntries)
}

// TestRecordVoteAccountingIgnoresParticipationWithoutEligibility pins the
// deliberate ignore semantics from plan amendment 2: a validator that
// participates on a non-functioning (ineligible) block is neither rejected
// nor credited. Both cases use a positive reward weight so the call proceeds
// past the initial no-op guard and actually exercises the eligibility gate,
// rather than short-circuiting before ever considering participation.
func (s *KeeperTestSuite) TestRecordVoteAccountingIgnoresParticipationWithoutEligibility() {
	s.Run("no existing record stays absent", func() {
		consAddr, validator := s.newBondedValidator(valAddr1)
		s.stakingKeeper.EXPECT().ValidatorByConsAddr(s.ctx, consAddr).Return(validator, nil)

		err := s.keeper.RecordVoteAccounting(s.ctx, consAddr, math.NewInt(5), false, true)
		s.Require().NoError(err)

		_, err = s.keeper.Attendance.Get(s.ctx, valAddr1)
		s.Require().True(errors.Is(err, collections.ErrNotFound), "expected no attendance record, got %v", err)
	})

	s.Run("existing record is left untouched", func() {
		consAddr, validator := s.newBondedValidator(valAddr1)
		s.stakingKeeper.EXPECT().ValidatorByConsAddr(s.ctx, consAddr).Return(validator, nil)
		s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr1, types.Attendance{EligibleBlocks: 3, AttendedBlocks: 2}))

		err := s.keeper.RecordVoteAccounting(s.ctx, consAddr, math.NewInt(5), false, true)
		s.Require().NoError(err)

		attendance, err := s.keeper.Attendance.Get(s.ctx, valAddr1)
		s.Require().NoError(err)
		s.Require().Equal(types.Attendance{EligibleBlocks: 3, AttendedBlocks: 2}, attendance)
	})
}

func (s *KeeperTestSuite) TestRecordVoteAccountingAccumulatesRewardWeightRegardlessOfEligibility() {
	tests := []struct {
		name         string
		eligible     bool
		participated bool
	}{
		{name: "eligible and participated", eligible: true, participated: true},
		{name: "eligible only", eligible: true, participated: false},
		{name: "ineligible with participation", eligible: false, participated: true},
		{name: "ineligible without participation", eligible: false, participated: false},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			consAddr, validator := s.newBondedValidator(valAddr1)
			s.stakingKeeper.EXPECT().ValidatorByConsAddr(s.ctx, consAddr).Return(validator, nil)

			s.Require().NoError(s.keeper.RecordVoteAccounting(s.ctx, consAddr, math.NewInt(5), tc.eligible, tc.participated))

			rewardWeight, err := s.keeper.RewardWeight.Get(s.ctx, valAddr1)
			s.Require().NoError(err)
			s.Require().True(math.NewInt(5).Equal(rewardWeight), "expected %s, got %s", math.NewInt(5), rewardWeight)
		})
	}
}

func (s *KeeperTestSuite) TestRecordVoteAccountingAccumulatesLegalPowerBeyondUint64() {
	consAddr, validator := s.newBondedValidator(valAddr1)
	s.stakingKeeper.EXPECT().ValidatorByConsAddr(s.ctx, consAddr).Return(validator, nil).Times(3)

	blockScore := math.NewInt(cmttypes.MaxTotalVotingPower).MulRaw(8)
	for range 3 {
		s.Require().NoError(s.keeper.RecordVoteAccounting(s.ctx, consAddr, blockScore, false, false))
	}

	stored, err := s.keeper.RewardWeight.Get(s.ctx, valAddr1)
	s.Require().NoError(err)
	s.Require().True(blockScore.MulRaw(3).Equal(stored), "expected %s, got %s", blockScore.MulRaw(3), stored)
}

func (s *KeeperTestSuite) TestRecordVoteAccountingRejectsInvalidRewardWeight() {
	consAddr := sdk.ConsAddress([]byte("validator___________"))

	testCases := []struct {
		name         string
		rewardWeight math.Int
		expectErr    string
	}{
		{
			name:         "nil reward weight",
			rewardWeight: math.Int{},
			expectErr:    "reward weight must be set",
		},
		{
			name:         "negative reward weight",
			rewardWeight: math.NewInt(-1),
			expectErr:    "reward weight must not be negative",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			err := s.keeper.RecordVoteAccounting(s.ctx, consAddr, tc.rewardWeight, false, false)
			s.Require().ErrorContains(err, tc.expectErr)
		})
	}
}

func (s *KeeperTestSuite) TestRecordVoteAccountingSkipsUnresolvedValidator() {
	consAddr := sdk.ConsAddress([]byte("missing_validator___"))
	s.stakingKeeper.EXPECT().
		ValidatorByConsAddr(s.ctx, consAddr).
		Return(nil, stakingtypes.ErrNoValidatorFound)

	s.Require().NoError(s.keeper.RecordVoteAccounting(s.ctx, consAddr, math.NewInt(3), true, true))

	attendanceEntries := 0
	err := s.keeper.Attendance.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ types.Attendance) (bool, error) {
		attendanceEntries++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Zero(attendanceEntries)

	rewardWeightEntries := 0
	err = s.keeper.RewardWeight.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ math.Int) (bool, error) {
		rewardWeightEntries++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Zero(rewardWeightEntries)
}

func (s *KeeperTestSuite) TestSettleRewards() {
	maxUint64 := ^uint64(0)
	largeScore := math.NewIntFromUint64(maxUint64).AddRaw(1)
	validator1 := stakingtypes.Validator{
		OperatorAddress: valAddr1.String(),
		Status:          stakingtypes.Bonded,
		Tokens:          math.NewInt(10),
	}
	validator2 := stakingtypes.Validator{
		OperatorAddress: valAddr2.String(),
		Status:          stakingtypes.Bonded,
		Tokens:          math.NewInt(10),
	}

	tests := []struct {
		name                     string
		setup                    func()
		rewardWindow             uint64
		rewardDistributionWindow uint64
		expectErr                string
		expectedEvents           []proto.Message
	}{
		{
			name: "empty reward weights return without distributing",
		},
		{
			name: "zero reward pool returns without distributing",
			setup: func() {
				s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, valAddr1, math.NewInt(10)))

				s.bankKeeper.EXPECT().
					GetAllBalances(s.ctx, sdk.AccAddress{1}).
					Return(sdk.NewCoins())
			},
		},
		{
			name: "distributes proportional rewards",
			setup: func() {
				s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, valAddr1, math.NewInt(10)))
				s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, valAddr2, math.NewInt(30)))

				s.bankKeeper.EXPECT().
					GetAllBalances(s.ctx, sdk.AccAddress{1}).
					Return(sdk.NewCoins(
						sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(400)),
						sdk.NewCoin(chain.SDRBaseDenom, math.NewInt(200)),
					))
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator1, nil)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr2).Return(validator2, nil)
				s.distrKeeper.EXPECT().AllocateTokensToValidator(
					s.ctx,
					validator1,
					sdk.NewDecCoinsFromCoins(
						sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(10)),
						sdk.NewCoin(chain.SDRBaseDenom, math.NewInt(5)),
					),
				).Return(nil)
				s.distrKeeper.EXPECT().AllocateTokensToValidator(
					s.ctx,
					validator2,
					sdk.NewDecCoinsFromCoins(
						sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(30)),
						sdk.NewCoin(chain.SDRBaseDenom, math.NewInt(15)),
					),
				).Return(nil)
				s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					s.ctx,
					types.ModuleName,
					"distribution",
					sdk.NewCoins(
						sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(40)),
						sdk.NewCoin(chain.SDRBaseDenom, math.NewInt(20)),
					),
				).Return(nil)
			},
			expectedEvents: []proto.Message{
				&types.EventOracleReward{
					Validator: valAddr1.String(),
					Rewards: sdk.NewCoins(
						sdk.NewInt64Coin(chain.NoahBaseDenom, 10),
						sdk.NewInt64Coin(chain.SDRBaseDenom, 5),
					),
				},
				&types.EventOracleReward{
					Validator: valAddr2.String(),
					Rewards: sdk.NewCoins(
						sdk.NewInt64Coin(chain.NoahBaseDenom, 30),
						sdk.NewInt64Coin(chain.SDRBaseDenom, 15),
					),
				},
			},
		},
		{
			name: "supports reward weight above maximum uint64 and maximum distribution window",
			setup: func() {
				s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, valAddr1, largeScore))

				rewardCoin := sdk.NewCoin(
					chain.NoahBaseDenom,
					largeScore,
				)
				expectedRewards := sdk.NewCoins(rewardCoin)
				s.bankKeeper.EXPECT().
					GetAllBalances(s.ctx, sdk.AccAddress{1}).
					Return(sdk.NewCoins(rewardCoin))
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator1, nil)
				s.distrKeeper.EXPECT().AllocateTokensToValidator(
					s.ctx,
					validator1,
					sdk.NewDecCoinsFromCoins(expectedRewards...),
				).Return(nil)
				s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					s.ctx,
					types.ModuleName,
					"distribution",
					expectedRewards,
				).Return(nil)
			},
			rewardWindow:             maxUint64,
			rewardDistributionWindow: maxUint64,
			expectedEvents: []proto.Message{&types.EventOracleReward{
				Validator: valAddr1.String(),
				Rewards:   sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, largeScore)),
			}},
		},
		{
			name: "send failure is returned",
			setup: func() {
				s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, valAddr1, math.NewInt(10)))

				s.bankKeeper.EXPECT().
					GetAllBalances(s.ctx, sdk.AccAddress{1}).
					Return(sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(100))))
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator1, nil)
				s.distrKeeper.EXPECT().
					AllocateTokensToValidator(s.ctx, validator1, gomock.Any()).
					Return(nil)
				s.bankKeeper.EXPECT().
					SendCoinsFromModuleToModule(s.ctx, types.ModuleName, "distribution", gomock.Any()).
					Return(errors.New("send failed"))
			},
			expectErr: "sending coins to distribution module",
		},
		{
			name: "missing validator is skipped",
			setup: func() {
				s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, valAddr1, math.NewInt(10)))

				s.bankKeeper.EXPECT().
					GetAllBalances(s.ctx, sdk.AccAddress{1}).
					Return(sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(100))))
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(nil, nil)
			},
		},
		{
			name: "missing validator error is skipped",
			setup: func() {
				s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, valAddr1, math.NewInt(10)))

				s.bankKeeper.EXPECT().
					GetAllBalances(s.ctx, sdk.AccAddress{1}).
					Return(sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(100))))
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(nil, stakingtypes.ErrNoValidatorFound)
			},
		},
		{
			name: "only transfers distributed rewards",
			setup: func() {
				s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, valAddr1, math.NewInt(10)))
				s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, valAddr2, math.NewInt(30)))

				s.bankKeeper.EXPECT().
					GetAllBalances(s.ctx, sdk.AccAddress{1}).
					Return(sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(400))))
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(nil, stakingtypes.ErrNoValidatorFound)
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr2).Return(validator2, nil)
				s.distrKeeper.EXPECT().AllocateTokensToValidator(
					s.ctx,
					validator2,
					sdk.NewDecCoinsFromCoins(sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(30))),
				).Return(nil)
				s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					s.ctx,
					types.ModuleName,
					"distribution",
					sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(30))),
				).Return(nil)
			},
			expectedEvents: []proto.Message{&types.EventOracleReward{
				Validator: valAddr2.String(),
				Rewards:   sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 30)),
			}},
		},
		{
			name: "allocation failure returns error",
			setup: func() {
				s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, valAddr1, math.NewInt(10)))

				s.bankKeeper.EXPECT().
					GetAllBalances(s.ctx, sdk.AccAddress{1}).
					Return(sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(100))))
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator1, nil)
				s.distrKeeper.EXPECT().
					AllocateTokensToValidator(s.ctx, validator1, gomock.Any()).
					Return(errors.New("allocation failed"))
			},
			expectErr: "allocating oracle rewards",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			rewardWindow := tc.rewardWindow
			if rewardWindow == 0 {
				rewardWindow = 10
			}
			rewardDistributionWindow := tc.rewardDistributionWindow
			if rewardDistributionWindow == 0 {
				rewardDistributionWindow = 100
			}
			err := s.keeper.SettleRewards(s.ctx, rewardWindow, rewardDistributionWindow)
			events := sdk.UnwrapSDKContext(s.ctx).EventManager().Events()
			if tc.expectErr != "" {
				s.Require().ErrorContains(err, tc.expectErr)
				s.Require().Empty(events)
				return
			}

			s.Require().NoError(err)
			if len(tc.expectedEvents) == 0 {
				s.Require().Empty(events)
			} else {
				s.requireTypedEvents(events, tc.expectedEvents...)
			}
		})
	}
}

// TestSettleAttendance pins the jail-only attendance settlement: no Slash
// call exists on the mock at all, so any attempt to slash would fail to
// compile, and every case below either arms Jail exactly once or leaves the
// staking mock with no expectations (gomock fails the test on any
// unexpected call, which is how the "no staking calls" cases are enforced).
// Two cases also cover the abort paths: a generic validator-lookup error and
// a jail failure, both propagated out as wrapped errors.
func (s *KeeperTestSuite) TestSettleAttendance() {
	tests := []struct {
		name                   string
		eligibleBlocks         uint64
		attendedBlocks         uint64
		minAttendance          math.LegacyDec
		attendanceWindowBlocks uint64 // 0 => default 20
		callsValidator         bool
		missingValidator       bool
		validatorErr           error
		status                 stakingtypes.BondStatus
		jailed                 bool
		expectJail             bool // must not combine with missingValidator: consAddr would be nil
		jailErr                error
		expectErr              string
	}{
		{
			name:           "jails bonded validator below minimum attendance",
			eligibleBlocks: 20,
			attendedBlocks: 0,
			minAttendance:  math.LegacyNewDecWithPrec(90, 2),
			callsValidator: true,
			status:         stakingtypes.Bonded,
			expectJail:     true,
		},
		{
			name:           "keeps bonded validator at or above minimum attendance",
			eligibleBlocks: 10,
			attendedBlocks: 5,
			minAttendance:  math.LegacyNewDecWithPrec(50, 2),
			callsValidator: false,
		},
		{
			// A sparse record means the validator was absent while the fleet
			// worked, so it is judged on the blocks it holds rather than excused
			// for holding few.
			name:           "sparse record is judged on the blocks it holds",
			eligibleBlocks: 9,
			attendedBlocks: 0,
			minAttendance:  math.LegacyNewDecWithPrec(90, 2),
			callsValidator: true,
			status:         stakingtypes.Bonded,
			expectJail:     true,
		},
		{
			name:           "sparse record meeting the ratio is kept",
			eligibleBlocks: 9,
			attendedBlocks: 9,
			minAttendance:  math.LegacyNewDecWithPrec(90, 2),
			callsValidator: false,
		},
		{
			// Only reachable by genesis import: required = ratio × 0 = 0, which
			// zero attended blocks satisfies.
			name:           "zero eligible record passes on the ratio alone",
			eligibleBlocks: 0,
			attendedBlocks: 0,
			minAttendance:  math.LegacyNewDecWithPrec(90, 2),
			callsValidator: false,
		},
		{
			name:           "zero minimum attendance disables jailing even with a terrible record",
			eligibleBlocks: 20,
			attendedBlocks: 0,
			minAttendance:  math.LegacyZeroDec(),
			callsValidator: false,
		},
		{
			name:           "already jailed validator is not re-jailed",
			eligibleBlocks: 20,
			attendedBlocks: 0,
			minAttendance:  math.LegacyNewDecWithPrec(90, 2),
			callsValidator: true,
			status:         stakingtypes.Bonded,
			jailed:         true,
		},
		{
			name:           "unbonded validator is skipped",
			eligibleBlocks: 20,
			attendedBlocks: 0,
			minAttendance:  math.LegacyNewDecWithPrec(90, 2),
			callsValidator: true,
			status:         stakingtypes.Unbonded,
		},
		{
			name:             "missing validator is skipped",
			eligibleBlocks:   20,
			attendedBlocks:   0,
			minAttendance:    math.LegacyNewDecWithPrec(90, 2),
			callsValidator:   true,
			missingValidator: true,
		},
		{
			name:             "missing validator error is skipped without error",
			eligibleBlocks:   20,
			attendedBlocks:   0,
			minAttendance:    math.LegacyNewDecWithPrec(90, 2),
			callsValidator:   true,
			missingValidator: true,
			validatorErr:     stakingtypes.ErrNoValidatorFound,
		},
		{
			name:             "validator lookup error propagates",
			eligibleBlocks:   20,
			attendedBlocks:   0,
			minAttendance:    math.LegacyNewDecWithPrec(90, 2),
			callsValidator:   true,
			missingValidator: true,
			validatorErr:     errors.New("staking keeper unavailable"),
			expectErr:        "getting validator",
		},
		{
			name:           "jail failure propagates",
			eligibleBlocks: 20,
			attendedBlocks: 0,
			minAttendance:  math.LegacyNewDecWithPrec(90, 2),
			callsValidator: true,
			status:         stakingtypes.Bonded,
			expectJail:     true,
			jailErr:        errors.New("jail rejected"),
			expectErr:      "jailing validator",
		},
		{
			name:           "full attendance passes at the maximum ratio",
			eligibleBlocks: 20,
			attendedBlocks: 20,
			minAttendance:  math.LegacyOneDec(),
			callsValidator: false,
		},
		{
			name:           "one absence jails at the maximum ratio",
			eligibleBlocks: 20,
			attendedBlocks: 19,
			minAttendance:  math.LegacyOneDec(),
			callsValidator: true,
			status:         stakingtypes.Bonded,
			expectJail:     true,
		},
		{
			// Window length no longer gates judgement at all; it only labels the
			// emitted event. A lone eligible block is still a judged block.
			name:                   "single eligible block is judged",
			eligibleBlocks:         1,
			attendedBlocks:         0,
			minAttendance:          math.LegacyNewDecWithPrec(90, 2),
			attendanceWindowBlocks: 1,
			callsValidator:         true,
			status:                 stakingtypes.Bonded,
			expectJail:             true,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			attendanceWindowBlocks := tc.attendanceWindowBlocks
			if attendanceWindowBlocks == 0 {
				attendanceWindowBlocks = 20
			}

			params, err := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(err)
			params.MinAttendancePerWindow = tc.minAttendance
			s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
			s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr1, types.Attendance{
				EligibleBlocks: tc.eligibleBlocks,
				AttendedBlocks: tc.attendedBlocks,
			}))

			var consAddr sdk.ConsAddress
			if tc.callsValidator {
				if tc.missingValidator {
					s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(nil, tc.validatorErr)
				} else {
					var validator stakingtypes.Validator
					consAddr, validator = s.newBondedValidator(valAddr1)
					validator.Status = tc.status
					validator.Jailed = tc.jailed
					s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator, nil)
				}
			}
			if tc.expectJail {
				s.stakingKeeper.EXPECT().Jail(s.ctx, consAddr).Return(tc.jailErr)
			}

			err = s.keeper.SettleAttendance(s.ctx, attendanceWindowBlocks)
			if tc.expectErr != "" {
				s.Require().ErrorContains(err, tc.expectErr)
			} else {
				s.Require().NoError(err)
			}

			events := sdk.UnwrapSDKContext(s.ctx).EventManager().Events()
			if tc.expectJail && tc.expectErr == "" {
				s.requireTypedEvents(events, &types.EventOracleJail{
					Validator:        valAddr1.String(),
					EligibleBlocks:   tc.eligibleBlocks,
					AttendedBlocks:   tc.attendedBlocks,
					AttendanceWindow: attendanceWindowBlocks,
				})
			} else {
				s.Require().Empty(events)
			}

			// SettleAttendance only ever reads the Attendance collection: the
			// caller (EndBlocker) owns clearing it after settlement.
			attendance, err := s.keeper.Attendance.Get(s.ctx, valAddr1)
			s.Require().NoError(err)
			s.Require().Equal(types.Attendance{EligibleBlocks: tc.eligibleBlocks, AttendedBlocks: tc.attendedBlocks}, attendance)
		})
	}
}

// TestSettleAttendanceContinuesWalkAfterJailing pins that the jail branch of
// the Attendance walk returns (false, nil), not (true, nil): the walk must
// keep going and judge every remaining record instead of stopping after the
// first jail. Both validators here are below threshold and each arms a
// Validator+Jail pair; gomock's automatic expectation check at test cleanup
// fails if the walk stops early, since the second pair would then go
// uncalled. It also asserts both EventOracleJail events in walk order
// (valAddr1 sorts before valAddr2), which no other test covers.
func (s *KeeperTestSuite) TestSettleAttendanceContinuesWalkAfterJailing() {
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.MinAttendancePerWindow = math.LegacyNewDecWithPrec(90, 2)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr1, types.Attendance{EligibleBlocks: 20, AttendedBlocks: 0}))
	s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr2, types.Attendance{EligibleBlocks: 20, AttendedBlocks: 0}))

	consAddr1, validator1 := s.newBondedValidator(valAddr1)
	consAddr2, validator2 := s.newBondedValidator(valAddr2)
	s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator1, nil)
	s.stakingKeeper.EXPECT().Jail(s.ctx, consAddr1).Return(nil)
	s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr2).Return(validator2, nil)
	s.stakingKeeper.EXPECT().Jail(s.ctx, consAddr2).Return(nil)

	s.Require().NoError(s.keeper.SettleAttendance(s.ctx, 20))

	events := sdk.UnwrapSDKContext(s.ctx).EventManager().Events()
	s.requireTypedEvents(events,
		&types.EventOracleJail{
			Validator:        valAddr1.String(),
			EligibleBlocks:   20,
			AttendedBlocks:   0,
			AttendanceWindow: 20,
		},
		&types.EventOracleJail{
			Validator:        valAddr2.String(),
			EligibleBlocks:   20,
			AttendedBlocks:   0,
			AttendanceWindow: 20,
		},
	)
}
