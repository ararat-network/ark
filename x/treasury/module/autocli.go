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
					RpcMethod: "MonetaryPolicy",
					Use:       "monetary-policy",
					Short:     "Query the current Treasury monetary policy",
					Example:   fmt.Sprintf("%s query treasury monetary-policy", version.AppName),
				},
				{
					RpcMethod: "TaxCap",
					Use:       "tax-cap [denom]",
					Short:     "Query the derived stability-tax cap for a denomination",
					Example:   fmt.Sprintf("%s query treasury tax-cap asdr", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
					},
				},
				{
					RpcMethod: "MonetaryMandate",
					Use:       "monetary-mandate",
					Short:     "Query the governed monetary-policy committee mandate",
					Example:   fmt.Sprintf("%s query treasury monetary-mandate", version.AppName),
				},
				{
					RpcMethod: "TaxCaps",
					Use:       "tax-caps",
					Short:     "Query every derived stability-tax cap",
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
					RpcMethod: "ComputeTax",
					Skip:      true,
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
					RpcMethod:   "SetMonetaryMandate",
					Use:         "set-monetary-mandate-proposal",
					Short:       "Submit a governance proposal to appoint, replace, or disable the monetary-policy committee",
					GovProposal: true,
				},
				{
					RpcMethod:   "UpdatePolicy",
					Use:         "update-policy-proposal",
					Short:       "Submit a governance proposal to update reversible Treasury policy outside committee bounds",
					GovProposal: true,
				},
				{
					RpcMethod: "CommitteeUpdatePolicy",
					Use:       "committee-update-policy",
					Short:     "Update reversible Treasury policy as the monetary-policy committee",
				},
			},
		},
	}
}
