package app_test

import (
	"context"
	"encoding/json"
	"testing"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	sdkante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/app/ante"
	appclient "github.com/ararat-network/ark/app/client"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// TestGasEstimateMatchesExecution pins the policy on gas estimates. Ark's
// decorators charge simulation what they charge block execution: the fee
// decorator makes its gate's reads without enforcing them and, when the
// estimate carries no fee, consumes SimulatedFeeTransferGas for the transfer
// it cannot make. Two imported decorators do not: wasmd's counter skips
// simulation outright, and the SDK's size decorator overcharges simulation
// for a stand-in signature larger than the real one. Ark accepts both rather
// than owning wasmd's code, and clients absorb the net through
// appclient.DefaultGasAdjustment.
//
// The test simulates the way clients do — unsigned, fee-less, paying in
// NOAH, a priced denomination — measures the difference the three make from
// those very transactions, and requires the difference between Simulate and
// FinalizeBlock to be exactly it, so anything else that starts diverging
// under simulation, or any change in what these three do, fails here. The
// stand-in is pinned two-sided against the transfer it replaces: never below
// it, and never far above.
//
// The multiplier is then pinned two-sided against the same transactions: no
// case may need more than it, and the worst of them must stay close enough
// to it that the margin above is not silently inflating every declared gas
// figure. Finally each estimate scaled by the multiplier is declared as the
// gas of a delivered transaction, which must succeed.
func TestGasEstimateMatchesExecution(t *testing.T) {
	const chainID = "ark-gas-estimate-test"

	validators := apptestutil.NewValidators(t, 1)
	// 999 rather than a round thousand: a balance one fee away from a digit
	// boundary would make the message's balance read a byte shorter in
	// execution than in a fee-less simulation.
	funder := apptestutil.NewFunder(t, sdk.NewCoins(
		sdk.NewCoin(sdk.DefaultBondDenom, sdk.DefaultPowerReduction.MulRaw(999)),
		sdk.NewInt64Coin(chain.USDBaseDenom, 10_000_000),
	))

	arkApp := app.NewArkApp(
		log.NewTestLogger(t),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
		baseapp.SetChainID(chainID),
	)
	genesisState, err := simtestutil.GenesisStateWithValSet(
		arkApp.AppCodec(), arkApp.DefaultGenesis(), validators.Set, funder.Accounts(), funder.Balance,
	)
	require.NoError(t, err)
	// The default params tax nothing. A rate, and a cap above the default
	// single base unit, so the taxed case collects a real figure.
	var treasuryGenesis treasurytypes.GenesisState
	arkApp.AppCodec().MustUnmarshalJSON(genesisState[treasurytypes.ModuleName], &treasuryGenesis)
	treasuryGenesis.Params.TransferTaxRate = math.LegacyMustNewDecFromStr("0.01")
	treasuryGenesis.Params.ReferenceTaxCap = math.NewInt(1_000_000)
	genesisState[treasurytypes.ModuleName] = arkApp.AppCodec().MustMarshalJSON(&treasuryGenesis)
	stateBytes, err := json.Marshal(genesisState)
	require.NoError(t, err)
	_, err = arkApp.InitChain(&abci.RequestInitChain{
		ChainId:         chainID,
		ConsensusParams: simtestutil.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)

	// deliver runs one transaction in its own block and commits it.
	height := int64(0)
	deliver := func(txBytes []byte) *abci.ExecTxResult {
		height++
		res, err := arkApp.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height:             height,
			NextValidatorsHash: validators.Set.Hash(),
			Txs:                [][]byte{txBytes},
		})
		require.NoError(t, err)
		_, err = arkApp.Commit()
		require.NoError(t, err)
		require.Len(t, res.TxResults, 1)
		return res.TxResults[0]
	}
	// deliverEmpty commits a block carrying no transactions.
	deliverEmpty := func() {
		height++
		_, err := arkApp.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height, NextValidatorsHash: validators.Set.Hash()})
		require.NoError(t, err)
		_, err = arkApp.Commit()
		require.NoError(t, err)
	}

	txConfig := arkApp.GetTxConfig()
	// sign builds the transaction, varying only what the case under test
	// varies. Hand-rolled rather than GenSignedMockTx because that attaches a
	// random memo, and a memo is execution gas the estimate also sees: it
	// dilutes the ratio pinned below, understating the worst case by about
	// 0.03. A real transaction carries none.
	sign := func(msg sdk.Msg, sequence uint64, fee sdk.Coins, gas uint64) []byte {
		signMode, err := authsigning.APISignModeToInternal(txConfig.SignModeHandler().DefaultMode())
		require.NoError(t, err)
		sig := signing.SignatureV2{
			PubKey:   funder.Key.PubKey(),
			Data:     &signing.SingleSignatureData{SignMode: signMode},
			Sequence: sequence,
		}
		builder := txConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(msg))
		require.NoError(t, builder.SetSignatures(sig))
		builder.SetFeeAmount(fee)
		builder.SetGasLimit(gas)

		signBytes, err := authsigning.GetSignBytesAdapter(
			context.Background(), txConfig.SignModeHandler(), signMode,
			authsigning.SignerData{
				Address:  funder.Address().String(),
				ChainID:  chainID,
				Sequence: sequence,
				PubKey:   funder.Key.PubKey(),
			},
			builder.GetTx(),
		)
		require.NoError(t, err)
		raw, err := funder.Key.Sign(signBytes)
		require.NoError(t, err)
		sig.Data.(*signing.SingleSignatureData).Signature = raw
		require.NoError(t, builder.SetSignatures(sig))

		bz, err := txConfig.TxEncoder()(builder.GetTx())
		require.NoError(t, err)
		return bz
	}
	// unsigned strips the signature bytes and keeps the public key, the
	// shape the SDK's BuildSimTx sends to Simulate.
	unsigned := func(signedBytes []byte) []byte {
		tx, err := txConfig.TxDecoder()(signedBytes)
		require.NoError(t, err)
		sigs, err := tx.(authsigning.Tx).GetSignaturesV2()
		require.NoError(t, err)
		for i := range sigs {
			sigs[i].Data = &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT}
		}
		builder, err := txConfig.WrapTxBuilder(tx)
		require.NoError(t, err)
		require.NoError(t, builder.SetSignatures(sigs...))
		bz, err := txConfig.TxEncoder()(builder.GetTx())
		require.NoError(t, err)
		return bz
	}
	decode := func(bz []byte) sdk.Tx {
		tx, err := txConfig.TxDecoder()(bz)
		require.NoError(t, err)
		return tx
	}
	const declaredGas = 200_000
	// NOAH at twice its priced floor per gas unit, so a block's worth of
	// utilisation cannot move the base fee past what is paid; the excess is
	// tip. The tax the message owes rides beside it, as the checker requires
	// and the deduction leaves out.
	feeFor := func(gas uint64, msg sdk.Msg) sdk.Coins {
		tax, err := arkApp.TreasuryKeeper.ComputeTax(
			arkApp.NewContextLegacy(true, cmtproto.Header{Height: height, ChainID: chainID}), []sdk.Msg{msg})
		require.NoError(t, err)
		return sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, math.NewIntFromUint64(gas).MulRaw(300_000_000_000))).Add(tax...)
	}

	// The decorators whose simulation differs from execution, measured from
	// the very transactions under test on the state simulation sees: what
	// each charges in block execution for the signed transaction, less what
	// it charges in simulation for the unsigned one. The size decorator is
	// part of it: simulation prices a stand-in signature while the unsigned
	// bytes lack the real one.
	feeDecorator := ante.NewFeeDecorator(arkApp.AccountKeeper, arkApp.BankKeeper, arkApp.FeeGrantKeeper, arkApp.TreasuryKeeper)
	decorators := []sdk.AnteDecorator{
		wasmkeeper.NewCountTXDecorator(runtime.NewKVStoreService(arkApp.GetKey(wasmtypes.StoreKey))),
		feeDecorator,
		sdkante.NewConsumeGasForTxSizeDecorator(arkApp.AccountKeeper),
	}
	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }

	recipient := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	sequence := uint64(0)
	worstRatio := 0.0
	for _, tc := range []struct {
		name string
		msg  sdk.Msg
	}{
		// One store write and no tax — the cheapest transaction the chain
		// has, so the imports' fixed shortfall is the largest fraction of
		// its estimate and it sets the multiplier.
		{"set withdraw address", distrtypes.NewMsgSetWithdrawAddress(funder.Address(), recipient)},
		// NOAH is the numeraire and owes no tax: the imports alone.
		{"untaxed noah send", banktypes.NewMsgSend(funder.Address(), recipient,
			sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1)))},
		// A taxed send adds the tax pricing and charge, which simulation
		// meters as execution does, so the gap stays the gate's. Its fee
		// carries the tax.
		{"taxed usd send", banktypes.NewMsgSend(funder.Address(), recipient,
			sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000)))},
	} {
		// An empty block first. The first one carries simulation and
		// execution past the genesis-height waivers; every one after it
		// lets the previous block's fees be swept and its tally cleared, so
		// the committed state simulation prices against is the state the
		// block will execute on.
		deliverEmpty()
		signedBytes := sign(tc.msg, sequence, feeFor(declaredGas, tc.msg), declaredGas)
		payingBytes := unsigned(signedBytes)
		feelessBytes := unsigned(sign(tc.msg, sequence, nil, declaredGas))

		checkCtx := arkApp.NewContextLegacy(true, cmtproto.Header{Height: height, ChainID: chainID})
		gasOf := func(decorator sdk.AnteDecorator, txBytes []byte, simulate bool) int64 {
			cached, _ := checkCtx.CacheContext()
			metered := cached.WithTxBytes(txBytes).WithGasMeter(storetypes.NewGasMeter(10_000_000))
			_, err := decorator.AnteHandle(metered, decode(txBytes), simulate, next)
			require.NoError(t, err)
			return int64(metered.GasMeter().GasConsumed())
		}
		// gap is what the three charge block execution for the signed
		// transaction beyond what they charge simulation for simBytes —
		// negative when simulation overcharges.
		gap := func(simBytes []byte) int64 {
			var total int64
			for _, d := range decorators {
				total += gasOf(d, signedBytes, false) - gasOf(d, simBytes, true)
			}
			return total
		}
		payingGap, feelessGap := gap(payingBytes), gap(feelessBytes)

		// The stand-in against the transfer it replaces. A fee-less estimate
		// meters the fee decorator as execution does except for the transfer,
		// which it charges as the stand-in, so taking the stand-in back out
		// leaves the transfer. The headroom above is the digit room to the
		// quantity bound on both balances.
		transfer := gasOf(feeDecorator, signedBytes, false) -
			(gasOf(feeDecorator, feelessBytes, true) - ante.SimulatedFeeTransferGas)
		require.GreaterOrEqual(t, int64(ante.SimulatedFeeTransferGas), transfer,
			"%s: the stand-in falls short of the transfer it replaces", tc.name)
		require.Less(t, int64(ante.SimulatedFeeTransferGas)-transfer, int64(3_000),
			"%s: the stand-in has drifted loose above the transfer it replaces", tc.name)

		paying, _, err := arkApp.Simulate(payingBytes)
		require.NoError(t, err)
		feeless, _, err := arkApp.Simulate(feelessBytes)
		require.NoError(t, err)

		executed := deliver(signedBytes)
		sequence++
		require.Zero(t, executed.Code, executed.Log)
		used := uint64(executed.GasUsed)
		t.Logf("%s: executed %d; simulated with fee %d (gap %d), fee-less %d (gap %d, ratio %.4f)",
			tc.name, used, paying.GasUsed, payingGap, feeless.GasUsed, feelessGap,
			float64(used)/float64(feeless.GasUsed))

		require.Equal(t, int64(used)-int64(paying.GasUsed), payingGap,
			"%s: a simulation with a fee differs by exactly what the three make", tc.name)
		require.Equal(t, int64(used)-int64(feeless.GasUsed), feelessGap,
			"%s: a fee-less simulation differs by exactly what the three make", tc.name)
		ratio := float64(used) / float64(feeless.GasUsed)
		worstRatio = max(worstRatio, ratio)
		require.LessOrEqual(t, ratio, appclient.DefaultGasAdjustment,
			"%s: execution outruns a fee-less estimate scaled by the multiplier", tc.name)

		// The gas a client declares: the fee-less estimate, scaled.
		declared := uint64(appclient.DefaultGasAdjustment * float64(feeless.GasUsed))
		auto := deliver(sign(tc.msg, sequence, feeFor(declared, tc.msg), declared))
		sequence++
		require.Zero(t, auto.Code, auto.Log, tc.name)
	}

	// The other half of the guard: the multiplier carries margin above the
	// worst case for messages cheaper than any measured here, but margin that
	// grows unnoticed is charged on every transaction, since the fee is
	// deducted in full on declared gas.
	require.Less(t, appclient.DefaultGasAdjustment-worstRatio, 0.2,
		"the multiplier has drifted loose: worst measured ratio %.4f against %v",
		worstRatio, appclient.DefaultGasAdjustment)
}
