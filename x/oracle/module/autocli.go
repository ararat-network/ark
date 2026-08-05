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
					RpcMethod: "MaxExchangeRateAgeOverrides",
					Use:       "max-exchange-rate-age-overrides",
					Short:     "Query the per-feed exchange rate staleness windows differing from the default",
					Example:   fmt.Sprintf("%s query oracle max-exchange-rate-age-overrides", version.AppName),
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
					RpcMethod:   "AddFeed",
					Use:         "add-feed-proposal [denom] [max-age]",
					Short:       "Submit a proposal to add a price feed and state its staleness window",
					Long:        "Schedule a denomination to join the active feed set and state how old its rate may be before it counts as stale. An omitted window adopts the chain default in Params.max_exchange_rate_age. A denomination already active, or already scheduled to join, is not an error — membership is simply satisfied — so the same proposal restates a live feed's window. Restating a live feed without a window returns it to the default, so a non-default window must be repeated whenever the feed is restated.",
					Example:     fmt.Sprintf("%s tx oracle add-feed-proposal agold 26h", version.AppName),
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
						{ProtoField: "max_age", Optional: true},
					},
				},
				{
					RpcMethod:   "RemoveFeed",
					Use:         "remove-feed-proposal [denom]",
					Short:       "Submit a proposal to remove a price feed",
					Long:        "Schedule a denomination to leave the active feed set. The proposal is rejected while a consumer still holds a claim on the feed; query feed-referents first to see what would block it. When the removal activates, the feed's stored rate and its staleness window are both pruned, so a feed that returns later re-states both.",
					Example:     fmt.Sprintf("%s tx oracle remove-feed-proposal agold", version.AppName),
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
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
