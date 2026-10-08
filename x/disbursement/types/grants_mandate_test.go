package types_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/disbursement/types"
)

func TestGrantsMandateValidation(t *testing.T) {
	allowance := math.NewInt(1_000)
	active := func() types.GrantsMandate {
		m := types.NewDisabledGrantsMandate(1)
		m.Committee = sdk.AccAddress(make([]byte, 20)).String()
		m.ActivationHeight, m.ExpiryHeight = 5, 10
		return m
	}
	for _, tc := range []struct {
		name   string
		mutate func(*types.GrantsMandate)
		valid  bool
	}{
		{"members only", func(*types.GrantsMandate) {}, true},
		{"compensation allowance", func(m *types.GrantsMandate) { m.CompensationAllowance = allowance }, true},
		{"nil allowance", func(m *types.GrantsMandate) { m.CompensationAllowance = math.Int{} }, false},
		{"negative allowance", func(m *types.GrantsMandate) { m.CompensationAllowance = math.NewInt(-1) }, false},
		{"allowance above 128 bits", func(m *types.GrantsMandate) {
			m.CompensationAllowance = math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 128))
		}, false},
		{"disabled with an allowance", func(m *types.GrantsMandate) {
			*m = types.NewDisabledGrantsMandate(1)
			m.CompensationAllowance = allowance
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := active()
			tc.mutate(&m)
			err := m.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
