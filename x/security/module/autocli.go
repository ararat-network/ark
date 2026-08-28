package security

import (
	"fmt"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"github.com/cosmos/cosmos-sdk/version"

	securityv1 "github.com/ararat-network/ark/api/ark/security/v1"
)

// AutoCLIOptions returns the security module's AutoCLI configuration.
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: securityv1.Query_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "SecurityMandate",
					Use:       "security-mandate",
					Short:     "Query the governed security committee appointment",
					Example:   fmt.Sprintf("%s query security security-mandate", version.AppName),
				},
				{
					RpcMethod: "CommitteePlan",
					Use:       "committee-plan",
					Short:     "Query the committee's recorded upgrade plan and whether it is still pending",
					Example:   fmt.Sprintf("%s query security committee-plan", version.AppName),
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: securityv1.Msg_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod:   "SetSecurityMandate",
					Use:         "set-security-mandate-proposal",
					Short:       "Submit a governance proposal to appoint, replace, or disable the security committee",
					GovProposal: true,
				},
				{
					RpcMethod: "CommitteePlanUpgrade",
					Use:       "committee-plan-upgrade [name] [height]",
					Short:     "Schedule an emergency upgrade plan as the security committee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "name"},
						{ProtoField: "height"},
					},
				},
				{
					RpcMethod: "CommitteeCancelUpgrade",
					Use:       "committee-cancel-upgrade",
					Short:     "Cancel the pending upgrade plan as the security committee",
				},
				{
					RpcMethod: "CommitteeRecoverClient",
					Use:       "committee-recover-client [subject-client-id] [substitute-client-id]",
					Short:     "Substitute an expired or frozen IBC client as the security committee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "subject_client_id"},
						{ProtoField: "substitute_client_id"},
					},
				},
			},
		},
	}
}
