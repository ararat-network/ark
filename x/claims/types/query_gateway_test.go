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

	"ark/x/claims/types"
)

type claimQueryGatewayServer struct {
	types.UnimplementedQueryServer

	claimID          uint64
	mandateQueried   bool
	insuranceQueried bool
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

func (server *claimQueryGatewayServer) InsuranceBalance(
	_ context.Context,
	_ *types.QueryInsuranceBalanceRequest,
) (*types.QueryInsuranceBalanceResponse, error) {
	server.insuranceQueried = true
	return nil, status.Error(codes.Unimplemented, "test route")
}

func TestClaimGatewayUsesNumericPathParameter(t *testing.T) {
	server := &claimQueryGatewayServer{}
	mux := runtime.NewServeMux()
	require.NoError(t, types.RegisterQueryHandlerServer(context.Background(), mux, server))
	request := httptest.NewRequest(http.MethodGet, "/ark/claims/v1/claims/42", nil)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, uint64(42), server.claimID)
}

// TestClaimGatewayPreservesMandateRoute pins that the mandate route is not
// swallowed by the numeric claim-id pattern that shares its prefix.
func TestClaimGatewayPreservesMandateRoute(t *testing.T) {
	server := &claimQueryGatewayServer{}
	mux := runtime.NewServeMux()
	require.NoError(t, types.RegisterQueryHandlerServer(context.Background(), mux, server))
	request := httptest.NewRequest(http.MethodGet, "/ark/claims/v1/mandate", nil)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusNotImplemented, response.Code, response.Body.String())
	require.True(t, server.mandateQueried)
	require.Zero(t, server.claimID)
}

// TestClaimGatewayPreservesInsuranceBalanceRoute pins the same prefix hazard for
// the fund route, which shares /ark/claims/v1/ with the numeric claim pattern.
func TestClaimGatewayPreservesInsuranceBalanceRoute(t *testing.T) {
	server := &claimQueryGatewayServer{}
	mux := runtime.NewServeMux()
	require.NoError(t, types.RegisterQueryHandlerServer(context.Background(), mux, server))
	request := httptest.NewRequest(http.MethodGet, "/ark/claims/v1/insurance_balance", nil)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusNotImplemented, response.Code, response.Body.String())
	require.True(t, server.insuranceQueried)
	require.Zero(t, server.claimID)
}
