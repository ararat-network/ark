package keeper

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"noah/x/market/types"
)

func TestValidateInputs(t *testing.T) {
	tests := []struct {
		name      string
		offerCoin sdk.Coin
		askDenom  string
		expectErr error
		errMsg    string
	}{
		{
			name: "valid input",
			offerCoin: sdk.Coin{
				Denom:  "ukrw",
				Amount: math.NewInt(1),
			},
			askDenom: "uusd",
		},
		{
			name: "recursive swap",
			offerCoin: sdk.Coin{
				Denom:  "ukrw",
				Amount: math.NewInt(1),
			},
			askDenom:  "ukrw",
			expectErr: types.ErrRecursiveSwap,
			errMsg:    "recursive swap",
		},
		{
			name: "zero amount",
			offerCoin: sdk.Coin{
				Denom:  "uusd",
				Amount: math.NewInt(0),
			},
			askDenom:  "ukrw",
			expectErr: errortypes.ErrInvalidCoins,
			errMsg:    "invalid coins",
		},
		{
			name: "negative amount",
			offerCoin: sdk.Coin{
				Denom:  "uusd",
				Amount: math.NewInt(-1),
			},
			askDenom:  "ukrw",
			expectErr: errortypes.ErrInvalidCoins,
			errMsg:    "invalid coins",
		},
		{
			name: "huge amount",
			offerCoin: sdk.Coin{
				Denom:  "uusd",
				Amount: math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 101)),
			},
			askDenom:  "ukrw",
			expectErr: errortypes.ErrInvalidCoins,
			errMsg:    "invalid coins",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateInputs(tc.offerCoin, tc.askDenom)
			if tc.expectErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.expectErr)
				require.ErrorContains(t, err, tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBuildSwapOutcome(t *testing.T) {
	tests := []struct {
		name              string
		swapDecCoin       sdk.DecCoin
		spread            math.LegacyDec
		expectedSwapDec   sdk.DecCoin
		expectedSwapCoin  sdk.Coin
		expectedSwapFee   sdk.DecCoin
		expectErr         error
		expectedErrorText string
	}{
		{
			name:             "zero spread retains truncation remainder as fee",
			swapDecCoin:      sdk.NewDecCoinFromDec("ukrw", math.LegacyMustNewDecFromStr("100.5")),
			spread:           math.LegacyZeroDec(),
			expectedSwapDec:  sdk.NewDecCoinFromDec("ukrw", math.LegacyMustNewDecFromStr("100.5")),
			expectedSwapCoin: sdk.NewCoin("ukrw", math.NewInt(100)),
			expectedSwapFee:  sdk.NewDecCoinFromDec("ukrw", math.LegacyMustNewDecFromStr("0.5")),
		},
		{
			name:             "positive spread deducts explicit fee",
			swapDecCoin:      sdk.NewDecCoinFromDec("uark", math.LegacyNewDec(100)),
			spread:           math.LegacyMustNewDecFromStr("0.2"),
			expectedSwapDec:  sdk.NewDecCoinFromDec("uark", math.LegacyNewDec(80)),
			expectedSwapCoin: sdk.NewCoin("uark", math.NewInt(80)),
			expectedSwapFee:  sdk.NewDecCoinFromDec("uark", math.LegacyNewDec(20)),
		},
		{
			name:             "truncation remainder is folded into fee",
			swapDecCoin:      sdk.NewDecCoinFromDec("ukrw", math.LegacyMustNewDecFromStr("100.55")),
			spread:           math.LegacyMustNewDecFromStr("0.1"),
			expectedSwapDec:  sdk.NewDecCoinFromDec("ukrw", math.LegacyMustNewDecFromStr("90.495")),
			expectedSwapCoin: sdk.NewCoin("ukrw", math.NewInt(90)),
			expectedSwapFee:  sdk.NewDecCoinFromDec("ukrw", math.LegacyMustNewDecFromStr("10.55")),
		},
		{
			name:              "zero swap coin returns error",
			swapDecCoin:       sdk.NewDecCoinFromDec("ukrw", math.LegacyMustNewDecFromStr("0.5")),
			spread:            math.LegacyZeroDec(),
			expectErr:         types.ErrZeroSwapCoin,
			expectedErrorText: "zero swap coin",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outcome, err := buildSwapOutcome(tc.swapDecCoin, tc.spread)
			if tc.expectErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.expectErr)
				require.ErrorContains(t, err, tc.expectedErrorText)
			} else {
				require.NoError(t, err)
				require.NotNil(t, outcome)
				require.Equal(t, tc.expectedSwapCoin, outcome.swapCoin)
				require.Equal(t, tc.expectedSwapFee.Denom, outcome.swapFee.Denom)
				require.True(t, tc.expectedSwapFee.Amount.Equal(outcome.swapFee.Amount))
				require.Equal(t, tc.expectedSwapDec.Denom, outcome.swapDecCoin.Denom)
				require.True(t, tc.expectedSwapDec.Amount.Equal(outcome.swapDecCoin.Amount))
			}
		})
	}
}
