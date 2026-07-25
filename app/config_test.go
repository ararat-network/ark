package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"ark/pkg/chain"
)

func TestNativeUnitConfiguration(t *testing.T) {
	require.Equal(t, chain.NoahBaseDenom, sdk.DefaultBondDenom)
	require.True(t, chain.NativeBaseAmount(1).Equal(sdk.DefaultPowerReduction))
	require.True(t, chain.NativeBaseAmount(10).Equal(govv1.DefaultMinDepositTokens))
	require.True(t, chain.NativeBaseAmount(50).Equal(govv1.DefaultMinExpeditedDepositTokens))
}
