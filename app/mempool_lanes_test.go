package app_test

import (
	"math/rand"
	"strings"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/server"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/mempool"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/ararat-network/ark/abci/lanes"
	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
)

// TestLaneMempoolOrdersCommitteeAndGovernanceFirst proves the wired mempool
// drains the priority lane before any normal-lane fee, and fee order within a
// lane, using real signed transactions. Block assembly from this order is the
// SDK default proposal handler's covered behaviour.
func TestLaneMempoolOrdersCommitteeAndGovernanceFirst(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)

	mp, ok := arkApp.Mempool().(*mempool.PriorityNonceMempool[lanes.Priority])
	require.True(t, ok, "BaseApp must be wired with the lane mempool")

	ctx := arkApp.NewContextLegacy(true, cmtproto.Header{Height: arkApp.LastBlockHeight()})
	r := rand.New(rand.NewSource(1))
	txConfig := arkApp.GetTxConfig()

	sender := func() (*secp256k1.PrivKey, string) {
		priv := secp256k1.GenPrivKey()
		return priv, sdk.AccAddress(priv.PubKey().Address()).String()
	}
	makeTx := func(priv *secp256k1.PrivKey, msg sdk.Msg) sdk.Tx {
		tx, err := simtestutil.GenSignedMockTx(
			r,
			txConfig,
			[]sdk.Msg{msg},
			sdk.Coins{},
			simtestutil.DefaultGenTxGas,
			"",
			[]uint64{0},
			[]uint64{0},
			priv,
		)
		require.NoError(t, err)
		return tx
	}

	richPriv, richAddr := sender()
	poorPriv, poorAddr := sender()
	votePriv, voteAddr := sender()

	richSend := makeTx(richPriv, &banktypes.MsgSend{FromAddress: richAddr, ToAddress: poorAddr})
	poorSend := makeTx(poorPriv, &banktypes.MsgSend{FromAddress: poorAddr, ToAddress: richAddr})
	vote := makeTx(votePriv, &govv1.MsgVote{ProposalId: 1, Voter: voteAddr, Option: govv1.VoteOption_VOTE_OPTION_YES})

	// The zero-fee vote arrives last, behind well-paying normal traffic. Lane
	// assignment reads the context priority, not the ante-reported gas.
	require.NoError(t, mp.Insert(ctx.WithPriority(1_000_000), richSend))
	require.NoError(t, mp.Insert(ctx.WithPriority(10), poorSend))
	require.NoError(t, mp.Insert(ctx.WithPriority(0), vote))

	var order []sdk.Msg
	for it := mp.Select(ctx, nil); it != nil; it = it.Next() {
		order = append(order, it.Tx().GetMsgs()[0])
	}

	require.Len(t, order, 3)
	require.IsType(t, &govv1.MsgVote{}, order[0], "priority lane must drain before any normal-lane fee")
	first, ok := order[1].(*banktypes.MsgSend)
	require.True(t, ok)
	require.Equal(t, richAddr, first.FromAddress, "fee must still order the normal lane")
	second, ok := order[2].(*banktypes.MsgSend)
	require.True(t, ok)
	require.Equal(t, poorAddr, second.FromAddress)
}

// TestMempoolSizingFollowsAppConfig covers the three app.toml states. The
// negative case is the load-bearing one: a disabled pool must stay a
// NoOpMempool, because the default proposal handler falls back to CometBFT's
// FIFO transactions for that type alone — a zero-capacity lane pool would
// propose empty blocks.
func TestMempoolSizingFollowsAppConfig(t *testing.T) {
	newApp := func(t *testing.T, maxTxs any) *app.ArkApp {
		t.Helper()
		appOptions := make(simtestutil.AppOptionsMap, 0)
		// Per-subtest home: the Wasm VM locks its cache directory exclusively.
		appOptions[flags.FlagHome] = t.TempDir()
		if maxTxs != nil {
			appOptions[server.FlagMempoolMaxTxs] = maxTxs
		}
		return app.NewArkApp(log.NewNopLogger(), dbm.NewMemDB(), true, appOptions)
	}

	t.Run("absent key keeps the lanes on", func(t *testing.T) {
		require.IsType(t, &mempool.PriorityNonceMempool[lanes.Priority]{}, newApp(t, nil).Mempool())
	})

	t.Run("configured cap keeps the lanes on", func(t *testing.T) {
		require.IsType(t, &mempool.PriorityNonceMempool[lanes.Priority]{}, newApp(t, 20000).Mempool())
	})

	t.Run("zero means unbounded and keeps the lanes on", func(t *testing.T) {
		require.IsType(t, &mempool.PriorityNonceMempool[lanes.Priority]{}, newApp(t, 0).Mempool())
	})

	t.Run("negative disables the pool without stranding the proposer", func(t *testing.T) {
		require.Equal(t, mempool.NoOpMempool{}, newApp(t, -1).Mempool())
	})
}

// TestPriorityMsgURLsCoverCommitteeSurface pins the lane list to the
// registered message surface in both directions: no phantom entries after a
// rename, and no committee message shipping without a lane decision.
func TestPriorityMsgURLsCoverCommitteeSurface(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)

	priority := app.PriorityLaneSet().URLs()
	registered := arkApp.InterfaceRegistry().ListImplementations(sdk.MsgInterfaceProtoName)
	registeredSet := make(map[string]struct{}, len(registered))
	for _, url := range registered {
		registeredSet[url] = struct{}{}
	}

	for url := range priority {
		require.Contains(t, registeredSet, url, "priority lane entry is not a registered message: %s", url)
	}

	for _, url := range registered {
		if !strings.HasPrefix(url, "/ark.") {
			continue
		}
		if strings.Contains(url, ".MsgCommittee") || strings.HasSuffix(url, ".MsgEmergencySuspendAsset") {
			require.Contains(t, priority, url, "committee message missing from the priority lane: %s", url)
		}
	}
}
