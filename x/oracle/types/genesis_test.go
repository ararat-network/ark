package types_test

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
	"ark/x/oracle/types"
)

func TestValidateGenesis(t *testing.T) {
	validatorAddress := sdk.ValAddress([]byte("validator-1-address")).String()
	otherValidatorAddress := sdk.ValAddress([]byte("validator-2-address")).String()

	tests := []struct {
		name      string
		mutate    func(*types.GenesisState)
		expectErr string
	}{
		{
			name:   "default is valid",
			mutate: func(gs *types.GenesisState) {},
		},
		{
			name: "accounting reward window must be positive",
			mutate: func(gs *types.GenesisState) {
				gs.Accounting.RewardWindow = 0
			},
			expectErr: "accounting reward window must be greater than zero",
		},
		{
			name: "accounting reward distribution window cannot be shorter than reward window",
			mutate: func(gs *types.GenesisState) {
				gs.Accounting.RewardDistributionWindow = gs.Accounting.RewardWindow - 1
			},
			expectErr: "accounting reward distribution window must be greater than or equal to reward window",
		},
		{
			name: "accounting attendance window must be positive",
			mutate: func(gs *types.GenesisState) {
				gs.Accounting.AttendanceWindow = 0
			},
			expectErr: "accounting attendance window must be greater than zero",
		},
		// ExchangeRates
		{
			name: "exchange rate empty denom",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = []types.ExchangeRate{
					{Denom: "", Rate: math.LegacyOneDec()},
				}
			},
			expectErr: "exchange rate denom must be an Ark-native base denom matching",
		},
		{
			name: "exchange rate denom must be canonical lowercase",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = []types.ExchangeRate{
					{Denom: "aUSD", Rate: math.LegacyOneDec()},
				}
			},
			expectErr: "exchange rate denom must be an Ark-native base denom matching",
		},
		{
			name: "exchange rate not positive",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = []types.ExchangeRate{
					{Denom: "ausd", Rate: math.LegacyZeroDec()},
				}
			},
			expectErr: "exchange rate for ausd must be positive",
		},
		{
			name: "exchange rate is nil",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = []types.ExchangeRate{
					{Denom: "ausd", Rate: math.LegacyDec{}},
				}
			},
			expectErr: "exchange rate for ausd must be set",
		},
		{
			name: "exchange rate is out of range",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = []types.ExchangeRate{
					{
						Denom: "ausd",
						Rate: math.LegacyNewDecFromBigInt(
							new(big.Int).Lsh(big.NewInt(1), 256),
						),
					},
				}
			},
			expectErr: "exchange rate for ausd must be representable",
		},
		{
			name: "sorted exchange rates are valid",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = []types.ExchangeRate{
					{Denom: "akrw", Rate: math.LegacyOneDec()},
					{Denom: "ausd", Rate: math.LegacyNewDec(2)},
				}
			},
		},
		{
			name: "unsorted exchange rates",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = []types.ExchangeRate{
					{Denom: "ausd", Rate: math.LegacyOneDec()},
					{Denom: "akrw", Rate: math.LegacyNewDec(2)},
				}
			},
			expectErr: "genesis exchange rates must be sorted by unique denom",
		},
		{
			name: "duplicate exchange rate denom",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = []types.ExchangeRate{
					{Denom: "ausd", Rate: math.LegacyOneDec()},
					{Denom: "ausd", Rate: math.LegacyNewDec(2)},
				}
			},
			expectErr: "genesis exchange rates must be sorted by unique denom",
		},
		{
			name: "exchange rate denom must be an active feed",
			mutate: func(gs *types.GenesisState) {
				gs.ExchangeRates = []types.ExchangeRate{
					{Denom: "afoo", Rate: math.LegacyOneDec()},
				}
			},
			expectErr: "exchange rate afoo is not an active feed",
		},
		// RewardWeights
		{
			name: "reward weight must be set",
			mutate: func(gs *types.GenesisState) {
				gs.RewardWeights = []types.RewardWeight{
					{ValidatorAddress: validatorAddress, RewardWeight: math.Int{}},
				}
			},
			expectErr: "reward weight must be set",
		},
		{
			name: "reward weight must not be negative",
			mutate: func(gs *types.GenesisState) {
				gs.RewardWeights = []types.RewardWeight{
					{ValidatorAddress: validatorAddress, RewardWeight: math.NewInt(-1)},
				}
			},
			expectErr: "reward weight must not be negative",
		},
		{
			name: "reward weight empty validator address",
			mutate: func(gs *types.GenesisState) {
				gs.RewardWeights = []types.RewardWeight{
					{ValidatorAddress: "", RewardWeight: math.NewInt(1)},
				}
			},
			expectErr: "reward weight validator address must not be empty",
		},
		{
			name: "sorted reward weights are valid",
			mutate: func(gs *types.GenesisState) {
				gs.RewardWeights = []types.RewardWeight{
					{ValidatorAddress: validatorAddress, RewardWeight: math.NewInt(1)},
					{ValidatorAddress: otherValidatorAddress, RewardWeight: math.NewInt(2)},
				}
			},
		},
		{
			name: "unsorted reward weights",
			mutate: func(gs *types.GenesisState) {
				gs.RewardWeights = []types.RewardWeight{
					{ValidatorAddress: otherValidatorAddress, RewardWeight: math.NewInt(1)},
					{ValidatorAddress: validatorAddress, RewardWeight: math.NewInt(2)},
				}
			},
			expectErr: "genesis reward weights must be sorted by unique validator address",
		},
		{
			name: "duplicate reward weight",
			mutate: func(gs *types.GenesisState) {
				gs.RewardWeights = []types.RewardWeight{
					{ValidatorAddress: validatorAddress, RewardWeight: math.NewInt(1)},
					{ValidatorAddress: validatorAddress, RewardWeight: math.NewInt(2)},
				}
			},
			expectErr: "genesis reward weights must be sorted by unique validator address",
		},
		{
			name: "reward weight invalid validator address",
			mutate: func(gs *types.GenesisState) {
				gs.RewardWeights = []types.RewardWeight{
					{ValidatorAddress: "not-a-validator-address", RewardWeight: math.NewInt(1)},
				}
			},
			expectErr: "reward weight validator address is invalid",
		},
		// AttendanceRecords
		{
			name: "attendance record empty validator address",
			mutate: func(gs *types.GenesisState) {
				gs.AttendanceRecords = []types.AttendanceRecord{
					{ValidatorAddress: "", Attendance: types.Attendance{EligibleBlocks: 1, AttendedBlocks: 1}},
				}
			},
			expectErr: "attendance record validator address must not be empty",
		},
		{
			name: "sorted attendance records are valid",
			mutate: func(gs *types.GenesisState) {
				gs.AttendanceRecords = []types.AttendanceRecord{
					{ValidatorAddress: validatorAddress, Attendance: types.Attendance{EligibleBlocks: 1, AttendedBlocks: 1}},
					{ValidatorAddress: otherValidatorAddress, Attendance: types.Attendance{EligibleBlocks: 2, AttendedBlocks: 2}},
				}
			},
		},
		{
			name: "unsorted attendance records",
			mutate: func(gs *types.GenesisState) {
				gs.AttendanceRecords = []types.AttendanceRecord{
					{ValidatorAddress: otherValidatorAddress, Attendance: types.Attendance{EligibleBlocks: 1, AttendedBlocks: 1}},
					{ValidatorAddress: validatorAddress, Attendance: types.Attendance{EligibleBlocks: 2, AttendedBlocks: 2}},
				}
			},
			expectErr: "genesis attendance records must be sorted by unique validator address",
		},
		{
			name: "duplicate attendance record",
			mutate: func(gs *types.GenesisState) {
				gs.AttendanceRecords = []types.AttendanceRecord{
					{ValidatorAddress: validatorAddress, Attendance: types.Attendance{EligibleBlocks: 1, AttendedBlocks: 1}},
					{ValidatorAddress: validatorAddress, Attendance: types.Attendance{EligibleBlocks: 2, AttendedBlocks: 2}},
				}
			},
			expectErr: "genesis attendance records must be sorted by unique validator address",
		},
		{
			name: "attendance record invalid validator address",
			mutate: func(gs *types.GenesisState) {
				gs.AttendanceRecords = []types.AttendanceRecord{
					{ValidatorAddress: "not-a-validator-address", Attendance: types.Attendance{EligibleBlocks: 1, AttendedBlocks: 1}},
				}
			},
			expectErr: "attendance record validator address is invalid",
		},
		{
			name: "attended above eligible",
			mutate: func(gs *types.GenesisState) {
				gs.AttendanceRecords = []types.AttendanceRecord{{
					ValidatorAddress: validatorAddress,
					Attendance:       types.Attendance{EligibleBlocks: 1, AttendedBlocks: 2},
				}}
			},
			expectErr: "attendance record attended blocks 2 exceed eligible blocks 1",
		},
		{
			name: "eligible above attendance window",
			mutate: func(gs *types.GenesisState) {
				gs.AttendanceRecords = []types.AttendanceRecord{{
					ValidatorAddress: validatorAddress,
					Attendance:       types.Attendance{EligibleBlocks: gs.Accounting.AttendanceWindow + 1, AttendedBlocks: 0},
				}}
			},
			expectErr: fmt.Sprintf("attendance record eligible blocks %d exceed attendance window %d",
				types.DefaultAttendanceWindow+1, types.DefaultAttendanceWindow),
		},
		// Feeds
		{
			name: "feed denom must be a valid symbol",
			mutate: func(gs *types.GenesisState) {
				gs.Feeds.Denoms = []string{"a"}
			},
			expectErr: "must be an Ark-native base denom matching",
		},
		{
			name: "feed denom must be lowercase",
			mutate: func(gs *types.GenesisState) {
				gs.Feeds.Denoms = []string{"aUSD"}
			},
			expectErr: "must be an Ark-native base denom matching",
		},
		{
			name: "feed denom cannot contain path separators",
			mutate: func(gs *types.GenesisState) {
				gs.Feeds.Denoms = []string{"afoo/bar"}
			},
			expectErr: "must be an Ark-native base denom matching",
		},
		{
			name: "numeraire cannot be configured as a feed",
			mutate: func(gs *types.GenesisState) {
				gs.Feeds.Denoms = []string{chain.NoahBaseDenom}
			},
			expectErr: "is the numeraire and is never priced",
		},
		{
			name: "duplicate feed",
			mutate: func(gs *types.GenesisState) {
				gs.Feeds.Denoms = []string{"ausd", "ausd"}
			},
			expectErr: "must be sorted by unique denom",
		},
		{
			name: "feeds must be sorted",
			mutate: func(gs *types.GenesisState) {
				gs.Feeds.Denoms = []string{"ausd", "akrw"}
			},
			expectErr: "must be sorted by unique denom",
		},
		{
			name: "maximum feeds is valid",
			mutate: func(gs *types.GenesisState) {
				gs.Feeds.Denoms = makeTestDenoms(types.MaxFeeds)
			},
		},
		{
			name: "too many feeds",
			mutate: func(gs *types.GenesisState) {
				gs.Feeds.Denoms = makeTestDenoms(types.MaxFeeds + 1)
			},
			expectErr: "exceeds maximum feeds",
		},
		{
			name: "addition in flight is valid",
			mutate: func(gs *types.GenesisState) {
				gs.Feeds.Transitions = []types.FeedTransition{{
					Denom:                "agold",
					Direction:            types.FeedDirection_FEED_DIRECTION_ADD,
					ActivationVoteHeight: 10,
				}}
			},
		},
		{
			name: "removal in flight is valid",
			mutate: func(gs *types.GenesisState) {
				gs.Feeds.Transitions = []types.FeedTransition{{
					Denom:                gs.Feeds.Denoms[0],
					Direction:            types.FeedDirection_FEED_DIRECTION_REMOVE,
					ActivationVoteHeight: 10,
				}}
			},
		},
		// Valid custom genesis
		{
			name: "custom valid genesis",
			mutate: func(gs *types.GenesisState) {
				params := types.DefaultParams()
				*gs = *types.NewGenesisState(
					params,
					[]types.ExchangeRate{
						{Denom: "ausd", Rate: math.LegacyOneDec()},
					},
					[]types.RewardWeight{
						{ValidatorAddress: validatorAddress, RewardWeight: math.NewInt(1)},
					},
					[]types.AttendanceRecord{
						{ValidatorAddress: otherValidatorAddress, Attendance: types.Attendance{}},
					},
					types.NewAccounting(params),
					types.Feeds{
						Denoms:  []string{"ausd"},
						Version: types.InitialFeedVersion,
					},
					"ausd",
				)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gs := types.DefaultGenesisState()
			tc.mutate(gs)
			err := gs.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.expectErr)
			}
		})
	}
}

func makeTestDenoms(count int) []string {
	denoms := make([]string, count)
	for i := range count {
		denoms[i] = fmt.Sprintf("a%03d", i)
	}
	return denoms
}
