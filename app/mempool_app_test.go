package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"google.golang.org/protobuf/encoding/protowire"

	abcicli "github.com/cometbft/cometbft/abci/client"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtcfg "github.com/cometbft/cometbft/config"
	cmtsync "github.com/cometbft/cometbft/libs/sync"
	cmtpool "github.com/cometbft/cometbft/mempool"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/proxy"
	rpccore "github.com/cometbft/cometbft/rpc/core"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/server"
	sim "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
	txsigning "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	auth "github.com/cosmos/cosmos-sdk/x/auth/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	gov "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/app/ante"
	"github.com/ararat-network/ark/app/mempool"
	apptest "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/mandate"
	"github.com/ararat-network/ark/pkg/telemetry"
	security "github.com/ararat-network/ark/x/security/types"
)

const admissionChain = "ark-admission-test"

var admissionTime = time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)

type admissionFixture struct {
	apps                     []*app.ArkApp
	voter, normal, committee apptest.Funder
}

func newAdmissionFixture(t *testing.T, n int, configure ...func(*app.ArkApp)) admissionFixture {
	t.Helper()
	coins := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, chain.NativeBaseAmount(1_000_000)), sdk.NewCoin(chain.XDRBaseDenom, math.NewIntWithDecimal(1, 23)))
	f := admissionFixture{voter: apptest.NewFunder(t, coins), normal: apptest.NewFunder(t, coins), committee: apptest.NewFunder(t, coins)}
	validators := apptest.NewValidators(t, 1)
	for i := 0; i < n; i++ {
		a := app.NewArkApp(log.NewNopLogger(), dbm.NewMemDB(), len(configure) == 0, sim.AppOptionsMap{flags.FlagHome: t.TempDir(), mempool.MaxTxsKey: 20}, baseapp.SetChainID(admissionChain))
		t.Cleanup(func() { require.NoError(t, a.Close()) })
		for _, setup := range configure {
			setup(a)
		}
		if len(configure) != 0 {
			require.NoError(t, a.LoadLatestVersion())
		}
		gen, err := sim.GenesisStateWithValSet(a.AppCodec(), a.DefaultGenesis(), validators.Set, []auth.GenesisAccount{f.voter.Account, f.normal.Account, f.committee.Account}, f.voter.Balance, f.normal.Balance, f.committee.Balance)
		require.NoError(t, err)
		apptest.CorrectBondedPool(t, a.AppCodec(), gen, 1)
		var gg gov.GenesisState
		a.AppCodec().MustUnmarshalJSON(gen["gov"], &gg)
		end := admissionTime.Add(48 * time.Hour)
		gg.StartingProposalId = 2
		gg.Proposals = []*gov.Proposal{{Id: 1, Status: gov.StatusVotingPeriod, VotingStartTime: &admissionTime, VotingEndTime: &end, SubmitTime: &admissionTime, DepositEndTime: &end, TotalDeposit: sdk.Coins{}, FinalTallyResult: &gov.TallyResult{YesCount: "0", AbstainCount: "0", NoCount: "0", NoWithVetoCount: "0"}}}
		gen["gov"] = a.AppCodec().MustMarshalJSON(&gg)
		sg := security.DefaultGenesisState()
		sg.SecurityMandate.Envelope = mandate.Envelope{Committee: f.committee.Address().String(), Term: 1, ActivationHeight: 1, ExpiryHeight: 1000}
		gen[security.ModuleName] = a.AppCodec().MustMarshalJSON(sg)
		bz, err := json.Marshal(gen)
		require.NoError(t, err)
		consensusParams := *sim.DefaultConsensusParams
		blockParams := *consensusParams.Block
		blockParams.MaxGas = 100_000_000
		consensusParams.Block = &blockParams
		_, err = a.InitChain(&abci.RequestInitChain{ChainId: admissionChain, Time: admissionTime, ConsensusParams: &consensusParams, AppStateBytes: bz})
		require.NoError(t, err)
		_, err = a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 1, Time: admissionTime})
		require.NoError(t, err)
		_, err = a.Commit()
		require.NoError(t, err)
		f.apps = append(f.apps, a)
	}
	return f
}

func signedPending(tb testing.TB, a *app.ArkApp, f apptest.Funder, seq uint64, msg sdk.Msg) []byte {
	tb.Helper()
	ctx := a.GetContextForCheckTx(nil)
	acc := a.AccountKeeper.GetAccount(ctx, f.Address())
	tx, err := sim.GenSignedMockTx(rand.New(rand.NewSource(1)), a.GetTxConfig(), []sdk.Msg{msg}, sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 1_000_000_000_000_000_000)), 1_000_000, admissionChain, []uint64{acc.GetAccountNumber()}, []uint64{seq}, f.Key)
	require.NoError(tb, err)
	bz, err := a.GetTxConfig().TxEncoder()(tx)
	require.NoError(tb, err)
	return bz
}

func sendMsg(f apptest.Funder) sdk.Msg {
	return &bank.MsgSend{FromAddress: f.Address().String(), ToAddress: f.Address().String(), Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 1))}
}

func voteMsg(f apptest.Funder) sdk.Msg {
	return &gov.MsgVote{ProposalId: 1, Voter: f.Address().String(), Option: gov.VoteOption_VOTE_OPTION_YES}
}

func committeeMsg(f apptest.Funder) sdk.Msg {
	return &security.MsgCommitteePlanUpgrade{Committee: f.Address().String(), ExpectedTerm: 1, Name: "emergency-test", Height: 100}
}

func fillNormal(t *testing.T, f admissionFixture) {
	t.Helper()
	for seq := uint64(0); seq < 18; seq++ {
		bz := signedPending(t, f.apps[0], f.normal, seq, sendMsg(f.normal))
		for _, a := range f.apps {
			res, err := a.CheckTx(&abci.RequestCheckTx{Tx: bz})
			require.NoError(t, err)
			require.Zero(t, res.Code, res.Log)
		}
	}
}

func TestAdmissionAtomicCapacity(t *testing.T) {
	t.Run("full normal pool does not consume a sequence or fee", func(t *testing.T) {
		f := newAdmissionFixture(t, 1)
		a := f.apps[0]
		fillNormal(t, f)
		bz := signedPending(t, a, f.normal, 18, sendMsg(f.normal))
		ctx := a.GetContextForCheckTx(nil)
		before := a.BankKeeper.GetBalance(ctx, f.normal.Address(), chain.XDRBaseDenom)
		res, err := a.CheckTx(&abci.RequestCheckTx{Tx: bz})
		require.NoError(t, err)
		require.Equal(t, mempool.ErrCapacity.ABCICode(), res.Code, res.Log)
		require.Equal(t, uint64(18), a.AccountKeeper.GetAccount(ctx, f.normal.Address()).GetSequence())
		require.Equal(t, before, a.BankKeeper.GetBalance(ctx, f.normal.Address(), chain.XDRBaseDenom))
		for _, test := range []struct {
			f    apptest.Funder
			msg  sdk.Msg
			lane int8
		}{{f.voter, voteMsg(f.voter), mempool.LaneGovernance}, {f.committee, committeeMsg(f.committee), mempool.LaneCommittee}} {
			res, err = a.CheckTx(&abci.RequestCheckTx{Tx: signedPending(t, a, test.f, 0, test.msg)})
			require.NoError(t, err)
			require.Zero(t, res.Code, res.Log)
		}
		counts, _ := a.Pool().Usage()
		require.Equal(t, [3]int{18, 1, 1}, counts)
	})
}

func TestAdmissionAuthenticatesBeforeReserving(t *testing.T) {
	for _, tc := range []struct {
		name   string
		forged bool
	}{{"invalid signature", true}, {"ineligible committee term", false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAdmissionFixture(t, 1)
			a := f.apps[0]
			signer := f.committee
			msg := committeeMsg(f.committee).(*security.MsgCommitteePlanUpgrade)
			if tc.forged {
				signer.Key = f.normal.Key
			} else {
				msg.ExpectedTerm++
			}
			ctx := a.GetContextForCheckTx(nil)
			balance := a.BankKeeper.GetBalance(ctx, f.committee.Address(), chain.XDRBaseDenom)
			res, err := a.CheckTx(&abci.RequestCheckTx{Tx: signedPending(t, a, signer, 0, msg)})
			require.NoError(t, err)
			require.NotZero(t, res.Code)
			if !tc.forged {
				require.Contains(t, res.Log, "term mismatch")
			}
			counts, _ := a.Pool().Usage()
			require.Equal(t, [3]int{}, counts)
			require.Zero(t, a.AccountKeeper.GetAccount(ctx, f.committee.Address()).GetSequence())
			require.Equal(t, balance, a.BankKeeper.GetBalance(ctx, f.committee.Address(), chain.XDRBaseDenom))
			// The authentic action can still take the reserved slot at the same nonce.
			res, err = a.CheckTx(&abci.RequestCheckTx{Tx: signedPending(t, a, f.committee, 0, committeeMsg(f.committee))})
			require.NoError(t, err)
			require.Zero(t, res.Code, res.Log)
		})
	}
}

// peerHeight stands in for the consensus reactor's peer state, which the
// mempool reactor consults before gossiping to a peer.
type peerHeight int64

func (h peerHeight) GetHeight() int64 { return int64(h) }

type heightReactor struct{ p2p.BaseReactor }

func newHeightReactor() *heightReactor {
	r := &heightReactor{}
	r.BaseReactor = *p2p.NewBaseReactor("HEIGHT", r)
	return r
}

func (*heightReactor) GetChannels() []*p2p.ChannelDescriptor { return nil }

func (*heightReactor) InitPeer(peer p2p.Peer) p2p.Peer {
	peer.Set(cmttypes.PeerStateKey, peerHeight(1))
	return peer
}

// newClist mirrors an app behind CometBFT's flood mempool as a node would.
func newClist(t *testing.T, a *app.ArkApp, size int) *cmtpool.CListMempool {
	t.Helper()
	cfg := cmtcfg.DefaultMempoolConfig()
	cfg.Size = size
	client := abcicli.NewLocalClient(new(cmtsync.Mutex), server.NewCometABCIWrapper(a))
	return cmtpool.NewCListMempool(cfg, proxy.NewAppConnMempool(client, proxy.NopMetrics()), 1)
}

// checkTx submits through the list, as RPC and peers do, and returns the app's response.
func checkTx(t *testing.T, mp *cmtpool.CListMempool, bz []byte) (*abci.ResponseCheckTx, error) {
	t.Helper()
	var res *abci.ResponseCheckTx
	err := mp.CheckTx(bz, func(r *abci.ResponseCheckTx) { res = r }, cmtpool.TxInfo{})
	return res, err
}

func TestFloodModePublicPropagation(t *testing.T) {
	t.Run("RPC admission gossips through saturated peers to another proposer", func(t *testing.T) {
		f := newAdmissionFixture(t, 3)
		var pools []*cmtpool.CListMempool
		var reactors []*cmtpool.Reactor
		for _, a := range f.apps {
			mp := newClist(t, a, 20)
			pools = append(pools, mp)
			reactors = append(reactors, cmtpool.NewReactor(cmtcfg.DefaultMempoolConfig(), mp, false))
		}
		for seq := uint64(0); seq < 18; seq++ {
			bz := signedPending(t, f.apps[0], f.normal, seq, sendMsg(f.normal))
			for _, mp := range pools {
				res, err := checkTx(t, mp, bz)
				require.NoError(t, err)
				require.Zero(t, res.Code, res.Log)
			}
		}
		switches := p2p.MakeConnectedSwitches(cmtcfg.TestConfig().P2P, 3, func(i int, s *p2p.Switch) *p2p.Switch {
			s.AddReactor("HEIGHT", newHeightReactor())
			s.AddReactor("MEMPOOL", reactors[i])
			return s
		}, p2p.Connect2Switches)
		t.Cleanup(func() {
			for _, s := range switches {
				require.NoError(t, s.Stop())
			}
		})
		env := rpccore.Environment{Mempool: pools[0], MempoolReactor: reactors[0]}
		request := &rpctypes.Context{HTTPReq: httptest.NewRequest("POST", "http://localhost/broadcast_tx_sync", nil)}
		vote := signedPending(t, f.apps[0], f.voter, 0, voteMsg(f.voter))
		committee := signedPending(t, f.apps[0], f.committee, 0, committeeMsg(f.committee))
		for _, bz := range [][]byte{vote, committee} {
			res, err := env.BroadcastTxSync(request, bz)
			require.NoError(t, err)
			require.Zero(t, res.Code, res.Log)
		}
		require.Eventually(t, func() bool {
			for _, a := range f.apps {
				p := a.Pool()
				if !p.Has(vote) || !p.Has(committee) {
					return false
				}
			}
			return true
		}, 10*time.Second, 20*time.Millisecond)
		for i, mp := range pools {
			require.Equal(t, f.apps[i].Pool().CountTx(), mp.Size(), "the list mirrors the pool")
		}
		proposer := f.apps[2]
		proposal, err := proposer.PrepareProposal(&abci.RequestPrepareProposal{Height: 2, Time: admissionTime.Add(6 * time.Second), MaxTxBytes: 1 << 20})
		require.NoError(t, err)
		require.Equal(t, 20, len(proposal.Txs))
		require.Equal(t, committee, proposal.Txs[0], "committee action fits its preferential gas and byte budgets")
		require.Equal(t, vote, proposal.Txs[1], "vote fits the reduced governance budgets before ordinary traffic")
		// Include the privileged pair and the first normal transactions, leaving
		// the rest for CometBFT's recheck to confirm against the rebuilt pool.
		block := proposal.Txs[:5]
		for _, a := range f.apps {
			res, err := a.ProcessProposal(&abci.RequestProcessProposal{Height: 2, Time: admissionTime.Add(6 * time.Second), Txs: block})
			require.NoError(t, err)
			require.Equal(t, abci.ResponseProcessProposal_ACCEPT, res.Status)
		}
		for i, a := range f.apps {
			res, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 2, Time: admissionTime.Add(6 * time.Second), Txs: block})
			require.NoError(t, err)
			for _, result := range res.TxResults {
				require.Zero(t, result.Code, result.Log)
			}
			// Consensus holds the list locked across Commit and its update.
			pools[i].Lock()
			_, err = a.Commit()
			require.NoError(t, err)
			require.NoError(t, pools[i].Update(2, cmttypes.ToTxs(block), res.TxResults, nil, nil))
			pools[i].Unlock()
			require.Equal(t, 15, a.Pool().CountTx())
			require.Equal(t, 15, pools[i].Size(), "SDK recheck keeps the list mirrored")
			plan, err := a.UpgradeKeeper.GetUpgradePlan(a.GetContextForCheckTx(nil))
			require.NoError(t, err)
			require.Equal(t, int64(100), plan.Height)
		}
	})
}

// Rechecks revalidate pending votes through SDK ante without changing the
// priority or reservation assigned at insertion, matching SDK index behaviour.
func TestRecheckPreservesInsertionPriority(t *testing.T) {
	f := newAdmissionFixture(t, 1)
	a := f.apps[0]
	fillNormal(t, f)
	vote := signedPending(t, a, f.voter, 0, voteMsg(f.voter))
	res, err := a.CheckTx(&abci.RequestCheckTx{Tx: vote})
	require.NoError(t, err)
	require.Zero(t, res.Code, res.Log)
	pool := a.Pool()
	counts, _ := pool.Usage()
	require.Equal(t, [3]int{18, 1, 0}, counts)

	_, err = a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 2, Time: admissionTime.Add(48 * time.Hour)})
	require.NoError(t, err)
	_, err = a.Commit()
	require.NoError(t, err)
	require.True(t, pool.Has(vote), "a lane shift must not evict a surviving transaction")
	counts, _ = pool.Usage()
	require.Equal(t, [3]int{18, 1, 0}, counts, "SDK recheck does not reprioritise existing entries")
	for _, e := range pool.Snapshot() {
		res, err := a.CheckTx(&abci.RequestCheckTx{Tx: proposalBytes(t, a, e), Type: abci.CheckTxType_Recheck})
		require.NoError(t, err)
		require.Zero(t, res.Code, res.Log)
	}

	res, err = a.CheckTx(&abci.RequestCheckTx{Tx: signedPending(t, a, f.normal, 18, sendMsg(f.normal))})
	require.NoError(t, err)
	require.Equal(t, mempool.ErrCapacity.ABCICode(), res.Code, "normal admission waits for the overflow to drain")
	res, err = a.CheckTx(&abci.RequestCheckTx{Tx: signedPending(t, a, f.committee, 0, committeeMsg(f.committee))})
	require.NoError(t, err)
	require.Zero(t, res.Code, res.Log)
	require.Equal(t, 20, pool.CountTx())
}

// Ark keeps no lock of its own: CometBFT's local ABCI client serialises
// admission and rechecks with the block lifecycle.
func TestConcurrentAdmissionAndCommit(t *testing.T) {
	f := newAdmissionFixture(t, 1)
	a := f.apps[0]
	client := abcicli.NewLocalClient(new(cmtsync.Mutex), server.NewCometABCIWrapper(a))
	bz := signedPending(t, a, f.normal, 0, sendMsg(f.normal))
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				_, _ = client.CheckTx(context.Background(), &abci.RequestCheckTx{Tx: bz})
			}
		}()
	}
	_, err := client.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: 2, Time: admissionTime.Add(6 * time.Second)})
	require.NoError(t, err)
	_, err = client.Commit(context.Background(), &abci.RequestCommit{})
	require.NoError(t, err)
	wg.Wait()
	require.LessOrEqual(t, a.Pool().CountTx(), 1)
	counts, _ := a.Pool().Usage()
	require.Equal(t, a.Pool().CountTx(), counts[0]+counts[1]+counts[2])
}

// CometBFT's list forgets a rejected transaction, so a successor that arrived
// before its predecessor is admitted on resubmission, and the list mirrors the pool.
func TestListForgetsRejectedSuccessor(t *testing.T) {
	f := newAdmissionFixture(t, 1)
	a := f.apps[0]
	mp := newClist(t, a, 20)
	first := signedPending(t, a, f.normal, 0, sendMsg(f.normal))
	next := signedPending(t, a, f.normal, 1, sendMsg(f.normal))
	res, err := checkTx(t, mp, next)
	require.NoError(t, err)
	require.Equal(t, sdkerrors.ErrWrongSequence.ABCICode(), res.Code)
	require.Equal(t, sdkerrors.ErrWrongSequence.Codespace(), res.Codespace)
	require.Zero(t, mp.Size())
	res, err = checkTx(t, mp, first)
	require.NoError(t, err)
	require.Zero(t, res.Code, res.Log)
	res, err = checkTx(t, mp, next)
	require.NoError(t, err)
	require.Zero(t, res.Code, res.Log)
	require.Equal(t, 2, mp.Size())
	require.Equal(t, 2, a.Pool().CountTx())
	require.ErrorIs(t, mp.CheckTx(first, nil, cmtpool.TxInfo{}), cmtpool.ErrTxInCache)
}

func TestMempoolMetricLifecycle(t *testing.T) {
	registry := prometheus.NewRegistry()
	provider, err := telemetry.NewPrometheusProvider("test", registry)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	otel.SetMeterProvider(provider)
	f := newAdmissionFixture(t, 1)
	a := f.apps[0]
	check := func(t *testing.T, want int) {
		t.Helper()
		families, err := registry.Gather()
		require.NoError(t, err)
		counts, sizes := a.Pool().Usage()
		require.Equal(t, want, counts[0])
		found := 0
		for _, family := range families {
			if family.GetName() != "ark_mempool_transactions" && family.GetName() != "ark_mempool_bytes" {
				continue
			}
			found++
			require.Len(t, family.Metric, 3)
			for _, m := range family.Metric {
				for _, label := range m.Label {
					if label.GetName() == "lane" {
						i := map[string]int{"normal": 0, "governance": 1, "committee": 2}[label.GetValue()]
						if family.GetName() == "ark_mempool_transactions" {
							require.Equal(t, float64(counts[i]), m.GetGauge().GetValue())
						} else {
							require.Equal(t, float64(sizes[i]), m.GetGauge().GetValue())
						}
					}
				}
			}
		}
		require.Equal(t, 2, found)
	}
	t.Run("initially empty", func(t *testing.T) { check(t, 0) })
	first := signedPending(t, a, f.normal, 0, sendMsg(f.normal))
	second := signedPending(t, a, f.normal, 1, sendMsg(f.normal))
	t.Run("admitted", func(t *testing.T) {
		for _, bz := range [][]byte{first, second} {
			res, err := a.CheckTx(&abci.RequestCheckTx{Tx: bz})
			require.NoError(t, err)
			require.Zero(t, res.Code, res.Log)
		}
		check(t, 2)
	})
	t.Run("commit and revalidate remainder", func(t *testing.T) {
		_, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 2, Time: admissionTime.Add(time.Second), Txs: [][]byte{first}})
		require.NoError(t, err)
		_, err = a.Commit()
		require.NoError(t, err)
		check(t, 1)
	})
}

func signedUnordered(t *testing.T, a *app.ArkApp, seq uint64, timeout time.Time, memo string, signers ...apptest.Funder) []byte {
	t.Helper()
	cfg := a.GetTxConfig()
	builder := cfg.NewTxBuilder()
	builder.SetUnordered(true)
	builder.SetTimeoutTimestamp(timeout)
	builder.SetMemo(memo)
	builder.SetGasLimit(1_000_000)
	// The native leg is a tip, making these outrank signedPending's untipped txs.
	builder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 1_000_000_000_000_000_000), sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(1))))
	sigs := make([]txsigning.SignatureV2, len(signers))
	msgs := make([]sdk.Msg, len(signers))
	for i, f := range signers {
		msgs[i] = sendMsg(f)
		sigs[i] = txsigning.SignatureV2{PubKey: f.Key.PubKey(), Sequence: seq, Data: &txsigning.SingleSignatureData{SignMode: txsigning.SignMode_SIGN_MODE_DIRECT}}
	}
	require.NoError(t, builder.SetMsgs(msgs...))
	require.NoError(t, builder.SetSignatures(sigs...))
	for i, f := range signers {
		acc := a.AccountKeeper.GetAccount(a.GetContextForCheckTx(nil), f.Address())
		var err error
		sigs[i], err = clienttx.SignWithPrivKey(context.Background(), txsigning.SignMode_SIGN_MODE_DIRECT, authsigning.SignerData{
			Address: f.Address().String(), ChainID: admissionChain, AccountNumber: acc.GetAccountNumber(), Sequence: seq, PubKey: f.Key.PubKey(),
		}, builder, f.Key, cfg, seq)
		require.NoError(t, err)
	}
	require.NoError(t, builder.SetSignatures(sigs...))
	bz, err := cfg.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return bz
}

func TestUnorderedMempoolLifecycle(t *testing.T) {
	t.Run("mixed transactions preserve replay protection through commit and recheck", func(t *testing.T) {
		f := newAdmissionFixture(t, 1)
		a := f.apps[0]
		pool := a.Pool()
		ordered := signedPending(t, a, f.normal, 0, sendMsg(f.normal))
		later := admissionTime.Add(2 * time.Minute)
		earlier := admissionTime.Add(time.Minute)
		u1 := signedUnordered(t, a, 0, later, "later timestamp arrives first", f.normal)
		u2 := signedUnordered(t, a, 0, earlier, "earlier timestamp arrives second", f.normal)
		for _, bz := range [][]byte{ordered, u1, u2} {
			res, err := a.CheckTx(&abci.RequestCheckTx{Tx: bz})
			require.NoError(t, err)
			require.Zero(t, res.Code, res.Log)
			require.Equal(t, uint64(1), a.AccountKeeper.GetAccount(a.GetContextForCheckTx(nil), f.normal.Address()).GetSequence())
		}
		before := pool.Snapshot()
		require.Len(t, before, 3)
		commit := func(height int64, txs [][]byte) {
			t.Helper()
			hash := make([]byte, 32)
			hash[0] = byte(height)
			blockTime := admissionTime.Add(time.Duration(height) * time.Second)
			processed, err := a.ProcessProposal(&abci.RequestProcessProposal{Height: height, Time: blockTime, Hash: hash, Txs: txs})
			require.NoError(t, err)
			require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processed.Status)
			res, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height, Time: blockTime, Hash: hash, Txs: txs})
			require.NoError(t, err)
			for _, r := range res.TxResults {
				require.Zero(t, r.Code, r.Log)
			}
			_, err = a.Commit()
			require.NoError(t, err)
		}
		commit(2, nil)
		after := pool.Snapshot()
		require.Len(t, after, 3)
		for i := range before {
			require.Equal(t, before[i].Key, after[i].Key)
		}
		proposal, err := a.PrepareProposal(&abci.RequestPrepareProposal{Height: 3, Time: admissionTime.Add(3 * time.Second), MaxTxBytes: 1 << 20})
		require.NoError(t, err)
		var expected [][]byte
		for _, e := range before {
			expected = append(expected, proposalBytes(t, a, e))
		}
		require.Equal(t, expected, proposal.Txs, "proposal follows SDK priority/nonce order")
		commit(3, [][]byte{u1})
		require.Equal(t, 2, pool.CountTx())
		conflict := signedUnordered(t, a, 0, later, "different bytes with a committed nonce", f.normal)
		res, err := a.CheckTx(&abci.RequestCheckTx{Tx: conflict})
		require.NoError(t, err)
		require.NotZero(t, res.Code)
		require.Contains(t, res.Log, "already used timeout")
		proposal, err = a.PrepareProposal(&abci.RequestPrepareProposal{Height: 4, Time: admissionTime.Add(4 * time.Second), MaxTxBytes: 1 << 20})
		require.NoError(t, err)
		require.ElementsMatch(t, [][]byte{u2, ordered}, proposal.Txs)
		commit(4, proposal.Txs)
		require.Zero(t, pool.CountTx())
		require.Equal(t, uint64(1), a.AccountKeeper.GetAccount(a.GetContextForCheckTx(nil), f.normal.Address()).GetSequence())
	})
}

func TestUnorderedMempoolValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		seq     uint64
		timeout time.Time
		want    string
	}{
		{"sequence must be zero", 1, admissionTime.Add(time.Minute), "sequence is not allowed"},
		{"missing timeout", 0, time.Time{}, "timeout_timestamp set"},
		{"expired", 0, admissionTime.Add(-time.Second), "tx timeout"},
		{"beyond maximum lifetime", 0, admissionTime.Add(10*time.Minute + time.Nanosecond), "ttl exceeds"},
		{"maximum lifetime is valid", 0, admissionTime.Add(10 * time.Minute), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAdmissionFixture(t, 1)
			a := f.apps[0]
			bz := signedUnordered(t, a, tc.seq, tc.timeout, tc.name, f.normal)
			res, err := a.CheckTx(&abci.RequestCheckTx{Tx: bz})
			require.NoError(t, err)
			if tc.want == "" {
				require.Zero(t, res.Code, res.Log)
			} else {
				require.NotZero(t, res.Code)
				require.Contains(t, res.Log, tc.want)
				require.Zero(t, a.Pool().CountTx())
			}
			require.Zero(t, a.AccountKeeper.GetAccount(a.GetContextForCheckTx(nil), f.normal.Address()).GetSequence())
		})
	}
	t.Run("replay checks every signer and rolls back earlier nonce writes", func(t *testing.T) {
		f := newAdmissionFixture(t, 1)
		a := f.apps[0]
		timeout := admissionTime.Add(time.Minute)
		first := signedUnordered(t, a, 0, timeout, "first", f.normal, f.voter)
		res, err := a.CheckTx(&abci.RequestCheckTx{Tx: first})
		require.NoError(t, err)
		require.Zero(t, res.Code, res.Log)
		conflict := signedUnordered(t, a, 0, timeout, "second signer conflicts", f.committee, f.voter)
		res, err = a.CheckTx(&abci.RequestCheckTx{Tx: conflict})
		require.NoError(t, err)
		require.NotZero(t, res.Code)
		require.Contains(t, res.Log, "already used timeout")
		has, err := a.AccountKeeper.ContainsUnorderedNonce(a.GetContextForCheckTx(nil), f.committee.Address(), timeout)
		require.NoError(t, err)
		require.False(t, has, "failure on the second signer must discard the first signer's nonce")
		distinct := signedUnordered(t, a, 0, timeout.Add(time.Nanosecond), "distinct nonce", f.normal, f.voter)
		res, err = a.CheckTx(&abci.RequestCheckTx{Tx: distinct})
		require.NoError(t, err)
		require.Zero(t, res.Code, res.Log)
	})
	t.Run("capacity rejection does not consume unordered nonce or fees", func(t *testing.T) {
		f := newAdmissionFixture(t, 1)
		a := f.apps[0]
		fillNormal(t, f)
		timeout := admissionTime.Add(time.Minute)
		bz := signedUnordered(t, a, 0, timeout, "capacity", f.normal)
		ctx := a.GetContextForCheckTx(nil)
		balance := a.BankKeeper.GetAllBalances(ctx, f.normal.Address())
		res, err := a.CheckTx(&abci.RequestCheckTx{Tx: bz})
		require.NoError(t, err)
		require.Equal(t, mempool.ErrCapacity.ABCICode(), res.Code, res.Log)
		has, err := a.AccountKeeper.ContainsUnorderedNonce(ctx, f.normal.Address(), timeout)
		require.NoError(t, err)
		require.False(t, has)
		require.Equal(t, balance, a.BankKeeper.GetAllBalances(ctx, f.normal.Address()))
		pool := a.Pool()
		require.NoError(t, pool.Remove(pool.Snapshot()[0].Tx))
		res, err = a.CheckTx(&abci.RequestCheckTx{Tx: bz})
		require.NoError(t, err)
		require.Zero(t, res.Code, res.Log)
	})
	t.Run("expired pending transactions are discarded on recheck", func(t *testing.T) {
		f := newAdmissionFixture(t, 1)
		a := f.apps[0]
		timeout := admissionTime.Add(time.Second)
		bz := signedUnordered(t, a, 0, timeout, "expires", f.normal)
		res, err := a.CheckTx(&abci.RequestCheckTx{Tx: bz})
		require.NoError(t, err)
		require.Zero(t, res.Code, res.Log)
		_, err = a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 2, Time: admissionTime.Add(2 * time.Second)})
		require.NoError(t, err)
		_, err = a.Commit()
		require.NoError(t, err)
		res, err = a.CheckTx(&abci.RequestCheckTx{Tx: bz, Type: abci.CheckTxType_Recheck})
		require.NoError(t, err)
		require.NotZero(t, res.Code)
		require.Zero(t, a.Pool().CountTx())
		has, err := a.AccountKeeper.ContainsUnorderedNonce(a.GetContextForCheckTx(nil), f.normal.Address(), timeout)
		require.NoError(t, err)
		require.False(t, has)
	})
}

func TestAdmissionExecutionModes(t *testing.T) {
	type observation struct {
		mode           sdk.ExecMode
		check, recheck bool
	}
	var observed []observation
	f := newAdmissionFixture(t, 1, func(a *app.ArkApp) {
		next := a.AnteHandler()
		a.SetAnteHandler(func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
			observed = append(observed, observation{ctx.ExecMode(), ctx.IsCheckTx(), ctx.IsReCheckTx()})
			return next(ctx, tx, simulate)
		})
	})
	a := f.apps[0]
	pool := a.Pool()
	admit := func(seq uint64) {
		t.Helper()
		res, err := a.CheckTx(&abci.RequestCheckTx{Tx: signedPending(t, a, f.normal, seq, sendMsg(f.normal))})
		require.NoError(t, err)
		require.Zero(t, res.Code, res.Log)
	}
	admit(0)
	_, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 2, Time: admissionTime.Add(time.Second)})
	require.NoError(t, err)
	_, err = a.Commit()
	require.NoError(t, err)
	require.Equal(t, 1, pool.CountTx(), "commit resets check state; CometBFT rechecks pending transactions next")
	// CometBFT drives the SDK recheck, including ante.
	res, err := a.CheckTx(&abci.RequestCheckTx{Tx: proposalBytes(t, a, pool.Snapshot()[0]), Type: abci.CheckTxType_Recheck})
	require.NoError(t, err)
	require.Zero(t, res.Code, res.Log)
	admit(1)
	require.Equal(t, []observation{
		{sdk.ExecModeCheck, true, false},
		{sdk.ExecModeReCheck, true, true},
		{sdk.ExecModeCheck, true, false},
	}, observed)
	require.Equal(t, 2, pool.CountTx())
}

// proposalBytes re-encodes a pending entry as proposals do.
func proposalBytes(t *testing.T, a *app.ArkApp, e mempool.Entry) []byte {
	t.Helper()
	bz, err := a.TxEncode(e.Tx)
	require.NoError(t, err)
	return bz
}

// sdkReference installs the unmodified SDK pool and Ark's ordinary ante chain,
// without the reservation wrapper. Fixtures call it before sealing BaseApp.
func sdkReference(a *app.ArkApp) {
	a.SetMempool(sdkmempool.DefaultPriorityMempool())
	a.SetAnteHandler(ante.NewAnteHandler(a.AppCodec(), a.GetTxConfig(), a.AccountKeeper, a.BankKeeper,
		a.FeeGrantKeeper, a.StakingKeeper, a.TreasuryKeeper, a.Privileges(), a.IBCKeeper,
		a.WasmKeeper.GetGasRegister(), wasmtypes.DefaultNodeConfig(), runtime.NewKVStoreService(a.GetKey(wasmtypes.StoreKey))))
	a.SetPrepareCheckStater(nil)
	handler := baseapp.NewDefaultProposalHandler(a.Mempool(), a)
	a.SetPrepareProposal(handler.PrepareProposalHandler())
	a.SetProcessProposal(handler.ProcessProposalHandler())
}

func TestCheckTxSDKResponseParity(t *testing.T) {
	for _, name := range []string{"fresh", "duplicate", "recheck", "malformed", "unknown type", "post failure", "unordered post failure"} {
		t.Run(name, func(t *testing.T) {
			configured := 0
			f := newAdmissionFixture(t, 2, func(a *app.ArkApp) {
				if configured == 1 {
					sdkReference(a)
				}
				configured++
				if name == "post failure" || name == "unordered post failure" {
					a.SetPostHandler(func(ctx sdk.Context, _ sdk.Tx, _, _ bool) (sdk.Context, error) {
						if ctx.ExecMode() == sdk.ExecModeCheck {
							return ctx, sdkerrors.ErrInvalidRequest.Wrap("post-handler failure")
						}
						return ctx, nil
					})
				}
			})
			left, right := f.apps[0], f.apps[1]
			// Both use the same ante policy; the reference uses the unwrapped SDK
			// pool. The transactions stay below every reservation limit.

			bz := signedPending(t, left, f.normal, 0, sendMsg(f.normal))
			if name == "unordered post failure" {
				bz = signedUnordered(t, left, 0, admissionTime.Add(time.Minute), "late failure", f.normal)
			}
			req := &abci.RequestCheckTx{Tx: bz}
			if name == "malformed" {
				req.Tx = []byte{0xff}
			}
			if name == "unknown type" {
				req.Type = abci.CheckTxType(99)
			}
			if name == "recheck" {
				req.Type = abci.CheckTxType_Recheck
			}
			l, le := left.CheckTx(req)
			r, re := right.App.CheckTx(req)
			require.Equal(t, re, le)
			require.Equal(t, r, l)
			if name == "duplicate" {
				l, le = left.CheckTx(req)
				r, re = right.App.CheckTx(req)
				require.Equal(t, re, le)
				require.Equal(t, r, l)
			}
			lc, rc := left.GetContextForCheckTx(nil), right.GetContextForCheckTx(nil)
			require.Equal(t, right.BankKeeper.GetAllBalances(rc, f.normal.Address()), left.BankKeeper.GetAllBalances(lc, f.normal.Address()))
			require.Equal(t, right.AccountKeeper.GetAccount(rc, f.normal.Address()).GetSequence(), left.AccountKeeper.GetAccount(lc, f.normal.Address()).GetSequence())
			require.Equal(t, right.Mempool().CountTx(), left.Pool().CountTx())
			if name == "unordered post failure" {
				lh, le := left.AccountKeeper.ContainsUnorderedNonce(lc, f.normal.Address(), admissionTime.Add(time.Minute))
				rh, re := right.AccountKeeper.ContainsUnorderedNonce(rc, f.normal.Address(), admissionTime.Add(time.Minute))
				require.Equal(t, re, le)
				require.Equal(t, rh, lh, "late failures retain exactly the SDK replay-state semantics")
			}
		})
	}
}

func TestAdmissionSDKCapacityRollback(t *testing.T) {
	t.Run("governance fallback cannot retain ante writes when normal storage is full", func(t *testing.T) {
		calls := 0
		f := newAdmissionFixture(t, 1, func(a *app.ArkApp) {
			next := a.AnteHandler()
			a.SetAnteHandler(func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
				calls++
				return next(ctx, tx, simulate)
			})
		})
		a := f.apps[0]
		fillNormal(t, f)
		msg := voteMsg(f.voter).(*gov.MsgVote)
		msg.ProposalId = 999 // Valid vote envelope, but no proposal exists to grant reserved priority.
		bz := signedPending(t, a, f.voter, 0, msg)
		pool := a.Pool()
		before := pool.Snapshot()
		ctx := a.GetContextForCheckTx(nil)
		balance := a.BankKeeper.GetAllBalances(ctx, f.voter.Address())
		callsBefore := calls
		res, err := a.CheckTx(&abci.RequestCheckTx{Tx: bz})
		require.NoError(t, err)
		require.Equal(t, mempool.ErrCapacity.ABCICode(), res.Code, res.Log)
		require.Greater(t, calls, callsBefore, "candidate capacity allowed real SDK ante execution")
		require.Equal(t, before, pool.Snapshot())
		require.Zero(t, a.AccountKeeper.GetAccount(ctx, f.voter.Address()).GetSequence())
		require.Equal(t, balance, a.BankKeeper.GetAllBalances(ctx, f.voter.Address()))
	})
}

// Keep the SDK runtime as the reference for fresh admission. Both apps use the
// same Ark ante chain; local overload has its own refusal code.
func TestAdmissionSDKParity(t *testing.T) {
	for _, kind := range []string{"normal", "governance", "committee", "future sequence", "bad signature", "bad committee term", "unordered", "expired unordered"} {
		t.Run(kind, func(t *testing.T) {
			configured := 0
			f := newAdmissionFixture(t, 2, func(a *app.ArkApp) {
				if configured == 1 {
					sdkReference(a)
				}
				configured++
			})
			signer := f.normal
			msg := sendMsg(signer)
			seq := uint64(0)
			switch kind {
			case "governance":
				signer = f.voter
				msg = voteMsg(signer)
			case "committee":
				signer = f.committee
				msg = committeeMsg(signer)
			case "future sequence":
				seq = 1
			case "bad signature":
				signer.Key = f.committee.Key
			case "bad committee term":
				signer = f.committee
				msg = committeeMsg(signer)
				msg.(*security.MsgCommitteePlanUpgrade).ExpectedTerm++
			}
			bz := signedPending(t, f.apps[0], signer, seq, msg)
			if kind == "unordered" || kind == "expired unordered" {
				timeout := admissionTime.Add(time.Minute)
				if kind == "expired unordered" {
					timeout = admissionTime.Add(-time.Second)
				}
				bz = signedUnordered(t, f.apps[0], 0, timeout, "SDK parity", signer)
			}
			custom, err := f.apps[0].CheckTx(&abci.RequestCheckTx{Tx: bz})
			require.NoError(t, err)
			upstream, err := f.apps[1].App.CheckTx(&abci.RequestCheckTx{Tx: bz})
			require.NoError(t, err)
			require.Equal(t, upstream, custom)
			require.Equal(t, upstream.Code == 0, f.apps[0].Pool().CountTx() == 1, "the pool admits exactly what the SDK accepted")
			for _, account := range []apptest.Funder{f.normal, f.voter, f.committee} {
				left, right := f.apps[0], f.apps[1]
				lc, rc := left.GetContextForCheckTx(nil), right.GetContextForCheckTx(nil)
				require.Equal(t, right.AccountKeeper.GetAccount(rc, account.Address()).GetSequence(), left.AccountKeeper.GetAccount(lc, account.Address()).GetSequence())
				require.Equal(t, right.BankKeeper.GetAllBalances(rc, account.Address()), left.BankKeeper.GetAllBalances(lc, account.Address()))
			}
		})
	}
}

func TestProposalSDKParity(t *testing.T) {
	for _, name := range []string{"fee ties", "mixed ordered and unordered", "byte limit", "SDK encoding"} {
		t.Run(name, func(t *testing.T) {
			configured := 0
			f := newAdmissionFixture(t, 2, func(a *app.ArkApp) {
				if configured == 1 {
					sdkReference(a)
				}
				configured++
			})
			left, right := f.apps[0], f.apps[1]
			for seq := uint64(0); seq < 3; seq++ {
				for _, signer := range []apptest.Funder{f.normal, f.voter, f.committee} {
					bz := signedPending(t, left, signer, seq, sendMsg(signer))
					if name == "SDK encoding" {
						// The decoder accepts a repeated body field; the SDK encoder
						// emits the effective body once when building the proposal.
						_, _, n := protowire.ConsumeField(bz)
						require.Positive(t, n)
						bz = append(append([]byte(nil), bz[:n]...), bz...)
					}
					for _, a := range f.apps {
						res, err := a.CheckTx(&abci.RequestCheckTx{Tx: bz})
						require.NoError(t, err)
						require.Zero(t, res.Code, res.Log)
					}
				}
			}
			if name == "mixed ordered and unordered" {
				for i := range 3 {
					bz := signedUnordered(t, left, 0, admissionTime.Add(time.Duration(i+1)*time.Minute), "unordered", f.normal)
					for _, a := range f.apps {
						res, err := a.CheckTx(&abci.RequestCheckTx{Tx: bz})
						require.NoError(t, err)
						require.Zero(t, res.Code, res.Log)
					}
				}
			}
			maxBytes := int64(1 << 20)
			if name == "byte limit" {
				maxBytes = 1100
			}
			req := &abci.RequestPrepareProposal{Height: 2, Time: admissionTime.Add(time.Second), MaxTxBytes: maxBytes}
			l, le := left.PrepareProposal(req)
			r, re := right.PrepareProposal(req)
			require.NoError(t, le)
			require.NoError(t, re)
			require.Equal(t, r.Txs, l.Txs)
			res, err := left.ProcessProposal(&abci.RequestProcessProposal{Height: 2, Time: req.Time, Hash: make([]byte, 32), Txs: l.Txs})
			require.NoError(t, err)
			require.Equal(t, abci.ResponseProcessProposal_ACCEPT, res.Status)
		})
	}
}

// The SDK handler must accept blocks built with capped lanes, while retaining
// its selected-sequence checks when a capped vote precedes an ordinary action.
func TestProposalVoteCapPreservesSDKValidity(t *testing.T) {
	for _, successor := range []bool{false, true} {
		t.Run(fmt.Sprint(successor), func(t *testing.T) {
			f := newAdmissionFixture(t, 1)
			a := f.apps[0]
			var want [][]byte
			for seq := uint64(0); seq < 6; seq++ {
				bz := signedPending(t, a, f.voter, seq, voteMsg(f.voter))
				res, err := a.CheckTx(&abci.RequestCheckTx{Tx: bz})
				require.NoError(t, err)
				require.Zero(t, res.Code, res.Log)
				if seq < 5 {
					want = append(want, bz)
				}
			}
			if successor {
				bz := signedPending(t, a, f.voter, 6, sendMsg(f.voter))
				res, err := a.CheckTx(&abci.RequestCheckTx{Tx: bz})
				require.NoError(t, err)
				require.Zero(t, res.Code, res.Log)
			}
			normal := signedPending(t, a, f.normal, 0, sendMsg(f.normal))
			res, err := a.CheckTx(&abci.RequestCheckTx{Tx: normal})
			require.NoError(t, err)
			require.Zero(t, res.Code, res.Log)
			want = append(want, normal)
			blockTime := admissionTime.Add(time.Second)
			proposal, err := a.PrepareProposal(&abci.RequestPrepareProposal{Height: 2, Time: blockTime, MaxTxBytes: 1 << 20})
			require.NoError(t, err)
			require.Equal(t, want, proposal.Txs, "the sixth 1M-gas vote exceeds the 5M-gas allowance")
			accepted, err := a.ProcessProposal(&abci.RequestProcessProposal{Height: 2, Time: blockTime, Hash: make([]byte, 32), Txs: proposal.Txs})
			require.NoError(t, err)
			require.Equal(t, abci.ResponseProcessProposal_ACCEPT, accepted.Status)
		})
	}
}

func TestReservedHeadroomSurvivesFinalizeUntilCometUpdate(t *testing.T) {
	t.Run("normal traffic cannot use storage still held by the flood list", func(t *testing.T) {
		f := newAdmissionFixture(t, 1)
		a := f.apps[0]
		mp := newClist(t, a, 20)
		var first []byte
		for seq := uint64(0); seq < 18; seq++ {
			bz := signedPending(t, a, f.normal, seq, sendMsg(f.normal))
			if seq == 0 {
				first = bz
			}
			res, err := checkTx(t, mp, bz)
			require.NoError(t, err)
			require.Zero(t, res.Code, res.Log)
		}
		block, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 2, Time: admissionTime.Add(time.Second), Txs: [][]byte{first}})
		require.NoError(t, err)
		require.Zero(t, block.TxResults[0].Code)
		require.Equal(t, 18, mp.Size())
		require.Equal(t, 18, a.Pool().CountTx())
		res, err := checkTx(t, mp, signedPending(t, a, f.normal, 18, sendMsg(f.normal)))
		require.NoError(t, err)
		require.Equal(t, mempool.ErrCapacity.ABCICode(), res.Code)
		for _, entry := range []struct {
			signer apptest.Funder
			msg    sdk.Msg
		}{{f.voter, voteMsg(f.voter)}, {f.committee, committeeMsg(f.committee)}} {
			res, err = checkTx(t, mp, signedPending(t, a, entry.signer, 0, entry.msg))
			require.NoError(t, err)
			require.Zero(t, res.Code, res.Log)
		}
		require.Equal(t, 20, mp.Size())
		mp.Lock()
		_, err = a.Commit()
		require.NoError(t, err)
		err = mp.Update(2, cmttypes.Txs{first}, block.TxResults, nil, nil)
		mp.Unlock()
		require.NoError(t, err)
		require.Equal(t, 19, mp.Size())
		require.Equal(t, 19, a.Pool().CountTx())
		res, err = checkTx(t, mp, signedPending(t, a, f.normal, 18, sendMsg(f.normal)))
		require.NoError(t, err)
		require.Zero(t, res.Code, res.Log)
	})
}
