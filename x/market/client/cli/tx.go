package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/version"

	appclient "github.com/ararat-network/ark/app/client"
	"github.com/ararat-network/ark/x/market/types"
)

// GetTxCmd returns the transaction commands for this module
func GetTxCmd() *cobra.Command {
	marketTxCmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Market transaction subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	marketTxCmd.AddCommand(
		GetSwapSendCmd(),
	)

	return marketTxCmd
}

// GetSwapSendCmd creates and sends a MsgSwapSend.
func GetSwapSendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "swap-send [offer-coin] [ask-denom] [minimum-receive] [to-address]",
		Args:  cobra.ExactArgs(4),
		Short: "Atomically swap currencies and send the output to another address",
		Long: strings.TrimSpace(`
Swap the offer-coin to the ask-denom currency at the oracle's effective exchange rate.
The swap fails unless it returns at least minimum-receive, and the resulting coins are
sent to to-address.
`),
		Example: strings.TrimSpace(fmt.Sprintf(`
%s tx market swap-send "1000000000000000000akrw" "ausd" "1ausd" "ararat1..."
`, version.AppName)),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			// Generate transaction factory for gas simulation
			txf, err := tx.NewFactoryCLI(clientCtx, cmd.Flags())
			if err != nil {
				return err
			}

			offerCoin, err := sdk.ParseCoinNormalized(args[0])
			if err != nil {
				return err
			}

			askDenom := args[1]
			minimumReceive, err := sdk.ParseCoinNormalized(args[2])
			if err != nil {
				return err
			}
			fromAddress := clientCtx.GetFromAddress()

			toAddress, err := sdk.AccAddressFromBech32(args[3])
			if err != nil {
				return err
			}

			msg := &types.MsgSwapSend{
				FromAddress:    fromAddress.String(),
				ToAddress:      toAddress.String(),
				OfferCoin:      offerCoin,
				AskDenom:       askDenom,
				MinimumReceive: minimumReceive,
			}
			if !clientCtx.GenerateOnly && txf.Fees().IsZero() {
				txf, err = appclient.WithAutomaticFees(clientCtx, txf, msg)
				if err != nil {
					return err
				}
			}

			// build and sign the transaction, then broadcast to Tendermint
			return tx.GenerateOrBroadcastTxWithFactory(clientCtx, txf, msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
