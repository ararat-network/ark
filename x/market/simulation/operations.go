package simulation

// DONTCOVER

import (
	"math/rand"
	"strings"

	marketv1 "noah/api/noah/market/v1"
	core "noah/types"
	"noah/x/market/types"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
)

// Simulation operation weights constants
const (
	OpWeightMsgSwap     = "op_weight_msg_swap"      //#nosec
	OpWeightMsgSwapSend = "op_weight_msg_swap_send" //#nosec

	DefaultWeightMsgSwap     = 100
	DefaultWeightMsgSwapSend = 100
)

// WeightedOperations returns all the operations from the module with their respective weights
func WeightedOperations(
	appParams simtypes.AppParams,
	cdc codec.JSONCodec,
	txGen client.TxConfig,
	ak types.AccountKeeper,
	bk types.BankKeeper,
	ok types.OracleKeeper,
) simulation.WeightedOperations {
	var weightMsgSwap int
	appParams.GetOrGenerate(
		OpWeightMsgSwap,
		&weightMsgSwap,
		nil,
		func(_ *rand.Rand) {
			weightMsgSwap = DefaultWeightMsgSwap
		},
	)

	var weightMsgSwapSend int
	appParams.GetOrGenerate(
		OpWeightMsgSwapSend,
		&weightMsgSwapSend,
		nil,
		func(_ *rand.Rand) {
			weightMsgSwapSend = DefaultWeightMsgSwapSend
		},
	)

	return simulation.WeightedOperations{
		simulation.NewWeightedOperation(
			weightMsgSwap,
			SimulateMsgSwap(txGen, ak, bk, ok),
		),
		simulation.NewWeightedOperation(
			weightMsgSwapSend,
			SimulateMsgSwapSend(txGen, ak, bk, ok),
		),
	}
}

// SimulateMsgSwap generates a MsgSwap with random values.
func SimulateMsgSwap(
	txGen client.TxConfig,
	ak types.AccountKeeper,
	bk types.BankKeeper,
	ok types.OracleKeeper,
) simtypes.Operation {
	return func(
		r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&marketv1.MsgSwap{})

		simAccount, _ := simtypes.RandomAcc(r, accs)
		account := ak.GetAccount(ctx, simAccount.Address)

		spendable := bk.SpendableCoins(ctx, simAccount.Address)

		offerDenom, askDenom, found := randomDenomPair(r, ctx, ok)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no available exchange rates"), nil, nil
		}

		fees, err := simtypes.RandomFees(r, ctx, spendable)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "unable to generate fees"), nil, err
		}

		amount := simtypes.RandomAmount(r, spendable.AmountOf(offerDenom).Sub(fees.AmountOf(offerDenom)))
		if amount.Equal(math.ZeroInt()) {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "not enough offer denom amount"), nil, nil
		}

		msg := &marketv1.MsgSwap{
			Trader: simAccount.Address.String(),
			OfferCoin: &basev1beta1.Coin{
				Denom:  offerDenom,
				Amount: amount.String(),
			},
			AskDenom: askDenom,
		}

		err = sendMsg(r, app, txGen, bk, msg, ctx, chainID, []cryptotypes.PrivKey{simAccount.PrivKey}, account)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "unable to deliver tx"), nil, err
		}

		return simtypes.NewOperationMsg(msg, true, ""), nil, nil
	}
}

// SimulateMsgSwapSend generates a MsgSwapSend with random values.
func SimulateMsgSwapSend(
	txGen client.TxConfig,
	ak types.AccountKeeper,
	bk types.BankKeeper,
	ok types.OracleKeeper,
) simtypes.Operation {
	return func(
		r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&marketv1.MsgSwapSend{})

		simAccount, _ := simtypes.RandomAcc(r, accs)
		receiverAccount, _ := simtypes.RandomAcc(r, accs)
		account := ak.GetAccount(ctx, simAccount.Address)

		spendable := bk.SpendableCoins(ctx, simAccount.Address)

		offerDenom, askDenom, found := randomDenomPair(r, ctx, ok)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no available exchange rates"), nil, nil
		}

		// Check send_enabled status of offer denom
		if !bk.IsSendEnabledCoin(ctx, sdk.Coin{Denom: offerDenom, Amount: math.NewInt(1)}) {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "offer denom send not enabled"), nil, nil
		}

		fees, err := simtypes.RandomFees(r, ctx, spendable)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "unable to generate fees"), nil, err
		}

		amount := simtypes.RandomAmount(r, spendable.AmountOf(offerDenom).Sub(fees.AmountOf(offerDenom)))
		if amount.Equal(math.ZeroInt()) {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "not enough offer denom amount"), nil, nil
		}

		msg := &marketv1.MsgSwapSend{
			FromAddress: simAccount.Address.String(),
			ToAddress:   receiverAccount.Address.String(),
			OfferCoin: &basev1beta1.Coin{
				Denom:  offerDenom,
				Amount: amount.String(),
			},
			AskDenom: askDenom,
		}

		err = sendMsg(r, app, txGen, bk, msg, ctx, chainID, []cryptotypes.PrivKey{simAccount.PrivKey}, account)
		if err != nil {
			if strings.Contains(err.Error(), "insufficient fee") {
				return simtypes.NoOpMsg(types.ModuleName, msgType, "ignore tax error"), nil, nil
			}
			return simtypes.NoOpMsg(types.ModuleName, msgType, "unable to deliver tx"), nil, err
		}

		return simtypes.NewOperationMsg(msg, true, ""), nil, nil
	}
}

// randomDenomPair picks a random offer/ask denom pair from available exchange rates.
func randomDenomPair(r *rand.Rand, ctx sdk.Context, ok types.OracleKeeper) (offerDenom, askDenom string, found bool) {
	var whitelist []string
	ok.IterateArkExchangeRates(ctx, func(denom string, _ math.LegacyDec) bool {
		whitelist = append(whitelist, denom)
		return false
	})

	whitelistLen := len(whitelist)
	if whitelistLen == 0 {
		return "", "", false
	}

	if randVal := simtypes.RandIntBetween(r, 0, whitelistLen*2); randVal < whitelistLen {
		offerDenom = core.MicroArkDenom
		askDenom = whitelist[randVal]
	} else {
		offerDenom = whitelist[randVal-whitelistLen]
		askDenom = core.MicroArkDenom
	}

	return offerDenom, askDenom, true
}

// sendMsg builds, signs, and delivers a simulation transaction.
func sendMsg(
	r *rand.Rand,
	app *baseapp.BaseApp,
	txGen client.TxConfig,
	bk types.BankKeeper,
	msg sdk.Msg,
	ctx sdk.Context,
	chainID string,
	privkeys []cryptotypes.PrivKey,
	account sdk.AccountI,
) error {
	spendable := bk.SpendableCoins(ctx, account.GetAddress())
	fees, err := simtypes.RandomFees(r, ctx, spendable)
	if err != nil {
		return err
	}

	tx, err := simtestutil.GenSignedMockTx(
		r,
		txGen,
		[]sdk.Msg{msg},
		fees,
		simtestutil.DefaultGenTxGas,
		chainID,
		[]uint64{account.GetAccountNumber()},
		[]uint64{account.GetSequence()},
		privkeys...,
	)
	if err != nil {
		return err
	}

	_, _, err = app.SimTxFinalizeBlock(txGen.TxEncoder(), tx)
	return err
}
