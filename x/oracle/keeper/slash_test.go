package keeper_test

import (
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

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
	}{
		{
			name:        "slashes bonded validator below min valid rate",
			missCount:   20,
			minValid:    math.LegacyNewDecWithPrec(90, 2),
			status:      stakingtypes.Bonded,
			expectSlash: true,
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
			name:      "jailed validator is not slashed",
			missCount: 20,
			minValid:  math.LegacyNewDecWithPrec(90, 2),
			status:    stakingtypes.Bonded,
			jailed:    true,
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

			powerReduction := math.NewInt(1_000_000)
			s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(powerReduction)

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
					s.stakingKeeper.EXPECT().Jail(s.ctx, consAddr)
				}
			}

			err = s.keeper.SettleSlash(s.ctx)
			s.Require().NoError(err)

			missCount, err := s.keeper.MissCount.Get(s.ctx, valAddr1)
			s.Require().NoError(err)
			s.Require().Equal(tc.missCount, missCount)
		})
	}
}
