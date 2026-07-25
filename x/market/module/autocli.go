package market

import (
	"fmt"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"github.com/cosmos/cosmos-sdk/version"

	marketv1 "ark/api/ark/market/v1"
)

// AutoCLIOptions returns the market module's AutoCLI configuration.
func (am AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: marketv1.Query_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "Swap",
					Use:       "swap [offer-coin] [ask-denom]",
					Short:     "Query a swap quote",
					Long:      "Simulate a swap and return the estimated output coin and swap fee. Quotes use current oracle and pool state and may change before execution.",
					Example:   fmt.Sprintf("%s query market swap 5000000000000000000anoah asdr", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "offer_coin"},
						{ProtoField: "ask_denom"},
					},
				},
				{
					RpcMethod: "ArkPoolDelta",
					Use:       "ark-pool-delta",
					Short:     "Query the Ark pool delta",
					Long:      "Query the current gap between the Ark pool and the base pool. A positive value means the pool is above target; a negative value means it is below target.",
					Example:   fmt.Sprintf("%s query market ark-pool-delta", version.AppName),
				},
				{
					RpcMethod: "Params",
					Use:       "params",
					Short:     "Query the current market parameters",
					Example:   fmt.Sprintf("%s query market params", version.AppName),
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: marketv1.Msg_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "Swap",
					Use:       "swap [offer-coin] [ask-denom] [minimum-receive]",
					Short:     "Atomically swap currencies at their target exchange rate",
					Long:      "Swap the offer coin to the ask denomination at the oracle's effective exchange rate. The swap fails unless it returns at least minimum-receive.",
					Example:   fmt.Sprintf(`%s tx market swap "1000000000000000000akrw" "ausd" "1ausd"`, version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "offer_coin"},
						{ProtoField: "ask_denom"},
						{ProtoField: "minimum_receive"},
					},
				},
				{
					RpcMethod: "SwapSend",
					Skip:      true,
				},
				{
					RpcMethod:   "UpdateParams",
					Use:         "update-params-proposal [params]",
					Short:       "Submit a proposal to update market parameters",
					Example:     fmt.Sprintf(`%s tx market update-params-proposal '{"base_pool":"1000000.0","pool_recovery_period":"18","min_stability_spread":"0.005"}'`, version.AppName),
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "params"},
					},
				},
			},
			EnhanceCustomCommand: true,
		},
	}
}
