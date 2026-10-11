package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/types"
)

func TestAccountBookUncommitted(t *testing.T) {
	book := types.EmptyBook(types.ContributorsName)
	require.NoError(t, book.Reserve(sdk.NewInt64Coin("ausd", 30)))
	for _, tc := range []struct {
		name    string
		balance sdk.Coin
		want    int64
		valid   bool
	}{
		{"custody above its reservations", sdk.NewInt64Coin("ausd", 50), 20, true},
		{"custody exactly reserved", sdk.NewInt64Coin("ausd", 30), 0, true},
		{"custody below its reservations", sdk.NewInt64Coin("ausd", 29), 0, false},
		{"a denomination with nothing reserved", chain.NoahCoin(math.NewInt(7)), 7, true},
		{"no custody and nothing reserved", chain.NoahCoin(math.ZeroInt()), 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uncommitted, err := book.Uncommitted(tc.balance)
			if !tc.valid {
				require.ErrorContains(t, err, "custody is below its commitments")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, uncommitted.Int64())
		})
	}
}

func TestAccountBookValidate(t *testing.T) {
	book := func(account string, reserved ...sdk.Coin) types.AccountBook {
		b := types.EmptyBook(account)
		b.Reserved = reserved
		return b
	}
	for _, tc := range []struct {
		name string
		book types.AccountBook
		err  string
	}{
		{"empty contributor book", book(types.ContributorsName), ""},
		{"member book holding NOAH", book(types.MembersName, chain.NoahCoin(math.OneInt())), ""},
		{"contributor book holding a stablecoin", book(types.ContributorsName, sdk.NewInt64Coin("ausd", 1)), ""},
		{"unknown account", book("treasury"), "is not a disbursement account"},
		{"member book holding a stablecoin", book(types.MembersName, sdk.NewInt64Coin("ausd", 1)), "the member book holds NOAH only"},
		{"reservation of nothing", book(types.ContributorsName, sdk.Coin{Denom: "ausd", Amount: math.ZeroInt()}), "reserved"},
		{"reservation past 128 bits", book(types.ContributorsName, sdk.Coin{Denom: "ausd", Amount: math.NewIntWithDecimal(1, 39)}), "at most 128 bits"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.book.Validate()
			if tc.err == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.err)
		})
	}
}

func TestAccountBookReserveAndRelease(t *testing.T) {
	book := types.EmptyBook(types.ContributorsName)
	require.NoError(t, book.Reserve(sdk.NewInt64Coin("ausd", 30)))
	require.ErrorContains(t, book.Release(sdk.NewInt64Coin("ausd", 31)), "reservations are below")
	require.ErrorContains(t, book.Release(chain.NoahCoin(math.OneInt())), "reservations are below")
	require.NoError(t, book.Release(sdk.NewInt64Coin("ausd", 30)))
	require.Empty(t, book.Reserved, "a denomination released whole leaves the book")
	require.ErrorContains(t, book.Reserve(sdk.Coin{Denom: "ausd", Amount: math.NewIntWithDecimal(1, 39)}), "at most 128 bits")
}
