package oracle

import (
	"fmt"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"github.com/cosmos/cosmos-sdk/version"

	oraclev1 "github.com/ararat-network/ark/api/ark/oracle/v1"
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
					Short:     "Query every fresh oracle rate in both readings: NOAH per unit, and units per NOAH",
					Example:   fmt.Sprintf("%s query oracle exchange-rates", version.AppName),
				},
				{
					RpcMethod: "ExchangeRate",
					Use:       "exchange-rate [denom]",
					Short:     "Query a denom's oracle rate in both readings: NOAH per unit, and units per NOAH",
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
					RpcMethod:   "AddFeed",
					Use:         "add-feed-proposal [denom]",
					Short:       "Submit a proposal to add a price feed",
					Long:        "Schedule a denomination to join the active feed set. A denomination already active, or already scheduled to join, is not an error — membership is simply satisfied — so re-sending the proposal is how governance re-approves a feed. Staleness is not stated here: every consumer without a window of its own takes Params.max_exchange_rate_age, and a consumer holding one states it in its own policy.",
					Example:     fmt.Sprintf("%s tx oracle add-feed-proposal agold", version.AppName),
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
					},
				},
				{
					RpcMethod:   "RemoveFeed",
					Use:         "remove-feed-proposal [denom]",
					Short:       "Submit a proposal to remove a price feed",
					Long:        "Schedule a denomination to leave the active feed set. The proposal is rejected while a consumer still holds a claim on the feed; query feed-referents first to see what would block it. When the removal activates the feed's stored rate is pruned, so a feed that returns later re-warms rather than serving a rate from before it left.",
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
					Example:     fmt.Sprintf("%s tx oracle set-reference-denom-proposal axdr", version.AppName),
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
