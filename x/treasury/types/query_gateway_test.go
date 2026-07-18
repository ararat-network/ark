package types_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"ark/x/treasury/types"
)

type claimQueryGatewayServer struct {
	types.UnimplementedQueryServer

	claimID        uint64
	mandateQueried bool
}

func (server *claimQueryGatewayServer) Claim(
	_ context.Context,
	request *types.QueryClaimRequest,
) (*types.QueryClaimResponse, error) {
	server.claimID = request.ClaimId
	return &types.QueryClaimResponse{}, nil
}

func (server *claimQueryGatewayServer) ClaimsMandate(
	_ context.Context,
	_ *types.QueryClaimsMandateRequest,
) (*types.QueryClaimsMandateResponse, error) {
	server.mandateQueried = true
	return nil, status.Error(codes.Unimplemented, "test route")
}

func TestClaimGatewayUsesNumericPathParameter(t *testing.T) {
	server := &claimQueryGatewayServer{}
	mux := runtime.NewServeMux()
	require.NoError(t, types.RegisterQueryHandlerServer(context.Background(), mux, server))
	request := httptest.NewRequest(http.MethodGet, "/ark/treasury/v1/claims/42", nil)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, uint64(42), server.claimID)
}

func TestClaimGatewayPreservesMandateRoute(t *testing.T) {
	server := &claimQueryGatewayServer{}
	mux := runtime.NewServeMux()
	require.NoError(t, types.RegisterQueryHandlerServer(context.Background(), mux, server))
	request := httptest.NewRequest(http.MethodGet, "/ark/treasury/v1/claims/mandate", nil)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusNotImplemented, response.Code, response.Body.String())
	require.True(t, server.mandateQueried)
	require.Zero(t, server.claimID)
}
