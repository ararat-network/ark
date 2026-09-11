package treasury

import (
	"fmt"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"github.com/cosmos/cosmos-sdk/version"

	treasuryv1 "github.com/ararat-network/ark/api/ark/treasury/v1"
)

// AutoCLIOptions returns the treasury module's AutoCLI configuration.
func (AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: treasuryv1.Query_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "Params",
					Use:       "params",
					Short:     "Query the Treasury parameters",
					Example:   fmt.Sprintf("%s query treasury params", version.AppName),
				},
				{
					RpcMethod: "EconomicPolicy",
					Use:       "economic-policy",
					Short:     "Query the current Treasury economic policy",
					Example:   fmt.Sprintf("%s query treasury economic-policy", version.AppName),
				},
				{
					RpcMethod: "TaxCap",
					Use:       "tax-cap [denom]",
					Short:     "Query the derived transfer-tax cap for a denomination",
					Example:   fmt.Sprintf("%s query treasury tax-cap axdr", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
					},
				},
				{
					RpcMethod: "EconomicMandate",
					Use:       "economic-mandate",
					Short:     "Query the governed economic-policy committee mandate",
					Example:   fmt.Sprintf("%s query treasury economic-mandate", version.AppName),
				},
				{
					RpcMethod: "TaxCaps",
					Use:       "tax-caps",
					Short:     "Query every derived transfer-tax cap",
					Example:   fmt.Sprintf("%s query treasury tax-caps", version.AppName),
				},
				{
					RpcMethod: "ConversionFactor",
					Use:       "conversion-factor [denom]",
					Short:     "Query a denomination's stored reference conversion factor",
					Example:   fmt.Sprintf("%s query treasury conversion-factor ausd", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
					},
				},
				{
					RpcMethod: "ConversionFactors",
					Use:       "conversion-factors",
					Short:     "Query the complete conversion-factor table with derivation heights",
					Example:   fmt.Sprintf("%s query treasury conversion-factors", version.AppName),
				},
				{
					RpcMethod: "GasPrice",
					Use:       "gas-price [denom]",
					Short:     "Query one accepted fee denomination's gas price",
					Example:   fmt.Sprintf("%s query treasury gas-price ausd", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
					},
				},
				{
					RpcMethod: "GasPrices",
					Use:       "gas-prices",
					Short:     "Query the complete fee-denomination gas price sheet",
					Example:   fmt.Sprintf("%s query treasury gas-prices", version.AppName),
				},
				{
					RpcMethod: "ComputeTax",
					Use:       "compute-tax",
					Short:     "Compute the transfer tax the given messages owe, which their transaction's fee must cover",
					Example: fmt.Sprintf(
						`%s query treasury compute-tax --messages '{"@type":"/cosmos.bank.v1beta1.MsgSend",`+
							`"from_address":"ark1...","to_address":"ark1...","amount":[{"denom":"axdr","amount":"1000"}]}'`,
						version.AppName,
					),
				},
				{
					RpcMethod: "FundStatus",
					Use:       "fund-status",
					Short:     "Query Treasury fund balances, liabilities, and targets",
					Example:   fmt.Sprintf("%s query treasury fund-status", version.AppName),
				},
				{
					RpcMethod: "RewardFunding",
					Use:       "reward-funding",
					Short:     "Query active Treasury reward-funding accounting",
					Example:   fmt.Sprintf("%s query treasury reward-funding", version.AppName),
				},
				{
					RpcMethod: "ExposureStatus",
					Use:       "exposure-status",
					Short:     "Query the risk state behind the fund-target multiplier",
					Example:   fmt.Sprintf("%s query treasury exposure-status", version.AppName),
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: treasuryv1.Msg_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod:   "UpdateParams",
					Use:         "update-params-proposal [params]",
					Short:       "Submit a governance proposal to update Treasury parameters",
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "params"},
					},
				},
				{
					RpcMethod:   "SetEconomicMandate",
					Use:         "set-economic-mandate-proposal",
					Short:       "Submit a governance proposal to appoint, replace, or disable the economic-policy committee",
					GovProposal: true,
				},
				{
					RpcMethod:   "UpdatePolicy",
					Use:         "update-policy-proposal",
					Short:       "Submit a governance proposal to update reversible Treasury policy outside committee bounds",
					GovProposal: true,
				},
				{
					RpcMethod:   "ReturnSubsidy",
					Use:         "return-subsidy-proposal",
					Short:       "Submit a governance proposal to return idle subsidy NOAH to the community pool",
					GovProposal: true,
				},
				{
					RpcMethod: "CommitteeUpdatePolicy",
					Use:       "committee-update-policy",
					Short:     "Update reversible Treasury policy as the economic-policy committee",
				},
			},
		},
	}
}
