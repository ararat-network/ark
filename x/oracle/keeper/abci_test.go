package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec/address"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	core "noah/types"
	"noah/x/oracle/types"
)

// mockIterator implements store.Iterator for testing methods that use ValidatorsPowerStoreIterator.
type mockIterator struct {
	values [][]byte
	pos    int
}

func newMockIterator(values ...[]byte) *mockIterator {
	return &mockIterator{values: values}
}

func (m *mockIterator) Domain() ([]byte, []byte) { return nil, nil }
func (m *mockIterator) Valid() bool              { return m.pos < len(m.values) }
func (m *mockIterator) Next()                    { m.pos++ }
func (m *mockIterator) Key() []byte              { return m.values[m.pos] }
func (m *mockIterator) Value() []byte            { return m.values[m.pos] }
func (m *mockIterator) Error() error             { return nil }
func (m *mockIterator) Close() error             { return nil }

var valCodec = address.NewBech32Codec("cosmosvaloper")

// makeValidator builds a stakingtypes.Validator with the given address, status, and consensus power.
func makeValidator(valAddr sdk.ValAddress, status stakingtypes.BondStatus, power int64) stakingtypes.Validator {
	operStr, _ := valCodec.BytesToString(valAddr)
	return stakingtypes.Validator{
		OperatorAddress: operStr,
		Status:          status,
		Tokens:          math.NewInt(1_000_000).MulRaw(power),
	}
}

// operStr returns the bech32 operator string for a ValAddress.
func operStr(valAddr sdk.ValAddress) string {
	s, _ := valCodec.BytesToString(valAddr)
	return s
}

// setupBuildValidatorClaimMapMocks sets up the standard staking mock expectations
// for BuildValidatorClaimMap with the given bonded validators and max validators cap.
func (s *KeeperTestSuite) setupBuildValidatorClaimMapMocks(
	bondedVals []sdk.ValAddress,
	powers []int64,
	maxVals uint32,
) {
	iterVals := make([][]byte, len(bondedVals))
	for i, v := range bondedVals {
		iterVals[i] = []byte(v)
	}

	s.stakingKeeper.EXPECT().MaxValidators(s.ctx).Return(maxVals)
	s.stakingKeeper.EXPECT().ValidatorsPowerStoreIterator(s.ctx).Return(newMockIterator(iterVals...))
	s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))

	for i, v := range bondedVals {
		s.stakingKeeper.EXPECT().Validator(s.ctx, v).Return(makeValidator(v, stakingtypes.Bonded, powers[i]))
	}
	if len(bondedVals) > 0 {
		s.stakingKeeper.EXPECT().ValidatorAddressCodec().Return(valCodec).AnyTimes()
	}
}

// setupEndBlockerVotePeriodMocks sets up all mocks needed when EndBlocker triggers
// a vote-period tally (BuildValidatorClaimMap -> Tally -> Reward -> ClearBallots -> ApplyWhitelist).
func (s *KeeperTestSuite) setupEndBlockerVotePeriodMocks() {
	s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroKRWDenom, math.LegacyNewDecWithPrec(25, 4)))

	for _, va := range []sdk.ValAddress{valAddr1, valAddr2} {
		s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, va, types.AggregateExchangeRateVote{
			ExchangeRateTuples: types.ExchangeRateTuples{{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(1000)}},
			Voter:              operStr(va),
		}))
	}

	s.setupBuildValidatorClaimMapMocks(
		[]sdk.ValAddress{valAddr1, valAddr2},
		[]int64{10, 10},
		100,
	)

	s.stakingKeeper.EXPECT().TotalBondedTokens(s.ctx).Return(math.NewInt(2_000_000))
	s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000)).AnyTimes()

	s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(
		mockModuleAccount{addr: sdk.AccAddress{1}},
	)
	s.bankKeeper.EXPECT().GetAllBalances(s.ctx, sdk.AccAddress{1}).Return(
		sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1_000_000))),
	)
	s.stakingKeeper.EXPECT().Validator(s.ctx, gomock.Any()).Return(
		makeValidator(valAddr1, stakingtypes.Bonded, 10),
	).AnyTimes()
	s.distrKeeper.EXPECT().AllocateTokensToValidator(s.ctx, gomock.Any(), gomock.Any()).AnyTimes()
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(s.ctx, types.ModuleName, "distribution", gomock.Any()).Return(nil)

	s.bankKeeper.EXPECT().GetDenomMetaData(s.ctx, gomock.Any()).Return(banktypes.Metadata{}, true).AnyTimes()
}

func (s *KeeperTestSuite) TestTallyExchangeRates() {
	tests := []struct {
		name           string
		voteTargets    map[string]math.LegacyDec
		setup          func()
		expectedDenoms []string // nil = expect no rates set
	}{
		{
			name: "single denom — rate set from ballot median",
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: math.LegacyNewDecWithPrec(25, 4),
			},
			setup: func() {
				for _, va := range []sdk.ValAddress{valAddr1, valAddr2} {
					s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, va, types.AggregateExchangeRateVote{
						ExchangeRateTuples: types.ExchangeRateTuples{{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(1000)}},
						Voter:              operStr(va),
					}))
				}
				s.stakingKeeper.EXPECT().TotalBondedTokens(s.ctx).Return(math.NewInt(2_000_000))
				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
			},
			expectedDenoms: []string{core.MicroKRWDenom},
		},
		{
			name: "multiple denoms — cross-rate computed via reference",
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: math.LegacyNewDecWithPrec(25, 4),
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
			},
			setup: func() {
				for _, va := range []sdk.ValAddress{valAddr1, valAddr2} {
					s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, va, types.AggregateExchangeRateVote{
						ExchangeRateTuples: types.ExchangeRateTuples{
							{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(1000)},
							{Denom: core.MicroUSDDenom, ExchangeRate: math.LegacyNewDec(1)},
						},
						Voter: operStr(va),
					}))
				}
				s.stakingKeeper.EXPECT().TotalBondedTokens(s.ctx).Return(math.NewInt(2_000_000))
				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
			},
			expectedDenoms: []string{core.MicroKRWDenom, core.MicroUSDDenom},
		},
		{
			name: "ballot below threshold — no rates set",
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: math.LegacyNewDecWithPrec(25, 4),
			},
			setup: func() {
				s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr1, types.AggregateExchangeRateVote{
					ExchangeRateTuples: types.ExchangeRateTuples{{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(1000)}},
					Voter:              operStr(valAddr1),
				}))
				s.stakingKeeper.EXPECT().TotalBondedTokens(s.ctx).Return(math.NewInt(100_000_000))
				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
			},
		},
		{
			name: "no votes — no rates set",
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: math.LegacyNewDecWithPrec(25, 4),
			},
			setup: func() {
				s.stakingKeeper.EXPECT().TotalBondedTokens(s.ctx).Return(math.NewInt(2_000_000))
				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
			},
		},
		{
			name: "clears pre-existing exchange rate",
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: math.LegacyNewDecWithPrec(25, 4),
			},
			setup: func() {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, "ustale", math.LegacyNewDec(999)))

				for _, va := range []sdk.ValAddress{valAddr1, valAddr2} {
					s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, va, types.AggregateExchangeRateVote{
						ExchangeRateTuples: types.ExchangeRateTuples{{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(1000)}},
						Voter:              operStr(va),
					}))
				}
				s.stakingKeeper.EXPECT().TotalBondedTokens(s.ctx).Return(math.NewInt(2_000_000))
				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
			},
			expectedDenoms: []string{core.MicroKRWDenom},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

			validatorClaimMap := map[string]types.Claim{
				operStr(valAddr1): types.NewClaim(10, 0, 0, valAddr1),
				operStr(valAddr2): types.NewClaim(10, 0, 0, valAddr2),
			}

			params, err := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(err)

			err = s.keeper.TallyExchangeRates(s.ctx, params, tc.voteTargets, validatorClaimMap)
			s.Require().NoError(err)

			for _, denom := range tc.expectedDenoms {
				rate, err := s.keeper.ExchangeRate.Get(s.ctx, denom)
				s.Require().NoError(err, "expected rate for %s", denom)
				s.Require().True(rate.IsPositive(), "rate for %s should be positive, got %s", denom, rate)
			}

			// Verify no extra rates (catches stale rates that should have been cleared)
			count := 0
			_ = s.keeper.ExchangeRate.Walk(s.ctx, nil, func(_ string, _ math.LegacyDec) (bool, error) {
				count++
				return false, nil
			})
			s.Require().Equal(len(tc.expectedDenoms), count, "unexpected exchange rate count")
		})
	}
}

func (s *KeeperTestSuite) TestEndBlocker() {
	tests := []struct {
		name  string
		setup func()
	}{
		{
			name: "mid-period — no tally, no slash",
			setup: func() {
				sdkCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(3)
				s.ctx = sdkCtx
			},
		},
		{
			name: "last block of vote period — tally runs",
			setup: func() {
				sdkCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(4)
				s.ctx = sdkCtx

				params, _ := s.keeper.Params.Get(s.ctx)
				params.VotePeriod = 5
				params.SlashWindow = 1000
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

				s.setupEndBlockerVotePeriodMocks()
			},
		},
		{
			name: "last block of slash window — slash runs",
			setup: func() {
				sdkCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(9)
				s.ctx = sdkCtx

				params, _ := s.keeper.Params.Get(s.ctx)
				params.VotePeriod = 5
				params.SlashWindow = 10
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

				s.setupEndBlockerVotePeriodMocks()
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

			err := s.keeper.EndBlocker(s.ctx)
			s.Require().NoError(err)
		})
	}
}

// --- Helpers ---

// mockModuleAccount implements sdk.ModuleAccountI for testing.
type mockModuleAccount struct {
	sdk.ModuleAccountI
	addr sdk.AccAddress
}

func (m mockModuleAccount) GetAddress() sdk.AccAddress { return m.addr }
