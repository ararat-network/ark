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
					RpcMethod: "Attendance",
					Use:       "attendance [validator]",
					Short:     "Query a validator's oracle attendance counters",
					Example:   fmt.Sprintf("%s query oracle attendance arkvaloper1...", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "validator_addr"},
					},
				},
				{
					RpcMethod: "Feeds",
					Use:       "feeds",
					Short:     "Query the active oracle feeds and scheduled transitions",
					Example:   fmt.Sprintf("%s query oracle feeds", version.AppName),
				},
				{
					RpcMethod: "FeedReferents",
					Use:       "feed-referents [denom]",
					Short:     "Query the consumer claims blocking removal of a feed",
					Example:   fmt.Sprintf("%s query oracle feed-referents ausd", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
					},
				},
				{
					RpcMethod: "ReferenceDenom",
					Use:       "reference-denom",
					Short:     "Query the protocol reference denomination",
					Example:   fmt.Sprintf("%s query oracle reference-denom", version.AppName),
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
					Example:     fmt.Sprintf(`%s tx oracle update-params-proposal '{"vote_threshold":"0.666666666666666667",...}'`, version.AppName),
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "params"},
					},
				},
				{
					RpcMethod:   "SetReferenceDenom",
					Use:         "set-reference-denom-proposal [reference-denom]",
					Short:       "Submit a proposal to re-point the protocol reference denomination",
					Example:     fmt.Sprintf("%s tx oracle set-reference-denom-proposal asdr", version.AppName),
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "reference_denom"},
					},
				},
			},
			EnhanceCustomCommand: true,
		},
	}
}
