package types

import (
	"context"

	"google.golang.org/grpc"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	transporttypes "ark/oracle/types"
	oracletypes "ark/x/oracle/types"
)

// OracleKeeper exposes the oracle state required by vote-extension creation and
// preblock processing.
type OracleKeeper interface {
	GetParams(ctx context.Context) (oracletypes.Params, error)
	SetExchangeRateWithEvent(ctx context.Context, exchangeRate oracletypes.ExchangeRate) error
	RecordVoteAccounting(ctx context.Context, validator sdk.ConsAddress, rewardWeight math.Int, missed bool) error
	GetVoteTargets(ctx context.Context, voteHeight int64) (oracletypes.VoteTargetSet, error)
	AdvanceVoteTargets(ctx context.Context) error
}

// OracleClient fetches prices for the vote-extension handler.
type OracleClient interface {
	Prices(ctx context.Context, in *transporttypes.OraclePricesRequest, opts ...grpc.CallOption) (*transporttypes.OraclePricesResponse, error)
}
