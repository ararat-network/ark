package app_test

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	protoio "github.com/cosmos/gogoproto/io"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sim "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/ararat-network/ark/abci/codec"
	"github.com/ararat-network/ark/pkg/encoding"
	"github.com/ararat-network/ark/pricefeed/api"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// sidecar serves fixed rates for every launch feed, dated when asked, as a running sidecar would.
type sidecar struct {
	api.UnimplementedPriceFeedServer
	rates map[string]math.LegacyDec
}

func (s *sidecar) Prices(context.Context, *api.PricesRequest) (*api.PricesResponse, error) {
	prices := make(map[string][]byte, len(s.rates))
	for denom, rate := range s.rates {
		encoded, err := encoding.EncodeCompactLegacyDec(rate)
		if err != nil {
			return nil, err
		}
		prices[denom] = encoded
	}
	return &api.PricesResponse{Prices: prices, Timestamp: time.Now()}, nil
}

// startSidecar listens on loopback and returns the app options that point a node's price-feed
// client at it.
func startSidecar(t *testing.T, rates map[string]math.LegacyDec) sim.AppOptionsMap {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	api.RegisterPriceFeedServer(server, &sidecar{rates: rates})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return sim.AppOptionsMap{
		"pricefeed.enabled":           true,
		"pricefeed.sidecar_addresses": []string{listener.Addr().String()},
	}
}

// lastCommit is what CometBFT hands the proposer and processors of height: the fixture's one
// validator committed the previous height with extension, signed over its canonical form.
func lastCommit(t *testing.T, f admissionFixture, height int64, extension []byte) (abci.ExtendedCommitInfo, abci.CommitInfo) {
	t.Helper()
	canonical := cmtproto.CanonicalVoteExtension{Extension: extension, Height: height - 1, ChainId: admissionChain}
	var signBytes bytes.Buffer
	require.NoError(t, protoio.NewDelimitedWriter(&signBytes).WriteMsg(&canonical))
	signature, err := f.validators.Keys[0].Sign(signBytes.Bytes())
	require.NoError(t, err)
	validator := abci.Validator{Address: f.validators.ConsAddress(0), Power: 1}
	vote := abci.ExtendedVoteInfo{Validator: validator, VoteExtension: extension, ExtensionSignature: signature, BlockIdFlag: cmtproto.BlockIDFlagCommit}
	return abci.ExtendedCommitInfo{Votes: []abci.ExtendedVoteInfo{vote}},
		abci.CommitInfo{Votes: []abci.VoteInfo{{Validator: validator, BlockIdFlag: cmtproto.BlockIDFlagCommit}}}
}

// TestVoteExtensionRoundTrip runs the oracle's consensus path between two real apps: the proposer
// extends its vote from a sidecar, the peer verifies the extension, the next proposal carries the
// commit ahead of every transaction and the peer accepts it, and both apply the same prices.
func TestVoteExtensionRoundTrip(t *testing.T) {
	rates := make(map[string]math.LegacyDec, len(oracletypes.DefaultFeedDenoms))
	for i, denom := range oracletypes.DefaultFeedDenoms {
		rates[denom] = math.LegacyNewDecWithPrec(int64(100+i), 2)
	}
	// The proposer refuses to select transactions on prices older than the oracle's freshness
	// limit by its own clock, so the chain starts now.
	f := newAdmissionFixtureWith(t, admissionConfig{
		options:        startSidecar(t, rates),
		start:          time.Now().UTC().Truncate(time.Second),
		voteExtensions: true,
	}, 2)
	proposer, peer := f.apps[0], f.apps[1]
	ctx, cancel := context.WithCancel(context.Background())
	feedDone := make(chan error, 1)
	go func() { feedDone <- proposer.RunPriceFeed(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-feedDone)
	})

	// block proposes height on the proposer, processes it on the peer, extends the proposer's
	// vote and verifies it on the peer, then finalises and commits it on both.
	block := func(height int64, extended abci.ExtendedCommitInfo, decided abci.CommitInfo) (proposal [][]byte, extension []byte) {
		t.Helper()
		blockTime := f.start.Add(time.Duration(height) * time.Second)
		hash := bytes.Repeat([]byte{byte(height)}, 32)
		prepared, err := proposer.PrepareProposal(&abci.RequestPrepareProposal{
			Height: height, Time: blockTime, MaxTxBytes: 100_000, LocalLastCommit: extended,
			NextValidatorsHash: f.validators.Set.Hash(),
		})
		require.NoError(t, err)
		processed, err := peer.ProcessProposal(&abci.RequestProcessProposal{
			Height: height, Time: blockTime, Hash: hash, Txs: prepared.Txs, ProposedLastCommit: decided,
			NextValidatorsHash: f.validators.Set.Hash(),
		})
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processed.Status)

		// The client's first poll may still be in flight.
		require.Eventually(t, func() bool {
			extended, err := proposer.ExtendVote(context.Background(), &abci.RequestExtendVote{Height: height, Time: blockTime, Hash: hash, Txs: prepared.Txs})
			require.NoError(t, err)
			extension = extended.VoteExtension
			return len(extension) > 0
		}, 10*time.Second, 50*time.Millisecond)
		verified, err := peer.VerifyVoteExtension(&abci.RequestVerifyVoteExtension{
			Height: height, Hash: hash, ValidatorAddress: f.validators.ConsAddress(0), VoteExtension: extension,
		})
		require.NoError(t, err)
		require.Equal(t, abci.ResponseVerifyVoteExtension_ACCEPT, verified.Status)
		// The peer checks what it accepts: bytes that are no report are refused.
		refused, err := peer.VerifyVoteExtension(&abci.RequestVerifyVoteExtension{
			Height: height, Hash: hash, ValidatorAddress: f.validators.ConsAddress(0), VoteExtension: []byte("not a report"),
		})
		require.NoError(t, err)
		require.Equal(t, abci.ResponseVerifyVoteExtension_REJECT, refused.Status)

		var appHashes [][]byte
		for _, a := range f.apps {
			res, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{
				Height: height, Time: blockTime, Hash: hash, Txs: prepared.Txs, DecidedLastCommit: decided,
				NextValidatorsHash: f.validators.Set.Hash(),
			})
			require.NoError(t, err)
			// The leading commit is protocol data, which fails to decode as a transaction.
			require.Equal(t, sdkerrors.ErrTxDecode.ABCICode(), res.TxResults[0].Code)
			for _, result := range res.TxResults[1:] {
				require.Zero(t, result.Code, result.Log)
			}
			appHashes = append(appHashes, res.AppHash)
			_, err = a.Commit()
			require.NoError(t, err)
		}
		require.Equal(t, appHashes[0], appHashes[1], "both apps reach the same state")
		return prepared.Txs, extension
	}

	// Height 1 committed with an empty report; height 2 extends the first priced one.
	extended, decided := lastCommit(t, f, 2, nil)
	_, extension := block(2, extended, decided)
	decoded, err := codec.DecodeVoteExtension(extension)
	require.NoError(t, err)
	require.Len(t, decoded.Rates, len(rates))

	send := signedPending(t, proposer, f.normal, 0, sendMsg(f.normal))
	admitted, err := proposer.CheckTx(&abci.RequestCheckTx{Tx: send})
	require.NoError(t, err)
	require.Zero(t, admitted.Code, admitted.Log)
	extended, decided = lastCommit(t, f, 3, extension)
	proposal, _ := block(3, extended, decided)
	encoded, err := codec.EncodeExtendedCommit(extended)
	require.NoError(t, err)
	require.Equal(t, [][]byte{encoded, send}, proposal, "the commit leads the proposal")

	for _, a := range f.apps {
		ctx := a.NewContext(true)
		for denom, rate := range rates {
			stored, err := a.OracleKeeper.GetExchangeRate(ctx, denom)
			require.NoError(t, err, denom)
			require.Equal(t, rate, stored, denom)
		}
	}
}
