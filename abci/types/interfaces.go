package types

import (
	"context"

	servertypes "github.com/skip-mev/connect/v2/service/servers/oracle/types"
	"google.golang.org/grpc"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oracletypes "noah/x/oracle/types"
)

// OracleKeeper defines the interface that must be fulfilled by the oracle keeper. This
// interface is utilised by the PreBlock handler to write oracle data to state for the
// supported assets.
type OracleKeeper interface {
	GetParams(ctx context.Context) (oracletypes.Params, error)
	SetExchangeRateWithEvent(ctx context.Context, exchangeRate oracletypes.ExchangeRate) error
	AddScoreWeight(ctx context.Context, validator sdk.ValAddress, scoreWeight uint64) error
	IncrementMissCount(ctx context.Context, validator sdk.ValAddress) error
}

// OracleClient defines the interface that must be fulfilled by the connect client.
// This interface is utilised by the vote extension handler to fetch prices.
type OracleClient interface {
	Prices(ctx context.Context, in *servertypes.QueryPricesRequest, opts ...grpc.CallOption) (*servertypes.QueryPricesResponse, error)
}
