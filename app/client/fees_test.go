package client

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	sdkclient "github.com/cosmos/cosmos-sdk/client"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	treasurytypes "ark/x/treasury/types"
)

type taxQuerierFunc func(
	context.Context,
	*treasurytypes.QueryComputeTaxRequest,
	...grpc.CallOption,
) (*treasurytypes.QueryComputeTaxResponse, error)

func (f taxQuerierFunc) ComputeTax(
	ctx context.Context,
	req *treasurytypes.QueryComputeTaxRequest,
	opts ...grpc.CallOption,
) (*treasurytypes.QueryComputeTaxResponse, error) {
	return f(ctx, req, opts...)
}

func TestWithAutomaticFees(t *testing.T) {
	msg := banktypes.NewMsgSend(
		sdk.AccAddress("sender"),
		sdk.AccAddress("recipient"),
		sdk.NewCoins(sdk.NewInt64Coin("asdr", 100)),
	)
	tax := sdk.NewCoins(sdk.NewInt64Coin("asdr", 3))

	t.Run("fixed gas adds tax and gas fee", func(t *testing.T) {
		querier := taxQuerierFunc(func(
			_ context.Context,
			req *treasurytypes.QueryComputeTaxRequest,
			_ ...grpc.CallOption,
		) (*treasurytypes.QueryComputeTaxResponse, error) {
			require.Len(t, req.Messages, 1)
			require.Equal(t, "/cosmos.bank.v1beta1.MsgSend", req.Messages[0].TypeUrl)
			return &treasurytypes.QueryComputeTaxResponse{Tax: tax}, nil
		})
		txf := clienttx.Factory{}.
			WithGas(100).
			WithGasPrices("0.25anoah")
		got, err := withAutomaticFees(sdkclient.Context{}, txf, querier, msg)

		require.NoError(t, err)
		require.Equal(t, uint64(100), got.Gas())
		require.Equal(t, sdk.NewCoins(
			sdk.NewInt64Coin("anoah", 25),
			sdk.NewInt64Coin("asdr", 3),
		), got.Fees())
		require.True(t, got.GasPrices().IsZero())
		require.False(t, got.SimulateAndExecute())
	})

	t.Run("explicit fees are left unchanged", func(t *testing.T) {
		querier := taxQuerierFunc(func(
			context.Context,
			*treasurytypes.QueryComputeTaxRequest,
			...grpc.CallOption,
		) (*treasurytypes.QueryComputeTaxResponse, error) {
			t.Fatal("tax should not be queried when fees are explicit")
			return nil, nil
		})

		txf := clienttx.Factory{}.WithFees("7anoah")
		got, err := withAutomaticFees(
			sdkclient.Context{Offline: true},
			txf,
			querier,
			msg,
		)

		require.NoError(t, err)
		require.Equal(t, txf.Fees(), got.Fees())
	})

	t.Run("offline mode requires explicit fees", func(t *testing.T) {
		_, err := withAutomaticFees(
			sdkclient.Context{Offline: true},
			clienttx.Factory{},
			nil,
			msg,
		)

		require.ErrorContains(t, err, "requires online mode")
	})

	t.Run("query errors are returned", func(t *testing.T) {
		queryErr := errors.New("query failed")
		querier := taxQuerierFunc(func(
			context.Context,
			*treasurytypes.QueryComputeTaxRequest,
			...grpc.CallOption,
		) (*treasurytypes.QueryComputeTaxResponse, error) {
			return nil, queryErr
		})

		_, err := withAutomaticFees(
			sdkclient.Context{},
			clienttx.Factory{},
			querier,
			msg,
		)

		require.ErrorIs(t, err, queryErr)
	})
}
