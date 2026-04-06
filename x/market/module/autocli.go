package market

import autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

// AutoCLIOptions returns the market module's AutoCLI configuration.
func (am AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{}
}
