package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	provider "noah/oracle/sidecar/providers/base"
)

func TestGetProvidersIsPackagePrivateMapSnapshot(t *testing.T) {
	oracle := &Runtime{
		providers: map[string]*provider.Provider{
			"unknown": nil,
		},
	}

	providers := oracle.getProviders()
	delete(providers, "unknown")

	require.Contains(t, oracle.getProviders(), "unknown")
}
