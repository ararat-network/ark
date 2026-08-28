package types

import (
	"context"

	"google.golang.org/grpc"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pricefeed/api"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// OracleKeeper exposes the oracle state required by vote-extension creation and
// preblock processing.
type OracleKeeper interface {
	GetParams(ctx context.Context) (oracletypes.Params, error)
	SetExchangeRateWithEvent(ctx context.Context, exchangeRate oracletypes.ExchangeRate) error
	RecordVoteAccounting(ctx context.Context, validator sdk.ConsAddress, rewardWeight math.Int, eligible bool, participated bool) error
	GetFeeds(ctx context.Context, voteHeight int64) (oracletypes.FeedSet, error)
	AdvanceFeeds(ctx context.Context) error
}

// PriceFeedClient fetches prices for the vote-extension handler.
type PriceFeedClient interface {
	Prices(ctx context.Context, in *api.PricesRequest, opts ...grpc.CallOption) (*api.PricesResponse, error)
}
