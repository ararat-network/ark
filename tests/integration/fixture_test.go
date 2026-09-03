package integration

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"

	cometabci "github.com/cometbft/cometbft/abci/types"
	cmtsecp256k1 "github.com/cometbft/cometbft/crypto/secp256k1"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdksecp256k1 "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"

	"github.com/ararat-network/ark/abci/codec"
	vetypes "github.com/ararat-network/ark/abci/voteextension/types"
	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/pkg/chain"
	arkencoding "github.com/ararat-network/ark/pkg/encoding"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// valuationComplete reports whether FundStatus valued every recognised
// liability this block, which is what makes its targets meaningful rather than
// zeroes that claim nothing. The response carries no flag for this: the two
// exclusion lists are the answer. A write-off is deliberately not among them,
// because it extinguishes the obligation rather than leaving it unvalued.
func valuationComplete(status *treasurytypes.QueryFundStatusResponse) bool {
	return len(status.UntrustedSuspendedSupply) == 0 && len(status.StaleMemberSupply) == 0
}

// goldDenom is a commodity unit priced ahead of being listed: the feed
// registry is keyed by denomination, and a feed may exist with no asset behind
// it. Everything registered later under this denomination prices through it
// without naming anything.
const goldDenom = "agold"

// activationFixture boots the real application with a single validator and
// drives full blocks through it, injecting the vote extension the validator
// would have produced for the previous height.
type activationFixture struct {
	t            *testing.T
	app          *app.ArkApp
	chainID      string
	validatorKey cmtsecp256k1.PrivKey
	validatorSet *cmttypes.ValidatorSet
	// trader holds a funded NOAH balance, which is what lets a test acquire
	// asset balances the only way the chain allows: by converting.
	trader    sdk.AccAddress
	height    int64
	blockTime time.Time
	// blockEvents holds the events the last finalised block emitted outside its
	// transactions. Conversion settlement runs in Market's EndBlocker, so its
	// allocation and disclosure land here rather than in the context a test's
	// closure holds.
	blockEvents []cometabci.Event
}

func newActivationFixture(t *testing.T) *activationFixture {
	t.Helper()

	const chainID = "ark-asset-activation-test"

	validatorKey := cmtsecp256k1.GenPrivKey()
	validatorSet := cmttypes.NewValidatorSet([]*cmttypes.Validator{
		cmttypes.NewValidator(validatorKey.PubKey(), 1),
	})
	accountKey := sdksecp256k1.GenPrivKey()
	account := authtypes.NewBaseAccount(accountKey.PubKey().Address().Bytes(), accountKey.PubKey(), 0, 0)
	balance := banktypes.Balance{
		Address: account.GetAddress().String(),
		Coins: sdk.NewCoins(sdk.NewCoin(
			sdk.DefaultBondDenom,
			chain.NativeBaseAmount(1_000_000),
		)),
	}

	arkApp := app.NewArkApp(
		log.NewTestLogger(t),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
		baseapp.SetChainID(chainID),
	)
	t.Cleanup(func() {
		require.NoError(t, arkApp.Close())
	})

	genesisState, err := simtestutil.GenesisStateWithValSet(
		arkApp.AppCodec(),
		arkApp.DefaultGenesis(),
		validatorSet,
		[]authtypes.GenesisAccount{account},
		balance,
	)
	require.NoError(t, err)

	// Blocks carry the previous commit, so the validator needs signing info
	// before slashing sees it.
	consensusAddress := sdk.ConsAddress(validatorKey.PubKey().Address())
	var slashingGenesis slashingtypes.GenesisState
	arkApp.AppCodec().MustUnmarshalJSON(genesisState[slashingtypes.ModuleName], &slashingGenesis)
	slashingGenesis.SigningInfos = []slashingtypes.SigningInfo{{
		Address: consensusAddress.String(),
		ValidatorSigningInfo: slashingtypes.NewValidatorSigningInfo(
			consensusAddress,
			1,
			0,
			time.Unix(0, 0).UTC(),
			false,
			0,
		),
	}}
	genesisState[slashingtypes.ModuleName] = arkApp.AppCodec().MustMarshalJSON(&slashingGenesis)

	stateBytes, err := json.Marshal(genesisState)
	require.NoError(t, err)

	consensusParams := proto.Clone(simtestutil.DefaultConsensusParams).(*cmtproto.ConsensusParams)
	if consensusParams.Abci == nil {
		consensusParams.Abci = &cmtproto.ABCIParams{}
	}
	consensusParams.Abci.VoteExtensionsEnableHeight = 1

	genesisTime := time.Unix(1_700_000_000, 0).UTC()
	_, err = arkApp.InitChain(&cometabci.RequestInitChain{
		ChainId:         chainID,
		InitialHeight:   1,
		Time:            genesisTime,
		ConsensusParams: consensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)

	fixture := &activationFixture{
		t:            t,
		app:          arkApp,
		chainID:      chainID,
		validatorKey: validatorKey,
		validatorSet: validatorSet,
		trader:       account.GetAddress(),
		blockTime:    genesisTime,
	}
	// Height 1 carries no vote extensions: they are only available once a
	// previous commit exists. Height 2 is the first block whose preblock
	// aggregates anything, so two blocks is what it takes for the chain to hold
	// a rate — and any test that quotes a conversion needs one.
	fixture.nextBlock(nil)
	fixture.nextBlock(nil)

	return fixture
}

// readCtx returns a read-only context over the last committed state. The
// finalise state is discarded on commit, so reads between blocks go through
// the check state.
func (f *activationFixture) readCtx() sdk.Context {
	return f.app.NewContextLegacy(true, cmtproto.Header{
		ChainID: f.chainID,
		Height:  f.height,
		Time:    f.blockTime,
	})
}

// nextBlock runs one full block. From height 2 on, the validator reports a
// rate for every feed in the epoch it voted against, so every block carries
// consensus evidence.
//
// tx, when set, runs against the incoming block's state before that block
// executes. That is where a message accepted in the previous block leaves the
// chain: after the previous preblock, before this one. Placing keeper calls
// anywhere else would let a block's own preblock observe work that, on a real
// chain, had not happened yet.
func (f *activationFixture) nextBlock(tx func(sdk.Context)) {
	f.t.Helper()

	voteHeight := f.height
	f.height++
	f.blockTime = f.blockTime.Add(time.Second)
	header := cmtproto.Header{
		ChainID: f.chainID,
		Height:  f.height,
		Time:    f.blockTime,
	}

	if tx != nil {
		tx(f.app.NewNextBlockContext(header))
	}

	var (
		txs        [][]byte
		lastCommit cometabci.CommitInfo
	)
	if voteHeight > 0 {
		validator := cometabci.Validator{
			Address: f.validatorKey.PubKey().Address(),
			Power:   1,
		}
		lastCommit = cometabci.CommitInfo{
			Votes: []cometabci.VoteInfo{{
				Validator:   validator,
				BlockIdFlag: cmtproto.BlockIDFlagCommit,
			}},
		}
		// The extended commit is injected the way the proposal handler injects
		// it. Extension signatures are consensus's business and are verified in
		// ProcessProposal, which the proposal-handler tests cover; the preblock
		// trusts what consensus decided.
		commit, err := codec.EncodeExtendedCommit(cometabci.ExtendedCommitInfo{
			Votes: []cometabci.ExtendedVoteInfo{{
				Validator:     validator,
				VoteExtension: f.voteExtension(voteHeight),
				BlockIdFlag:   cmtproto.BlockIDFlagCommit,
			}},
		})
		require.NoError(f.t, err)
		txs = [][]byte{commit}
	}

	res, err := f.app.FinalizeBlock(&cometabci.RequestFinalizeBlock{
		Height:             f.height,
		Time:               f.blockTime,
		Txs:                txs,
		DecidedLastCommit:  lastCommit,
		NextValidatorsHash: f.validatorSet.Hash(),
	})
	require.NoError(f.t, err)
	f.blockEvents = res.Events

	_, err = f.app.Commit()
	require.NoError(f.t, err)
}

// advanceToFeed runs blocks until a scheduled feed has been promoted into the
// active set.
func (f *activationFixture) advanceToFeed(denom string) {
	f.t.Helper()

	for range oracletypes.FeedActivationDelayBlocks + 2 {
		feeds, err := f.app.OracleKeeper.GetFeeds(f.readCtx(), f.height)
		require.NoError(f.t, err)
		if slices.Contains(feeds.Denoms, denom) {
			return
		}
		f.nextBlock(nil)
	}

	feeds, err := f.app.OracleKeeper.GetFeeds(f.readCtx(), f.height)
	require.NoError(f.t, err)
	require.Contains(f.t, feeds.Denoms, denom, "feed %s never activated", denom)
}

// advancePastFeed drives blocks until a scheduled removal has activated and the
// denomination has left the feed set.
func (f *activationFixture) advancePastFeed(denom string) {
	f.t.Helper()

	for range oracletypes.FeedActivationDelayBlocks + 2 {
		feeds, err := f.app.OracleKeeper.GetFeeds(f.readCtx(), f.height)
		require.NoError(f.t, err)
		if !slices.Contains(feeds.Denoms, denom) {
			return
		}
		f.nextBlock(nil)
	}

	feeds, err := f.app.OracleKeeper.GetFeeds(f.readCtx(), f.height)
	require.NoError(f.t, err)
	require.NotContains(f.t, feeds.Denoms, denom, "feed %s never left the set", denom)
}

// voteExtension builds the report the validator would have produced at
// voteHeight: one rate per feed in the epoch addressed by that height.
func (f *activationFixture) voteExtension(voteHeight int64) []byte {
	f.t.Helper()

	feeds, err := f.app.OracleKeeper.GetFeeds(f.readCtx(), voteHeight)
	require.NoError(f.t, err)

	rates := make(map[string][]byte, len(feeds.Denoms))
	for _, denom := range feeds.Denoms {
		encoded, err := arkencoding.EncodeCompactLegacyDec(fixtureRate)
		require.NoError(f.t, err)
		rates[denom] = encoded
	}

	encoded, err := codec.EncodeVoteExtension(vetypes.OracleVoteExtension{
		TargetVersion: feeds.Version,
		Rates:         rates,
	})
	require.NoError(f.t, err)

	return encoded
}
