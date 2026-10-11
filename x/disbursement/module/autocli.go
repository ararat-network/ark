package disbursement

import (
	"fmt"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"github.com/cosmos/cosmos-sdk/version"

	disbursementv1 "github.com/ararat-network/ark/api/ark/disbursement/v1"
)

// AutoCLIOptions returns the disbursement module's AutoCLI configuration.
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: disbursementv1.Query_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "Params",
					Use:       "params",
					Short:     "Query the disbursement parameters, the ownership cap among them",
					Example:   fmt.Sprintf("%s query disbursement params", version.AppName),
				},
				{
					RpcMethod: "GrantsMandate",
					Use:       "grants-mandate",
					Short:     "Query the grants committee appointment, whether it is active, and the term's compensation usage",
					Example:   fmt.Sprintf("%s query disbursement grants-mandate", version.AppName),
				},
				{
					RpcMethod: "RegistrarMandate",
					Use:       "registrar-mandate",
					Short:     "Query the registrar appointment and whether it is active",
					Example:   fmt.Sprintf("%s query disbursement registrar-mandate", version.AppName),
				},
				{
					RpcMethod: "Balance",
					Use:       "balance [denom]",
					Short:     "Query each account's custody, reservations, and uncommitted amount, and the member tranche; NOAH and every registered asset held by default",
					Example:   fmt.Sprintf("%s query disbursement balance anoah", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom", Optional: true},
					},
				},
				{
					RpcMethod: "Grant",
					Use:       "grant [id]",
					Short:     "Query a grant and its original terms",
					Example:   fmt.Sprintf("%s query disbursement grant 1", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "id"},
					},
				},
				{
					RpcMethod: "Grants",
					Use:       "grants",
					Short:     "Query grants, optionally by beneficiary, committee term, registrar term, or kind",
					Example:   fmt.Sprintf("%s query disbursement grants --beneficiary ark1...", version.AppName),
				},
				{
					RpcMethod: "GrantJournal",
					Use:       "grant-journal",
					Short:     "Query the permanent grant journal, optionally by grant ID",
					Example:   fmt.Sprintf("%s query disbursement grant-journal --grant-id 1", version.AppName),
				},
				{
					RpcMethod: "Member",
					Use:       "member [address]",
					Short:     "Query a permanent member registration",
					Example:   fmt.Sprintf("%s query disbursement member ark1...", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "address"},
					},
				},
				{
					RpcMethod: "Members",
					Use:       "members",
					Short:     "Query member registrations in address order",
					Example:   fmt.Sprintf("%s query disbursement members", version.AppName),
				},
				{
					RpcMethod: "Beneficiary",
					Use:       "beneficiary [address]",
					Short:     "Query contributor control, committed ownership, the recorded seat, and the remaining ownership under the cap",
					Example:   fmt.Sprintf("%s query disbursement beneficiary ark1...", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "address"},
					},
				},
				{
					RpcMethod: "BeneficiaryJournal",
					Use:       "beneficiary-journal [address]",
					Short:     "Query a beneficiary's controller changes",
					Example:   fmt.Sprintf("%s query disbursement beneficiary-journal ark1...", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "address"},
					},
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: disbursementv1.Msg_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod:   "UpdateParams",
					Use:         "update-params-proposal [params]",
					Short:       "Submit a governance proposal to replace the disbursement parameters",
					Example:     fmt.Sprintf(`%s tx disbursement update-params-proposal '{"member_amount":"20000000000000000000000",...}'`, version.AppName),
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "params"},
					},
				},
				{
					RpcMethod:   "SetGrantsMandate",
					Use:         "set-grants-mandate-proposal",
					Short:       "Submit a governance proposal to appoint, replace, or disable the grants committee",
					GovProposal: true,
				},
				{
					RpcMethod:   "OpenTranche",
					Use:         "open-tranche-proposal",
					Short:       "Submit a governance proposal to open member-pool NOAH to registration",
					GovProposal: true,
				},
				{
					RpcMethod:   "ReturnUnallocated",
					Use:         "return-unallocated-proposal",
					Short:       "Submit a governance proposal to return uncommitted contributor custody to the community pool",
					GovProposal: true,
				},
				{
					RpcMethod:   "GrantOwnership",
					Use:         "grant-ownership-proposal",
					Short:       "Submit a governance proposal to award a NOAH ownership grant",
					GovProposal: true,
				},
				{
					RpcMethod:   "Compensate",
					Use:         "compensate-proposal",
					Short:       "Submit a governance proposal to award a fully funded compensation grant",
					GovProposal: true,
				},
				{
					RpcMethod:   "CancelGrants",
					Use:         "cancel-grants-proposal",
					Short:       "Submit a governance proposal to cancel grants while retaining earned debt",
					GovProposal: true,
				},
				{
					RpcMethod:   "ReinstateMembers",
					Use:         "reinstate-members-proposal",
					Short:       "Submit a governance proposal to resume suspended member payments",
					GovProposal: true,
				},
				{
					RpcMethod: "CommitteeRegistrar",
					Use:       "committee-registrar",
					Short:     "Appoint, replace, or disable the registrar as the grants committee",
				},
				{
					RpcMethod: "RegisterMembers",
					Use:       "register-members",
					Short:     "Register members as the registrar; the block pays each first period after the hold",
				},
				{
					RpcMethod: "CommitteeSuspend",
					Use:       "committee-suspend",
					Short:     "Suspend member payments as the grants committee",
				},
				{
					RpcMethod: "CommitteeReinstate",
					Use:       "committee-reinstate",
					Short:     "Resume suspended member payments as the grants committee",
				},
				{
					RpcMethod: "CommitteeCompensate",
					Use:       "committee-compensate",
					Short:     "Award compensation within the term's allowance as the grants committee",
				},
				{
					RpcMethod: "SetPayee",
					Use:       "set-payee",
					Short:     "Change a grant destination without changing beneficiary identity",
				},
				{
					RpcMethod: "SetController",
					Use:       "set-controller",
					Short:     "Rotate a contributor's controller",
				},
			},
		},
	}
}
