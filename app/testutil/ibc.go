package testutil

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	ibctesting "github.com/cosmos/ibc-go/v11/testing"
	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/log/v2"

	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/app/ante"
	"github.com/ararat-network/ark/pkg/chain"
)

// NewIBCCoordinator returns an ibc-go coordinator whose every chain is an Ark
// application, for driving packets between two Ark chains through the real
// client, connection, and channel handshakes.
//
// Each chain gets its own home, because the Wasm VM locks its cache
// directory. The coordinator signs through the ante chain with a fee the
// consensus base fee would refuse, so the gate is off for the test.
func NewIBCCoordinator(t *testing.T, chains int) *ibctesting.Coordinator {
	t.Helper()

	gate := ante.BaseFeeGate()
	ante.SetBaseFeeGate(false)
	t.Cleanup(func() { ante.SetBaseFeeGate(gate) })
	ibctesting.DefaultTestingAppInit = func() (ibctesting.TestingApp, map[string]json.RawMessage) {
		arkApp := app.NewArkApp(log.NewNopLogger(), dbm.NewMemDB(), true, simtestutil.NewAppOptionsWithFlagHome(t.TempDir()))
		return arkApp, arkApp.DefaultGenesis()
	}
	coord := ibctesting.NewCoordinator(t, chains)
	for i := 1; i <= chains; i++ {
		installArkSender(t, coord.GetChain(ibctesting.GetChainID(i)))
	}
	return coord
}

// ArkChain returns the Ark application behind a coordinator chain.
func ArkChain(t *testing.T, c *ibctesting.TestChain) *app.ArkApp {
	t.Helper()

	arkApp, ok := c.App.(*app.ArkApp)
	require.True(t, ok, "chain %s is not an Ark application", c.ChainID)
	return arkApp
}

// installArkSender replaces the chain's signer. ibc-go signs with a
// zero-amount fee coin, which Ark's ante refuses as malformed, so Ark chains
// sign with a real NOAH fee and reproduce the chain's own commit bookkeeping.
func installArkSender(t *testing.T, c *ibctesting.TestChain) {
	t.Helper()

	c.SendMsgsOverride = func(msgs ...sdk.Msg) (*abci.ExecTxResult, error) {
		c.Coordinator.UpdateTimeForChain(c)
		// An empty block first: the chain's commit is the only thing that
		// resets its private next-block-context flag, and NextBlock reaches
		// it where this override cannot.
		c.NextBlock()

		account := c.SenderAccount
		defer func() { require.NoError(t, account.SetSequence(account.GetSequence()+1)) }()

		tx, err := simtestutil.GenSignedMockTx(
			rand.New(rand.NewSource(1)), c.TxConfig, msgs,
			sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000_000)),
			simtestutil.DefaultGenTxGas, c.ChainID,
			[]uint64{account.GetAccountNumber()}, []uint64{account.GetSequence()}, c.SenderPrivKey,
		)
		if err != nil {
			return nil, err
		}
		txBytes, err := c.TxConfig.TxEncoder()(tx)
		if err != nil {
			return nil, err
		}
		res, err := c.App.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height:             c.App.LastBlockHeight() + 1,
			Time:               c.ProposedHeader.GetTime(),
			NextValidatorsHash: c.NextVals.Hash(),
			Txs:                [][]byte{txBytes},
		})
		if err != nil {
			return nil, err
		}

		// What TestChain.commitBlock does, minus the private flag.
		_, err = c.App.Commit()
		require.NoError(t, err)
		c.LatestCommittedHeader = c.CurrentTMClientHeader()
		c.TrustedValidators[uint64(c.ProposedHeader.Height)] = c.NextVals
		c.Vals = c.NextVals
		c.NextVals = ibctesting.ApplyValSetChanges(c, c.Vals, res.ValidatorUpdates)
		c.Vals.IncrementProposerPriority(1)
		c.ProposedHeader = cmtproto.Header{
			ChainID:            c.ChainID,
			Height:             c.App.LastBlockHeight() + 1,
			AppHash:            c.App.LastCommitID().Hash,
			Time:               c.ProposedHeader.Time,
			ValidatorsHash:     c.Vals.Hash(),
			NextValidatorsHash: c.NextVals.Hash(),
			ProposerAddress:    c.Vals.Proposer.Address,
		}

		require.Len(t, res.TxResults, 1)
		txResult := res.TxResults[0]
		if txResult.Code != 0 {
			return txResult, fmt.Errorf("%s/%d: %q", txResult.Codespace, txResult.Code, txResult.Log)
		}
		c.Coordinator.IncrementTime()
		return txResult, nil
	}
}
