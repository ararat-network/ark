package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/encoding"
	"github.com/ararat-network/ark/pricefeed/api"
	"github.com/ararat-network/ark/pricefeed/validation"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

func TestCheckCmdOwnsFlags(t *testing.T) {
	flags := newCheckCmd(&rootOptions{}).Flags()
	defaults := validation.DefaultConfig()

	require.Equal(t, defaultAddress, flags.Lookup(flagAddress).DefValue)
	require.Equal(t, defaultChainAddress, flags.Lookup(flagChainAddress).DefValue)
	require.Equal(t, defaults.BurnInPeriod.String(), flags.Lookup(flagBurnInPeriod).DefValue)
	require.Equal(t, defaults.ValidationPeriod.String(), flags.Lookup(flagValidationPeriod).DefValue)
	require.Equal(t, defaults.MaxResponseAge.String(), flags.Lookup(flagMaxResponseAge).DefValue)
	require.Equal(t, defaults.MaxFutureSkew.String(), flags.Lookup(flagMaxFutureSkew).DefValue)
	require.Equal(t, defaults.RequestTimeout.String(), flags.Lookup(flagRequestTimeout).DefValue)
	require.Equal(t, defaults.FeedRefreshInterval.String(), flags.Lookup(flagFeedRefreshInterval).DefValue)
	require.Equal(t, "1000", flags.Lookup(flagNumChecks).DefValue)
	require.Equal(t, "99", flags.Lookup(flagRequiredPriceLivenessPercent).DefValue)
}

func TestWriteCheckOutcomePrintsResultsBeforeReturningFailure(t *testing.T) {
	out := new(bytes.Buffer)

	err := writeCheckOutcome(
		out,
		validation.LivenessResults{
			"ausd": 100,
			"akrw": 50,
		},
		errors.New("akrw below threshold"),
	)

	require.ErrorContains(t, err, "check failed")
	require.Equal(t, "liveness results:\nakrw: 50.00%\nausd: 100.00%\n", out.String())
}

func TestWriteCheckOutcomePrintsSuccess(t *testing.T) {
	out := new(bytes.Buffer)

	err := writeCheckOutcome(
		out,
		validation.LivenessResults{"ausd": 100},
		nil,
	)

	require.NoError(t, err)
	require.Equal(t, "liveness results:\nausd: 100.00%\ncheck passed\n", out.String())
}

func TestWriteCheckOutcomePrintsDisabledState(t *testing.T) {
	out := new(bytes.Buffer)

	err := writeCheckOutcome(
		out,
		nil,
		fmt.Errorf("load initial active feeds: %w", validation.ErrNoActiveFeeds),
	)

	require.NoError(t, err)
	require.Equal(t, "check skipped: no active feeds (oracle voting disabled)\n", out.String())
}

func TestRunCheckUsesExternalRPCs(t *testing.T) {
	rawPrice, err := encoding.EncodeCompactLegacyDec(math.LegacyNewDec(1))
	require.NoError(t, err)

	sidecarServer := &checkPriceFeedServer{rawPrice: rawPrice}
	queryServer := &checkQueryServer{denoms: []string{"ausd"}}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	api.RegisterPriceFeedServer(server, sidecarServer)
	oracletypes.RegisterQueryServer(server, queryServer)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	cfg := validation.DefaultConfig()
	cfg.BurnInPeriod = 0
	cfg.ValidationPeriod = 10 * time.Millisecond
	cfg.NumChecks = 1
	cfg.FeedRefreshInterval = time.Hour
	cfg.RequestTimeout = 100 * time.Millisecond
	results, err := runCheck(
		context.Background(),
		log.NewNopLogger(),
		checkOptions{
			address:      "passthrough:///localhost:20742",
			chainAddress: "passthrough:///localhost:20627",
			cfg:          cfg,
		},
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
	)

	require.NoError(t, err)
	require.Equal(t, validation.LivenessResults{"ausd": 100}, results)
	require.True(t, sidecarServer.called)
	require.True(t, queryServer.called)
}

func TestRunCheckRejectsEmptyAddresses(t *testing.T) {
	tests := []struct {
		name    string
		options checkOptions
		wantErr string
	}{
		{
			name: "sidecar address",
			options: checkOptions{
				chainAddress: defaultChainAddress,
				cfg:          validation.DefaultConfig(),
			},
			wantErr: "sidecar address cannot be empty",
		},
		{
			name: "chain address",
			options: checkOptions{
				address: defaultAddress,
				cfg:     validation.DefaultConfig(),
			},
			wantErr: "chain address cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := runCheck(context.Background(), log.NewNopLogger(), tt.options)

			require.Nil(t, results)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

type checkPriceFeedServer struct {
	api.UnimplementedPriceFeedServer

	rawPrice []byte
	called   bool
}

func (s *checkPriceFeedServer) Prices(
	context.Context,
	*api.PricesRequest,
) (*api.PricesResponse, error) {
	s.called = true
	return &api.PricesResponse{
		Prices:    map[string][]byte{"ausd": s.rawPrice},
		Timestamp: time.Now().UTC(),
	}, nil
}

type checkQueryServer struct {
	oracletypes.UnimplementedQueryServer

	denoms []string
	called bool
}

func (s *checkQueryServer) Feeds(
	context.Context,
	*oracletypes.QueryFeedsRequest,
) (*oracletypes.QueryFeedsResponse, error) {
	s.called = true
	return &oracletypes.QueryFeedsResponse{
		Feeds: oracletypes.Feeds{
			Denoms:  append([]string(nil), s.denoms...),
			Version: oracletypes.InitialFeedVersion,
		},
	}, nil
}
