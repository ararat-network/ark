package treasury

import (
	"fmt"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"github.com/cosmos/cosmos-sdk/version"

	treasuryv1 "ark/api/ark/treasury/v1"
)

// AutoCLIOptions returns the treasury module's AutoCLI configuration.
func (am AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: treasuryv1.Query_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "TaxRate",
					Use:       "tax-rate",
					Short:     "Query the stability tax rate of the current epoch",
					Example:   fmt.Sprintf("%s query treasury tax-rate", version.AppName),
				},
				{
					RpcMethod: "TaxCap",
					Use:       "tax-cap [denom]",
					Short:     "Query the current stability tax cap for a denom",
					Long:      "Query the current stability tax cap for a denom. The stability tax levied on a tx is capped regardless of transaction size.",
					Example:   fmt.Sprintf("%s query treasury tax-cap ukrw", version.AppName),
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "denom"},
					},
				},
				{
					RpcMethod: "TaxCaps",
					Use:       "tax-caps",
					Short:     "Query the current stability tax caps for all denoms",
					Long:      "Query the current stability tax caps for all denoms. The stability tax levied on a tx is capped regardless of transaction size.",
					Example:   fmt.Sprintf("%s query treasury tax-caps", version.AppName),
				},
				{
					RpcMethod: "RewardWeight",
					Use:       "reward-weight",
					Short:     "Query the current reward weight of the current epoch",
					Example:   fmt.Sprintf("%s query treasury reward-weight", version.AppName),
				},
				{
					RpcMethod: "SeigniorageProceeds",
					Use:       "seigniorage-proceeds",
					Short:     "Query the seigniorage proceeds for the current epoch",
					Example:   fmt.Sprintf("%s query treasury seigniorage-proceeds", version.AppName),
				},
				{
					RpcMethod: "TaxProceeds",
					Use:       "tax-proceeds",
					Short:     "Query the tax proceeds for the current epoch",
					Example:   fmt.Sprintf("%s query treasury tax-proceeds", version.AppName),
				},
				{
					RpcMethod: "Indicators",
					Use:       "indicators",
					Short:     "Query the current treasury indicators",
					Example:   fmt.Sprintf("%s query treasury indicators", version.AppName),
				},
				{
					RpcMethod: "Params",
					Use:       "params",
					Short:     "Query the current treasury parameters",
					Example:   fmt.Sprintf("%s query treasury params", version.AppName),
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: treasuryv1.Msg_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod:   "UpdateParams",
					Use:         "update-params-proposal [params]",
					Short:       "Submit a proposal to update treasury parameters",
					Example:     fmt.Sprintf(`%s tx treasury update-params-proposal '{"tax_policy":{...},"reward_policy":{...}}'`, version.AppName),
					GovProposal: true,
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "params"},
					},
				},
			},
		},
	}
}
