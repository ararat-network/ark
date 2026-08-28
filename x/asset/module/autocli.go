package asset

import (
	"fmt"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"github.com/cosmos/cosmos-sdk/version"

	assetv1 "github.com/ararat-network/ark/api/ark/asset/v1"
)

// AutoCLIOptions returns the asset module's AutoCLI configuration.
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: assetv1.Query_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "Params",
					Use:       "params",
					Short:     "Query the asset module parameters",
					Example: fmt.Sprintf(
						"%s query asset params",
						version.AppName,
					),
				},
				{
					RpcMethod: "Asset",
					Use:       "asset [denom]",
					Short:     "Query one registered asset",
					Example: fmt.Sprintf(
						"%s query asset asset ausd",
						version.AppName,
					),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
					},
				},
				{
					RpcMethod: "Assets",
					Use:       "assets",
					Short:     "Query registered assets",
					Example: fmt.Sprintf(
						"%s query asset assets",
						version.AppName,
					),
				},
				{
					RpcMethod: "SettlementPlan",
					Use:       "settlement-plan [denom]",
					Short:     "Query an asset's active settlement plan",
					Example: fmt.Sprintf(
						"%s query asset settlement-plan ausd",
						version.AppName,
					),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
					},
				},
				{
					RpcMethod: "ResolutionHistory",
					Use:       "write-off-history [denom]",
					Short:     "Query an asset's append-only write-off history",
					Example: fmt.Sprintf(
						"%s query asset write-off-history ausd",
						version.AppName,
					),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
					},
				},
				{
					RpcMethod: "EmergencyMandate",
					Use:       "emergency-mandate",
					Short:     "Query the emergency committee mandate and its consumed actions",
					Example: fmt.Sprintf(
						"%s query asset emergency-mandate",
						version.AppName,
					),
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: assetv1.Msg_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "UpdateParams",
					Use:       "update-params-proposal [params]",
					Short:     "Submit a proposal to update asset parameters",
					Example: fmt.Sprintf(
						`%s tx asset update-params-proposal '{"settlement_activation_delay_blocks":"14400"}'`,
						version.AppName,
					),
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "params"},
					},
				},
				assetProposalCommand(
					"RegisterAsset",
					"register-asset-proposal [denom]",
					"Submit a proposal to register an asset",
					"denom",
				),
				assetProposalCommand(
					"HaltIssuance",
					"halt-issuance-proposal [denom] [expected-version]",
					"Submit a proposal to halt asset issuance",
					"denom",
					"expected_version",
				),
				assetProposalCommand(
					"ResumeIssuance",
					"resume-issuance-proposal [denom] [expected-version]",
					"Submit a proposal to resume asset issuance",
					"denom",
					"expected_version",
				),
				assetProposalCommand(
					"SuspendAsset",
					"suspend-asset-proposal [denom] [expected-version]",
					"Submit a proposal to suspend an asset",
					"denom",
					"expected_version",
				),
				assetProposalCommand(
					"OpenSettlement",
					"open-settlement-proposal [denom] [expected-version] [redemption-rate] [earliest-closing-height]",
					"Submit a proposal to open asset settlement",
					"denom",
					"expected_version",
					"redemption_rate",
					"earliest_closing_height",
				),
				assetProposalCommand(
					"CancelSettlement",
					"cancel-settlement-proposal [denom] [expected-version]",
					"Submit a proposal to cancel an unactivated asset settlement",
					"denom",
					"expected_version",
				),
				assetProposalCommand(
					"RecoverAsset",
					"recover-asset-proposal [denom] [expected-version]",
					"Submit a proposal to begin asset recovery",
					"denom",
					"expected_version",
				),
				assetProposalCommand(
					"WriteOffAsset",
					"write-off-asset-proposal [denom] [expected-version]",
					"Submit a proposal to write off an asset",
					"denom",
					"expected_version",
				),
				assetProposalCommand(
					"FinaliseRetirement",
					"finalise-retirement-proposal [denom] [expected-version] [max-residual-supply]",
					"Submit a proposal to finalise asset retirement",
					"denom",
					"expected_version",
					"max_residual_supply",
				),
				assetProposalCommand(
					"SetEmergencyMandate",
					"set-emergency-mandate-proposal [committee] [activation-height] [expiry-height]",
					"Submit a proposal to replace or disable the emergency mandate",
					"committee",
					"activation_height",
					"expiry_height",
				),
				{
					RpcMethod: "EmergencySuspendAsset",
					Use:       "emergency-suspend-asset [denom] [expected-term]",
					Short:     "Suspend an asset as the emergency committee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
						{ProtoField: "expected_term"},
					},
				},
			},
			EnhanceCustomCommand: true,
		},
	}
}

func assetProposalCommand(
	rpcMethod string,
	use string,
	short string,
	protoFields ...string,
) *autocliv1.RpcCommandOptions {
	positionalArgs := make(
		[]*autocliv1.PositionalArgDescriptor,
		len(protoFields),
	)
	for i, protoField := range protoFields {
		positionalArgs[i] = &autocliv1.PositionalArgDescriptor{
			ProtoField: protoField,
		}
	}

	return &autocliv1.RpcCommandOptions{
		RpcMethod:      rpcMethod,
		Use:            use,
		Short:          short,
		GovProposal:    true,
		PositionalArgs: positionalArgs,
	}
}
