package client

import (
	"context"
	"fmt"
	"math/big"

	"google.golang.org/grpc"

	sdkmath "cosmossdk.io/math"

	sdkclient "github.com/cosmos/cosmos-sdk/client"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	treasurytypes "ark/x/treasury/types"
)

type taxQuerier interface {
	ComputeTax(
		context.Context,
		*treasurytypes.QueryComputeTaxRequest,
		...grpc.CallOption,
	) (*treasurytypes.QueryComputeTaxResponse, error)
}

// WithAutomaticFees returns a transaction factory whose fees cover the current
// stability tax and any gas fee derived from the configured gas prices. An
// explicitly configured fee is left unchanged.
func WithAutomaticFees(
	clientCtx sdkclient.Context,
	txf clienttx.Factory,
	msgs ...sdk.Msg,
) (clienttx.Factory, error) {
	return withAutomaticFees(
		clientCtx,
		txf,
		treasurytypes.NewQueryClient(clientCtx),
		msgs...,
	)
}

func withAutomaticFees(
	clientCtx sdkclient.Context,
	txf clienttx.Factory,
	querier taxQuerier,
	msgs ...sdk.Msg,
) (clienttx.Factory, error) {
	if !txf.Fees().IsZero() {
		return txf, nil
	}
	if clientCtx.Offline {
		return txf, fmt.Errorf("automatic stability tax calculation requires online mode; provide --fees")
	}

	packedMsgs := make([]*codectypes.Any, len(msgs))
	for i, msg := range msgs {
		packed, err := codectypes.NewAnyWithValue(msg)
		if err != nil {
			return txf, fmt.Errorf("packing message %d for stability tax query: %w", i, err)
		}
		packedMsgs[i] = packed
	}

	response, err := querier.ComputeTax(
		clientCtx.GetCmdContextWithFallback(),
		&treasurytypes.QueryComputeTaxRequest{Messages: packedMsgs},
	)
	if err != nil {
		return txf, fmt.Errorf("querying stability tax: %w", err)
	}
	if response == nil {
		return txf, fmt.Errorf("querying stability tax: empty response")
	}

	tax := response.Tax
	gas := txf.Gas()
	gasPrices := txf.GasPrices()
	if txf.SimulateAndExecute() {
		simulationFactory := txf.
			WithFees(tax.String()).
			WithGasPrices("")

		preparedFactory, err := simulationFactory.Prepare(clientCtx)
		if err != nil {
			return simulationFactory, fmt.Errorf("estimating gas: %w", err)
		}
		_, gas, err = clienttx.CalculateGas(clientCtx, preparedFactory, msgs...)
		if err != nil {
			return preparedFactory, fmt.Errorf("estimating gas: %w", err)
		}
		txf = preparedFactory
	}

	var gasFee sdk.Coins
	if !gasPrices.IsZero() && gas != 0 {
		gasLimit := sdkmath.LegacyNewDecFromBigInt(new(big.Int).SetUint64(gas))
		gasFee = make(sdk.Coins, len(gasPrices))
		for i, gasPrice := range gasPrices {
			amount := gasPrice.Amount.Mul(gasLimit).Ceil().RoundInt()
			gasFee[i] = sdk.NewCoin(gasPrice.Denom, amount)
		}
		gasFee = gasFee.Sort()
	}
	fees := tax.Add(gasFee...)
	return txf.
		WithFees(fees.String()).
		WithGas(gas).
		WithGasPrices("").
		WithSimulateAndExecute(false), nil
}
