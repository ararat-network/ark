package oracle

import autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

// AutoCLIOptions returns the oracle module's AutoCLI configuration.
func (am AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{}
}
