package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/version"

	"noah/x/oracle/types"
)

// GetTxCmd returns the transaction commands for this module
func GetTxCmd() *cobra.Command {
	oracleTxCmd := &cobra.Command{
		Use:                        "oracle",
		Short:                      "Oracle transaction subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	oracleTxCmd.AddCommand(
		GetCmdPrevote(),
		GetCmdVote(),
	)

	return oracleTxCmd
}

// GetCmdPrevote will create a Prevote tx and sign it with the given key.
func GetCmdPrevote() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prevote [salt] [exchange-rates] [validator]",
		Args:  cobra.RangeArgs(2, 3),
		Short: "Submit an oracle prevote",
		Long: strings.TrimSpace(`
Submit a hashed oracle prevote for one or more exchange rates.
The hash is SHA256("{salt}:{exchange_rate}{denom},...,{exchange_rate}{denom}:{validator}").
Use the same salt and exchange rates when submitting the matching vote in the next vote period.
Exchange rates use decimal coin format, such as 8888.0ukrw,1.243uusd,0.99usdr.
When voting as a feeder, pass the validator operator address as the optional validator argument.
`),
		Example: strings.TrimSpace(fmt.Sprintf(`
%s tx oracle prevote 1234 8888.0ukrw,1.243uusd,0.99usdr
%s tx oracle prevote 1234 8888.0ukrw,1.243uusd,0.99usdr noahvaloper1...
`, version.AppName, version.AppName)),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			salt := args[0]
			exchangeRatesStr := args[1]
			_, err = types.ParseExchangeRates(exchangeRatesStr)
			if err != nil {
				return sdkerrors.Wrapf(errortypes.ErrInvalidCoins, "parsing exchange rates %q: %v", exchangeRatesStr, err)
			}

			// Get from address
			voter := clientCtx.GetFromAddress()

			// By default the voter is voting on behalf of itself
			validator := sdk.ValAddress(voter)

			// Override validator if validator is given
			if len(args) == 3 {
				parsedVal, err := sdk.ValAddressFromBech32(args[2])
				if err != nil {
					return sdkerrors.Wrapf(errortypes.ErrInvalidAddress, "invalid validator address %q: %v", args[2], err)
				}
				validator = parsedVal
			}

			hash := types.GetVoteHash(salt, exchangeRatesStr, validator)
			msgs := []sdk.Msg{types.NewMsgPrevote(hash, voter, validator)}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msgs...)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// GetCmdVote will create a Vote tx and sign it with the given key.
func GetCmdVote() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vote [salt] [exchange-rates] [validator]",
		Args:  cobra.RangeArgs(2, 3),
		Short: "Submit an oracle vote",
		Long: strings.TrimSpace(`
Reveal an oracle vote that matches a prevote submitted in the previous vote period.
The salt and exchange rates must match the values used to build the prevote hash.
Exchange rates use decimal coin format, such as 8888.0ukrw,1.243uusd,0.99usdr.
When voting as a feeder, pass the validator operator address as the optional validator argument.
`),
		Example: strings.TrimSpace(fmt.Sprintf(`
%s tx oracle vote 1234 8888.0ukrw,1.243uusd,0.99usdr
%s tx oracle vote 1234 8888.0ukrw,1.243uusd,0.99usdr noahvaloper1...
`, version.AppName, version.AppName)),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			salt := args[0]
			if len(salt) > 4 || len(salt) < 1 {
				return sdkerrors.Wrap(types.ErrInvalidSaltLength, "salt length must be [1, 4]")
			}

			exchangeRatesStr := args[1]
			if l := len(exchangeRatesStr); l == 0 {
				return sdkerrors.Wrap(errortypes.ErrUnknownRequest, "must provide at least one oracle exchange rate")
			} else if l > 4096 {
				return sdkerrors.Wrap(errortypes.ErrInvalidRequest, "exchange rates string can not exceed 4096 characters")
			}
			exchangeRates, err := types.ParseExchangeRates(exchangeRatesStr)
			if err != nil {
				return sdkerrors.Wrapf(errortypes.ErrInvalidCoins, "parsing exchange rates %q: %v", exchangeRatesStr, err)
			}
			for _, exchangeRate := range exchangeRates {
				// Check overflow bit length
				if exchangeRate.Rate.BigInt().BitLen() > 255+math.LegacyDecimalPrecisionBits {
					return sdkerrors.Wrap(types.ErrInvalidExchangeRate, "overflow")
				}
			}

			// Get from address
			voter := clientCtx.GetFromAddress()

			// By default the voter is voting on behalf of itself
			validator := sdk.ValAddress(voter)

			// Override validator if validator is given
			if len(args) == 3 {
				parsedVal, err := sdk.ValAddressFromBech32(args[2])
				if err != nil {
					return sdkerrors.Wrapf(errortypes.ErrInvalidAddress, "invalid validator address %q: %v", args[2], err)
				}
				validator = parsedVal
			}

			msgs := []sdk.Msg{types.NewMsgVote(salt, exchangeRatesStr, voter, validator)}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msgs...)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
