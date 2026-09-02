package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPprofHandlerRegistersIndexAndNamedProfiles(t *testing.T) {
	handler := newPprofHandler()

	tests := []string{
		"/debug/pprof/",
		"/debug/pprof/goroutine?debug=1",
	}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			resp := httptest.NewRecorder()

			handler.ServeHTTP(resp, req)

			require.Equal(t, http.StatusOK, resp.Code)
		})
	}
}
