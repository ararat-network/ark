package types

import (
	"context"

	"google.golang.org/grpc"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	transporttypes "ark/oracle/types"
	oracletypes "ark/x/oracle/types"
)

// OracleKeeper defines the interface that must be fulfilled by the oracle keeper. This
// interface is utilised by the PreBlock handler to write oracle data to state for the
// supported assets.
type OracleKeeper interface {
	GetParams(ctx context.Context) (oracletypes.Params, error)
	SetExchangeRateWithEvent(ctx context.Context, exchangeRate oracletypes.ExchangeRate) error
	RecordVoteAccounting(ctx context.Context, validator sdk.ConsAddress, scoreWeight math.Int, missed bool) error
	GetVoteTargets(ctx context.Context) ([]string, error)
	SyncVoteTargets(ctx context.Context, oldVoteTargets []string) error
}

// OracleClient defines the interface that must be fulfilled by the connect client.
// This interface is utilised by the vote extension handler to fetch prices.
type OracleClient interface {
	Prices(ctx context.Context, in *transporttypes.OraclePricesRequest, opts ...grpc.CallOption) (*transporttypes.OraclePricesResponse, error)
}
