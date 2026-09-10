// Package cmd owns the arkd command tree: the client context, AutoCLI
// expansion, the start command's hooks, app.toml, and the commands the SDK
// does not supply. app/ owns keeper wiring and consensus lifecycle.
package cmd

import (
	"context"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	"cosmossdk.io/client/v2/autocli"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/app"
	appclient "github.com/ararat-network/ark/app/client"
)

// serviceName names the binary, its logs, and its metrics alike, so the
// signals join on one name.
const serviceName = app.Name + "d"

// NewRootCmd builds the arkd root command. main calls it once.
func NewRootCmd() *cobra.Command {
	var (
		autoCliOpts        autocli.AppOptions
		moduleBasicManager module.BasicManager
		clientCtx          client.Context
	)

	if err := depinject.Inject(
		depinject.Configs(app.AppConfig,
			depinject.Supply(
				log.NewNopLogger(),
				unwiredWasmKeeper{},
			),
			depinject.Provide(
				ProvideClientContext,
			),
		),
		&autoCliOpts,
		&moduleBasicManager,
		&clientCtx,
	); err != nil {
		panic(err)
	}

	// IBC and Wasm are wired outside depinject, so appBuilder never sees them
	// and their codecs, interfaces, and commands are registered by hand.
	manualBasics := module.BasicManager{}
	for _, basics := range []module.BasicManager{
		app.IBCModuleBasics(),
		app.WasmModuleBasics(),
		app.GMPModuleBasics(),
	} {
		basics.RegisterLegacyAminoCodec(clientCtx.LegacyAmino)
		basics.RegisterInterfaces(clientCtx.InterfaceRegistry)
		for name, basic := range basics {
			manualBasics[name] = basic
			moduleBasicManager[name] = basic
		}
	}

	rootCmd := &cobra.Command{
		Use:           serviceName,
		Short:         "Ark node and client",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// set the default command outputs
			cmd.SetOut(cmd.OutOrStdout())
			cmd.SetErr(cmd.ErrOrStderr())

			clientCtx = clientCtx.WithCmdContext(cmd.Context())
			clientCtx, err := client.ReadPersistentCommandFlags(clientCtx, cmd.Flags())
			if err != nil {
				return err
			}

			clientCtx, err = config.ReadFromClientConfig(clientCtx)
			if err != nil {
				return err
			}

			if err := client.SetCmdClientContextHandler(clientCtx, cmd); err != nil {
				return err
			}

			customAppTemplate, customAppConfig := initAppConfig()
			customCMTConfig := initCometBFTConfig()

			return server.InterceptConfigsPreRunHandler(cmd, customAppTemplate, customAppConfig, customCMTConfig)
		},
	}

	initRootCmd(rootCmd, clientCtx.TxConfig, moduleBasicManager, manualBasics)

	nodeCmds := nodeservice.NewNodeCommands()
	if autoCliOpts.ModuleOptions == nil {
		autoCliOpts.ModuleOptions = make(map[string]*autocliv1.ModuleOptions)
	}
	autoCliOpts.ModuleOptions[nodeCmds.Name()] = nodeCmds.AutoCLIOptions()

	if err := autoCliOpts.EnhanceRootCommand(rootCmd); err != nil {
		panic(err)
	}
	txCmd, _, err := rootCmd.Find([]string{"tx"})
	if err != nil {
		panic(err)
	}
	dressTxCommands(txCmd)

	return rootCmd
}

// dressTxCommands runs after AutoCLI expansion, setting Ark gas defaults and wrapping transaction
// builders with fee pricing. Utilities acting on already-built transactions keep their SDK fee
// flags. See cmd/arkd/README.md.
func dressTxCommands(cmd *cobra.Command) {
	if f := cmd.Flags().Lookup(flags.FlagGasAdjustment); f != nil {
		value := strconv.FormatFloat(appclient.DefaultGasAdjustment, 'f', -1, 64)
		f.DefValue = value
		if err := f.Value.Set(value); err != nil {
			panic(err)
		}
	}
	if _, manual := cmd.Annotations[manualFeesAnnotation]; !manual {
		if f := cmd.Flags().Lookup(flags.FlagTip); f != nil {
			f.Usage = appclient.TipFlagUsage
		}
		if cmd.Flags().Lookup(flags.FlagFees) != nil {
			appclient.PriceTransactions(cmd)
		}
	}
	for _, sub := range cmd.Commands() {
		dressTxCommands(sub)
	}
}

// ProvideClientContext is exported because depinject accepts only exported
// providers; nothing outside the package calls it.
func ProvideClientContext(
	appCodec codec.Codec,
	interfaceRegistry codectypes.InterfaceRegistry,
	txConfigOpts authtx.ConfigOptions,
	legacyAmino *codec.LegacyAmino,
) client.Context {
	clientCtx := client.Context{}.
		WithCodec(appCodec).
		WithInterfaceRegistry(interfaceRegistry).
		WithLegacyAmino(legacyAmino).
		WithInput(os.Stdin).
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithHomeDir(app.DefaultNodeHome).
		WithViper("") // uses by default the binary name as prefix

	txConfig, err := authtx.NewTxConfigWithOptions(clientCtx.Codec, txConfigOpts)
	if err != nil {
		panic(err)
	}

	return clientCtx.WithTxConfig(txConfig)
}

// unwiredWasmKeeper stands in for the Wasm keeper the appointing modules read
// the contract store through, in this graph that builds no state. Reading it
// is a wiring error.
type unwiredWasmKeeper struct{}

func (unwiredWasmKeeper) HasContractInfo(context.Context, sdk.AccAddress) bool {
	panic("contract store read from a graph with no state")
}
