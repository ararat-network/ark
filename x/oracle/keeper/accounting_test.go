package keeper_test

import (
	"errors"

	cmttypes "github.com/cometbft/cometbft/types"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"

	chain "ark/pkg/chain"
	"ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestAccountingCounters() {
	pubKey := ed25519.GenPrivKey().PubKey()
	validator, err := stakingtypes.NewValidator(valAddr1.String(), pubKey, stakingtypes.Description{})
	s.Require().NoError(err)
	consAddr, err := validator.GetConsAddr()
	s.Require().NoError(err)

	s.stakingKeeper.EXPECT().ValidatorByConsAddr(s.ctx, consAddr).Return(validator, nil).Times(2)

	s.Require().NoError(s.keeper.RecordVoteAccounting(s.ctx, consAddr, math.NewInt(3), true))
	s.Require().NoError(s.keeper.RecordVoteAccounting(s.ctx, consAddr, math.NewInt(4), true))
	missCount, err := s.keeper.MissCount.Get(s.ctx, valAddr1)
	s.Require().NoError(err)
	s.Require().Equal(uint64(2), missCount)

	rewardWeight, err := s.keeper.RewardWeight.Get(s.ctx, valAddr1)
	s.Require().NoError(err)
	s.Require().True(math.NewInt(7).Equal(rewardWeight))
}

func (s *KeeperTestSuite) TestRecordVoteAccountingEmptyUpdateSkipsValidatorLookup() {
	consAddr := sdk.ConsAddress([]byte("missing_validator___"))

	s.Require().NoError(s.keeper.RecordVoteAccounting(s.ctx, consAddr, math.ZeroInt(), false))

	entries := 0
	err := s.keeper.RewardWeight.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ math.Int) (bool, error) {
		entries++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Zero(entries)
}

func (s *KeeperTestSuite) TestRecordVoteAccountingAccumulatesLegalPowerBeyondUint64() {
	pubKey := ed25519.GenPrivKey().PubKey()
	validator, err := stakingtypes.NewValidator(valAddr1.String(), pubKey, stakingtypes.Description{})
	s.Require().NoError(err)
	consAddr, err := validator.GetConsAddr()
	s.Require().NoError(err)

	s.stakingKeeper.EXPECT().ValidatorByConsAddr(s.ctx, consAddr).Return(validator, nil).Times(3)

	blockScore := math.NewInt(cmttypes.MaxTotalVotingPower).MulRaw(8)
	for range 3 {
		s.Require().NoError(s.keeper.RecordVoteAccounting(s.ctx, consAddr, blockScore, false))
	}

	stored, err := s.keeper.RewardWeight.Get(s.ctx, valAddr1)
	s.Require().NoError(err)
	s.Require().True(blockScore.MulRaw(3).Equal(stored))
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
			err := s.keeper.RecordVoteAccounting(s.ctx, consAddr, tc.rewardWeight, false)
			s.Require().ErrorContains(err, tc.expectErr)
		})
	}
}

func (s *KeeperTestSuite) TestAccountingCountersSkipUnresolvedConsensusAddress() {
	consAddr := sdk.ConsAddress([]byte("missing_validator___"))
	s.stakingKeeper.EXPECT().
		ValidatorByConsAddr(s.ctx, consAddr).
		Return(nil, stakingtypes.ErrNoValidatorFound).
		Times(1)

	s.Require().NoError(s.keeper.RecordVoteAccounting(s.ctx, consAddr, math.NewInt(3), true))

	missCountEntries := 0
	err := s.keeper.MissCount.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ uint64) (bool, error) {
		missCountEntries++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Zero(missCountEntries)

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
			s.Require().NoError(s.keeper.RewardWeight.Clear(s.ctx, nil))
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
			eventsBefore := len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
			err := s.keeper.SettleRewards(s.ctx, rewardWindow, rewardDistributionWindow)
			events := sdk.UnwrapSDKContext(s.ctx).EventManager().Events()[eventsBefore:]
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

func (s *KeeperTestSuite) TestSettleSlash() {
	tests := []struct {
		name             string
		missCount        uint64
		minValid         math.LegacyDec
		status           stakingtypes.BondStatus
		jailed           bool
		missingValidator bool
		missingErr       bool
		expectSlash      bool
		expectJail       bool
	}{
		{
			name:        "slashes bonded validator below min valid rate",
			missCount:   20,
			minValid:    math.LegacyNewDecWithPrec(90, 2),
			status:      stakingtypes.Bonded,
			expectSlash: true,
			expectJail:  true,
		},
		{
			name:      "keeps bonded validator above min valid rate",
			missCount: 4,
			minValid:  math.LegacyNewDecWithPrec(50, 2),
		},
		{
			name:      "boundary valid vote rate is not slashed",
			missCount: 10,
			minValid:  math.LegacyNewDecWithPrec(50, 2),
		},
		{
			name:      "unbonded validator is not slashed",
			missCount: 20,
			minValid:  math.LegacyNewDecWithPrec(90, 2),
			status:    stakingtypes.Unbonded,
		},
		{
			name:        "slashes unbonding validator below min valid rate",
			missCount:   20,
			minValid:    math.LegacyNewDecWithPrec(90, 2),
			status:      stakingtypes.Unbonding,
			expectSlash: true,
			expectJail:  true,
		},
		{
			name:        "already jailed validator is slashed without jailing again",
			missCount:   20,
			minValid:    math.LegacyNewDecWithPrec(90, 2),
			status:      stakingtypes.Bonded,
			jailed:      true,
			expectSlash: true,
		},
		{
			name:        "already jailed unbonding validator is slashed without jailing again",
			missCount:   20,
			minValid:    math.LegacyNewDecWithPrec(90, 2),
			status:      stakingtypes.Unbonding,
			jailed:      true,
			expectSlash: true,
		},
		{
			name:             "missing validator is not slashed",
			missCount:        20,
			minValid:         math.LegacyNewDecWithPrec(90, 2),
			missingValidator: true,
		},
		{
			name:             "missing validator error is not slashed",
			missCount:        20,
			minValid:         math.LegacyNewDecWithPrec(90, 2),
			missingValidator: true,
			missingErr:       true,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(100)

			params, err := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(err)
			params.SlashWindow = 20
			params.MinValidPerWindow = tc.minValid
			params.SlashFraction = math.LegacyNewDecWithPrec(1, 4)
			s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
			s.Require().NoError(s.keeper.MissCount.Set(s.ctx, valAddr1, tc.missCount))
			eventsBefore := len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())

			powerReduction := math.NewInt(1_000_000)
			s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(powerReduction)
			s.stakingKeeper.EXPECT().BondDenom(s.ctx).Return(chain.NoahBaseDenom, nil)

			if tc.missingValidator {
				var err error
				if tc.missingErr {
					err = stakingtypes.ErrNoValidatorFound
				}
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(nil, err)
			} else if tc.status != stakingtypes.Unspecified {
				pubKey := ed25519.GenPrivKey().PubKey()
				validator, err := stakingtypes.NewValidator(valAddr1.String(), pubKey, stakingtypes.Description{})
				s.Require().NoError(err)
				validator.Status = tc.status
				validator.Jailed = tc.jailed
				validator.Tokens = powerReduction.MulRaw(10)

				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(validator, nil)

				if tc.expectSlash {
					consAddr, err := validator.GetConsAddr()
					s.Require().NoError(err)
					s.stakingKeeper.EXPECT().Slash(
						s.ctx,
						consAddr,
						100-sdk.ValidatorUpdateDelay-1,
						int64(10),
						params.SlashFraction,
					).Return(math.NewInt(1), nil)
					if tc.expectJail {
						s.stakingKeeper.EXPECT().Jail(s.ctx, consAddr)
					}
				}
			}

			err = s.keeper.SettleSlash(s.ctx, 20)
			s.Require().NoError(err)

			missCount, err := s.keeper.MissCount.Get(s.ctx, valAddr1)
			s.Require().NoError(err)
			s.Require().Equal(tc.missCount, missCount)

			events := sdk.UnwrapSDKContext(s.ctx).EventManager().Events()[eventsBefore:]
			if tc.expectSlash {
				s.requireTypedEvents(events, &types.EventOracleSlash{
					Validator:   valAddr1.String(),
					AmountDenom: chain.NoahBaseDenom,
					Amount:      math.NewInt(1),
					MissCount:   20,
					SlashWindow: 20,
				})
			} else {
				s.Require().Empty(events)
			}
		})
	}
}

func (s *KeeperTestSuite) TestSettleSlashReadsBondDenomOnce() {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(100)

	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.MinValidPerWindow = math.LegacyNewDecWithPrec(90, 2)
	params.SlashFraction = math.LegacyNewDecWithPrec(1, 4)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.MissCount.Set(s.ctx, valAddr1, 20))
	s.Require().NoError(s.keeper.MissCount.Set(s.ctx, valAddr2, 20))

	powerReduction := math.NewInt(1_000_000)
	s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(powerReduction)
	s.stakingKeeper.EXPECT().BondDenom(s.ctx).Return(chain.NoahBaseDenom, nil).Times(1)

	expectedEvents := make([]proto.Message, 0, 2)
	for _, valAddr := range []sdk.ValAddress{valAddr1, valAddr2} {
		pubKey := ed25519.GenPrivKey().PubKey()
		validator, err := stakingtypes.NewValidator(valAddr.String(), pubKey, stakingtypes.Description{})
		s.Require().NoError(err)
		validator.Status = stakingtypes.Bonded
		validator.Tokens = powerReduction.MulRaw(10)
		consAddr, err := validator.GetConsAddr()
		s.Require().NoError(err)

		s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr).Return(validator, nil)
		s.stakingKeeper.EXPECT().Slash(
			s.ctx,
			consAddr,
			100-sdk.ValidatorUpdateDelay-1,
			int64(10),
			params.SlashFraction,
		).Return(math.NewInt(1), nil)
		s.stakingKeeper.EXPECT().Jail(s.ctx, consAddr)
		expectedEvents = append(expectedEvents, &types.EventOracleSlash{
			Validator:   valAddr.String(),
			AmountDenom: chain.NoahBaseDenom,
			Amount:      math.NewInt(1),
			MissCount:   20,
			SlashWindow: 20,
		})
	}

	eventsBefore := len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
	s.Require().NoError(s.keeper.SettleSlash(s.ctx, 20))
	s.requireTypedEvents(
		sdk.UnwrapSDKContext(s.ctx).EventManager().Events()[eventsBefore:],
		expectedEvents...,
	)
}
