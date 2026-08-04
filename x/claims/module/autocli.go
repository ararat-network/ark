package claims

import (
	"fmt"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"github.com/cosmos/cosmos-sdk/version"

	claimsv1 "ark/api/ark/claims/v1"
)

// AutoCLIOptions returns the claims module's AutoCLI configuration.
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: claimsv1.Query_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "Params",
					Use:       "params",
					Short:     "Query the Claims parameters",
					Example:   fmt.Sprintf("%s query claims params", version.AppName),
				},
				{
					RpcMethod: "ClaimsMandate",
					Use:       "mandate",
					Short:     "Query the Claims mandate and its term allowance",
					Example:   fmt.Sprintf("%s query claims mandate", version.AppName),
				},
				{
					RpcMethod: "InsuranceBalance",
					Use:       "insurance-balance",
					Short:     "Query the Insurance balance and the amount reserved by pending claims",
					Example:   fmt.Sprintf("%s query claims insurance-balance", version.AppName),
				},
				{
					RpcMethod: "Claim",
					Use:       "claim [claim-id]",
					Short:     "Query one claim",
					Example:   fmt.Sprintf("%s query claims claim 1", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "claim_id"},
					},
				},
				{
					RpcMethod: "Claims",
					Use:       "list",
					Short:     "Query the paginated claim audit record",
					Example:   fmt.Sprintf("%s query claims list", version.AppName),
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: claimsv1.Msg_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod:   "UpdateParams",
					Use:         "update-params-proposal [params]",
					Short:       "Submit a governance proposal to update Claims parameters",
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "params"},
					},
				},
				{
					RpcMethod:   "SetClaimsMandate",
					Use:         "set-mandate-proposal",
					Short:       "Submit a governance proposal to set the Claims committee mandate",
					GovProposal: true,
				},
				{
					RpcMethod:   "SubmitClaim",
					Use:         "submit-claim-proposal",
					Short:       "Submit a governance proposal to record a claim outside the committee allowance",
					GovProposal: true,
				},
				{
					RpcMethod: "CommitteeSubmitClaim",
					Use:       "committee-submit-claim",
					Short:     "Submit a claim as the configured Claims committee",
				},
				{
					RpcMethod:   "CancelClaim",
					Use:         "cancel-claim-proposal [claim-id]",
					Short:       "Submit a governance proposal to cancel any pending claim",
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "claim_id"},
					},
				},
				{
					RpcMethod: "CommitteeCancelClaim",
					Use:       "committee-cancel-claim [claim-id]",
					Short:     "Cancel a pending non-governance claim as the Claims committee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "claim_id"},
					},
				},
			},
		},
	}
}
