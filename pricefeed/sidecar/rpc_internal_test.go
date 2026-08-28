package sidecar

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/log/v2"
)

func TestRecoverUnaryPanicMapsHandlerErrorsToStatusCodes(t *testing.T) {
	srv := &server{logger: log.NewNopLogger()}

	testCases := []struct {
		name string
		err  error
		code codes.Code
	}{
		{
			name: "nil request",
			err:  ErrNilRequest,
			code: codes.InvalidArgument,
		},
		{
			name: "oracle not running",
			err:  ErrOracleNotRunning,
			code: codes.Unavailable,
		},
		{
			name: "context canceled",
			err:  context.Canceled,
			code: codes.Canceled,
		},
		{
			name: "deadline exceeded",
			err:  context.DeadlineExceeded,
			code: codes.DeadlineExceeded,
		},
		{
			name: "existing status error",
			err:  status.Error(codes.ResourceExhausted, "rate limited"),
			code: codes.ResourceExhausted,
		},
		{
			name: "unexpected handler error",
			err:  errors.New("conversion failed"),
			code: codes.Internal,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			response, err := srv.recoverUnaryPanic(
				context.Background(),
				nil,
				nil,
				func(context.Context, any) (any, error) {
					return nil, tc.err
				},
			)

			require.Nil(t, response)
			require.Equal(t, tc.code, status.Code(err))
		})
	}
}

func TestRecoverUnaryPanicRecoversServicePanic(t *testing.T) {
	srv := &server{logger: log.NewNopLogger()}

	response, err := srv.recoverUnaryPanic(
		context.Background(),
		nil,
		nil,
		func(context.Context, any) (any, error) {
			panic("prices exploded")
		},
	)

	require.Nil(t, response)
	require.Equal(t, codes.Internal, status.Code(err))
	require.Equal(t, "oracle rpc panicked", status.Convert(err).Message())
}
