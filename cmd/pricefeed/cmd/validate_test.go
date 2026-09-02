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
	sdkmath "cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/encoding"
	"github.com/ararat-network/ark/pricefeed/api"
	"github.com/ararat-network/ark/pricefeed/validation"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

func TestValidateCmdOwnsValidationFlags(t *testing.T) {
	cmd := newValidateCmd()
	defaults := validation.DefaultConfig()

	require.Equal(t, defaultSidecarAddress, cmd.Flags().Lookup(flagSidecarAddress).DefValue)
	require.Equal(t, defaultChainAddress, cmd.Flags().Lookup(flagChainAddress).DefValue)
	require.Equal(t, defaults.BurnInPeriod.String(), cmd.Flags().Lookup(flagBurnInPeriod).DefValue)
	require.Equal(t, defaults.ValidationPeriod.String(), cmd.Flags().Lookup(flagValidationPeriod).DefValue)
	require.Equal(t, defaults.MaxResponseAge.String(), cmd.Flags().Lookup(flagMaxResponseAge).DefValue)
	require.Equal(t, defaults.MaxFutureSkew.String(), cmd.Flags().Lookup(flagMaxFutureSkew).DefValue)
	require.Equal(t, defaults.RequestTimeout.String(), cmd.Flags().Lookup(flagRequestTimeout).DefValue)
	require.Equal(t, defaults.FeedRefreshInterval.String(), cmd.Flags().Lookup(flagFeedRefreshInterval).DefValue)
	require.Equal(t, "1000", cmd.Flags().Lookup(flagNumChecks).DefValue)
	require.Equal(t, "99", cmd.Flags().Lookup(flagRequiredPriceLivenessPercent).DefValue)
}

func TestValidateCmdPrintsResultsBeforeReturningFailure(t *testing.T) {
	out := new(bytes.Buffer)

	err := writeValidationOutcome(
		out,
		validation.LivenessResults{
			"ausd": 100,
			"akrw": 50,
		},
		errors.New("akrw below threshold"),
	)

	require.ErrorContains(t, err, "oracle validation failed")
	require.Equal(t, "oracle validation results:\nakrw: 50.00%\nausd: 100.00%\n", out.String())
}

func TestValidateCmdPrintsSuccess(t *testing.T) {
	out := new(bytes.Buffer)

	err := writeValidationOutcome(
		out,
		validation.LivenessResults{"ausd": 100},
		nil,
	)

	require.NoError(t, err)
	require.Equal(t, "oracle validation results:\nausd: 100.00%\noracle validation passed\n", out.String())
}

func TestValidateCmdPrintsDisabledState(t *testing.T) {
	out := new(bytes.Buffer)

	err := writeValidationOutcome(
		out,
		nil,
		fmt.Errorf("load initial active feeds: %w", validation.ErrNoActiveFeeds),
	)

	require.NoError(t, err)
	require.Equal(t, "oracle validation skipped: no active feeds (oracle voting disabled)\n", out.String())
}

func TestRunValidationUsesExternalRPCs(t *testing.T) {
	rawPrice, err := encoding.EncodeCompactLegacyDec(sdkmath.LegacyNewDec(1))
	require.NoError(t, err)

	oracleServer := &validationPriceFeedServer{rawPrice: rawPrice}
	queryServer := &validationQueryServer{denoms: []string{"ausd"}}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	api.RegisterPriceFeedServer(server, oracleServer)
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
	results, err := runValidation(
		context.Background(),
		log.NewNopLogger(),
		validateOptions{
			sidecarAddress: "passthrough:///oracle-validation",
			chainAddress:   "passthrough:///chain-validation",
			cfg:            cfg,
		},
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
	)

	require.NoError(t, err)
	require.Equal(t, validation.LivenessResults{"ausd": 100}, results)
	require.True(t, oracleServer.called)
	require.True(t, queryServer.called)
}

func TestRunValidationRejectsEmptyAddresses(t *testing.T) {
	tests := []struct {
		name    string
		options validateOptions
		wantErr string
	}{
		{
			name: "oracle address",
			options: validateOptions{
				chainAddress: defaultChainAddress,
				cfg:          validation.DefaultConfig(),
			},
			wantErr: "oracle address cannot be empty",
		},
		{
			name: "chain address",
			options: validateOptions{
				sidecarAddress: defaultSidecarAddress,
				cfg:            validation.DefaultConfig(),
			},
			wantErr: "chain address cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := runValidation(context.Background(), log.NewNopLogger(), tt.options)

			require.Nil(t, results)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

type validationPriceFeedServer struct {
	api.UnimplementedPriceFeedServer

	rawPrice []byte
	called   bool
}

func (s *validationPriceFeedServer) Prices(
	context.Context,
	*api.PricesRequest,
) (*api.PricesResponse, error) {
	s.called = true
	return &api.PricesResponse{
		Prices:    map[string][]byte{"ausd": s.rawPrice},
		Timestamp: time.Now().UTC(),
	}, nil
}

type validationQueryServer struct {
	oracletypes.UnimplementedQueryServer

	denoms []string
	called bool
}

func (s *validationQueryServer) Feeds(
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
