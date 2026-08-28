package types_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/x/oracle/types"
)

// Each combined bound asserts one message from both its lower and upper cases,
// so those strings live once.
const (
	voteThresholdOutOfRange        = "VoteThreshold must be between"
	functioningThresholdOutOfRange = "FunctioningBlockThreshold must be between"
	exchangeRateAgeOutOfRange      = "MaxExchangeRateAge must be greater than zero and at most"
)

func TestParamsValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.Params)
		expectErr string
		check     func(*testing.T, types.Params)
	}{
		{
			name:   "default is valid",
			mutate: func(p *types.Params) {},
			check: func(t *testing.T, p types.Params) {
				require.Equal(t, math.LegacyMustNewDecFromStr("0.666666666666666667"), p.VoteThreshold)
				require.Equal(t, time.Minute, p.MaxExchangeRateAge)
			},
		},
		// VoteThreshold
		{
			name:      "vote threshold missing",
			mutate:    func(p *types.Params) { p.VoteThreshold = math.LegacyDec{} },
			expectErr: "VoteThreshold must be set",
		},
		{
			name:      "vote threshold below 50%",
			mutate:    func(p *types.Params) { p.VoteThreshold = math.LegacyNewDecWithPrec(499, 3) },
			expectErr: voteThresholdOutOfRange,
		},
		{
			name:      "vote threshold zero",
			mutate:    func(p *types.Params) { p.VoteThreshold = math.LegacyZeroDec() },
			expectErr: voteThresholdOutOfRange,
		},
		{
			name:   "vote threshold at 50%",
			mutate: func(p *types.Params) { p.VoteThreshold = types.MinVoteThreshold },
		},
		{
			name:   "vote threshold at 100%",
			mutate: func(p *types.Params) { p.VoteThreshold = math.LegacyOneDec() },
		},
		{
			name:      "vote threshold above 100%",
			mutate:    func(p *types.Params) { p.VoteThreshold = math.LegacyNewDecWithPrec(1001, 3) },
			expectErr: voteThresholdOutOfRange,
		},
		// RewardBand
		{
			name:      "reward band missing",
			mutate:    func(p *types.Params) { p.RewardBand = math.LegacyDec{} },
			expectErr: "RewardBand must be set",
		},
		{
			name:      "reward band negative",
			mutate:    func(p *types.Params) { p.RewardBand = math.LegacyNewDecWithPrec(-1, 2) },
			expectErr: "RewardBand must be between [0, 1]",
		},
		{
			name:      "reward band above one",
			mutate:    func(p *types.Params) { p.RewardBand = math.LegacyNewDecWithPrec(101, 2) },
			expectErr: "RewardBand must be between [0, 1]",
		},
		{
			name:   "reward band at one",
			mutate: func(p *types.Params) { p.RewardBand = math.LegacyOneDec() },
		},
		{
			name:   "reward band at zero",
			mutate: func(p *types.Params) { p.RewardBand = math.LegacyZeroDec() },
		},
		// RewardWindow
		{
			name:      "reward window zero",
			mutate:    func(p *types.Params) { p.RewardWindow = 0 },
			expectErr: "RewardWindow must be between one and",
		},
		{
			name:   "reward window at the domain cap",
			mutate: func(p *types.Params) { p.RewardWindow = types.MaxRewardWindow },
		},
		{
			// A cadence no chain reaches does not slow settlement down, it
			// switches it off: the window is settlement's only trigger.
			name:      "reward window above the domain cap",
			mutate:    func(p *types.Params) { p.RewardWindow = types.MaxRewardWindow + 1 },
			expectErr: "RewardWindow must be between one and",
		},
		// RewardDistributionWindow
		{
			name:      "reward distribution window less than reward window",
			mutate:    func(p *types.Params) { p.RewardDistributionWindow = p.RewardWindow - 1 },
			expectErr: "RewardDistributionWindow must be greater than or equal with RewardWindow",
		},
		{
			name:   "reward distribution window equal to reward window",
			mutate: func(p *types.Params) { p.RewardDistributionWindow = p.RewardWindow },
		},
		// AttendanceWindow
		{
			name:      "attendance window zero",
			mutate:    func(p *types.Params) { p.AttendanceWindow = 0 },
			expectErr: "AttendanceWindow must be between one and",
		},
		{
			name:   "attendance window at the domain cap",
			mutate: func(p *types.Params) { p.AttendanceWindow = types.MaxAttendanceWindow },
		},
		{
			// Unreachable means attendance never grades, which is the deadman
			// switch jailing depends on.
			name:      "attendance window above the domain cap",
			mutate:    func(p *types.Params) { p.AttendanceWindow = types.MaxAttendanceWindow + 1 },
			expectErr: "AttendanceWindow must be between one and",
		},
		{
			name:   "attendance window at one",
			mutate: func(p *types.Params) { p.AttendanceWindow = 1 },
		},
		// MinAttendancePerWindow
		{
			name:      "min attendance per window missing",
			mutate:    func(p *types.Params) { p.MinAttendancePerWindow = math.LegacyDec{} },
			expectErr: "MinAttendancePerWindow must be set",
		},
		{
			name:      "min attendance per window negative",
			mutate:    func(p *types.Params) { p.MinAttendancePerWindow = math.LegacyNewDec(-1) },
			expectErr: "MinAttendancePerWindow must be between [0, 1]",
		},
		{
			name:      "min attendance per window above one",
			mutate:    func(p *types.Params) { p.MinAttendancePerWindow = math.LegacyNewDecWithPrec(101, 2) },
			expectErr: "MinAttendancePerWindow must be between [0, 1]",
		},
		{
			name:   "min attendance per window at zero",
			mutate: func(p *types.Params) { p.MinAttendancePerWindow = math.LegacyZeroDec() },
		},
		{
			name:   "min attendance per window at one",
			mutate: func(p *types.Params) { p.MinAttendancePerWindow = math.LegacyOneDec() },
		},
		// FunctioningBlockThreshold
		{
			name:      "functioning block threshold missing",
			mutate:    func(p *types.Params) { p.FunctioningBlockThreshold = math.LegacyDec{} },
			expectErr: "FunctioningBlockThreshold must be set",
		},
		{
			name:      "functioning block threshold below 50%",
			mutate:    func(p *types.Params) { p.FunctioningBlockThreshold = math.LegacyNewDecWithPrec(49, 2) },
			expectErr: functioningThresholdOutOfRange,
		},
		{
			name:      "functioning block threshold zero",
			mutate:    func(p *types.Params) { p.FunctioningBlockThreshold = math.LegacyZeroDec() },
			expectErr: functioningThresholdOutOfRange,
		},
		{
			name:      "functioning block threshold negative",
			mutate:    func(p *types.Params) { p.FunctioningBlockThreshold = math.LegacyNewDec(-1) },
			expectErr: functioningThresholdOutOfRange,
		},
		{
			name:   "functioning block threshold at 50%",
			mutate: func(p *types.Params) { p.FunctioningBlockThreshold = types.MinFunctioningBlockThreshold },
		},
		{
			name:   "functioning block threshold at 100%",
			mutate: func(p *types.Params) { p.FunctioningBlockThreshold = math.LegacyOneDec() },
		},
		{
			name:      "functioning block threshold above 100%",
			mutate:    func(p *types.Params) { p.FunctioningBlockThreshold = math.LegacyNewDecWithPrec(101, 2) },
			expectErr: functioningThresholdOutOfRange,
		},
		// ParticipationThreshold
		{
			name:      "participation threshold missing",
			mutate:    func(p *types.Params) { p.ParticipationThreshold = math.LegacyDec{} },
			expectErr: "ParticipationThreshold must be set",
		},
		{
			name:      "participation threshold negative",
			mutate:    func(p *types.Params) { p.ParticipationThreshold = math.LegacyNewDecWithPrec(-1, 2) },
			expectErr: "ParticipationThreshold must be between",
		},
		{
			name:      "participation threshold above 50%",
			mutate:    func(p *types.Params) { p.ParticipationThreshold = math.LegacyNewDecWithPrec(501, 3) },
			expectErr: "ParticipationThreshold must be between",
		},
		{
			name:   "participation threshold at zero",
			mutate: func(p *types.Params) { p.ParticipationThreshold = math.LegacyZeroDec() },
		},
		{
			name:   "participation threshold at 50%",
			mutate: func(p *types.Params) { p.ParticipationThreshold = types.MaxParticipationThreshold },
		},
		// MaxExchangeRateAge
		{
			name:      "max exchange rate age zero",
			mutate:    func(p *types.Params) { p.MaxExchangeRateAge = 0 },
			expectErr: exchangeRateAgeOutOfRange,
		},
		{
			name:      "max exchange rate age negative",
			mutate:    func(p *types.Params) { p.MaxExchangeRateAge = -time.Second },
			expectErr: exchangeRateAgeOutOfRange,
		},
		{
			name:   "max exchange rate age at the domain cap",
			mutate: func(p *types.Params) { p.MaxExchangeRateAge = types.MaxAllowedExchangeRateAge },
		},
		{
			// A window that never elapses does not loosen the staleness gate,
			// it removes it: every rate reads fresh forever.
			name:      "max exchange rate age above the domain cap",
			mutate:    func(p *types.Params) { p.MaxExchangeRateAge = types.MaxAllowedExchangeRateAge + time.Second },
			expectErr: exchangeRateAgeOutOfRange,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			tc.mutate(&p)
			err := p.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.expectErr)
			}
			if tc.check != nil {
				tc.check(t, p)
			}
		})
	}
}

// TestDefaultFeedsClonesDenoms pins that a caller mutating the returned feed
// set cannot reach through into the package-level launch list.
func TestDefaultFeedsClonesDenoms(t *testing.T) {
	feeds := types.DefaultFeeds()
	require.NotEmpty(t, feeds.Denoms)
	feeds.Denoms[0] = "amutated"

	fresh := types.DefaultFeeds()
	require.Equal(t, types.DefaultFeedDenoms, fresh.Denoms)
	require.NotEqual(t, feeds.Denoms[0], fresh.Denoms[0])
}
