package types

import (
	"context"

	"google.golang.org/grpc"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	servertypes "noah/service/servers/oracle/types"
	oracletypes "noah/x/oracle/types"
)

// OracleKeeper defines the interface that must be fulfilled by the oracle keeper. This
// interface is utilised by the PreBlock handler to write oracle data to state for the
// supported assets.
type OracleKeeper interface {
	GetParams(ctx context.Context) (oracletypes.Params, error)
	SetExchangeRateWithEvent(ctx context.Context, exchangeRate oracletypes.ExchangeRate) error
	AddScoreWeight(ctx context.Context, validator sdk.ConsAddress, scoreWeight uint64) error
	IncrementMissCount(ctx context.Context, validator sdk.ConsAddress) error
	GetVoteTargets(ctx context.Context) (map[string]math.LegacyDec, error)
	SyncTobinTax(ctx context.Context, oldTobinTaxes map[string]math.LegacyDec) error
}

// OracleClient defines the interface that must be fulfilled by the connect client.
// This interface is utilised by the vote extension handler to fetch prices.
type OracleClient interface {
	Prices(ctx context.Context, in *servertypes.OraclePricesRequest, opts ...grpc.CallOption) (*servertypes.OraclePricesResponse, error)
}
