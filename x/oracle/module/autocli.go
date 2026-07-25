package oracle

import (
	"fmt"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"github.com/cosmos/cosmos-sdk/version"

	oraclev1 "ark/api/ark/oracle/v1"
)

// AutoCLIOptions returns the oracle module's AutoCLI configuration.
func (am AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: oraclev1.Query_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "ExchangeRates",
					Use:       "exchange-rates",
					Short:     "Query all current oracle exchange rates",
					Example:   fmt.Sprintf("%s query oracle exchange-rates", version.AppName),
				},
				{
					RpcMethod: "ExchangeRate",
					Use:       "exchange-rate [denom]",
					Short:     "Query the current oracle exchange rate for a denom",
					Example:   fmt.Sprintf("%s query oracle exchange-rate akrw", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
					},
				},
				{
					RpcMethod: "Params",
					Use:       "params",
					Short:     "Query the current oracle parameters",
					Example:   fmt.Sprintf("%s query oracle params", version.AppName),
				},
				{
					RpcMethod: "RewardWeight",
					Use:       "reward-weight [validator]",
					Short:     "Query the oracle reward weight for a validator",
					Example:   fmt.Sprintf("%s query oracle reward-weight arkvaloper1...", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "validator_addr"},
					},
				},
				{
					RpcMethod: "MissCount",
					Use:       "miss [validator]",
					Short:     "Query the oracle miss count for a validator",
					Example:   fmt.Sprintf("%s query oracle miss arkvaloper1...", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "validator_addr"},
					},
				},
				{
					RpcMethod: "VoteTargets",
					Use:       "vote-targets",
					Short:     "Query the current oracle vote targets",
					Example:   fmt.Sprintf("%s query oracle vote-targets", version.AppName),
				},
				{
					RpcMethod: "TobinTaxes",
					Use:       "tobin-taxes",
					Short:     "Query all current oracle Tobin taxes",
					Example:   fmt.Sprintf("%s query oracle tobin-taxes", version.AppName),
				},
				{
					RpcMethod: "TobinTax",
					Use:       "tobin-tax [denom]",
					Short:     "Query the current oracle Tobin tax for a denom",
					Example:   fmt.Sprintf("%s query oracle tobin-tax akrw", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
					},
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: oraclev1.Msg_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod:   "UpdateParams",
					Use:         "update-params-proposal [params]",
					Short:       "Submit a proposal to update oracle parameters",
					Example:     fmt.Sprintf(`%s tx oracle update-params-proposal '{"vote_period":"5",...}'`, version.AppName),
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
