package simulation

import (
	"context"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/disbursement/keeper"
	"github.com/ararat-network/ark/x/disbursement/types"
)

// payeeChange is a grant with remaining entitlement and the controller who may redirect it.
type payeeChange struct {
	grant      types.Grant
	controller string
}

// MsgSetPayeeFactory redirects a grant with remaining entitlement to another run account, signed by
// its controller: the member itself, or a contributor record's controller.
func MsgSetPayeeFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgSetPayee] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgSetPayee) {
		var candidates []payeeChange
		if err := k.Grants.Walk(ctx, nil, func(_ uint64, g types.Grant) (bool, error) {
			if g.Remaining.IsZero() {
				return false, nil
			}
			controller := g.Beneficiary
			if g.Kind != types.GrantKind_GRANT_KIND_MEMBER {
				address, err := sdk.AccAddressFromBech32(g.Beneficiary)
				if err != nil {
					return true, err
				}
				record, err := k.Beneficiaries.Get(ctx, address)
				if err != nil {
					return true, err
				}
				controller = record.Controller
			}
			if testData.HasAccount(controller) {
				candidates = append(candidates, payeeChange{grant: g, controller: controller})
			}
			return false, nil
		}); err != nil {
			reporter.Skip("iterating grants: " + err.Error())
			return nil, nil
		}
		if len(candidates) == 0 {
			reporter.Skip("no grant with remaining entitlement under a run-owned controller")
			return nil, nil
		}

		change := candidates[testData.Rand().Intn(len(candidates))]
		signer := testData.GetAccount(reporter, change.controller)
		payee := testData.AnyAccount(reporter)
		if reporter.IsSkipped() {
			return nil, nil
		}
		if payee.AddressBech32 == change.grant.Payee {
			reporter.Skip("drawn payee is the current one")
			return nil, nil
		}
		return []simsx.SimAccount{signer}, &types.MsgSetPayee{
			Sender:  change.controller,
			GrantId: change.grant.Id,
			Payee:   payee.AddressBech32,
		}
	}
}

// MsgSetControllerFactory hands a contributor record to another run account, signed by its current
// controller.
func MsgSetControllerFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgSetController] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgSetController) {
		var candidates []types.Beneficiary
		if err := k.Beneficiaries.Walk(ctx, nil, func(_ sdk.AccAddress, record types.Beneficiary) (bool, error) {
			if testData.HasAccount(record.Controller) {
				candidates = append(candidates, record)
			}
			return false, nil
		}); err != nil {
			reporter.Skip("iterating beneficiaries: " + err.Error())
			return nil, nil
		}
		if len(candidates) == 0 {
			reporter.Skip("no contributor record under a run-owned controller")
			return nil, nil
		}

		record := candidates[testData.Rand().Intn(len(candidates))]
		signer := testData.GetAccount(reporter, record.Controller)
		controller := testData.AnyAccount(reporter)
		if reporter.IsSkipped() {
			return nil, nil
		}
		if controller.AddressBech32 == record.Controller {
			reporter.Skip("drawn controller is the current one")
			return nil, nil
		}
		return []simsx.SimAccount{signer}, &types.MsgSetController{
			Sender:      record.Controller,
			Beneficiary: record.Address,
			Controller:  controller.AddressBech32,
		}
	}
}
