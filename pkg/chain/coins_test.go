package chain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
)

// amounts spans what the constructors distinguish: zero, which decides whether
// a coin set is empty, and positive amounts either side of the int64 boundary.
var amounts = []math.Int{
	math.ZeroInt(),
	math.OneInt(),
	math.NewInt(1_000_000),
	chain.NativeBaseAmount(1),
	math.NewIntFromBigInt(math.NewInt(1).BigInt().Lsh(math.NewInt(1).BigInt(), 100)),
}

// TestValidateNoahCoin covers the shape rule and, in the zero case, the thing
// the helper deliberately does not decide: zero is a well-formed NOAH coin, so
// callers that must reject it assert positivity themselves.
func TestValidateNoahCoin(t *testing.T) {
	tests := []struct {
		name    string
		coin    sdk.Coin
		wantErr string
	}{
		{name: "positive", coin: chain.NoahCoin(math.OneInt())},
		{name: "zero", coin: chain.NoahCoin(math.ZeroInt())},
		{
			name:    "unset",
			coin:    sdk.Coin{},
			wantErr: "invalid test coin: invalid denom",
		},
		{
			name:    "unset amount",
			coin:    sdk.Coin{Denom: chain.NoahBaseDenom},
			wantErr: "invalid test coin: amount is nil",
		},
		{
			name:    "negative",
			coin:    sdk.Coin{Denom: chain.NoahBaseDenom, Amount: math.NewInt(-1)},
			wantErr: "invalid test coin: negative coin amount",
		},
		{
			name:    "wrong denom",
			coin:    sdk.NewInt64Coin(chain.USDBaseDenom, 1),
			wantErr: "test coin must be denominated in anoah",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := chain.ValidateNoahCoin("test coin", tt.coin)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

// TestNoahCoinMatchesSDKConstructor is the whole claim the helper makes: it
// produces what sdk.NewCoin produces and differs only in not matching a regular
// expression against a constant on the way.
func TestNoahCoinMatchesSDKConstructor(t *testing.T) {
	for _, amount := range amounts {
		t.Run(amount.String(), func(t *testing.T) {
			require.Equal(
				t,
				sdk.NewCoin(chain.NoahBaseDenom, amount),
				chain.NoahCoin(amount),
			)
		})
	}
}

func TestNoahCoinsMatchesSDKConstructor(t *testing.T) {
	for _, amount := range amounts {
		t.Run(amount.String(), func(t *testing.T) {
			require.Equal(
				t,
				sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, amount)),
				chain.NoahCoins(amount),
			)
		})
	}
}

// TestNoahCoinsDropsZero pins the sanitising the helper kept, separately from
// the equivalence above: a zero amount is an empty set rather than a set
// holding nothing, because bank rejects a zero-amount entry outright.
func TestNoahCoinsDropsZero(t *testing.T) {
	require.Equal(t, sdk.Coins{}, chain.NoahCoins(math.ZeroInt()))
	require.Empty(t, chain.NoahCoins(math.ZeroInt()))
	require.Len(t, chain.NoahCoins(math.OneInt()), 1)
}

func TestNoahDecCoinMatchesSDKConstructor(t *testing.T) {
	decAmounts := []math.LegacyDec{
		math.LegacyZeroDec(),
		math.LegacyOneDec(),
		math.LegacyMustNewDecFromStr("0.000000000000000001"),
		math.LegacyMustNewDecFromStr("123456789.987654321"),
	}
	for _, amount := range decAmounts {
		t.Run(amount.String(), func(t *testing.T) {
			require.Equal(
				t,
				sdk.NewDecCoinFromDec(chain.NoahBaseDenom, amount),
				chain.NoahDecCoin(amount),
			)
		})
	}
}

// TestCoinConstructorsPanicOnUnusableAmounts keeps the guards the helpers did
// retain: the amount is the part a caller can get wrong, so it is still
// checked, and it still fails the way the SDK fails it.
func TestCoinConstructorsPanicOnUnusableAmounts(t *testing.T) {
	unusable := []struct {
		name   string
		amount math.Int
	}{
		{name: "unset", amount: math.Int{}},
		{name: "negative", amount: math.NewInt(-1)},
	}

	for _, tt := range unusable {
		t.Run(tt.name, func(t *testing.T) {
			require.Panics(t, func() { sdk.NewCoin(chain.NoahBaseDenom, tt.amount) })
			require.Panics(t, func() { chain.NoahCoin(tt.amount) })
			require.Panics(t, func() { chain.NoahCoins(tt.amount) })
		})
	}

	t.Run("negative decimal", func(t *testing.T) {
		negative := math.LegacyOneDec().Neg()
		require.Panics(t, func() { sdk.NewDecCoinFromDec(chain.NoahBaseDenom, negative) })
		require.Panics(t, func() { chain.NoahDecCoin(negative) })
	})
	t.Run("unset decimal", func(t *testing.T) {
		require.Panics(t, func() { chain.NoahDecCoin(math.LegacyDec{}) })
	})
}
