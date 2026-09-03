package market

import (
	"fmt"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"github.com/cosmos/cosmos-sdk/version"

	marketv1 "github.com/ararat-network/ark/api/ark/market/v1"
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
					Example:   fmt.Sprintf("%s query market swap 5000000000000000000anoah axdr", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "offer_coin"},
						{ProtoField: "ask_denom"},
					},
				},
				{
					RpcMethod: "Pool",
					Use:       "pool",
					Short:     "Query the live virtual-pool state",
					Long:      "Query the base pool depth and the current gap between the Ark pool and that depth, in one snapshot. A positive delta means the pool is above target; a negative one means it is below target.",
					Example:   fmt.Sprintf("%s query market pool", version.AppName),
				},
				{
					RpcMethod: "Params",
					Use:       "params",
					Short:     "Query the current market parameters",
					Example:   fmt.Sprintf("%s query market params", version.AppName),
				},
				{
					RpcMethod: "TobinTax",
					Use:       "tobin-tax [denom]",
					Short:     "Query the effective Tobin rate for a denomination",
					Long:      "Query the conversion spread a denomination contributes: its governance-set override when one exists, the default rate otherwise. This is the rate a swap will actually charge.",
					Example:   fmt.Sprintf("%s query market tobin-tax amnt", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
					},
				},
				{
					RpcMethod: "TobinTaxOverrides",
					Use:       "tobin-tax-overrides",
					Short:     "Query the per-denomination Tobin exceptions",
					Long:      "Query every denomination governance has moved off the default Tobin rate. Denominations absent from this list convert at the default.",
					Example:   fmt.Sprintf("%s query market tobin-tax-overrides", version.AppName),
				},
				{
					RpcMethod: "ConversionPolicy",
					Use:       "conversion-policy",
					Short:     "Query the live conversion-capacity pair",
					Long:      "Query the virtual-pool depth and recovery period, which together set how much conversion the protocol absorbs before the spread widens.",
					Example:   fmt.Sprintf("%s query market conversion-policy", version.AppName),
				},
				{
					RpcMethod: "ConversionMandate",
					Use:       "conversion-mandate",
					Short:     "Query the governed conversion committee mandate",
					Long:      "Query the committee appointed to resize conversion capacity, the corridor it may act within, and whether that appointment is usable at the current height.",
					Example:   fmt.Sprintf("%s query market conversion-mandate", version.AppName),
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
					Long:      "Swap the offer coin to the ask denomination at the oracle's effective exchange rate. Supply minimum-receive to fail the swap unless it returns at least that much; omit it to accept market execution.",
					Example:   fmt.Sprintf(`%s tx market swap "1000000000000000000akrw" "ausd" "740000000000000ausd"`, version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "offer_coin"},
						{ProtoField: "ask_denom"},
						{ProtoField: "minimum_receive", Optional: true},
					},
				},
				{
					RpcMethod: "SwapSend",
					Use:       "swap-send [offer-coin] [ask-denom] [to-address] [minimum-receive]",
					Short:     "Atomically swap currencies and send the output to another address",
					Long:      "Swap the offer coin to the ask denomination at the oracle's effective exchange rate and send the output to to-address. Supply minimum-receive to fail the swap unless it returns at least that much; omit it to accept market execution.",
					Example:   fmt.Sprintf(`%s tx market swap-send "1000000000000000000akrw" "ausd" "ark1..." "740000000000000ausd"`, version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "offer_coin"},
						{ProtoField: "ask_denom"},
						{ProtoField: "to_address"},
						{ProtoField: "minimum_receive", Optional: true},
					},
				},
				{
					RpcMethod: "Settle",
					Use:       "settle [offer-coin]",
					Short:     "Redeem a suspended asset at its settlement rate",
					Long:      "Redeem a suspended asset for NOAH at the rate governance approved in its settlement plan. This is not a swap: the plan rate is fixed, the offered asset is burned, and no other conversion route accepts a suspended asset.",
					Example:   fmt.Sprintf(`%s tx market settle "1000000000000000000akrw"`, version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "offer_coin"},
					},
				},
				{
					RpcMethod:   "UpdateParams",
					Use:         "update-params-proposal [params]",
					Short:       "Submit a proposal to update market parameters",
					Example:     fmt.Sprintf(`%s tx market update-params-proposal '{"default_tobin_tax":"0.0025"}'`, version.AppName),
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "params"},
					},
				},
				{
					RpcMethod:   "SetTobinTaxOverride",
					Use:         "set-tobin-tax-override-proposal [denom] [tobin-tax]",
					Short:       "Submit a proposal to set a per-denomination Tobin rate",
					Long:        "Move one denomination off the default Tobin rate. The denomination must already be registered as an asset, whatever its status, so a listing proposal can set the override in the same act that lists the asset.",
					Example:     fmt.Sprintf("%s tx market set-tobin-tax-override-proposal amnt 0.02", version.AppName),
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
						{ProtoField: "tobin_tax"},
					},
				},
				{
					RpcMethod:   "RemoveTobinTaxOverride",
					Use:         "remove-tobin-tax-override-proposal [denom]",
					Short:       "Submit a proposal to return a denomination to the default Tobin rate",
					Example:     fmt.Sprintf("%s tx market remove-tobin-tax-override-proposal amnt", version.AppName),
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
					},
				},
				{
					RpcMethod:   "SetConversionMandate",
					Use:         "set-conversion-mandate-proposal",
					Short:       "Submit a governance proposal to appoint, replace, or disable the conversion committee",
					Long:        "Appoint the committee that may retune conversion during an emergency, bounded by a corridor over depth, the recovery period, and the stability-spread floor. An empty committee disables the mandate. Bounds must be denominated in the live base pool unit. Equal bounds on a field pin it; a minimum equal to the live value delegates a raise-only power over it.",
					GovProposal: true,
				},
				{
					RpcMethod:   "UpdatePolicy",
					Use:         "update-policy-proposal",
					Short:       "Submit a governance proposal to retune conversion outside committee bounds",
					GovProposal: true,
				},
				{
					RpcMethod: "CommitteeUpdatePolicy",
					Use:       "committee-update-policy",
					Short:     "Retune conversion as the conversion committee",
					Long:      "Apply a depth, recovery period, and stability-spread floor inside the mandate corridor as the appointed committee. The expected term must match the live appointment, so a transaction prepared under a replaced mandate fails.",
				},
				{
					RpcMethod: "CommitteeSetTobinTax",
					Use:       "committee-set-tobin-tax [denom] [tobin-tax]",
					Short:     "Set a per-denomination Tobin rate as the conversion committee",
					Long:      "Create or replace one Tobin override inside the band the mandate delegates: at least the chain-wide default rate and at most the mandate's cap. Going below the default is a governance proposal, as is removing an override. The expected term must match the live appointment.",
					Example:   fmt.Sprintf("%s tx market committee-set-tobin-tax amnt 0.02", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
						{ProtoField: "tobin_tax"},
					},
				},
			},
		},
	}
}
