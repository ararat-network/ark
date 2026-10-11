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
	const allowanceBits = "grants mandate: allowance: amount must be nonnegative and at most 128 bits"
	for _, tc := range []struct {
		name   string
		mutate func(*types.GrantsMandate)
		err    string
	}{
		{"members only", func(*types.GrantsMandate) {}, ""},
		{"compensation allowance", func(m *types.GrantsMandate) { m.CompensationAllowance = allowance }, ""},
		{"nil allowance", func(m *types.GrantsMandate) { m.CompensationAllowance = math.Int{} }, allowanceBits},
		{"negative allowance", func(m *types.GrantsMandate) { m.CompensationAllowance = math.NewInt(-1) }, allowanceBits},
		{"allowance above 128 bits", func(m *types.GrantsMandate) {
			m.CompensationAllowance = math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 128))
		}, allowanceBits},
		{"window inverted", func(m *types.GrantsMandate) { m.ActivationTime, m.ExpiryTime = 10, 5 }, "grants mandate: activation time must precede expiry time"},
		{"disabled with an allowance", func(m *types.GrantsMandate) {
			*m = types.NewDisabledGrantsMandate(1)
			m.CompensationAllowance = allowance
		}, "grants mandate: a disabled mandate carries no award power"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := active()
			tc.mutate(&m)
			err := m.Validate()
			if tc.err == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.err)
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
		err    string
	}{
		{"appointed", func(*types.RegistrarMandate) {}, ""},
		{"window inverted", func(m *types.RegistrarMandate) { m.ActivationTime, m.ExpiryTime = 10, 5 }, "registrar mandate: activation time must precede expiry time"},
		{"appointed at term zero", func(m *types.RegistrarMandate) { m.Term = 0 }, "registrar mandate: configured term must be positive"},
		{"disabled", func(m *types.RegistrarMandate) { *m = types.NewDisabledRegistrarMandate(1) }, ""},
		{"disabled with a window", func(m *types.RegistrarMandate) {
			*m = types.NewDisabledRegistrarMandate(1)
			m.ExpiryTime = 10
		}, "registrar mandate: disabled envelope must not have an activation or expiry time"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := active()
			tc.mutate(&m)
			err := m.Validate()
			if tc.err == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.err)
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
	const outside = "must sit inside the committee's [10, 20)"
	for _, tc := range []struct {
		name      string
		committee types.GrantsMandate
		registrar types.RegistrarMandate
		err       string
	}{
		{"inside", committee, registrar(10, 20), ""},
		{"strictly inside", committee, registrar(12, 18), ""},
		{"starts before", committee, registrar(9, 20), outside},
		{"ends after", committee, registrar(10, 21), outside},
		{"committee disabled", types.NewDisabledGrantsMandate(1), registrar(10, 20), "a registrar is appointed under a disabled grants mandate"},
		{"registrar disabled under a disabled committee", types.NewDisabledGrantsMandate(1), types.NewDisabledRegistrarMandate(1), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.registrar.WithinCommittee(tc.committee)
			if tc.err == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.err)
		})
	}
}
