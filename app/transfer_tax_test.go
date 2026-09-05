package app_test

import (
	"context"
	"encoding/json"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"

	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// TestTransferTaxChargesOnlySuccessfulTransactions pins D82 end to end,
// through FinalizeBlock: a signed transaction pays its transfer tax exactly
// when its messages succeed. One whose message fails — a send to a blocked
// address — pays the gas fee and nothing else; one whose messages leave the
// payer short of the tax fails at the charge, pays the gas fee, and moves no
// principal; one that succeeds pays both. A granter is charged on the same
// terms, and the allowance records what left the granter and nothing more:
// the gas fee on a failed transaction, gas fee and tax on a successful one.
// The last case is what charging after the messages makes possible rather
// than merely fairer: a transfer whose tax the payer cannot afford until an
// earlier message in the same transaction has funded it.
func TestTransferTaxChargesOnlySuccessfulTransactions(t *testing.T) {
	const chainID = "ark-transfer-tax-test"

	validators := apptestutil.NewValidators(t, 1)
	funder := apptestutil.NewFunder(t, sdk.NewCoins(
		sdk.NewCoin(sdk.DefaultBondDenom, sdk.DefaultPowerReduction.MulRaw(1_000)),
		sdk.NewInt64Coin(chain.XDRBaseDenom, 1_000_000_000_000_000_000),
		sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000),
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
	var treasuryGenesis treasurytypes.GenesisState
	arkApp.AppCodec().MustUnmarshalJSON(genesisState[treasurytypes.ModuleName], &treasuryGenesis)
	treasuryGenesis.Params.TransferTaxRate = math.LegacyMustNewDecFromStr("0.01")
	treasuryGenesis.Params.ReferenceTaxCap = math.NewInt(1_000_000)
	genesisState[treasurytypes.ModuleName] = arkApp.AppCodec().MustMarshalJSON(&treasuryGenesis)
	stateBytes, err := json.Marshal(genesisState)
	require.NoError(t, err)
	_, err = arkApp.InitChain(&cmtabci.RequestInitChain{
		ChainId:         chainID,
		ConsensusParams: simtestutil.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)

	height := int64(0)
	// deliver runs the transactions in one block and commits it.
	deliver := func(txs ...[]byte) []*cmtabci.ExecTxResult {
		height++
		res, err := arkApp.FinalizeBlock(&cmtabci.RequestFinalizeBlock{
			Height:             height,
			NextValidatorsHash: validators.Set.Hash(),
			Txs:                txs,
		})
		require.NoError(t, err)
		_, err = arkApp.Commit()
		require.NoError(t, err)
		require.Len(t, res.TxResults, len(txs))
		return res.TxResults
	}
	committed := func() sdk.Context {
		return arkApp.NewContextLegacy(true, cmtproto.Header{Height: height, ChainID: chainID})
	}
	usd := func(addr sdk.AccAddress) math.Int {
		return arkApp.BankKeeper.GetBalance(committed(), addr, chain.USDBaseDenom).Amount
	}
	xdr := func(addr sdk.AccAddress) math.Int {
		return arkApp.BankKeeper.GetBalance(committed(), addr, chain.XDRBaseDenom).Amount
	}
	taxCollector := authtypes.NewModuleAddress(treasurytypes.TransferTaxCollectorName)

	// The launch floor is 10^11 axdr per gas unit; the reference leg is
	// charged exactly ceil(price × gas), and the tax rides beside it.
	const gas = 200_000
	gasFee := sdk.NewInt64Coin(chain.XDRBaseDenom, 20_000_000_000_000_000)
	feeFor := func(msgs ...sdk.Msg) sdk.Coins {
		tax, _, err := arkApp.TreasuryKeeper.ComputeTax(committed(), msgs)
		require.NoError(t, err)
		return sdk.NewCoins(gasFee).Add(tax...)
	}
	taxOn := func(amount int64) math.Int {
		tax, _, err := arkApp.TreasuryKeeper.ComputeTax(committed(), []sdk.Msg{banktypes.NewMsgSend(
			funder.Address(), funder.Address(), sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, amount)))})
		require.NoError(t, err)
		return tax.AmountOf(chain.USDBaseDenom)
	}
	txConfig := arkApp.GetTxConfig()
	sign := func(key *secp256k1.PrivKey, number, sequence uint64, granter sdk.AccAddress, msgs ...sdk.Msg) []byte {
		signMode, err := authsigning.APISignModeToInternal(txConfig.SignModeHandler().DefaultMode())
		require.NoError(t, err)
		sig := signing.SignatureV2{
			PubKey:   key.PubKey(),
			Data:     &signing.SingleSignatureData{SignMode: signMode},
			Sequence: sequence,
		}
		builder := txConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(msgs...))
		require.NoError(t, builder.SetSignatures(sig))
		builder.SetFeeAmount(feeFor(msgs...))
		builder.SetGasLimit(gas)
		builder.SetFeeGranter(granter)
		signBytes, err := authsigning.GetSignBytesAdapter(
			context.Background(), txConfig.SignModeHandler(), signMode,
			authsigning.SignerData{
				Address:       sdk.AccAddress(key.PubKey().Address()).String(),
				ChainID:       chainID,
				AccountNumber: number,
				Sequence:      sequence,
				PubKey:        key.PubKey(),
			},
			builder.GetTx(),
		)
		require.NoError(t, err)
		raw, err := key.Sign(signBytes)
		require.NoError(t, err)
		sig.Data.(*signing.SingleSignatureData).Signature = raw
		require.NoError(t, builder.SetSignatures(sig))
		bz, err := txConfig.TxEncoder()(builder.GetTx())
		require.NoError(t, err)
		return bz
	}
	accountNumber := func(addr sdk.AccAddress) uint64 {
		account := arkApp.AccountKeeper.GetAccount(committed(), addr)
		require.NotNil(t, account)
		return account.GetAccountNumber()
	}

	// Past the genesis-height waiver.
	deliver()
	funderNumber := accountNumber(funder.Address())
	sequence := uint64(0)
	blocked := authtypes.NewModuleAddress(distrtypes.ModuleName)
	recipient := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())

	t.Run("a failed message pays gas and no tax", func(t *testing.T) {
		usdBefore, xdrBefore := usd(funder.Address()), xdr(funder.Address())
		res := deliver(sign(funder.Key, funderNumber, sequence, nil,
			banktypes.NewMsgSend(funder.Address(), blocked, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100_000)))))
		sequence++
		require.NotZero(t, res[0].Code)
		require.Contains(t, res[0].Log, "not allowed to receive funds")
		require.Equal(t, usdBefore, usd(funder.Address()))
		require.Equal(t, xdrBefore.Sub(gasFee.Amount), xdr(funder.Address()))
		require.True(t, usd(taxCollector).IsZero())
	})

	t.Run("a payer the messages leave short of the tax pays gas and moves nothing", func(t *testing.T) {
		usdBefore, xdrBefore := usd(funder.Address()), xdr(funder.Address())
		res := deliver(sign(funder.Key, funderNumber, sequence, nil,
			banktypes.NewMsgSend(funder.Address(), recipient, sdk.NewCoins(sdk.NewCoin(chain.USDBaseDenom, usdBefore)))))
		sequence++
		require.NotZero(t, res[0].Code)
		require.Contains(t, res[0].Log, "collecting transfer tax")
		require.Equal(t, usdBefore, usd(funder.Address()))
		require.True(t, usd(recipient).IsZero())
		require.Equal(t, xdrBefore.Sub(gasFee.Amount), xdr(funder.Address()))
		require.True(t, usd(taxCollector).IsZero())
	})

	t.Run("a successful message pays gas and tax", func(t *testing.T) {
		usdBefore, xdrBefore := usd(funder.Address()), xdr(funder.Address())
		tax := taxOn(100_000)
		require.True(t, tax.IsPositive())
		res := deliver(sign(funder.Key, funderNumber, sequence, nil,
			banktypes.NewMsgSend(funder.Address(), recipient, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100_000)))))
		sequence++
		require.Zero(t, res[0].Code, res[0].Log)
		require.Equal(t, usdBefore.SubRaw(100_000).Sub(tax), usd(funder.Address()))
		require.Equal(t, math.NewInt(100_000), usd(recipient))
		require.Equal(t, xdrBefore.Sub(gasFee.Amount), xdr(funder.Address()))
		require.Equal(t, tax, usd(taxCollector))
	})

	// A grantee the funder sponsors: funded with the principal it will send
	// and granted an allowance wide enough for every charge below.
	grantee := secp256k1.GenPrivKey()
	granteeAddr := sdk.AccAddress(grantee.PubKey().Address())
	allowance := sdk.NewCoins(gasFee.AddAmount(gasFee.Amount), sdk.NewInt64Coin(chain.USDBaseDenom, 10_000))
	grant, err := feegrant.NewMsgGrantAllowance(&feegrant.BasicAllowance{SpendLimit: allowance}, funder.Address(), granteeAddr)
	require.NoError(t, err)
	res := deliver(sign(funder.Key, funderNumber, sequence, nil,
		banktypes.NewMsgSend(funder.Address(), granteeAddr, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100_000))),
		grant,
	))
	sequence++
	require.Zero(t, res[0].Code, res[0].Log)
	granteeNumber := accountNumber(granteeAddr)
	granteeSequence := uint64(0)
	spendLimit := func() sdk.Coins {
		allowance, err := arkApp.FeeGrantKeeper.GetAllowance(committed(), funder.Address(), granteeAddr)
		require.NoError(t, err)
		return allowance.(*feegrant.BasicAllowance).SpendLimit
	}
	require.Equal(t, allowance, spendLimit())

	t.Run("a granter of a failed message pays gas alone and the allowance says so", func(t *testing.T) {
		usdBefore, xdrBefore := usd(funder.Address()), xdr(funder.Address())
		res := deliver(sign(grantee, granteeNumber, granteeSequence, funder.Address(),
			banktypes.NewMsgSend(granteeAddr, blocked, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 50_000)))))
		granteeSequence++
		require.NotZero(t, res[0].Code)
		require.Equal(t, usdBefore, usd(funder.Address()))
		require.Equal(t, xdrBefore.Sub(gasFee.Amount), xdr(funder.Address()))
		require.Equal(t, math.NewInt(100_000), usd(granteeAddr))
		require.Equal(t, allowance.Sub(gasFee), spendLimit())
	})

	t.Run("a granter of a successful message pays gas and tax and the allowance says so", func(t *testing.T) {
		usdBefore, xdrBefore := usd(funder.Address()), xdr(funder.Address())
		taxBefore := usd(taxCollector)
		tax := taxOn(50_000)
		res := deliver(sign(grantee, granteeNumber, granteeSequence, funder.Address(),
			banktypes.NewMsgSend(granteeAddr, recipient, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 50_000)))))
		granteeSequence++
		require.Zero(t, res[0].Code, res[0].Log)
		require.Equal(t, usdBefore.Sub(tax), usd(funder.Address()))
		require.Equal(t, xdrBefore.Sub(gasFee.Amount), xdr(funder.Address()))
		require.Equal(t, math.NewInt(50_000), usd(granteeAddr))
		require.Equal(t, taxBefore.Add(tax), usd(taxCollector))
		require.Equal(t, allowance.Sub(gasFee).Sub(gasFee).Sub(sdk.NewCoin(chain.USDBaseDenom, tax)), spendLimit())
	})

	t.Run("a transfer the messages fund is taxed rather than refused", func(t *testing.T) {
		// The payer holds none of the taxed denomination when the ante runs
		// and acquires it from the very message that owes the tax, pulling
		// the funder's coins under an authz grant as the transaction's fee
		// payer. The charge reads the balance the messages left, so this is
		// taxed and succeeds; an affordability check in the ante would read
		// the balance before them and refuse it.
		payer := secp256k1.GenPrivKey()
		payerAddr := sdk.AccAddress(payer.PubKey().Address())
		authorisation, err := authz.NewMsgGrant(funder.Address(), payerAddr,
			banktypes.NewSendAuthorization(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000)), nil), nil)
		require.NoError(t, err)
		res := deliver(sign(funder.Key, funderNumber, sequence, nil,
			banktypes.NewMsgSend(funder.Address(), payerAddr, sdk.NewCoins(gasFee.AddAmount(gasFee.Amount))),
			authorisation,
		))
		sequence++
		require.Zero(t, res[0].Code, res[0].Log)
		require.True(t, usd(payerAddr).IsZero(), "the payer must reach the ante holding no taxed denomination")

		taxBefore := usd(taxCollector)
		tax := taxOn(100_000)
		exec := authz.NewMsgExec(payerAddr, []sdk.Msg{banktypes.NewMsgSend(
			funder.Address(), payerAddr, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100_000)))})
		res = deliver(sign(payer, accountNumber(payerAddr), 0, nil, &exec))
		require.Zero(t, res[0].Code, res[0].Log)
		require.Equal(t, math.NewInt(100_000).Sub(tax), usd(payerAddr))
		require.Equal(t, taxBefore.Add(tax), usd(taxCollector))
	})
}
