package pagination_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/ararat-network/ark/pkg/pagination"
)

func TestBounded(t *testing.T) {
	tests := []struct {
		name    string
		request *query.PageRequest
		want    *query.PageRequest
	}{
		{name: "unset", want: &query.PageRequest{Limit: pagination.MaxLimit}},
		{name: "zero limit", request: &query.PageRequest{}, want: &query.PageRequest{Limit: pagination.MaxLimit}},
		{name: "limit at the cap", request: &query.PageRequest{Limit: pagination.MaxLimit}, want: &query.PageRequest{Limit: pagination.MaxLimit}},
		{name: "key", request: &query.PageRequest{Key: []byte{1}, Limit: 2}, want: &query.PageRequest{Key: []byte{1}, Limit: 2}},
		{name: "reverse", request: &query.PageRequest{Reverse: true}, want: &query.PageRequest{Limit: pagination.MaxLimit, Reverse: true}},
		{name: "offset", request: &query.PageRequest{Offset: 1}},
		{name: "key and offset", request: &query.PageRequest{Key: []byte{1}, Offset: 1}},
		{name: "total", request: &query.PageRequest{CountTotal: true}},
		{name: "limit past the cap", request: &query.PageRequest{Limit: pagination.MaxLimit + 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pagination.Bounded(tt.request)
			if tt.want == nil {
				require.Equal(t, codes.InvalidArgument, status.Code(err))
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestBoundedLeavesTheRequest pins that filling in the limit never writes through to the caller's request.
func TestBoundedLeavesTheRequest(t *testing.T) {
	request := &query.PageRequest{Key: []byte{1}}
	got, err := pagination.Bounded(request)
	require.NoError(t, err)
	require.Equal(t, pagination.MaxLimit, got.Limit)
	require.Zero(t, request.Limit)
}
