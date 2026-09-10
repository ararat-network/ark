package app_test

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/app/mempool"
	apptestutil "github.com/ararat-network/ark/app/testutil"
)

// TestLaneMempoolOrdersCommitteeAndGovernanceFirst proves the wired pool
// drains the priority lane before any normal-lane fee, and fee order within a
// lane, using real signed transactions. Proposal assembly adds bounded gas and
// byte budgets on top of the same scheduler.
func TestLaneMempoolOrdersCommitteeAndGovernanceFirst(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	pool := arkApp.Pool()
	r := rand.New(rand.NewSource(1))
	txConfig := arkApp.GetTxConfig()

	sender := func() (*secp256k1.PrivKey, string) {
		priv := secp256k1.GenPrivKey()
		return priv, sdk.AccAddress(priv.PubKey().Address()).String()
	}
	admit := func(priv *secp256k1.PrivKey, msg sdk.Msg, lane int8, fee int64) []byte {
		tx, err := simtestutil.GenSignedMockTx(r, txConfig, []sdk.Msg{msg}, sdk.Coins{}, simtestutil.DefaultGenTxGas, "", []uint64{0}, []uint64{0}, priv)
		require.NoError(t, err)
		bz, err := txConfig.TxEncoder()(tx)
		require.NoError(t, err)
		require.NoError(t, pool.Insert(mempool.WithLane(sdk.Context{}.WithContext(context.Background()).WithTxBytes(bz).WithPriority(fee), lane), tx))
		return bz
	}

	richPriv, richAddr := sender()
	poorPriv, poorAddr := sender()
	votePriv, voteAddr := sender()

	// The zero-fee vote arrives last, behind well-paying normal traffic.
	richSend := admit(richPriv, &banktypes.MsgSend{FromAddress: richAddr, ToAddress: poorAddr}, mempool.LaneNormal, 1_000_000)
	poorSend := admit(poorPriv, &banktypes.MsgSend{FromAddress: poorAddr, ToAddress: richAddr}, mempool.LaneNormal, 10)
	vote := admit(votePriv, &govv1.MsgVote{ProposalId: 1, Voter: voteAddr, Option: govv1.VoteOption_VOTE_OPTION_YES}, mempool.LaneGovernance, 0)

	var order [][]byte
	pool.SelectBy(context.Background(), nil, func(tx sdk.Tx) bool {
		bz, err := txConfig.TxEncoder()(tx)
		require.NoError(t, err)
		order = append(order, bz)
		return true
	})
	require.Equal(t, [][]byte{vote, richSend, poorSend}, order, "priority lane drains first; fee still orders the normal lane")
}

// Capacity must never disable application proposal verification.
func TestMempoolSizingFollowsAppConfig(t *testing.T) {
	newApp := func(t *testing.T, maxTxs any) *app.ArkApp {
		t.Helper()
		appOptions := make(simtestutil.AppOptionsMap, 0)
		// Per-subtest home: the Wasm VM locks its cache directory exclusively.
		appOptions[flags.FlagHome] = t.TempDir()
		if maxTxs != nil {
			appOptions[mempool.MaxTxsKey] = maxTxs
		}
		return app.NewArkApp(log.NewNopLogger(), dbm.NewMemDB(), true, appOptions)
	}

	check := func(t *testing.T, a *app.ArkApp) {
		t.Helper()
		require.Same(t, a.Pool(), a.Mempool(), "BaseApp holds the SDK-backed reservation pool")
		require.NotNil(t, a.Pool())
	}
	t.Run("absent key keeps the lanes on", func(t *testing.T) { check(t, newApp(t, nil)) })
	t.Run("configured cap keeps the lanes on", func(t *testing.T) { check(t, newApp(t, 20000)) })
	t.Run("zero selects the bounded default and keeps the lanes on", func(t *testing.T) { check(t, newApp(t, 0)) })

	for _, value := range []any{-1, -100, "bad", 1.5} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			require.Panics(t, func() { newApp(t, value) })
		})
	}
}

// TestPriorityMsgURLsCoverCommitteeSurface pins the lane list to the
// registered message surface in both directions: no phantom entries after a
// rename, and no committee message shipping without a lane decision.
func TestPriorityMsgURLsCoverCommitteeSurface(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)

	priority := arkApp.Privileges().URLs()
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

// The SDK's default ProcessProposal skips verification for its own NoOpMempool;
// the reservation pool retains the normal SDK verification path.
func TestLaneMempoolKeepsProposalVerification(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: 1})
	malformed := &cmtabci.RequestProcessProposal{Txs: [][]byte{[]byte("malformed transaction")}}
	response, err := baseapp.NewDefaultProposalHandler(arkApp.Pool(), arkApp).ProcessProposalHandler()(ctx, malformed)
	require.NoError(t, err)
	require.Equal(t, cmtabci.ResponseProcessProposal_REJECT, response.Status)
	response, err = baseapp.NewDefaultProposalHandler(sdkmempool.NoOpMempool{}, arkApp).ProcessProposalHandler()(ctx, malformed)
	require.NoError(t, err)
	require.Equal(t, cmtabci.ResponseProcessProposal_ACCEPT, response.Status, "NoOpMempool would disable verification")
}

func TestMempoolByteLimitsReachAdmission(t *testing.T) {
	t.Run("configured bytes reach pool and CheckTx", func(t *testing.T) {
		a := app.NewArkApp(log.NewNopLogger(), dbm.NewMemDB(), true, simtestutil.AppOptionsMap{
			flags.FlagHome:                 t.TempDir(),
			mempool.MaxPoolBytesKey:        8000,
			mempool.MaxTransactionBytesKey: 512,
		})
		t.Cleanup(func() { require.NoError(t, a.Close()) })
		result, err := a.CheckTx(&cmtabci.RequestCheckTx{Tx: make([]byte, 513)})
		require.NoError(t, err)
		require.NotZero(t, result.Code)
		require.NotEmpty(t, result.Log)
	})
}
