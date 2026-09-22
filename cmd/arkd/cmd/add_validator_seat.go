package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"cosmossdk.io/core/address"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/ararat-network/ark/app/genesis"
)

const flagGenesisTime = "genesis-time"

// newAddValidatorSeatCmd returns genesis add-validator-seat: one equal seat
// granted from the community pool, run once per genesis validator before
// gentx collection. The seat vests from the file's genesis_time, which
// --genesis-time sets when the file has none. The seat policy is fixed in
// pkg/chain.
func newAddValidatorSeatCmd(defaultNodeHome string, addressCodec address.Codec) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-validator-seat [operator-address]",
		Short: "Grant one equal validator seat in genesis.json",
		Long: `Grant one equal validator seat in genesis.json: a continuous vesting account at
the operator holding the seat grant, vesting from four to ten years after
genesis_time, a liquid float beside it, both moved out of the community pool,
and both reward targets raised by one seat's share. Supply is unchanged. The
file's genesis_time must be set first, by --genesis-time or by hand, and every
seat vests from the same one. Run once per genesis validator before gentx
collection; the seat policy is fixed in pkg/chain.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAddValidatorSeat(cmd, args[0], addressCodec)
		},
	}
	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	cmd.Flags().String(flagGenesisTime, "", "The launch time the seat vests from, RFC 3339; sets genesis_time when the file has none")
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
	if flagValue, _ := cmd.Flags().GetString(flagGenesisTime); flagValue != "" {
		genesisTime, err := time.Parse(time.RFC3339, flagValue)
		if err != nil {
			return fmt.Errorf("parse --%s %q: %w", flagGenesisTime, flagValue, err)
		}
		if !appGenesis.GenesisTime.IsZero() && !appGenesis.GenesisTime.Equal(genesisTime) {
			return fmt.Errorf("genesis_time is already %s, not %s: every seat vests from the same time",
				appGenesis.GenesisTime.Format(time.RFC3339), genesisTime.Format(time.RFC3339))
		}
		appGenesis.GenesisTime = genesisTime
	}
	if appGenesis.GenesisTime.IsZero() {
		return errors.New("genesis_time is unset: pass --" + flagGenesisTime + " or set it in the file, since the seat vests from it")
	}
	var appState map[string]json.RawMessage
	if err := json.Unmarshal(appGenesis.AppState, &appState); err != nil {
		return fmt.Errorf("unmarshal app state: %w", err)
	}
	if err := genesis.AddValidatorSeat(clientCtx.Codec, appState, operator, appGenesis.GenesisTime); err != nil {
		return err
	}
	if appGenesis.AppState, err = json.Marshal(appState); err != nil {
		return fmt.Errorf("marshal app state: %w", err)
	}
	return genutil.ExportGenesisFile(appGenesis, config.GenesisFile())
}
