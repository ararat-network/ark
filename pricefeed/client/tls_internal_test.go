package client

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/log/v2"

	apitestutil "github.com/ararat-network/ark/pricefeed/api/testutil"
)

func TestCachedPriceClientObservesSidecarVersion(t *testing.T) {
	first := freshResponse()
	first.Version = "v1"
	second := freshResponse()
	second.Version = "v2"
	rpc := apitestutil.NewMockPriceFeedClient(gomock.NewController(t))
	gomock.InOrder(
		rpc.EXPECT().Prices(gomock.Any(), gomock.Any(), gomock.Any()).Return(first, nil),
		rpc.EXPECT().Prices(gomock.Any(), gomock.Any(), gomock.Any()).Return(first, nil),
		rpc.EXPECT().Prices(gomock.Any(), gomock.Any(), gomock.Any()).Return(second, nil),
	)
	logs := new(bytes.Buffer)
	client, err := NewClient(log.NewLogger(logs, log.ColorOption(false)), validClientConfig())
	require.NoError(t, err)

	for range 3 {
		client.fetchPrices(context.Background(), rpc)
	}

	require.Equal(t, "v2", client.versions[client.config.SidecarAddress])
	output := logs.String()
	require.Equal(t, 2, strings.Count(output, "sidecar version"), output)
	require.Contains(t, output, "previous=v1 version=v2")
}

func TestCachedPriceClientReportsIncompatibleSidecar(t *testing.T) {
	rpc := apitestutil.NewMockPriceFeedClient(gomock.NewController(t))
	rpc.EXPECT().
		Prices(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, status.Error(codes.Unimplemented, "unknown service ark.pricefeed.v1.PriceFeed"))
	logs := new(bytes.Buffer)
	client, err := NewClient(log.NewLogger(logs, log.ColorOption(false)), validClientConfig())
	require.NoError(t, err)

	client.fetchPrices(context.Background(), rpc)

	require.Contains(t, logs.String(), "not compatible with this node")
	require.Contains(t, logs.String(), "Unimplemented")
}
