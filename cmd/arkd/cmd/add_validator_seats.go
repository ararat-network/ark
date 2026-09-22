package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"cosmossdk.io/core/address"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/ararat-network/ark/app/genesis"
)

// newAddValidatorSeatsCmd returns genesis add-validator-seats: one equal seat
// per genesis validator, granted from the community pool in one write, run
// once before gentx collection. The seats vest from the file's genesis_time,
// which assembly sets first. The seat policy is fixed in pkg/chain.
func newAddValidatorSeatsCmd(defaultNodeHome string, addressCodec address.Codec) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-validator-seats [operator-address]...",
		Short: "Grant every equal validator seat in genesis.json",
		Long: `Grant one equal validator seat per operator in genesis.json: a continuous
vesting account holding the seat grant, vesting from four to ten years after
genesis_time, a liquid float beside it, both moved out of the community pool,
and both reward targets raised by one seat's share. Supply is unchanged. Set
genesis_time in the file first; every seat vests from it. Run once, naming
every genesis validator, before gentx collection; nothing is written unless
every seat passes. The seat policy is fixed in pkg/chain.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAddValidatorSeats(cmd, args, addressCodec)
		},
	}
	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	return cmd
}

func runAddValidatorSeats(cmd *cobra.Command, operatorArgs []string, addressCodec address.Codec) error {
	clientCtx := client.GetClientContextFromCmd(cmd)
	config := server.GetServerContextFromCmd(cmd).Config
	config.SetRoot(clientCtx.HomeDir)

	operators := make([]sdk.AccAddress, 0, len(operatorArgs))
	for _, arg := range operatorArgs {
		operator, err := addressCodec.StringToBytes(arg)
		if err != nil {
			return fmt.Errorf("parse operator address %q: %w", arg, err)
		}
		operators = append(operators, operator)
	}
	appGenesis, err := genutiltypes.AppGenesisFromFile(config.GenesisFile())
	if err != nil {
		return err
	}
	var appState map[string]json.RawMessage
	if err := json.Unmarshal(appGenesis.AppState, &appState); err != nil {
		return fmt.Errorf("unmarshal app state: %w", err)
	}
	if err := genesis.AddValidatorSeats(clientCtx.Codec, appState, operators, appGenesis.GenesisTime); err != nil {
		return err
	}
	if appGenesis.AppState, err = json.Marshal(appState); err != nil {
		return fmt.Errorf("marshal app state: %w", err)
	}
	return genutil.ExportGenesisFile(appGenesis, config.GenesisFile())
}
