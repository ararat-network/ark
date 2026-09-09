package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"math/rand"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtcfg "github.com/cometbft/cometbft/config"
	cmtpool "github.com/cometbft/cometbft/mempool"
	"github.com/cometbft/cometbft/p2p"
	rpccore "github.com/cometbft/cometbft/rpc/core"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/server"
	sim "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
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
		require.Equal(t, abci.CodeTypeRetry, res.Code)
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
		counts, _ := a.Mempool().(*mempool.Pool).Usage()
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
			counts, _ := a.Mempool().(*mempool.Pool).Usage()
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

type reactorClient struct{ abci.Application }

func (reactorClient) Flush(context.Context) error { return nil }

func TestAppModePublicPropagation(t *testing.T) {
	t.Run("RPC admission gossips through saturated peers to another proposer", func(t *testing.T) {
		f := newAdmissionFixture(t, 3)
		fillNormal(t, f)
		var pools []*cmtpool.AppMempool
		var reactors []*cmtpool.AppReactor
		for _, a := range f.apps {
			cfg := cmtcfg.DefaultMempoolConfig()
			cfg.Type = cmtcfg.MempoolTypeApp
			cfg.Size = 1
			cfg.ReapInterval = 20 * time.Millisecond
			cfg.ReapMaxBytes = 4096
			mp := cmtpool.NewAppMempool(cfg, reactorClient{server.NewCometABCIWrapper(a)})
			pools = append(pools, mp)
			reactors = append(reactors, cmtpool.NewAppReactor(cfg, mp, false))
		}
		switches := p2p.MakeConnectedSwitches(cmtcfg.TestConfig().P2P, 3, func(i int, s *p2p.Switch) *p2p.Switch { s.AddReactor("MEMPOOL", reactors[i]); return s }, p2p.Connect2Switches)
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
				p := a.Mempool().(*mempool.Pool)
				if !p.Has(vote) || !p.Has(committee) {
					return false
				}
			}
			return true
		}, 10*time.Second, 20*time.Millisecond)
		proposer := f.apps[2]
		proposal, err := proposer.PrepareProposal(&abci.RequestPrepareProposal{Height: 2, Time: admissionTime.Add(6 * time.Second), MaxTxBytes: 1 << 20})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(proposal.Txs), 2)
		require.Equal(t, committee, proposal.Txs[0], "committee action fits its preferential gas and byte budgets")
		require.Equal(t, vote, proposal.Txs[1], "vote fits the reduced governance budgets before ordinary traffic")
		for _, a := range f.apps {
			res, err := a.ProcessProposal(&abci.RequestProcessProposal{Height: 2, Time: admissionTime.Add(6 * time.Second), Txs: proposal.Txs})
			require.NoError(t, err)
			require.Equal(t, abci.ResponseProcessProposal_ACCEPT, res.Status)
		}
		for _, a := range f.apps {
			res, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 2, Time: admissionTime.Add(6 * time.Second), Txs: proposal.Txs})
			require.NoError(t, err)
			for _, result := range res.TxResults {
				require.Zero(t, result.Code, result.Log)
			}
			_, err = a.Commit()
			require.NoError(t, err)
			require.Zero(t, a.Mempool().CountTx())
			plan, err := a.UpgradeKeeper.GetUpgradePlan(a.GetContextForCheckTx(nil))
			require.NoError(t, err)
			require.Equal(t, int64(100), plan.Height)
		}
	})
}

func TestConcurrentAdmissionAndCommit(t *testing.T) {
	t.Run("state reset and admission are fenced", func(t *testing.T) {
		f := newAdmissionFixture(t, 1)
		a := f.apps[0]
		bz := signedPending(t, a, f.normal, 0, sendMsg(f.normal))
		var wg sync.WaitGroup
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 5; j++ {
					_, _ = a.CheckTx(&abci.RequestCheckTx{Tx: bz})
					_, _ = a.ReapTxs(&abci.RequestReapTxs{MaxBytes: 4096})
				}
			}()
		}
		_, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 2, Time: admissionTime.Add(6 * time.Second)})
		require.NoError(t, err)
		_, err = a.Commit()
		require.NoError(t, err)
		wg.Wait()
		require.Equal(t, 1, a.Mempool().CountTx())
	})
}

func TestGossipRotationAcrossCommit(t *testing.T) {
	for _, included := range []bool{false, true} {
		name := "empty block"
		if included {
			name = "committed predecessor"
		}
		t.Run(name, func(t *testing.T) {
			f := newAdmissionFixture(t, 1)
			a := f.apps[0]
			var txs [][]byte
			for seq := uint64(0); seq < 3; seq++ {
				bz := signedPending(t, a, f.normal, seq, sendMsg(f.normal))
				res, err := a.CheckTx(&abci.RequestCheckTx{Tx: bz})
				require.NoError(t, err)
				require.Zero(t, res.Code, res.Log)
				txs = append(txs, bz)
			}
			pool := a.Mempool().(*mempool.Pool)
			now := admissionTime
			batchBytes := uint64(len(txs[0]) + 10)
			for i := 0; i < 2; i++ {
				require.Equal(t, [][]byte{txs[i]}, pool.Gossip(batchBytes, 0, now))
			}
			before := pool.Snapshot()
			var committed [][]byte
			if included {
				committed = txs[:1]
			}
			_, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 2, Time: admissionTime.Add(time.Second), Txs: committed})
			require.NoError(t, err)
			_, err = a.Commit()
			require.NoError(t, err)
			after := pool.Snapshot()
			require.Len(t, after, len(before)-len(committed))
			for i, e := range after {
				old := before[i+len(committed)]
				require.Equal(t, old.Key, e.Key)
				require.Equal(t, old.Added, e.Added)
				require.Equal(t, old.LastGossip, e.LastGossip)
			}
			require.Equal(t, [][]byte{txs[2]}, pool.Gossip(batchBytes, 0, now.Add(time.Second)))
			require.Empty(t, pool.Gossip(batchBytes, 0, now.Add(2*time.Second)))
			require.Equal(t, [][]byte{after[0].Bytes}, pool.Gossip(batchBytes, 0, now.Add(5*time.Second)))
		})
	}
}

func TestPeerSequenceRetry(t *testing.T) {
	t.Run("rebroadcast repairs a missing predecessor while RPC keeps its error", func(t *testing.T) {
		f := newAdmissionFixture(t, 1)
		a := f.apps[0]
		first := signedPending(t, a, f.normal, 0, sendMsg(f.normal))
		next := signedPending(t, a, f.normal, 1, sendMsg(f.normal))
		res, err := a.CheckTx(&abci.RequestCheckTx{Tx: next})
		require.NoError(t, err)
		require.Equal(t, sdkerrors.ErrWrongSequence.ABCICode(), res.Code)
		require.Equal(t, sdkerrors.ErrWrongSequence.Codespace(), res.Codespace)
		peer, err := a.InsertTx(&abci.RequestInsertTx{Tx: next})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeRetry, peer.Code)
		require.Zero(t, a.Mempool().CountTx())

		cfg := cmtcfg.DefaultMempoolConfig()
		cfg.Type = cmtcfg.MempoolTypeApp
		cfg.CheckTxRetryDelay = time.Millisecond
		mp := cmtpool.NewAppMempool(cfg, reactorClient{server.NewCometABCIWrapper(a)})
		require.Error(t, mp.InsertTx(next))
		require.NoError(t, mp.InsertTx(first))
		require.Eventually(t, func() bool { return mp.InsertTx(next) == nil }, time.Second, time.Millisecond)
		require.Equal(t, 2, a.Mempool().CountTx())
	})
	t.Run("invalid signatures remain non-retryable", func(t *testing.T) {
		f := newAdmissionFixture(t, 1)
		a := f.apps[0]
		signer := f.normal
		signer.Key = f.committee.Key
		bz := signedPending(t, a, signer, 0, sendMsg(f.normal))
		res, err := a.InsertTx(&abci.RequestInsertTx{Tx: bz})
		require.NoError(t, err)
		require.NotZero(t, res.Code)
		require.Less(t, res.Code, abci.CodeTypeRetry)
		require.Zero(t, a.Mempool().CountTx())
	})
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
		counts, sizes := a.Mempool().(*mempool.Pool).Usage()
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
	t.Run("snapshot reset", func(t *testing.T) {
		_, _ = a.ApplySnapshotChunk(&abci.RequestApplySnapshotChunk{})
		check(t, 0)
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
		pool := a.Mempool().(*mempool.Pool)
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
		require.Greater(t, before[1].Fee, before[0].Fee)
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
			require.Equal(t, before[i].Added, after[i].Added)
		}
		proposal, err := a.PrepareProposal(&abci.RequestPrepareProposal{Height: 3, Time: admissionTime.Add(3 * time.Second), MaxTxBytes: 1 << 20})
		require.NoError(t, err)
		require.Equal(t, [][]byte{u1, u2, ordered}, proposal.Txs, "unordered timestamps do not impose sender ordering")
		commit(3, [][]byte{u1})
		require.Equal(t, 2, pool.CountTx())
		conflict := signedUnordered(t, a, 0, later, "different bytes with a committed nonce", f.normal)
		res, err := a.CheckTx(&abci.RequestCheckTx{Tx: conflict})
		require.NoError(t, err)
		require.NotZero(t, res.Code)
		require.Contains(t, res.Log, "already used timeout")
		proposal, err = a.PrepareProposal(&abci.RequestPrepareProposal{Height: 4, Time: admissionTime.Add(4 * time.Second), MaxTxBytes: 1 << 20})
		require.NoError(t, err)
		require.Equal(t, [][]byte{u2, ordered}, proposal.Txs)
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
				require.Zero(t, a.Mempool().CountTx())
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
		require.Equal(t, abci.CodeTypeRetry, res.Code)
		has, err := a.AccountKeeper.ContainsUnorderedNonce(ctx, f.normal.Address(), timeout)
		require.NoError(t, err)
		require.False(t, has)
		require.Equal(t, balance, a.BankKeeper.GetAllBalances(ctx, f.normal.Address()))
		pool := a.Mempool().(*mempool.Pool)
		pool.RemoveBytes(pool.Snapshot()[0].Bytes)
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
		require.Zero(t, a.Mempool().CountTx())
		has, err := a.AccountKeeper.ContainsUnorderedNonce(a.GetContextForCheckTx(nil), f.normal.Address(), timeout)
		require.NoError(t, err)
		require.False(t, has)
	})
}

func TestAdmissionExecutionModes(t *testing.T) {
	for _, peer := range []bool{false, true} {
		name := "RPC"
		if peer {
			name = "peer"
		}
		t.Run(name, func(t *testing.T) {
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
			pool := a.Mempool().(*mempool.Pool)
			handler := a
			admit := func(seq uint64) {
				t.Helper()
				bz := signedPending(t, a, f.normal, seq, sendMsg(f.normal))
				if peer {
					res, err := handler.InsertTx(&abci.RequestInsertTx{Tx: bz})
					require.NoError(t, err)
					require.Zero(t, res.Code)
				} else {
					res, err := handler.CheckTx(&abci.RequestCheckTx{Tx: bz})
					require.NoError(t, err)
					require.Zero(t, res.Code, res.Log)
				}
			}
			admit(0)
			_, err := handler.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 2, Time: admissionTime.Add(time.Second)})
			require.NoError(t, err)
			_, err = handler.Commit()
			require.NoError(t, err)
			require.Equal(t, 1, pool.CountTx(), "internal recheck retains the pending transaction")
			admit(1)
			require.Equal(t, []observation{
				{sdk.ExecModeCheck, true, false},
				{sdk.ExecModeReCheck, true, true},
				{sdk.ExecModeCheck, true, false},
			}, observed)
			require.Equal(t, 2, pool.CountTx())
		})
	}
}

func TestAdmissionRejectsExternalRecheck(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		pending, unordered, full  bool
		malformed, empty, unknown bool
	}{
		{name: "new ordered transaction"},
		{name: "pending ordered transaction", pending: true},
		{name: "new unordered transaction", unordered: true},
		{name: "pending unordered transaction", pending: true, unordered: true},
		{name: "full pool", full: true},
		{name: "malformed transaction", malformed: true},
		{name: "empty transaction", empty: true},
		{name: "unknown request type", unknown: true, malformed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAdmissionFixture(t, 1)
			a := f.apps[0]
			seq := uint64(0)
			if tc.full {
				fillNormal(t, f)
				seq = 18
			}
			timeout := admissionTime.Add(time.Minute)
			bz := signedPending(t, a, f.normal, seq, sendMsg(f.normal))
			if tc.unordered {
				bz = signedUnordered(t, a, 0, timeout, "external recheck", f.normal)
			}
			if tc.pending {
				res, err := a.CheckTx(&abci.RequestCheckTx{Tx: bz})
				require.NoError(t, err)
				require.Zero(t, res.Code, res.Log)
			}
			if tc.malformed {
				bz = []byte{0xff}
			}
			if tc.empty {
				bz = nil
			}
			pool := a.Mempool().(*mempool.Pool)
			before := pool.Snapshot()
			counts, sizes := pool.Usage()
			ctx := a.GetContextForCheckTx(nil)
			balance := a.BankKeeper.GetAllBalances(ctx, f.normal.Address())
			sequence := a.AccountKeeper.GetAccount(ctx, f.normal.Address()).GetSequence()
			nonce, err := a.AccountKeeper.ContainsUnorderedNonce(ctx, f.normal.Address(), timeout)
			require.NoError(t, err)
			requestType := abci.CheckTxType_Recheck
			want := sdkerrors.ErrNotSupported
			message := "external recheck is unsupported"
			if tc.unknown {
				requestType = abci.CheckTxType(99)
				want = sdkerrors.ErrInvalidRequest
				message = "unknown CheckTx type"
			}
			res, err := a.CheckTx(&abci.RequestCheckTx{Tx: bz, Type: requestType})
			require.NoError(t, err)
			require.Equal(t, want.ABCICode(), res.Code)
			require.Equal(t, want.Codespace(), res.Codespace)
			require.Contains(t, res.Log, message)
			require.Zero(t, res.GasWanted)
			require.Zero(t, res.GasUsed)
			require.Equal(t, before, pool.Snapshot())
			afterCounts, afterSizes := pool.Usage()
			require.Equal(t, counts, afterCounts)
			require.Equal(t, sizes, afterSizes)
			require.Equal(t, balance, a.BankKeeper.GetAllBalances(ctx, f.normal.Address()))
			require.Equal(t, sequence, a.AccountKeeper.GetAccount(ctx, f.normal.Address()).GetSequence())
			afterNonce, err := a.AccountKeeper.ContainsUnorderedNonce(ctx, f.normal.Address(), timeout)
			require.NoError(t, err)
			require.Equal(t, nonce, afterNonce)
		})
	}
}

func TestAdmissionRollsBackPostHandlerFailure(t *testing.T) {
	for _, unordered := range []bool{false, true} {
		name := "ordered"
		if unordered {
			name = "unordered"
		}
		t.Run(name, func(t *testing.T) {
			var target []byte
			fail := true
			calls := 0
			var inspect func(sdk.Context)
			f := newAdmissionFixture(t, 1, func(a *app.ArkApp) {
				next := ante.NewPostHandler(a.AccountKeeper, a.BankKeeper, a.FeeGrantKeeper)
				a.SetPostHandler(func(ctx sdk.Context, tx sdk.Tx, simulate, success bool) (sdk.Context, error) {
					if ctx.ExecMode() == sdk.ExecModeCheck && bytes.Equal(ctx.TxBytes(), target) {
						calls++
						require.True(t, success)
						require.True(t, a.Mempool().(*mempool.Pool).Has(target), "SDK inserted before post-handler execution")
						inspect(ctx)
						if fail {
							return ctx, sdkerrors.ErrInvalidRequest.Wrap("post-handler failure")
						}
					}
					return next(ctx, tx, simulate, success)
				})
			})
			a := f.apps[0]
			pool := a.Mempool().(*mempool.Pool)
			existing := signedPending(t, a, f.voter, 0, voteMsg(f.voter))
			res, err := a.CheckTx(&abci.RequestCheckTx{Tx: existing})
			require.NoError(t, err)
			require.Zero(t, res.Code, res.Log)
			timeout := admissionTime.Add(time.Minute)
			target = signedPending(t, a, f.normal, 0, sendMsg(f.normal))
			if unordered {
				target = signedUnordered(t, a, 0, timeout, "late failure", f.normal)
			}
			before := pool.Snapshot()
			counts, sizes := pool.Usage()
			parent := a.GetContextForCheckTx(nil)
			balance := a.BankKeeper.GetAllBalances(parent, f.normal.Address())
			inspect = func(ctx sdk.Context) {
				require.NotEqual(t, balance, a.BankKeeper.GetAllBalances(ctx, f.normal.Address()), "ante fees exist in the SDK branch")
				require.Equal(t, balance, a.BankKeeper.GetAllBalances(parent, f.normal.Address()), "parent state remains unchanged")
			}
			res, err = a.CheckTx(&abci.RequestCheckTx{Tx: target})
			require.NoError(t, err)
			require.Equal(t, sdkerrors.ErrInvalidRequest.ABCICode(), res.Code)
			require.Contains(t, res.Log, "post-handler failure")
			require.Positive(t, res.GasUsed)
			require.Equal(t, before, pool.Snapshot())
			c, b := pool.Usage()
			require.Equal(t, counts, c)
			require.Equal(t, sizes, b)
			require.Equal(t, balance, a.BankKeeper.GetAllBalances(parent, f.normal.Address()))
			require.Zero(t, a.AccountKeeper.GetAccount(parent, f.normal.Address()).GetSequence())
			has, err := a.AccountKeeper.ContainsUnorderedNonce(parent, f.normal.Address(), timeout)
			require.NoError(t, err)
			require.False(t, has)
			fail = false
			res, err = a.CheckTx(&abci.RequestCheckTx{Tx: target})
			require.NoError(t, err)
			require.Zero(t, res.Code, res.Log)
			require.True(t, pool.Has(target))
			require.True(t, pool.Has(existing))
			require.Equal(t, 2, calls)
			require.NotEqual(t, balance, a.BankKeeper.GetAllBalances(parent, f.normal.Address()))
			res, err = a.CheckTx(&abci.RequestCheckTx{Tx: target})
			require.NoError(t, err)
			require.Equal(t, abci.CodeTypeRetry, res.Code)
			require.Equal(t, 2, calls, "duplicates never reach SDK execution or rollback")
			require.True(t, pool.Has(target))
		})
	}
}

// Recheck and proposal verification mirror RunTx past ante: the post chain
// runs on a discarded branch and a failure there rejects, as at admission.
// Storage and proposal space are reserved only once it passes.
func TestPendingValidationRunsPostHandler(t *testing.T) {
	newFixture := func(t *testing.T, match func(sdk.Context) bool, target *[]byte, calls *int) admissionFixture {
		t.Helper()
		return newAdmissionFixture(t, 1, func(a *app.ArkApp) {
			next := ante.NewPostHandler(a.AccountKeeper, a.BankKeeper, a.FeeGrantKeeper)
			a.SetPostHandler(func(ctx sdk.Context, tx sdk.Tx, simulate, success bool) (sdk.Context, error) {
				if match(ctx) && bytes.Equal(ctx.TxBytes(), *target) {
					*calls++
					require.True(t, success)
					require.False(t, simulate)
					return ctx, sdkerrors.ErrInvalidRequest.Wrap("post-handler failure")
				}
				return next(ctx, tx, simulate, success)
			})
		})
	}
	t.Run("recheck", func(t *testing.T) {
		var target []byte
		calls := 0
		f := newFixture(t, func(ctx sdk.Context) bool { return ctx.IsReCheckTx() }, &target, &calls)
		a := f.apps[0]
		pool := a.Mempool().(*mempool.Pool)
		target = signedPending(t, a, f.normal, 0, sendMsg(f.normal))
		res, err := a.CheckTx(&abci.RequestCheckTx{Tx: target})
		require.NoError(t, err)
		require.Zero(t, res.Code, res.Log)
		require.True(t, pool.Has(target))
		_, err = a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 2, Time: admissionTime.Add(time.Second)})
		require.NoError(t, err)
		_, err = a.Commit()
		require.NoError(t, err)
		require.Equal(t, 1, calls)
		require.False(t, pool.Has(target), "a post-chain failure on recheck retires the transaction")
		require.Zero(t, a.AccountKeeper.GetAccount(a.GetContextForCheckTx(nil), f.normal.Address()).GetSequence(), "a failed recheck keeps no ante writes")
	})
	t.Run("proposal", func(t *testing.T) {
		var target []byte
		calls := 0
		f := newFixture(t, func(ctx sdk.Context) bool { return ctx.ExecMode() == sdk.ExecModePrepareProposal }, &target, &calls)
		a := f.apps[0]
		pool := a.Mempool().(*mempool.Pool)
		other := signedPending(t, a, f.voter, 0, sendMsg(f.voter))
		target = signedPending(t, a, f.normal, 0, sendMsg(f.normal))
		for _, bz := range [][]byte{other, target} {
			res, err := a.CheckTx(&abci.RequestCheckTx{Tx: bz})
			require.NoError(t, err)
			require.Zero(t, res.Code, res.Log)
		}
		proposal, err := a.PrepareProposal(&abci.RequestPrepareProposal{Height: 2, Time: admissionTime.Add(time.Second), MaxTxBytes: 1 << 20})
		require.NoError(t, err)
		require.Equal(t, 1, calls)
		require.Equal(t, [][]byte{other}, proposal.Txs, "a post-chain failure in proposal verification skips the candidate")
		require.True(t, pool.Has(target), "skipped candidates stay pending until recheck")
	})
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
		pool := a.Mempool().(*mempool.Pool)
		before := pool.Snapshot()
		ctx := a.GetContextForCheckTx(nil)
		balance := a.BankKeeper.GetAllBalances(ctx, f.voter.Address())
		callsBefore := calls
		res, err := a.CheckTx(&abci.RequestCheckTx{Tx: bz})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeRetry, res.Code, res.Log)
		require.Greater(t, calls, callsBefore, "candidate capacity allowed real SDK ante execution")
		require.Equal(t, before, pool.Snapshot())
		require.Zero(t, a.AccountKeeper.GetAccount(ctx, f.voter.Address()).GetSequence())
		require.Equal(t, balance, a.BankKeeper.GetAllBalances(ctx, f.voter.Address()))
	})
}

// Keep the SDK runtime as the reference for fresh admission. Both apps use the
// same Ark ante chain; local overload and external recheck have separate policies.
func TestAdmissionSDKParity(t *testing.T) {
	for _, kind := range []string{"normal", "governance", "committee", "future sequence", "bad signature", "bad committee term", "unordered", "expired unordered"} {
		t.Run(kind, func(t *testing.T) {
			f := newAdmissionFixture(t, 2)
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
			require.Equal(t, f.apps[1].Mempool().CountTx(), f.apps[0].Mempool().CountTx())
			for _, account := range []apptest.Funder{f.normal, f.voter, f.committee} {
				left, right := f.apps[0], f.apps[1]
				lc, rc := left.GetContextForCheckTx(nil), right.GetContextForCheckTx(nil)
				require.Equal(t, right.AccountKeeper.GetAccount(rc, account.Address()).GetSequence(), left.AccountKeeper.GetAccount(lc, account.Address()).GetSequence())
				require.Equal(t, right.BankKeeper.GetAllBalances(rc, account.Address()), left.BankKeeper.GetAllBalances(lc, account.Address()))
			}
		})
	}
}
