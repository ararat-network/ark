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
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/ararat-network/ark/app/genesis"
	"github.com/ararat-network/ark/pkg/chain"
)

// flagFounder names the operator whose seat is the founder's; flagAnyCount
// lifts the launch count bounds for a testnet.
const (
	flagFounder  = "founder"
	flagAnyCount = "any-count"
)

// newAddValidatorSeatsCmd returns genesis add-validator-seats: one seat per
// genesis validator, the founder's at half the others' sum, and the founder's
// grant and stream, granted in one write, run once before gentx collection.
// The seats vest from the file's genesis_time, which assembly sets first. The
// seat policy and the launch count bounds are fixed in pkg/chain.
func newAddValidatorSeatsCmd(defaultNodeHome string, addressCodec address.Codec) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-validator-seats --founder <operator-address> [operator-address]...",
		Short: "Grant every validator seat in genesis.json",
		Long: fmt.Sprintf(`Grant one validator seat per operator in genesis.json: a continuous vesting
account holding the seat grant, vesting from four to eight years after
genesis_time, a liquid float beside it, both moved out of the community pool,
and both reward targets raised by one seat's share. The founder, named with
--founder and one of the operators, holds half the other seats' sum instead,
a third of genesis bonded stake, on the same schedule from the same pool.
The founder's %dM NOAH grant for the work before launch and %dM NOAH
compensation stream for the work after it are written beside the seats as
governance awards from the contributor pool, clocked from genesis_time.
Supply is unchanged. Set genesis_time in the file first;
every seat vests from it. Run once, naming every genesis validator, between
%d and %d of them unless --%s lifts the bounds for a testnet, up to what the
community pool funds, before gentx collection: a file already seated is
refused, and nothing is written unless every seat passes. The seat policy
and the count bounds are fixed in pkg/chain.`,
			chain.FounderGrantNoah/1_000_000, chain.FounderStreamNoah/1_000_000, chain.MinLaunchSeats, chain.MaxLaunchSeats, flagAnyCount),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			founder, err := cmd.Flags().GetString(flagFounder)
			if err != nil {
				return err
			}
			anyCount, err := cmd.Flags().GetBool(flagAnyCount)
			if err != nil {
				return err
			}
			return runAddValidatorSeats(cmd, args, founder, anyCount, addressCodec)
		},
	}
	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	cmd.Flags().String(flagFounder, "", "The operator whose seat is the founder's, one of the operators named")
	cmd.Flags().Bool(flagAnyCount, false, "Seat any count the community pool funds, for a testnet; a launch stays within the bounds")
	_ = cmd.MarkFlagRequired(flagFounder)
	return cmd
}

func runAddValidatorSeats(cmd *cobra.Command, operatorArgs []string, founderArg string, anyCount bool, addressCodec address.Codec) error {
	clientCtx := client.GetClientContextFromCmd(cmd)
	config := server.GetServerContextFromCmd(cmd).Config
	config.SetRoot(clientCtx.HomeDir)

	if !anyCount && (len(operatorArgs) < chain.MinLaunchSeats || len(operatorArgs) > chain.MaxLaunchSeats) {
		return fmt.Errorf("%d seats named: launch assembly seats between %d and %d, and --%s lifts the bounds for a testnet", len(operatorArgs), chain.MinLaunchSeats, chain.MaxLaunchSeats, flagAnyCount)
	}
	operators := make([]sdk.AccAddress, 0, len(operatorArgs))
	for _, arg := range operatorArgs {
		operator, err := addressCodec.StringToBytes(arg)
		if err != nil {
			return fmt.Errorf("parse operator address %q: %w", arg, err)
		}
		operators = append(operators, operator)
	}
	founder, err := addressCodec.StringToBytes(founderArg)
	if err != nil {
		return fmt.Errorf("parse founder address %q: %w", founderArg, err)
	}
	appGenesis, err := genutiltypes.AppGenesisFromFile(config.GenesisFile())
	if err != nil {
		return err
	}
	var appState map[string]json.RawMessage
	if err := json.Unmarshal(appGenesis.AppState, &appState); err != nil {
		return fmt.Errorf("unmarshal app state: %w", err)
	}
	if err := genesis.AddValidatorSeats(clientCtx.Codec, appState, operators, founder, appGenesis.GenesisTime); err != nil {
		return err
	}
	if appGenesis.AppState, err = json.Marshal(appState); err != nil {
		return fmt.Errorf("marshal app state: %w", err)
	}
	return exportGenesisFile(appGenesis, config.GenesisFile())
}
