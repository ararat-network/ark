package cli

import (
	"strings"

	marketv1 "noah/api/noah/market/v1"
	"noah/x/market/types"

	"github.com/spf13/cobra"

	base "cosmossdk.io/api/cosmos/base/v1beta1"
	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	feeutils "github.com/classic-terra/core/custom/auth/client/utils"
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
		Use:   "swap [offer-coin] [ask-denom] [to-address]",
		Args:  cobra.RangeArgs(2, 3),
		Short: "Atomically swap currencies at their target exchange rate",
		Long: strings.TrimSpace(`
Swap the offer-coin to the ask-denom currency at the oracle's effective exchange rate.

$ noahd market swap "1000ukrw" "uusd"

The to-address can be specified. A default to-address is trader.

$ noahd market swap "1000ukrw" "uusd" "noah1..."
`),
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

			offerCoinStr := args[0]
			askDenom := args[1]
			fromAddress := clientCtx.GetFromAddress()

			offerCoin, err := sdk.ParseCoinNormalized(offerCoinStr)
			if err != nil {
				return err
			}
			if offerCoin.Amount.LTE(math.ZeroInt()) || offerCoin.Amount.BigInt().BitLen() > 100 {
				return sdkerrors.Wrap(errortypes.ErrInvalidCoins, offerCoin.String())
			}
			if offerCoin.Denom == askDenom {
				return sdkerrors.Wrap(types.ErrRecursiveSwap, askDenom)
			}

			var msg sdk.Msg
			if len(args) == 3 {
				toAddress, err := sdk.AccAddressFromBech32(args[2])
				if err != nil {
					return sdkerrors.Wrapf(errortypes.ErrInvalidAddress, "Invalid to address (%s)", err)
				}

				msg = &marketv1.MsgSwapSend{
					FromAddress: fromAddress.String(),
					ToAddress:   toAddress.String(),
					OfferCoin: &base.Coin{
						Denom:  offerCoin.Denom,
						Amount: offerCoin.Amount.String(),
					},
					AskDenom: askDenom,
				}

				if !clientCtx.GenerateOnly && txf.Fees().IsZero() {
					// estimate tax and gas
					stdFee, err := feeutils.ComputeFeesWithCmd(clientCtx, cmd.Flags(), msg)
					if err != nil {
						return err
					}

					// override gas and fees
					txf = txf.
						WithFees(stdFee.Amount.String()).
						WithGas(stdFee.Gas).
						WithSimulateAndExecute(false).
						WithGasPrices("")
				}
			} else {
				msg = &marketv1.MsgSwap{
					Trader: fromAddress.String(),
					OfferCoin: &base.Coin{
						Denom:  offerCoin.Denom,
						Amount: offerCoin.Amount.String(),
					},
					AskDenom: askDenom,
				}
			}

			// build and sign the transaction, then broadcast to Tendermint
			return tx.GenerateOrBroadcastTxWithFactory(clientCtx, txf, msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
