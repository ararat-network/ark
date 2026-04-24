package cli

import (
	"strings"

	"github.com/spf13/cobra"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

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
		GetCmdDelegateFeederPermission(),
		GetCmdAggregateExchangeRatePrevote(),
		GetCmdAggregateExchangeRateVote(),
	)

	return oracleTxCmd
}

// GetCmdDelegateFeederPermission will create a feeder permission delegation tx and sign it with the given key.
func GetCmdDelegateFeederPermission() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-feeder [feeder]",
		Args:  cobra.ExactArgs(1),
		Short: "Delegate the permission to vote for the oracle to an address",
		Long: strings.TrimSpace(`
Delegate the permission to submit exchange rate votes for the oracle to an address.

Delegation can keep your validator operator key offline and use a separate replaceable key online.

$ noahd tx oracle set-feeder noah1...

where "noah1..." is the address you want to delegate your voting rights to.
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			// Get from address
			voter := clientCtx.GetFromAddress()

			// The address the right is being delegated from
			validator := sdk.ValAddress(voter)

			feederStr := args[0]
			feeder, err := sdk.AccAddressFromBech32(feederStr)
			if err != nil {
				return sdkerrors.Wrapf(errortypes.ErrInvalidAddress, "invalid feeder address %q: %v", feederStr, err)
			}

			msgs := []sdk.Msg{types.NewMsgDelegateFeedConsent(validator, feeder)}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msgs...)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// GetCmdAggregateExchangeRatePrevote will create a aggregateExchangeRatePrevote tx and sign it with the given key.
func GetCmdAggregateExchangeRatePrevote() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aggregate-prevote [salt] [exchange-rates] [validator]",
		Args:  cobra.RangeArgs(2, 3),
		Short: "Submit an oracle aggregate prevote for the exchange rates of Luna",
		Long: strings.TrimSpace(`
Submit an oracle aggregate prevote for the exchange rates of Luna denominated in multiple denoms.
The purpose of aggregate prevote is to hide aggregate exchange rate vote with hash which is formatted 
as hex string in SHA256("{salt}:{exchange_rate}{denom},...,{exchange_rate}{denom}:{voter}")

# Aggregate Prevote
$ noahd tx oracle aggregate-prevote 1234 8888.0ukrw,1.243uusd,0.99usdr 

where "ukrw,uusd,usdr" is the denominating currencies, and "8888.0,1.243,0.99" is the exchange rates of micro Luna in micro denoms from the voter's point of view.

If voting from a voting delegate, set "validator" to the address of the validator to vote on behalf of:
$ noahd tx oracle aggregate-prevote 1234 8888.0ukrw,1.243uusd,0.99usdr noahvaloper1...
`),
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

// GetCmdAggregateExchangeRateVote will create a aggregateExchangeRateVote tx and sign it with the given key.
func GetCmdAggregateExchangeRateVote() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aggregate-vote [salt] [exchange-rates] [validator]",
		Args:  cobra.RangeArgs(2, 3),
		Short: "Submit an oracle aggregate vote for the exchange_rates of Luna",
		Long: strings.TrimSpace(`
Submit a aggregate vote for the exchange_rates of Luna w.r.t the input denom. Companion to a prevote submitted in the previous vote period. 

$ noahd tx oracle aggregate-vote 1234 8888.0ukrw,1.243uusd,0.99usdr 

where "ukrw,uusd,usdr" is the denominating currencies, and "8888.0,1.243,0.99" is the exchange rates of micro Luna in micro denoms from the voter's point of view.

"salt" should match the salt used to generate the SHA256 hex in the aggregated pre-vote. 

If voting from a voting delegate, set "validator" to the address of the validator to vote on behalf of:
$ noahd tx oracle aggregate-vote 1234 8888.0ukrw,1.243uusd,0.99usdr noahvaloper1....
`),
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
