package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/version"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/ararat-network/ark/pkg/chain"
)

func TestNativeUnitConfiguration(t *testing.T) {
	require.Equal(t, chain.NoahBaseDenom, sdk.DefaultBondDenom)
	require.True(t, chain.NativeBaseAmount(1).Equal(sdk.DefaultPowerReduction))
	require.True(t, chain.NativeBaseAmount(10).Equal(govv1.DefaultMinDepositTokens))
	require.True(t, chain.NativeBaseAmount(50).Equal(govv1.DefaultMinExpeditedDepositTokens))
	require.Equal(t, "0.100000000000000000", govv1.DefaultParams().MinInitialDepositRatio)
}

// The keyring service name is the OS credential store's service label on the
// default backend, so an unset version.Name would silently file Ark keys under
// "cosmos" and a later rename would leave them unreachable.
func TestKeyringServiceName(t *testing.T) {
	require.Equal(t, Name, version.Name)
	require.Equal(t, Name, sdk.KeyringServiceName())
	require.Equal(t, Name+"d", version.AppName)
}
