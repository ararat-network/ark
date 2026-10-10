package types_test

import (
	"bytes"
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
		m.ActivationTime, m.ExpiryTime = 5, 10
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

func TestRegistrarMandateValidation(t *testing.T) {
	active := func() types.RegistrarMandate {
		m := types.NewDisabledRegistrarMandate(1)
		m.Committee = sdk.AccAddress(make([]byte, 20)).String()
		m.ActivationTime, m.ExpiryTime = 5, 10
		return m
	}
	for _, tc := range []struct {
		name   string
		mutate func(*types.RegistrarMandate)
		valid  bool
	}{
		{"appointed", func(*types.RegistrarMandate) {}, true},
		{"window inverted", func(m *types.RegistrarMandate) { m.ActivationTime, m.ExpiryTime = 10, 5 }, false},
		{"appointed at term zero", func(m *types.RegistrarMandate) { m.Term = 0 }, false},
		{"disabled", func(m *types.RegistrarMandate) { *m = types.NewDisabledRegistrarMandate(1) }, true},
		{"disabled with a window", func(m *types.RegistrarMandate) {
			*m = types.NewDisabledRegistrarMandate(1)
			m.ExpiryTime = 10
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

func TestRegistrarWithinCommittee(t *testing.T) {
	committee := types.NewDisabledGrantsMandate(1)
	committee.Committee, committee.ActivationTime, committee.ExpiryTime = sdk.AccAddress(make([]byte, 20)).String(), 10, 20
	registrar := func(activation, expiry uint64) types.RegistrarMandate {
		m := types.NewDisabledRegistrarMandate(1)
		m.Committee, m.ActivationTime, m.ExpiryTime = sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(), activation, expiry
		return m
	}
	for _, tc := range []struct {
		name      string
		committee types.GrantsMandate
		registrar types.RegistrarMandate
		valid     bool
	}{
		{"inside", committee, registrar(10, 20), true},
		{"strictly inside", committee, registrar(12, 18), true},
		{"starts before", committee, registrar(9, 20), false},
		{"ends after", committee, registrar(10, 21), false},
		{"committee disabled", types.NewDisabledGrantsMandate(1), registrar(10, 20), false},
		{"registrar disabled under a disabled committee", types.NewDisabledGrantsMandate(1), types.NewDisabledRegistrarMandate(1), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.registrar.WithinCommittee(tc.committee)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
