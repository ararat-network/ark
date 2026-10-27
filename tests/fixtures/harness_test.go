package fixtures

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	dbm "github.com/cosmos/cosmos-db"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtsecp256k1 "github.com/cometbft/cometbft/crypto/secp256k1"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdksecp256k1 "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/abci/codec"
	vetypes "github.com/ararat-network/ark/abci/voteextension/types"
	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/encoding"
	"github.com/ararat-network/ark/pkg/mandate"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
	disbursementtypes "github.com/ararat-network/ark/x/disbursement/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	securitytypes "github.com/ararat-network/ark/x/security/types"
	treasurykeeper "github.com/ararat-network/ark/x/treasury/keeper"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// The chain every fixture is produced on. Nothing here is random: keys derive
// from fixed secrets, the clock starts at a fixed instant, and every block adds
// one second unless a step jumps it, so two runs on one chain version produce
// identical files.
const (
	chainID      = "ark-fixtures-1"
	genesisUnix  = 1_700_000_000
	windowExpiry = 4_102_444_800 // 2100-01-01, a mandate window no fixture closes
	// transferTaxRate and referenceTaxCap are the launch values, app/genesis/genesis.json.
	transferTaxRate = "0.005"
	referenceTaxCap = int64(100)
)

// usdRate is the oracle rate the fixture validator reports for ausd, NOAH per
// USD at the launch bootstrap; every other feed reports one.
var (
	usdRate = math.LegacyMustNewDecFromStr("1.371")
	half    = math.LegacyMustNewDecFromStr("0.5")
)

// account is a signing key with a fixed secret, so the SDK's tests can sign
// with the same key and must produce the same signature.
type account struct {
	Name    string
	Key     *sdksecp256k1.PrivKey
	Address sdk.AccAddress
}

func newAccount(name string) account {
	key := sdksecp256k1.GenPrivKeyFromSecret([]byte("ark decoding fixtures: " + name))
	return account{Name: name, Key: key, Address: key.PubKey().Address().Bytes()}
}

// accountNames lists the signers in genesis order; account numbers follow it.
var accountNames = []string{"committee", "registrar", "member", "trader", "member2", "member3", "other"}

type harness struct {
	t          *testing.T
	app        *app.ArkApp
	validators apptestutil.Validators
	accounts   map[string]account
	height     int64
	blockTime  time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	keys := []cmtsecp256k1.PrivKey{cmtsecp256k1.GenPrivKeySecp256k1([]byte("ark decoding fixtures: validator"))}
	validators := apptestutil.Validators{
		Set:  cmttypes.NewValidatorSet([]*cmttypes.Validator{cmttypes.NewValidator(keys[0].PubKey(), 1)}),
		Keys: keys,
	}

	accounts := make(map[string]account, len(accountNames)+1)
	genAccounts := make([]authtypes.GenesisAccount, 0, len(accountNames)+1)
	balances := make([]banktypes.Balance, 0, len(accountNames)+4)
	// The validator's operator signs with the same secp256k1 scalar as its
	// consensus key, so it is an account like the others.
	operatorKey := &sdksecp256k1.PrivKey{Key: keys[0]}
	accounts["validator"] = account{Name: "validator", Key: operatorKey, Address: operatorKey.PubKey().Address().Bytes()}
	for _, name := range append(slices.Clone(accountNames), "validator") {
		a := accounts[name]
		if name != "validator" {
			a = newAccount(name)
			accounts[name] = a
		}
		genAccounts = append(genAccounts, authtypes.NewBaseAccount(a.Address, a.Key.PubKey(), 0, 0))
		balances = append(balances, banktypes.Balance{
			Address: a.Address.String(),
			Coins:   sdk.NewCoins(chain.NoahCoin(chain.NativeBaseAmount(1_000_000))),
		})
	}
	// The member pool holds the tranche governance has opened, three member grants.
	params := disbursementtypes.DefaultParams()
	tranche := params.MemberAmount.MulRaw(3)
	balances = append(balances, banktypes.Balance{
		Address: authtypes.NewModuleAddress(disbursementtypes.MembersName).String(),
		Coins:   sdk.NewCoins(chain.NoahCoin(tranche)),
	})
	// The custodies hold what the committee fixtures move: Reserve NOAH to
	// deploy and fund with and ausd paper to burn, Insurance NOAH enough to buy
	// a claim but short of its target so the Reserve can fund it, and the
	// contributor pool an award draws.
	balances = append(balances,
		banktypes.Balance{
			Address: authtypes.NewModuleAddress(reservetypes.StrategicReserveName).String(),
			Coins:   sdk.NewCoins(chain.NoahCoin(chain.NativeBaseAmount(10_000)), sdk.NewCoin(chain.USDBaseDenom, chain.NativeBaseAmount(100))),
		},
		banktypes.Balance{
			Address: authtypes.NewModuleAddress(claimstypes.InsuranceName).String(),
			Coins:   sdk.NewCoins(chain.NoahCoin(chain.NativeBaseAmount(20))),
		},
		banktypes.Balance{
			Address: authtypes.NewModuleAddress(disbursementtypes.ContributorsName).String(),
			Coins:   sdk.NewCoins(chain.NoahCoin(chain.NativeBaseAmount(100_000))),
		},
	)

	arkApp := app.NewArkApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
		baseapp.SetChainID(chainID),
	)
	t.Cleanup(func() { require.NoError(t, arkApp.Close()) })

	genesisState, err := simtestutil.GenesisStateWithValSet(
		arkApp.AppCodec(), arkApp.DefaultGenesis(), validators.Set, genAccounts, balances...,
	)
	require.NoError(t, err)
	apptestutil.SeedSigningInfos(t, arkApp.AppCodec(), genesisState, validators.ConsAddresses(), 1)

	// The grants committee and its registrar are appointed in genesis, which
	// import takes verbatim, so registration is an ordinary transaction from
	// the first block.
	var dg disbursementtypes.GenesisState
	arkApp.AppCodec().MustUnmarshalJSON(genesisState[disbursementtypes.ModuleName], &dg)
	dg.GrantsMandate = disbursementtypes.GrantsMandate{
		Envelope:              mandate.Envelope{Term: 1, Committee: accounts["committee"].Address.String(), ActivationTime: 1, ExpiryTime: windowExpiry},
		CompensationAllowance: chain.NativeBaseAmount(1_000_000),
	}
	dg.RegistrarMandate = disbursementtypes.RegistrarMandate{
		Envelope: mandate.Envelope{Term: 1, Committee: accounts["registrar"].Address.String(), ActivationTime: 1, ExpiryTime: windowExpiry},
	}
	dg.MemberTranche = tranche
	genesisState[disbursementtypes.ModuleName] = arkApp.AppCodec().MustMarshalJSON(&dg)

	// The code default taxes nothing; the fixtures tax at the launch rate so
	// a stablecoin fee carries the tax leg the fee contract describes.
	var tg treasurytypes.GenesisState
	arkApp.AppCodec().MustUnmarshalJSON(genesisState[treasurytypes.ModuleName], &tg)
	tg.Params.TransferTaxRate = math.LegacyMustNewDecFromStr(transferTaxRate)
	tg.Params.ReferenceTaxCap = chain.NativeBaseAmount(referenceTaxCap)
	// Every other committee is the same key under its module's mandate, with
	// bounds wide enough for one policy move each.
	envelope := mandate.Envelope{Term: 1, Committee: accounts["committee"].Address.String(), ActivationTime: 1, ExpiryTime: windowExpiry}
	tg.EconomicMandate = treasurytypes.EconomicMandate{Envelope: envelope, MinimumPolicy: treasurytypes.DefaultEconomicPolicy(), MaximumPolicy: economicPolicyMax()}
	genesisState[treasurytypes.ModuleName] = arkApp.AppCodec().MustMarshalJSON(&tg)

	var mg markettypes.GenesisState
	arkApp.AppCodec().MustUnmarshalJSON(genesisState[markettypes.ModuleName], &mg)
	mg.ConversionMandate = markettypes.ConversionMandate{
		Envelope: envelope, MinimumPolicy: markettypes.DefaultConversionPolicy(), MaximumPolicy: wideConversionPolicy(),
		MaxTobinTax: math.LegacyMustNewDecFromStr("0.01"),
	}
	genesisState[markettypes.ModuleName] = arkApp.AppCodec().MustMarshalJSON(&mg)

	var cg claimstypes.GenesisState
	arkApp.AppCodec().MustUnmarshalJSON(genesisState[claimstypes.ModuleName], &cg)
	cg.ClaimsMandate = claimstypes.ClaimsMandate{Envelope: envelope, CommitteeClaimLimit: chain.NativeBaseAmount(1_000)}
	genesisState[claimstypes.ModuleName] = arkApp.AppCodec().MustMarshalJSON(&cg)

	var rg reservetypes.GenesisState
	arkApp.AppCodec().MustUnmarshalJSON(genesisState[reservetypes.ModuleName], &rg)
	rg.Mandate = reservetypes.ReserveMandate{
		Envelope:            envelope,
		DeploymentAllowance: chain.NoahCoin(chain.NativeBaseAmount(1_000)),
		MinimumNoahBalance:  chain.NoahCoin(chain.NativeBaseAmount(100)),
		Destinations:        []string{accounts["other"].Address.String()},
	}
	genesisState[reservetypes.ModuleName] = arkApp.AppCodec().MustMarshalJSON(&rg)

	var sg securitytypes.GenesisState
	arkApp.AppCodec().MustUnmarshalJSON(genesisState[securitytypes.ModuleName], &sg)
	sg.SecurityMandate = securitytypes.SecurityMandate{Envelope: envelope}
	genesisState[securitytypes.ModuleName] = arkApp.AppCodec().MustMarshalJSON(&sg)

	var ag assettypes.GenesisState
	arkApp.AppCodec().MustUnmarshalJSON(genesisState[assettypes.ModuleName], &ag)
	ag.EmergencyMandate = assettypes.EmergencyMandate{Envelope: envelope}
	genesisState[assettypes.ModuleName] = arkApp.AppCodec().MustMarshalJSON(&ag)

	stateBytes, err := json.Marshal(genesisState)
	require.NoError(t, err)

	consensusParams := gogoproto.Clone(simtestutil.DefaultConsensusParams).(*cmtproto.ConsensusParams)
	if consensusParams.Abci == nil {
		consensusParams.Abci = &cmtproto.ABCIParams{}
	}
	consensusParams.Abci.VoteExtensionsEnableHeight = 1

	genesisTime := time.Unix(genesisUnix, 0).UTC()
	_, err = arkApp.InitChain(&cmtabci.RequestInitChain{
		ChainId:         chainID,
		InitialHeight:   1,
		Time:            genesisTime,
		ConsensusParams: consensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)

	h := &harness{t: t, app: arkApp, validators: validators, accounts: accounts, blockTime: genesisTime}
	// Height 1 carries no vote extensions and height 2 is the first whose
	// preblock aggregates a rate, so two blocks give the chain its prices.
	h.block(nil)
	h.block(nil)
	return h
}

func (h *harness) account(name string) account {
	a, ok := h.accounts[name]
	require.True(h.t, ok, "no fixture account %q", name)
	return a
}

// readCtx reads the last committed state.
func (h *harness) readCtx() sdk.Context {
	return h.app.NewContextLegacy(true, cmtproto.Header{ChainID: chainID, Height: h.height, Time: h.blockTime})
}

func (h *harness) validatorOperator() sdk.ValAddress {
	return sdk.ValAddress(h.validators.Validator(0).Address)
}

// jump moves the clock before the next block; a block adds one second on top.
func (h *harness) jump(d time.Duration) { h.blockTime = h.blockTime.Add(d) }

// block finalises and commits one block carrying txs after the injected
// extended commit, and returns the block's response: results index the
// user transactions after the commit at index zero.
func (h *harness) block(txs [][]byte) *cmtabci.ResponseFinalizeBlock {
	h.t.Helper()

	voteHeight := h.height
	h.height++
	h.blockTime = h.blockTime.Add(time.Second)

	var (
		blockTxs   [][]byte
		lastCommit cmtabci.CommitInfo
	)
	if voteHeight > 0 {
		validator := cmtabci.Validator{Address: h.validators.Keys[0].PubKey().Address(), Power: 1}
		lastCommit = cmtabci.CommitInfo{Votes: []cmtabci.VoteInfo{{Validator: validator, BlockIdFlag: cmtproto.BlockIDFlagCommit}}}
		commit, err := codec.EncodeExtendedCommit(cmtabci.ExtendedCommitInfo{
			Votes: []cmtabci.ExtendedVoteInfo{{Validator: validator, VoteExtension: h.voteExtension(voteHeight), BlockIdFlag: cmtproto.BlockIDFlagCommit}},
		})
		require.NoError(h.t, err)
		blockTxs = append(blockTxs, commit)
	}
	blockTxs = append(blockTxs, txs...)

	res, err := h.app.FinalizeBlock(&cmtabci.RequestFinalizeBlock{
		Height:             h.height,
		Time:               h.blockTime,
		Txs:                blockTxs,
		DecidedLastCommit:  lastCommit,
		NextValidatorsHash: h.validators.Set.Hash(),
	})
	require.NoError(h.t, err)
	_, err = h.app.Commit()
	require.NoError(h.t, err)
	if voteHeight > 0 {
		res.TxResults = res.TxResults[1:]
	}
	return res
}

// voteExtension is the validator's report for voteHeight: usdRate for ausd and
// one for every other feed in that height's epoch.
func (h *harness) voteExtension(voteHeight int64) []byte {
	h.t.Helper()

	feeds, err := h.app.OracleKeeper.GetFeeds(h.readCtx(), voteHeight)
	require.NoError(h.t, err)
	rates := make(map[string][]byte, len(feeds.Denoms))
	for _, denom := range feeds.Denoms {
		rate := math.LegacyOneDec()
		if denom == chain.USDBaseDenom {
			rate = usdRate
		}
		encoded, err := encoding.EncodeCompactLegacyDec(rate)
		require.NoError(h.t, err)
		rates[denom] = encoded
	}
	encoded, err := codec.EncodeVoteExtension(vetypes.OracleVoteExtension{TargetVersion: feeds.Version, Rates: rates})
	require.NoError(h.t, err)
	return encoded
}

// signed is one transaction as the harness built it: the sign documents in
// both modes, the signature, and the encoded bytes.
type signed struct {
	signer        account
	accountNumber uint64
	sequence      uint64
	signDoc       []byte
	bodyBytes     []byte
	authInfoBytes []byte
	aminoBytes    []byte
	signature     []byte
	txBytes       []byte
	tx            sdk.Tx
}

// sign builds a transaction over msgs from signer with the declared fee and
// gas, signed in direct mode, and also records the amino sign bytes a Ledger
// signs over the same transaction.
func (h *harness) sign(signer account, msgs []sdk.Msg, fee sdk.Coins, gas uint64, memo string) signed {
	h.t.Helper()

	txConfig := h.app.TxConfig()
	acc := h.app.AccountKeeper.GetAccount(h.readCtx(), signer.Address)
	require.NotNil(h.t, acc, "%s has no account", signer.Name)
	accountNumber, sequence := acc.GetAccountNumber(), acc.GetSequence()
	signerData := authsigning.SignerData{
		Address: signer.Address.String(), ChainID: chainID,
		AccountNumber: accountNumber, Sequence: sequence, PubKey: signer.Key.PubKey(),
	}

	builder := txConfig.NewTxBuilder()
	require.NoError(h.t, builder.SetMsgs(msgs...))
	builder.SetFeeAmount(fee)
	builder.SetGasLimit(gas)
	builder.SetMemo(memo)

	bytesFor := func(mode signing.SignMode) []byte {
		sig := signing.SignatureV2{PubKey: signer.Key.PubKey(), Data: &signing.SingleSignatureData{SignMode: mode}, Sequence: sequence}
		require.NoError(h.t, builder.SetSignatures(sig))
		bz, err := authsigning.GetSignBytesAdapter(context.Background(), txConfig.SignModeHandler(), mode, signerData, builder.GetTx())
		require.NoError(h.t, err)
		return bz
	}
	aminoBytes := bytesFor(signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON)
	signDoc := bytesFor(signing.SignMode_SIGN_MODE_DIRECT)

	signature, err := signer.Key.Sign(signDoc)
	require.NoError(h.t, err)
	require.NoError(h.t, builder.SetSignatures(signing.SignatureV2{
		PubKey: signer.Key.PubKey(), Sequence: sequence,
		Data: &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT, Signature: signature},
	}))
	txBytes, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(h.t, err)
	tx, err := txConfig.TxDecoder()(txBytes)
	require.NoError(h.t, err)

	var raw sdktx.TxRaw
	require.NoError(h.t, gogoproto.Unmarshal(txBytes, &raw))

	return signed{
		signer: signer, accountNumber: accountNumber, sequence: sequence,
		signDoc: signDoc, bodyBytes: raw.BodyBytes, authInfoBytes: raw.AuthInfoBytes,
		aminoBytes: aminoBytes, signature: signature, txBytes: txBytes, tx: tx,
	}
}

// feeQuote is the fee contract applied to one transaction at the last
// committed height: the tax the messages owe, the chosen denomination's gas
// price, and the declaration padded the way arkd pads it.
type feeQuote struct {
	Tax          sdk.Coins
	GasPrice     treasurytypes.GasPrice
	GasPriceJSON json.RawMessage
	Required     sdk.Coin
	Declared     sdk.Coins
	GasLimit     uint64
	FeeDenom     string
	PadStable    string
	PadNoah      string
}

const (
	stablePad = "1.1"
	noahPad   = "1.05"
)

// quoteFee prices msgs with the fee paid in feeDenom. With a stable gas leg
// every leg is padded, for free; with a NOAH gas leg NOAH is padded as a tip
// and the tax legs ride beside it exact, since a stable leg's slack pays the
// base fee before NOAH does and would leave the whole NOAH leg as tip.
func (h *harness) quoteFee(msgs []sdk.Msg, feeDenom string, gas uint64) feeQuote {
	h.t.Helper()

	ctx := h.readCtx()
	tax, _, err := h.app.TreasuryKeeper.ComputeTax(ctx, msgs)
	require.NoError(h.t, err)
	prices, err := treasurykeeper.NewQueryServerImpl(h.app.TreasuryKeeper).GasPrices(ctx, &treasurytypes.QueryGasPricesRequest{})
	require.NoError(h.t, err)
	var price treasurytypes.GasPrice
	found := false
	for _, p := range prices.GasPrices {
		if p.Denom == feeDenom {
			price, found = p, true
		}
	}
	require.True(h.t, found, "no gas price for %s", feeDenom)

	required := sdk.NewCoin(feeDenom, price.GasPrice.MulInt64(int64(gas)).Ceil().TruncateInt())
	noahPays := feeDenom == chain.NoahBaseDenom
	declared := sdk.NewCoins()
	for _, leg := range tax.Add(required) {
		pad := math.LegacyOneDec()
		switch {
		case leg.Denom == chain.NoahBaseDenom:
			pad = math.LegacyMustNewDecFromStr(noahPad)
		case !noahPays:
			pad = math.LegacyMustNewDecFromStr(stablePad)
		}
		declared = declared.Add(sdk.NewCoin(leg.Denom, math.LegacyNewDecFromInt(leg.Amount).Mul(pad).Ceil().TruncateInt()))
	}
	return feeQuote{Tax: tax, GasPrice: price, GasPriceJSON: h.app.AppCodec().MustMarshalJSON(&price), Required: required, Declared: declared, GasLimit: gas, FeeDenom: feeDenom, PadStable: stablePad, PadNoah: noahPad}
}

func txHash(txBytes []byte) string {
	sum := sha256.Sum256(txBytes)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// typedJSON decodes a typed event back to its proto message and renders it as
// proto JSON; nil for an event that is not a typed proto event. A block event
// carries a mode attribute whose value is a bare word, not JSON, which the
// SDK's parser cannot take, so it is dropped first; a decoder must do the same.
func (h *harness) typedJSON(event cmtabci.Event) json.RawMessage {
	attributes := make([]cmtabci.EventAttribute, 0, len(event.Attributes))
	for _, a := range event.Attributes {
		if a.Key != "mode" {
			attributes = append(attributes, a)
		}
	}
	msg, err := sdk.ParseTypedEvent(cmtabci.Event{Type: event.Type, Attributes: attributes})
	if err != nil {
		return nil
	}
	bz, err := h.app.AppCodec().MarshalJSON(msg)
	if err != nil {
		return nil
	}
	return bz
}

func describeResult(res *cmtabci.ExecTxResult) string {
	if res.Code == 0 {
		return "ok"
	}
	return fmt.Sprintf("code %d (%s)", res.Code, res.Codespace)
}
