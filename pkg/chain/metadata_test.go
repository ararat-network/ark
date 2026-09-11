package chain_test

import (
	"strings"
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

func TestNativeAssetMetadata(t *testing.T) {
	tests := []struct {
		name            string
		denom           string
		wantDescription string
	}{
		{
			name:            "launch denomination names its currency",
			denom:           chain.USDBaseDenom,
			wantDescription: "An Ark currency tracking the United States dollar.",
		},
		{
			name:            "the euro takes no article capital",
			denom:           chain.EURBaseDenom,
			wantDescription: "An Ark currency tracking the euro.",
		},
		{
			// A denomination registered without a table entry keeps this
			// text for good: the derivation is checked at every import.
			name:            "unnamed denomination stays neutral",
			denom:           "amnt",
			wantDescription: "An Ark currency.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata := chain.NativeAssetMetadata(tt.denom)

			require.NoError(t, metadata.Validate())
			require.Equal(t, tt.wantDescription, metadata.Description)
			require.Equal(t, tt.denom, metadata.Base)
			require.Equal(t, tt.denom[1:], metadata.Display)
			require.Equal(t, "Ark"+strings.ToUpper(tt.denom[1:]), metadata.Name)
			require.Equal(t, "ark"+strings.ToUpper(tt.denom[1:]), metadata.Symbol)
		})
	}
}
