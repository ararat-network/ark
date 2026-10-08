package types_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/types"
)

func TestGrantEntryValidation(t *testing.T) {
	actor := sdk.AccAddress(make([]byte, 20)).String()
	a, b := sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(), sdk.AccAddress(bytes.Repeat([]byte{2}, 20)).String()
	noah, zero := chain.NoahCoin(math.NewInt(5)), chain.NoahCoin(math.ZeroInt())
	base := func(action types.GrantAction) types.GrantEntry {
		e := types.GrantEntry{Id: 1, GrantId: 7, Action: action, Actor: actor, Height: 3, Time: 9, Amount: noah, Offer: math.ZeroInt()}
		switch action {
		case types.GrantAction_GRANT_ACTION_CONVERTED:
			e.Amount, e.Offer = sdk.NewInt64Coin("ausd", 5), math.NewInt(5)
		case types.GrantAction_GRANT_ACTION_PAID:
			e.Subject = a
		case types.GrantAction_GRANT_ACTION_PAYEE_CHANGED:
			e.Amount, e.Subject = zero, b
		case types.GrantAction_GRANT_ACTION_SUSPENDED, types.GrantAction_GRANT_ACTION_REINSTATED:
			e.Amount = zero
		}
		return e
	}
	with := func(action types.GrantAction, mutate func(*types.GrantEntry)) types.GrantEntry {
		e := base(action)
		mutate(&e)
		return e
	}
	for _, action := range []types.GrantAction{
		types.GrantAction_GRANT_ACTION_CREATED, types.GrantAction_GRANT_ACTION_CONVERTED, types.GrantAction_GRANT_ACTION_PAID,
		types.GrantAction_GRANT_ACTION_CANCELLED, types.GrantAction_GRANT_ACTION_PAYEE_CHANGED, types.GrantAction_GRANT_ACTION_SUSPENDED,
		types.GrantAction_GRANT_ACTION_REINSTATED,
	} {
		t.Run("valid "+action.String(), func(t *testing.T) { require.NoError(t, base(action).Validate()) })
	}
	for _, tc := range []struct {
		name  string
		entry types.GrantEntry
	}{
		{"unspecified action", base(types.GrantAction_GRANT_ACTION_UNSPECIFIED)},
		{"zero ID", with(types.GrantAction_GRANT_ACTION_CREATED, func(e *types.GrantEntry) { e.Id = 0 })},
		{"zero height", with(types.GrantAction_GRANT_ACTION_CREATED, func(e *types.GrantEntry) { e.Height = 0 })},
		{"bad actor", with(types.GrantAction_GRANT_ACTION_CREATED, func(e *types.GrantEntry) { e.Actor = "nobody" })},
		{"created moves nothing", with(types.GrantAction_GRANT_ACTION_CREATED, func(e *types.GrantEntry) { e.Amount = zero })},
		{"created with a subject", with(types.GrantAction_GRANT_ACTION_CREATED, func(e *types.GrantEntry) { e.Subject = a })},
		{"created without a grant", with(types.GrantAction_GRANT_ACTION_CREATED, func(e *types.GrantEntry) { e.GrantId = 0 })},
		{"converted without an offer", with(types.GrantAction_GRANT_ACTION_CONVERTED, func(e *types.GrantEntry) { e.Offer = math.ZeroInt() })},
		{"converted with a negative offer", with(types.GrantAction_GRANT_ACTION_CONVERTED, func(e *types.GrantEntry) { e.Offer = math.NewInt(-1) })},
		{"unset offer", with(types.GrantAction_GRANT_ACTION_CREATED, func(e *types.GrantEntry) { e.Offer = math.Int{} })},
		{"paid without a payee", with(types.GrantAction_GRANT_ACTION_PAID, func(e *types.GrantEntry) { e.Subject = "" })},
		{"paid moves nothing", with(types.GrantAction_GRANT_ACTION_PAID, func(e *types.GrantEntry) { e.Amount = zero })},
		{"payee moves principal", with(types.GrantAction_GRANT_ACTION_PAYEE_CHANGED, func(e *types.GrantEntry) { e.Amount = noah })},
		{"cancelled with an offer", with(types.GrantAction_GRANT_ACTION_CANCELLED, func(e *types.GrantEntry) { e.Offer = noah.Amount })},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Error(t, tc.entry.Validate()) })
	}
}

func TestBeneficiaryEntryValidation(t *testing.T) {
	a, b := sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(), sdk.AccAddress(bytes.Repeat([]byte{2}, 20)).String()
	base := types.BeneficiaryEntry{Id: 1, Beneficiary: a, Actor: a, Height: 3, Time: 9, Controller: b}
	require.NoError(t, base.Validate())
	for _, tc := range []struct {
		name   string
		mutate func(*types.BeneficiaryEntry)
	}{
		{"zero ID", func(r *types.BeneficiaryEntry) { r.Id = 0 }},
		{"zero height", func(r *types.BeneficiaryEntry) { r.Height = 0 }},
		{"bad actor", func(r *types.BeneficiaryEntry) { r.Actor = "nobody" }},
		{"bad controller", func(r *types.BeneficiaryEntry) { r.Controller = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.mutate(&r)
			require.Error(t, r.Validate())
		})
	}
}
