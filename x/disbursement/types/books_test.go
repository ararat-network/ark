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
