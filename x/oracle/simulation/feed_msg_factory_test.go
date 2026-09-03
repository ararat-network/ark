package simulation_test

import (
	"context"
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/x/oracle/keeper"
	"github.com/ararat-network/ark/x/oracle/simulation"
	"github.com/ararat-network/ark/x/oracle/types"
)

// feedFixture pairs a real Oracle keeper with the chain data source the
// factories read, so an emitted message can be delivered through the keeper it
// targets.
type feedFixture struct {
	ctx      context.Context
	keeper   *keeper.Keeper
	testData *simsx.ChainDataSource
	reporter *simsx.BasicSimulationReporter
}

func newFeedFixture(t *testing.T, feeds types.Feeds) feedFixture {
	t.Helper()

	ctx, oracleKeeper, accountKeeper := newOracleSimulationKeeper(t)
	require.NoError(t, oracleKeeper.Feeds.Set(ctx, feeds))
	require.NoError(t, oracleKeeper.ReferenceDenom.Set(ctx, ""))

	r := rand.New(rand.NewSource(1))
	testData := simsx.NewChainDataSource(
		ctx,
		r,
		accountKeeper,
		nil,
		addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
		simtypes.RandomAccounts(r, 1)...,
	)

	return feedFixture{ctx: ctx, keeper: oracleKeeper, testData: testData, reporter: simsx.NewBasicSimulationReporter()}
}

func govAddress() string {
	return authtypes.NewModuleAddress(govtypes.ModuleName).String()
}

func TestMsgAddFeedFactory(t *testing.T) {
	t.Run("names a denomination the registry lacks", func(t *testing.T) {
		f := newFeedFixture(t, types.Feeds{Version: 1, Denoms: []string{types.DefaultFeedDenoms[0]}})

		_, msg := simulation.MsgAddFeedFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, govAddress(), msg.Authority)
		require.NotEqual(t, types.DefaultFeedDenoms[0], msg.Denom)
		require.Contains(t, types.DefaultFeedDenoms, msg.Denom)
		// The factory's whole job is to emit what the handler accepts.
		require.NoError(t, f.keeper.ScheduleFeedTransition(f.ctx, msg.Denom, types.FeedDirection_FEED_DIRECTION_ADD))
	})

	t.Run("skips once every launch denomination has a feed", func(t *testing.T) {
		f := newFeedFixture(t, types.Feeds{Version: 1, Denoms: slices.Clone(types.DefaultFeedDenoms)})

		_, msg := simulation.MsgAddFeedFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.True(t, f.reporter.IsSkipped())
		require.Contains(t, f.reporter.Comment(), "already has a feed")
		require.Nil(t, msg)
	})

	t.Run("skips a denomination already transitioning", func(t *testing.T) {
		pending := make([]types.FeedTransition, 0, len(types.DefaultFeedDenoms))
		for _, denom := range types.DefaultFeedDenoms {
			pending = append(pending, types.FeedTransition{
				Denom:                denom,
				Direction:            types.FeedDirection_FEED_DIRECTION_ADD,
				ActivationVoteHeight: 100,
			})
		}
		f := newFeedFixture(t, types.Feeds{Version: 1, Transitions: pending})

		_, msg := simulation.MsgAddFeedFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.True(t, f.reporter.IsSkipped())
		require.Nil(t, msg)
	})
}

func TestMsgRemoveFeedFactory(t *testing.T) {
	t.Run("names an unreferenced feed", func(t *testing.T) {
		f := newFeedFixture(t, types.Feeds{Version: 1, Denoms: []string{types.DefaultFeedDenoms[0]}})

		_, msg := simulation.MsgRemoveFeedFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, types.DefaultFeedDenoms[0], msg.Denom)
		require.NoError(t, f.keeper.ScheduleFeedTransition(f.ctx, msg.Denom, types.FeedDirection_FEED_DIRECTION_REMOVE))
	})

	t.Run("skips the protocol reference, which refers to itself", func(t *testing.T) {
		f := newFeedFixture(t, types.Feeds{Version: 1, Denoms: []string{types.DefaultFeedDenoms[0]}})
		require.NoError(t, f.keeper.ReferenceDenom.Set(f.ctx, types.DefaultFeedDenoms[0]))

		_, msg := simulation.MsgRemoveFeedFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.True(t, f.reporter.IsSkipped())
		require.Contains(t, f.reporter.Comment(), "every feed is referenced")
		require.Nil(t, msg)
	})

	t.Run("skips an empty registry", func(t *testing.T) {
		f := newFeedFixture(t, types.Feeds{Version: 1})

		_, msg := simulation.MsgRemoveFeedFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.True(t, f.reporter.IsSkipped())
		require.Nil(t, msg)
	})
}

func TestMsgSetReferenceDenomFactory(t *testing.T) {
	t.Run("names an active feed at a rate inside the bound", func(t *testing.T) {
		f := newFeedFixture(t, types.Feeds{Version: 1, Denoms: []string{types.DefaultFeedDenoms[0]}})

		_, msg := simulation.MsgSetReferenceDenomFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, types.DefaultFeedDenoms[0], msg.ReferenceDenom)
		require.True(t, msg.OutgoingRate.IsPositive())
		require.True(t, msg.OutgoingRate.LTE(types.MaxOutgoingReferenceRate))
	})

	t.Run("skips without an active feed", func(t *testing.T) {
		f := newFeedFixture(t, types.Feeds{Version: 1})

		_, msg := simulation.MsgSetReferenceDenomFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.True(t, f.reporter.IsSkipped())
		require.Contains(t, f.reporter.Comment(), "no active feed")
		require.Nil(t, msg)
	})
}
