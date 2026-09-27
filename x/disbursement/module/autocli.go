package disbursement

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	disbursementv1 "github.com/ararat-network/ark/api/ark/disbursement/v1"
)

// AutoCLIOptions exposes typed disbursement messages and public queries.
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{Service: disbursementv1.Query_ServiceDesc.ServiceName, RpcCommandOptions: []*autocliv1.RpcCommandOptions{
			{RpcMethod: "Params", Use: "params", Short: "Show operational params and immutable ownership policy"},
			{RpcMethod: "GrantsMandate", Use: "grants-mandate", Short: "Show the committee appointment, whether it is active, and the term's awards"},
			{RpcMethod: "Grant", Use: "grant [id]", Short: "Show a grant and its original terms", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}}},
			{RpcMethod: "Grants", Use: "grants", Short: "List grants, optionally by beneficiary"},
			{RpcMethod: "Member", Use: "member [address]", Short: "Look up a permanent member registration", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}}},
			{RpcMethod: "Beneficiary", Use: "beneficiary [address]", Short: "Show contributor control, cumulative ownership, and founding status", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}}},
			{RpcMethod: "Releasable", Use: "releasable [id]", Short: "Show earned principal, payable funds, and live ownership limits", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}}},
			{RpcMethod: "Balance", Use: "balance [denom]", Short: "Show custody, reservations, payments, and unallocated funds", PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "denom"}}},
			{RpcMethod: "Totals", Use: "totals", Short: "List per-denomination accounting totals"},
			{RpcMethod: "Journal", Use: "journal", Short: "List permanent operations, optionally by grant ID"},
			{RpcMethod: "Issuance", Use: "issuance", Short: "Show usage of the current rolling member window"},
			{RpcMethod: "ConversionOrders", Use: "conversion-orders", Short: "List open stablecoin conversion orders"},
		}}, Tx: &autocliv1.ServiceCommandDescriptor{Service: disbursementv1.Msg_ServiceDesc.ServiceName, RpcCommandOptions: []*autocliv1.RpcCommandOptions{
			{RpcMethod: "UpdateParams", Use: "update-params-proposal", Short: "Propose operational disbursement parameters", GovProposal: true},
			{RpcMethod: "SetGrantsMandate", Use: "set-grants-mandate-proposal", Short: "Propose appointing, replacing, or disabling the grants committee", GovProposal: true},
			{RpcMethod: "CreateGrant", Use: "create-grant-proposal", Short: "Propose a fully funded ownership or compensation award", GovProposal: true},
			{RpcMethod: "CommitteeRegister", Use: "committee-register", Short: "Register members and pay their first period as the committee", GovProposal: false},
			{RpcMethod: "Release", Use: "release", Short: "Pay available entitlement to each recorded payee", GovProposal: false},
			{RpcMethod: "CancelGrants", Use: "cancel-grants-proposal", Short: "Propose cancellation while retaining earned debt", GovProposal: true},
			{RpcMethod: "CommitteeSuspend", Use: "committee-suspend", Short: "Suspend member payments as the committee", GovProposal: false},
			{RpcMethod: "CommitteeReinstate", Use: "committee-reinstate", Short: "Resume suspended member payments as the committee", GovProposal: false},
			{RpcMethod: "ReinstateMembers", Use: "reinstate-members-proposal", Short: "Propose resuming suspended member payments", GovProposal: true},
			{RpcMethod: "VoidSuspensions", Use: "void-suspensions-proposal", Short: "Propose invalidating every suspension of a replaced committee term", GovProposal: true},
			{RpcMethod: "SetPayee", Use: "set-payee", Short: "Change a grant destination without changing beneficiary identity", GovProposal: false},
			{RpcMethod: "SetController", Use: "set-controller", Short: "Rotate a contributor's controller", GovProposal: false},
			{RpcMethod: "ReturnUnallocated", Use: "return-unallocated-proposal", Short: "Propose returning idle custody or contributor-pool NOAH to the community pool", GovProposal: true},
			{RpcMethod: "OpenTranche", Use: "open-tranche-proposal", Short: "Propose opening pool NOAH to new grants", GovProposal: true},
			{RpcMethod: "AuthoriseConversion", Use: "authorise-conversion-proposal", Short: "Propose converting open contributor NOAH into a stablecoin", GovProposal: true},
			{RpcMethod: "CancelConversion", Use: "cancel-conversion-proposal", Short: "Propose returning a conversion order to the open contributor tranche", GovProposal: true},
			{RpcMethod: "Convert", Use: "convert", Short: "Execute part of an authorised conversion order", GovProposal: false},
			{RpcMethod: "CommitteeCompensate", Use: "committee-compensate", Short: "Award compensation within the term's allowance as the committee", GovProposal: false},
			{RpcMethod: "CancelTermGrants", Use: "cancel-term-grants-proposal", Short: "Propose cancelling every unfinished committee award of one term", GovProposal: true},
		}},
	}
}
