// Package pagination bounds the work a list query may ask of a node.
package pagination

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cosmos/cosmos-sdk/types/query"
)

// MaxLimit is the most entries one page returns, and the page size when a request sets none.
const MaxLimit uint64 = 100

// Bounded checks a list query's page request and returns the one to serve: key pagination, forward
// or reverse, with no total counting and at most MaxLimit entries. Queries are free to their caller
// and the records behind them mostly only grow, so an offset or a total would cost a read per entry
// skipped or counted. The SDK's own paginator sets no ceiling. The caller's request is not modified.
func Bounded(p *query.PageRequest) (*query.PageRequest, error) {
	if p == nil {
		return &query.PageRequest{Limit: MaxLimit}, nil
	}
	if p.Offset != 0 || p.CountTotal || p.Limit > MaxLimit {
		return nil, status.Errorf(codes.InvalidArgument, "use key pagination, without total counting, with a limit at most %d", MaxLimit)
	}
	bounded := *p
	if bounded.Limit == 0 {
		bounded.Limit = MaxLimit
	}
	return &bounded, nil
}
