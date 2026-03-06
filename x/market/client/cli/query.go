package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"

	marketv1 "noah/api/noah/market/v1"
	"noah/x/market/types"
)

// GetQueryCmd returns the cli query commands for this module
func GetQueryCmd() *cobra.Command {
	marketQueryCmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the market module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	marketQueryCmd.AddCommand(
		GetCmdQuerySwap(),
		GetCmdQueryNoahPoolDelta(),
		GetCmdQueryParams(),
	)

	return marketQueryCmd
}

// GetCmdQuerySwap implements the query swap simulation result command.
func GetCmdQuerySwap() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "swap [offer-coin] [ask-denom]",
		Args:  cobra.ExactArgs(2),
		Short: "Query a quote for a swap operation",
		Long: strings.TrimSpace(`
Query a quote for how many coins can be received in a swap operation. Note; rates are dynamic and can quickly change.

$ noahd query swap 5000000uark usdr
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := marketv1.NewQueryClient(clientCtx)

			// parse offerCoin
			offerCoinStr := args[0]
			_, err = sdk.ParseCoinNormalized(offerCoinStr)
			if err != nil {
				return err
			}

			askDenom := args[1]

			res, err := queryClient.Swap(context.Background(),
				&marketv1.QuerySwapRequest{OfferCoin: offerCoinStr, AskDenom: askDenom},
			)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GetCmdQueryNoahPoolDelta implements the query mint pool delta command.
func GetCmdQueryNoahPoolDelta() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "noah-pool-delta",
		Args:  cobra.NoArgs,
		Short: "Query noah pool delta",
		Long: `Query noah pool delta, which is usdr amount used for swap operation from the NoahPool.
It can be negative if the market wants more Noah than Ark, and vice versa if the market wants more Ark.

$ noahd query market noah-pool-delta
	`,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := marketv1.NewQueryClient(clientCtx)

			res, err := queryClient.NoahPoolDelta(context.Background(),
				&marketv1.QueryNoahPoolDeltaRequest{},
			)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GetCmdQueryParams implements the query params command.
func GetCmdQueryParams() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Args:  cobra.NoArgs,
		Short: "Query the current market params",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := marketv1.NewQueryClient(clientCtx)

			res, err := queryClient.Params(context.Background(), &marketv1.QueryParamsRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
