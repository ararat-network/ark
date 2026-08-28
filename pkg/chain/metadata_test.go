package chain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/pkg/chain"
)

func TestNoahMetadata(t *testing.T) {
	metadata := chain.NoahMetadata()

	require.NoError(t, metadata.Validate())
	require.Equal(t, chain.NoahBaseDenom, metadata.Base)
	require.Equal(t, "noah", metadata.Display)
	require.Equal(t, "NOAH", metadata.Symbol)
	require.Equal(t, []*banktypes.DenomUnit{
		{Denom: chain.NoahBaseDenom, Exponent: 0},
		{Denom: "noah", Exponent: chain.NativeDisplayExponent},
	}, metadata.DenomUnits)
}
