package app

import (
	"github.com/stretchr/testify/assert/yaml"

	_ "cosmossdk.io/x/evidence"        // import for side-effects
	_ "cosmossdk.io/x/feegrant/module" // import for side-effects
	_ "cosmossdk.io/x/upgrade"         // import for side-effects
	_ "embed"
	_ "github.com/cosmos/cosmos-sdk/x/auth/tx/config" // import for side-effects
	_ "github.com/cosmos/cosmos-sdk/x/auth/vesting"   // import for side-effects
	_ "github.com/cosmos/cosmos-sdk/x/authz/module"   // import for side-effects
	_ "github.com/cosmos/cosmos-sdk/x/bank"           // import for side-effects
	_ "github.com/cosmos/cosmos-sdk/x/consensus"      // import for side-effects
	_ "github.com/cosmos/cosmos-sdk/x/distribution"   // import for side-effects
	_ "github.com/cosmos/cosmos-sdk/x/epochs"         // import for side-effects
	_ "github.com/cosmos/cosmos-sdk/x/mint"           // import for side-effects
	_ "github.com/cosmos/cosmos-sdk/x/protocolpool"   // import for side-effects
	_ "github.com/cosmos/cosmos-sdk/x/slashing"       // import for side-effects
	_ "github.com/cosmos/cosmos-sdk/x/staking"        // import for side-effects

	"cosmossdk.io/core/appconfig"
	"cosmossdk.io/depinject"

	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/cosmos-sdk/x/gov"
	govclient "github.com/cosmos/cosmos-sdk/x/gov/client"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

var (
	//go:embed app_config.yaml
	AppConfigYAML []byte
	AppConfig     = depinject.Configs(
		appconfig.LoadYAML(AppConfigYAML),
		depinject.Supply(
			// supply custom module basics
			map[string]module.AppModuleBasic{
				genutiltypes.ModuleName: genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator),
				govtypes.ModuleName: gov.NewAppModuleBasic(
					[]govclient.ProposalHandler{},
				),
			},
		),
	)
)

// This section is just to pull out moduleAccPerms and blockAccAddrs from the yaml
// file for testing purposes
type appConfigData struct {
	Modules []moduleConfigData `yaml:"modules"`
}
type moduleConfigData struct {
	Name   string                 `yaml:"name"`
	Config map[string]interface{} `yaml:"config"`
}

var (
	moduleAccPerms map[string][]string
	blockAccAddrs  []string
)

func init() {
	var cfg appConfigData
	if err := yaml.Unmarshal(AppConfigYAML, &cfg); err != nil {
		panic("failed to parse app_config.yaml: " + err.Error())
	}

	moduleAccPerms = make(map[string][]string)

	for _, mod := range cfg.Modules {
		switch mod.Name {
		case "auth":
			if perms, ok := mod.Config["module_account_permissions"].([]interface{}); ok {
				for _, p := range perms {
					if perm, ok := p.(map[string]interface{}); ok {
						account, _ := perm["account"].(string)
						var permissions []string
						if permList, ok := perm["permissions"].([]interface{}); ok {
							for _, pv := range permList {
								if ps, ok := pv.(string); ok {
									permissions = append(permissions, ps)
								}
							}
						}
						moduleAccPerms[account] = permissions
					}
				}
			}
		case "bank":
			if addrs, ok := mod.Config["blocked_module_accounts_override"].([]interface{}); ok {
				for _, addr := range addrs {
					if a, ok := addr.(string); ok {
						blockAccAddrs = append(blockAccAddrs, a)
					}
				}
			}
		}
	}
}

// BlockedAddresses returns all the app's blocked account addresses.
func BlockedAddresses() map[string]bool {
	result := make(map[string]bool)

	if len(blockAccAddrs) > 0 {
		for _, addr := range blockAccAddrs {
			result[addr] = true
		}
	} else {
		for addr := range moduleAccPerms {
			result[addr] = true
		}
	}

	return result
}
