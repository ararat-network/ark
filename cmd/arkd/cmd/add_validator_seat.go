package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"cosmossdk.io/core/address"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/ararat-network/ark/app/genesis"
)

// newAddValidatorSeatCmd returns genesis add-validator-seat: one equal seat
// granted from the community pool, run once per genesis validator before
// gentx collection. The seat policy is fixed in pkg/chain.
func newAddValidatorSeatCmd(defaultNodeHome string, addressCodec address.Codec) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-validator-seat [operator-address]",
		Short: "Grant one equal validator seat in genesis.json",
		Long: `Grant one equal validator seat in genesis.json: a permanently locked account at
the operator holding the seat grant, a liquid float beside it, both moved out of
the community pool, and both reward targets raised by one seat's share. Supply
is unchanged. Run once per genesis validator before gentx collection; the seat
policy is fixed in pkg/chain.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAddValidatorSeat(cmd, args[0], addressCodec)
		},
	}
	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	return cmd
}

func runAddValidatorSeat(cmd *cobra.Command, operatorArg string, addressCodec address.Codec) error {
	clientCtx := client.GetClientContextFromCmd(cmd)
	config := server.GetServerContextFromCmd(cmd).Config
	config.SetRoot(clientCtx.HomeDir)

	operator, err := addressCodec.StringToBytes(operatorArg)
	if err != nil {
		return fmt.Errorf("parse operator address %q: %w", operatorArg, err)
	}
	appGenesis, err := genutiltypes.AppGenesisFromFile(config.GenesisFile())
	if err != nil {
		return err
	}
	var appState map[string]json.RawMessage
	if err := json.Unmarshal(appGenesis.AppState, &appState); err != nil {
		return fmt.Errorf("unmarshal app state: %w", err)
	}
	if err := genesis.AddValidatorSeat(clientCtx.Codec, appState, operator); err != nil {
		return err
	}
	if appGenesis.AppState, err = json.Marshal(appState); err != nil {
		return fmt.Errorf("marshal app state: %w", err)
	}
	return genutil.ExportGenesisFile(appGenesis, config.GenesisFile())
}
