package app

import (
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"ark/pkg/chain"
)

func TestDefaultGenesisIncludesNoahMetadata(t *testing.T) {
	arkApp := NewArkApp(
		log.NewTestLogger(t),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
	)

	bankGenesis := banktypes.GetGenesisStateFromAppState(
		arkApp.AppCodec(),
		arkApp.DefaultGenesis(),
	)

	require.Len(t, bankGenesis.DenomMetadata, 1)
	metadata := bankGenesis.DenomMetadata[0]
	require.NoError(t, metadata.Validate())
	require.Equal(t, chain.NoahBaseDenom, metadata.Base)
	require.Equal(t, "noah", metadata.Display)
	require.Equal(t, "NOAH", metadata.Name)
	require.Equal(t, "NOAH", metadata.Symbol)
	require.Equal(t, chain.NoahMetadata().Description, metadata.Description)
	require.Len(t, metadata.DenomUnits, 2)
	require.Equal(t, chain.NoahBaseDenom, metadata.DenomUnits[0].Denom)
	require.Zero(t, metadata.DenomUnits[0].Exponent)
	require.Empty(t, metadata.DenomUnits[0].Aliases)
	require.Equal(t, "noah", metadata.DenomUnits[1].Denom)
	require.Equal(t, uint32(chain.NativeDisplayExponent), metadata.DenomUnits[1].Exponent)
	require.Empty(t, metadata.DenomUnits[1].Aliases)

	var govGenesis govv1.GenesisState
	arkApp.AppCodec().MustUnmarshalJSON(
		arkApp.DefaultGenesis()[govtypes.ModuleName],
		&govGenesis,
	)
	require.NotNil(t, govGenesis.Params)
	require.Equal(
		t,
		sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(10))),
		sdk.Coins(govGenesis.Params.MinDeposit),
	)
	require.Equal(
		t,
		sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(50))),
		sdk.Coins(govGenesis.Params.ExpeditedMinDeposit),
	)
}
