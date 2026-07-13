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

	// feeutils "ark/custom/auth/client/utils"
	"ark/x/market/types"
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
		GetSwapCmd(),
	)

	return marketTxCmd
}

// GetSwapCmd will create and send a MsgSwap
func GetSwapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "swap [offer-coin] [ask-denom] [minimum-receive] [to-address]",
		Args:  cobra.RangeArgs(3, 4),
		Short: "Atomically swap currencies at their target exchange rate",
		Long: strings.TrimSpace(`
Swap the offer-coin to the ask-denom currency at the oracle's effective exchange rate.
The swap fails unless it returns at least minimum-receive.
If to-address is omitted, the swapped coins are sent back to the trader.
If to-address is provided, the swapped coins are sent to that address.
`),
		Example: strings.TrimSpace(fmt.Sprintf(`
%s tx market swap "1000ukrw" "uusd" "1uusd"
%s tx market swap "1000ukrw" "uusd" "1uusd" "ark1..."
`, version.AppName, version.AppName)),
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

			var msg sdk.Msg
			if len(args) == 4 {
				toAddress, err := sdk.AccAddressFromBech32(args[3])
				if err != nil {
					return err
				}

				swapSendMsg := types.NewMsgSwapSend(fromAddress, toAddress, offerCoin, askDenom, minimumReceive)
				msg = swapSendMsg

				if !clientCtx.GenerateOnly && txf.Fees().IsZero() {
					// estimate tax and gas
					// stdFee, err := feeutils.ComputeFeesWithCmd(clientCtx, cmd.Flags(), msg)
					// if err != nil {
					// 	return err
					// }

					// override gas and fees
					txf = txf.
						// WithFees(stdFee.Amount.String()).
						// WithGas(stdFee.Gas).
						WithSimulateAndExecute(false).
						WithGasPrices("")
				}
			} else {
				swapMsg := types.NewMsgSwap(fromAddress, offerCoin, askDenom, minimumReceive)
				msg = swapMsg
			}

			// build and sign the transaction, then broadcast to Tendermint
			return tx.GenerateOrBroadcastTxWithFactory(clientCtx, txf, msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
