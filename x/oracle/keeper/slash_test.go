package keeper_test

import (
	"errors"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func (s *KeeperTestSuite) TestSlashAndResetMissCounts() {
	type missCount struct {
		operator sdk.ValAddress
		count    uint64
	}

	tests := []struct {
		name        string
		minValid    math.LegacyDec
		missCounts  []missCount
		status      stakingtypes.BondStatus
		jailed      bool
		expectSlash bool
	}{
		{
			name:        "slashes bonded validator below min valid rate",
			minValid:    math.LegacyNewDecWithPrec(90, 2),
			missCounts:  []missCount{{operator: valAddr1, count: 5}},
			status:      stakingtypes.Bonded,
			expectSlash: true,
		},
		{
			name:       "keeps bonded validator above min valid rate",
			minValid:   math.LegacyNewDecWithPrec(50, 2),
			missCounts: []missCount{{operator: valAddr1, count: 4}},
		},
		{
			name:       "boundary valid vote rate is not slashed",
			minValid:   math.LegacyNewDecWithPrec(50, 2),
			missCounts: []missCount{{operator: valAddr1, count: 5}},
		},
		{
			name:       "unbonded validator is not slashed",
			minValid:   math.LegacyNewDecWithPrec(90, 2),
			missCounts: []missCount{{operator: valAddr1, count: 5}},
			status:     stakingtypes.Unbonded,
		},
		{
			name:       "jailed validator is not slashed",
			minValid:   math.LegacyNewDecWithPrec(90, 2),
			missCounts: []missCount{{operator: valAddr1, count: 5}},
			status:     stakingtypes.Bonded,
			jailed:     true,
		},
		{
			name:     "clears all counters across multiple validators",
			minValid: math.LegacyNewDecWithPrec(50, 2),
			missCounts: []missCount{
				{operator: valAddr1, count: 4},
				{operator: valAddr2, count: 1},
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(100)

			params, err := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(err)
			params.VotePeriod = 10
			params.SlashWindow = 100
			params.MinValidPerWindow = tc.minValid
			params.SlashFraction = math.LegacyNewDecWithPrec(1, 4)
			s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

			for _, missCount := range tc.missCounts {
				s.Require().NoError(s.keeper.MissCount.Set(s.ctx, missCount.operator, missCount.count))
			}

			s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))

			if tc.status != stakingtypes.Unspecified {
				pubKey := ed25519.GenPrivKey().PubKey()
				validator, err := stakingtypes.NewValidator(valAddr1.String(), pubKey, stakingtypes.Description{})
				s.Require().NoError(err)
				validator.Status = tc.status
				validator.Jailed = tc.jailed
				validator.Tokens = math.NewInt(1_000_000).MulRaw(10)

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
					)
					s.stakingKeeper.EXPECT().Jail(s.ctx, consAddr)
				}
			}

			err = s.keeper.SlashAndResetMissCounts(s.ctx)
			s.Require().NoError(err)

			for _, missCount := range tc.missCounts {
				_, err := s.keeper.MissCount.Get(s.ctx, missCount.operator)
				s.Require().True(errors.Is(err, collections.ErrNotFound), "expected miss counter to be removed, got %v", err)
			}
		})
	}
}
