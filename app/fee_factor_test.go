package app_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/pkg/chain"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// TestFeeAtThresholdExecutesAfterARateMove prices a NOAH fee at exactly what the committed state
// requires, then moves the reference rate in the block that carries it, as its PreBlock would. The
// block's fees were fixed when the previous block committed, so the fee the proposal check accepted
// still pays at execution; the move reaches the block after.
func TestFeeAtThresholdExecutesAfterARateMove(t *testing.T) {
	f := newAdmissionFixture(t, 1)
	a := f.apps[0]
	gas := uint64(1_000_000)

	// Block 2 prices NOAH at one reference unit; its end fixes that for block 3.
	at := f.start
	setReferenceRate(t, a, 2, at.Add(time.Second), math.LegacyOneDec())
	finalise(t, a, 2, at.Add(time.Second))

	required := requiredNoahFee(t, a, gas)
	pay := &bank.MsgSend{FromAddress: f.normal.Address().String(), ToAddress: f.voter.Address().String(), Amount: sdk.NewCoins(chain.NoahCoin(math.OneInt()))}
	tx := signedPendingFee(t, a, f.normal, 0, sdk.NewCoins(required), pay)
	res, err := a.CheckTx(&abci.RequestCheckTx{Tx: tx})
	require.NoError(t, err)
	require.Zero(t, res.Code, res.Log)

	// NOAH weakens a fifth in block 3: the requirement read at execution would rise past the fee.
	setReferenceRate(t, a, 3, at.Add(2*time.Second), math.LegacyNewDecWithPrec(12, 1))
	results := finalise(t, a, 3, at.Add(2*time.Second), tx)
	require.Zero(t, results[0].Code, results[0].Log)

	// The move prices block 4.
	require.True(t, requiredNoahFee(t, a, gas).Amount.GT(required.Amount))
}

func setReferenceRate(t *testing.T, a *app.ArkApp, height int64, at time.Time, rate math.LegacyDec) {
	t.Helper()
	ctx := a.NewNextBlockContext(cmtproto.Header{ChainID: admissionChain, Height: height, Time: at})
	require.NoError(t, a.OracleKeeper.ExchangeRate.Set(ctx, chain.XDRBaseDenom, oracletypes.ExchangeRate{
		Denom: chain.XDRBaseDenom, Rate: rate, BlockTime: chain.BlockSeconds(ctx), BlockHeight: uint64(height),
	}))
}

func finalise(t *testing.T, a *app.ArkApp, height int64, at time.Time, txs ...[]byte) []*abci.ExecTxResult {
	t.Helper()
	res, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height, Time: at, Txs: txs})
	require.NoError(t, err)
	_, err = a.Commit()
	require.NoError(t, err)
	return res.TxResults
}

func requiredNoahFee(t *testing.T, a *app.ArkApp, gas uint64) sdk.Coin {
	t.Helper()
	ctx := a.GetContextForCheckTx(nil)
	params, err := a.TreasuryKeeper.Params.Get(ctx)
	require.NoError(t, err)
	price, err := a.TreasuryKeeper.BaseGasPrice.Get(ctx)
	require.NoError(t, err)
	required, _, err := a.TreasuryKeeper.GetRequiredGasFee(ctx, params, price, gas, chain.NoahBaseDenom)
	require.NoError(t, err)
	return required
}
