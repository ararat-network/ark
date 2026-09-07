package app_test

import (
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
	"github.com/cosmos/cosmos-sdk/server"
	sim "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	auth "github.com/cosmos/cosmos-sdk/x/auth/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	gov "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/ararat-network/ark/app"
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

func newAdmissionFixture(t *testing.T, n int) admissionFixture {
	t.Helper()
	coins := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, chain.NativeBaseAmount(1_000_000)), sdk.NewCoin(chain.XDRBaseDenom, math.NewIntWithDecimal(1, 23)))
	f := admissionFixture{voter: apptest.NewFunder(t, coins), normal: apptest.NewFunder(t, coins), committee: apptest.NewFunder(t, coins)}
	validators := apptest.NewValidators(t, 1)
	for i := 0; i < n; i++ {
		a := app.NewArkApp(log.NewNopLogger(), dbm.NewMemDB(), true, sim.AppOptionsMap{flags.FlagHome: t.TempDir(), server.FlagMempoolMaxTxs: 20}, baseapp.SetChainID(admissionChain))
		t.Cleanup(func() { require.NoError(t, a.Close()) })
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
