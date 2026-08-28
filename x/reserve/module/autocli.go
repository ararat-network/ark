package reserve

import (
	"fmt"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"github.com/cosmos/cosmos-sdk/version"

	reservev1 "github.com/ararat-network/ark/api/ark/reserve/v1"
)

// AutoCLIOptions returns the reserve module's AutoCLI configuration.
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: reservev1.Query_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "Mandate",
					Use:       "mandate",
					Short:     "Query the Reserve mandate and its term deployment allowance",
					Example:   fmt.Sprintf("%s query reserve mandate", version.AppName),
				},
				{
					RpcMethod: "Balance",
					Use:       "balance",
					Short:     "Query the Reserve balance and its outstanding deployment",
					Example:   fmt.Sprintf("%s query reserve balance", version.AppName),
				},
				{
					RpcMethod: "RecognitionPolicy",
					Use:       "recognition-policy",
					Short:     "Query the asset eligibility set",
					Example:   fmt.Sprintf("%s query reserve recognition-policy", version.AppName),
				},
				{
					RpcMethod: "RecognisedCapital",
					Use:       "recognised-capital",
					Short:     "Query recognised capital and its per-asset decomposition",
					Example:   fmt.Sprintf("%s query reserve recognised-capital", version.AppName),
				},
				{
					RpcMethod: "Position",
					Use:       "position [position-id]",
					Short:     "Query one position",
					Example:   fmt.Sprintf("%s query reserve position 1", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "position_id"},
					},
				},
				{
					RpcMethod: "OpenPositions",
					Use:       "open-positions",
					Short:     "Query the paginated set of positions still open",
					Example:   fmt.Sprintf("%s query reserve open-positions", version.AppName),
				},
				{
					RpcMethod: "ClosedPositions",
					Use:       "closed-positions",
					Short:     "Query the paginated record of closed positions",
					Example:   fmt.Sprintf("%s query reserve closed-positions", version.AppName),
				},
				{
					RpcMethod: "Ledger",
					Use:       "ledger",
					Short:     "Query the paginated accounting ledger",
					Example:   fmt.Sprintf("%s query reserve ledger --position-id 1", version.AppName),
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: reservev1.Msg_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod:   "SetReserveMandate",
					Use:         "set-mandate-proposal",
					Short:       "Submit a governance proposal to set the Reserve committee mandate",
					GovProposal: true,
				},
				{
					RpcMethod:   "SetRecognitionPolicy",
					Use:         "set-recognition-policy-proposal",
					Short:       "Submit a governance proposal to replace the asset eligibility set",
					GovProposal: true,
				},
				{
					RpcMethod:   "FundBuffer",
					Use:         "fund-redemption-buffer-proposal",
					Short:       "Submit a governance proposal to transfer Reserve NOAH to the Redemption Buffer",
					GovProposal: true,
				},
				{
					RpcMethod:   "FundInsurance",
					Use:         "fund-insurance-proposal",
					Short:       "Submit a governance proposal to transfer Reserve NOAH to Insurance",
					GovProposal: true,
				},
				{
					RpcMethod:   "BurnReserveAssets",
					Use:         "burn-proposal",
					Short:       "Submit a governance proposal to burn Reserve custody, including NOAH",
					GovProposal: true,
				},
				{
					RpcMethod:   "CorrectPosition",
					Use:         "correct-position-proposal [position-id]",
					Short:       "Submit a governance proposal to restate a position",
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "position_id"},
					},
				},
				{
					RpcMethod:   "ClearImpairment",
					Use:         "clear-impairment-proposal [position-id]",
					Short:       "Submit a governance proposal to restore a position's recognition credit",
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "position_id"},
					},
				},
				{
					RpcMethod:   "MarkImpaired",
					Use:         "mark-impaired-proposal [position-id]",
					Short:       "Submit a governance proposal to zero a position's recognition credit",
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "position_id"},
					},
				},
				{
					RpcMethod:   "ClosePosition",
					Use:         "close-position-proposal [position-id]",
					Short:       "Submit a governance proposal to close a position",
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "position_id"},
					},
				},
				{
					RpcMethod:   "ReverseReturn",
					Use:         "reverse-return-proposal [position-id]",
					Short:       "Submit a governance proposal to undo a return attribution",
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "position_id"},
					},
				},
				{
					RpcMethod: "CommitteeDeploy",
					Use:       "deploy",
					Short:     "Deploy Reserve NOAH to a mandate destination as the Reserve committee",
				},
				{
					RpcMethod: "CommitteeRecordUpdate",
					Use:       "record-update [position-id]",
					Short:     "Restate a position's attested holding as the Reserve committee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "position_id"},
					},
				},
				{
					RpcMethod: "CommitteeAttributeReturn",
					Use:       "attribute-return [position-id]",
					Short:     "Attribute a returned inflow to a position as the Reserve committee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "position_id"},
					},
				},
				{
					RpcMethod: "CommitteeReverseReturn",
					Use:       "reverse-return [position-id]",
					Short:     "Undo a return attribution as the Reserve committee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "position_id"},
					},
				},
				{
					RpcMethod: "CommitteeMarkImpaired",
					Use:       "mark-impaired [position-id]",
					Short:     "Zero a position's recognition credit as the Reserve committee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "position_id"},
					},
				},
				{
					RpcMethod: "CommitteeClearImpairment",
					Use:       "clear-impairment [position-id]",
					Short:     "Restore a position's recognition credit as the Reserve committee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "position_id"},
					},
				},
				{
					RpcMethod: "CommitteeCorrectPosition",
					Use:       "correct-position [position-id]",
					Short:     "Restate a position as the Reserve committee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "position_id"},
					},
				},
				{
					RpcMethod: "CommitteeClosePosition",
					Use:       "close-position [position-id]",
					Short:     "Close a position as the Reserve committee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "position_id"},
					},
				},
				{
					RpcMethod: "CommitteeBurnPaper",
					Use:       "burn-paper [amounts]",
					Short:     "Burn credit-zero, non-NOAH custody as the Reserve committee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "amounts"},
					},
				},
				{
					RpcMethod: "CommitteeBurnSurplus",
					Use:       "burn-surplus [amount]",
					Short:     "Burn Reserve NOAH above the capital requirement as the Reserve committee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "amount"},
					},
				},
				{
					RpcMethod: "CommitteeFundBuffer",
					Use:       "fund-redemption-buffer [amount]",
					Short:     "Top the Redemption Buffer up toward its target as the Reserve committee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "amount"},
					},
				},
				{
					RpcMethod: "CommitteeFundInsurance",
					Use:       "fund-insurance [amount]",
					Short:     "Top Insurance up toward its target as the Reserve committee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "amount"},
					},
				},
			},
		},
	}
}
