package cmd

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"cosmossdk.io/log/v2"
	sdkmath "cosmossdk.io/math"

	transporttypes "ark/oracle/types"
	"ark/oracle/validation"
	"ark/pkg/encoding"
	oracletypes "ark/x/oracle/types"
)

func TestValidateCmdOwnsValidationFlags(t *testing.T) {
	cmd := newValidateCmd()
	defaults := validation.DefaultConfig()

	require.Equal(t, defaultOracleAddress, cmd.Flags().Lookup(flagOracleAddress).DefValue)
	require.Equal(t, defaultChainAddress, cmd.Flags().Lookup(flagChainAddress).DefValue)
	require.Equal(t, defaults.BurnInPeriod.String(), cmd.Flags().Lookup(flagBurnInPeriod).DefValue)
	require.Equal(t, defaults.ValidationPeriod.String(), cmd.Flags().Lookup(flagValidationPeriod).DefValue)
	require.Equal(t, defaults.MaxResponseAge.String(), cmd.Flags().Lookup(flagMaxResponseAge).DefValue)
	require.Equal(t, defaults.MaxFutureSkew.String(), cmd.Flags().Lookup(flagMaxFutureSkew).DefValue)
	require.Equal(t, defaults.RequestTimeout.String(), cmd.Flags().Lookup(flagRequestTimeout).DefValue)
	require.Equal(t, defaults.DenomRefreshInterval.String(), cmd.Flags().Lookup(flagDenomRefreshInterval).DefValue)
	require.Equal(t, "1000", cmd.Flags().Lookup(flagNumChecks).DefValue)
	require.Equal(t, "99", cmd.Flags().Lookup(flagRequiredPriceLivenessPercent).DefValue)
}

func TestValidateCmdPrintsResultsBeforeReturningFailure(t *testing.T) {
	out := new(bytes.Buffer)

	err := writeValidationOutcome(
		out,
		validation.LivenessResults{
			"uusd": 100,
			"ukrw": 50,
		},
		errors.New("ukrw below threshold"),
	)

	require.ErrorContains(t, err, "oracle validation failed")
	require.Equal(t, "oracle validation results:\nukrw: 50.00%\nuusd: 100.00%\n", out.String())
}

func TestValidateCmdPrintsSuccess(t *testing.T) {
	out := new(bytes.Buffer)

	err := writeValidationOutcome(
		out,
		validation.LivenessResults{"uusd": 100},
		nil,
	)

	require.NoError(t, err)
	require.Equal(t, "oracle validation results:\nuusd: 100.00%\noracle validation passed\n", out.String())
}

func TestRunValidationUsesExternalRPCs(t *testing.T) {
	rawPrice, err := encoding.EncodeLegacyDec(sdkmath.LegacyNewDec(1))
	require.NoError(t, err)

	oracleServer := &validationOracleServer{rawPrice: rawPrice}
	queryServer := &validationQueryServer{denoms: []string{"uusd"}}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	transporttypes.RegisterOracleServer(server, oracleServer)
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
	cfg.DenomRefreshInterval = time.Hour
	cfg.RequestTimeout = 100 * time.Millisecond
	results, err := runValidation(
		context.Background(),
		log.NewNopLogger(),
		validateOptions{
			oracleAddress: "passthrough:///oracle-validation",
			chainAddress:  "passthrough:///chain-validation",
			cfg:           cfg,
		},
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
	)

	require.NoError(t, err)
	require.Equal(t, validation.LivenessResults{"uusd": 100}, results)
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
				oracleAddress: defaultOracleAddress,
				cfg:           validation.DefaultConfig(),
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

type validationOracleServer struct {
	transporttypes.UnimplementedOracleServer

	rawPrice []byte
	called   bool
}

func (s *validationOracleServer) Prices(
	context.Context,
	*transporttypes.OraclePricesRequest,
) (*transporttypes.OraclePricesResponse, error) {
	s.called = true
	return &transporttypes.OraclePricesResponse{
		Prices:    map[string][]byte{"uusd": s.rawPrice},
		Timestamp: time.Now().UTC(),
	}, nil
}

type validationQueryServer struct {
	oracletypes.UnimplementedQueryServer

	denoms []string
	called bool
}

func (s *validationQueryServer) VoteTargets(
	context.Context,
	*oracletypes.QueryVoteTargetsRequest,
) (*oracletypes.QueryVoteTargetsResponse, error) {
	s.called = true
	return &oracletypes.QueryVoteTargetsResponse{
		VoteTargets: append([]string(nil), s.denoms...),
	}, nil
}
