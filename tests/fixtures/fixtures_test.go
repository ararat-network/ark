// Package fixtures produces the decoding fixtures: real transactions signed
// through the app's own encoder, run through real blocks, and written with
// their sign documents, bytes, results, and events. Every decoder of Ark
// transactions, in Go or TypeScript, is tested against these files, and the
// SDK's producer must reproduce their bytes and signatures from the fixed keys.
//
// go test ./tests/fixtures verifies the files against the chain as built;
// go test ./tests/fixtures -update-fixtures rewrites them, a deliberate step in a chain
// release.
package fixtures

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
	disbursementtypes "github.com/ararat-network/ark/x/disbursement/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	securitytypes "github.com/ararat-network/ark/x/security/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

var update = flag.Bool("update-fixtures", false, "rewrite the fixture files from the chain as built")

const fixtureDir = "testdata/decoding"

// The file shapes. Every integer that may exceed 2^53 is a decimal string, as
// in the chain's own JSON.
type (
	keysFile struct {
		Notes     string       `json:"notes"`
		ChainID   string       `json:"chain_id"`
		Validator validatorRec `json:"validator"`
		Accounts  []accountRec `json:"accounts"`
	}
	validatorRec struct {
		Operator        string `json:"operator"`
		ConsensusPubKey string `json:"consensus_public_key"`
	}
	accountRec struct {
		Name          string `json:"name"`
		PrivateKeyHex string `json:"private_key_hex"`
		PublicKey     string `json:"public_key"`
		Address       string `json:"address"`
	}
	fixtureFile struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		ChainID     string          `json:"chain_id"`
		Height      int64           `json:"height"`
		BlockTime   string          `json:"block_time"`
		Signer      *signerRec      `json:"signer,omitempty"`
		Fee         *feeRec         `json:"fee,omitempty"`
		SignDirect  *signDirectRec  `json:"sign_mode_direct,omitempty"`
		SignAmino   *signAminoRec   `json:"sign_mode_amino,omitempty"`
		Signature   string          `json:"signature,omitempty"`
		TxBytes     string          `json:"tx_bytes,omitempty"`
		TxHash      string          `json:"tx_hash,omitempty"`
		Tx          json.RawMessage `json:"tx,omitempty"`
		Result      *resultRec      `json:"result,omitempty"`
		Events      []eventRec      `json:"events,omitempty"`
		BlockEvents []eventRec      `json:"block_events,omitempty"`
	}
	signerRec struct {
		Key           string `json:"key"`
		Address       string `json:"address"`
		PublicKey     string `json:"public_key"`
		AccountNumber string `json:"account_number"`
		Sequence      string `json:"sequence"`
	}
	feeRec struct {
		FeeDenom  string          `json:"fee_denom"`
		GasLimit  string          `json:"gas_limit"`
		Tax       sdk.Coins       `json:"tax"`
		GasPrice  json.RawMessage `json:"gas_price"`
		Required  sdk.Coin        `json:"required"`
		PadStable string          `json:"pad_stable"`
		PadNoah   string          `json:"pad_noah"`
		Declared  sdk.Coins       `json:"declared"`
	}
	signDirectRec struct {
		SignDoc       string `json:"sign_doc"`
		BodyBytes     string `json:"body_bytes"`
		AuthInfoBytes string `json:"auth_info_bytes"`
	}
	signAminoRec struct {
		SignBytes string `json:"sign_bytes"`
	}
	resultRec struct {
		Code      uint32 `json:"code"`
		Codespace string `json:"codespace"`
		Log       string `json:"log"`
		GasWanted string `json:"gas_wanted"`
		GasUsed   string `json:"gas_used"`
	}
	eventRec struct {
		Type       string          `json:"type"`
		Attributes []attributeRec  `json:"attributes"`
		Typed      json.RawMessage `json:"typed,omitempty"`
	}
	attributeRec struct {
		Key   string `json:"key"`
		Value string `json:"value"`
		Index bool   `json:"index"`
	}
)

// step is one fixture: a transaction from signer over msgs, or, with txFree,
// a block alone after jump.
type step struct {
	name        string
	description string
	signer      string
	msgs        func(h *harness) []sdk.Msg
	feeDenom    string
	gas         uint64
	memo        string
	// override replaces the declared fee the quote produced, for refusals.
	override    func(q feeQuote) sdk.Coins
	jump        time.Duration
	blockEvents bool
	txFree      bool
}

func noah(n int64) sdk.Coin { return chain.NoahCoin(chain.NativeBaseAmount(n)) }

func usd(n int64) sdk.Coin { return sdk.NewCoin(chain.USDBaseDenom, chain.NativeBaseAmount(n)) }

// firstTranche is the set the first member tranche needs, run first on one
// chain; later steps see earlier state.
var firstTranche = []step{
	{
		name:        "staking_delegate",
		description: "A member stakes NOAH with the validator, the transaction the app sends for each payment that arrived.",
		signer:      "member", feeDenom: chain.NoahBaseDenom, gas: 250_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&stakingtypes.MsgDelegate{DelegatorAddress: h.account("member").Address.String(), ValidatorAddress: h.validatorOperator().String(), Amount: noah(10)}}
		},
	},
	{
		name:        "gov_submit_proposal",
		description: "A text proposal with no messages and the minimum deposit, so the vote fixture has a proposal in its voting period.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 250_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&govv1.MsgSubmitProposal{
				InitialDeposit: sdk.NewCoins(noah(1_000)), Proposer: h.account("trader").Address.String(),
				Metadata: "ipfs://bafyfixtureproposal", Title: "Fixture proposal", Summary: "A text proposal with no messages, for the vote fixture.",
			}}
		},
	},
	{
		name:        "gov_vote",
		description: "A member votes yes on proposal 1; the ante requires at least one NOAH staked by the voter.",
		signer:      "member", feeDenom: chain.NoahBaseDenom, gas: 250_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&govv1.MsgVote{ProposalId: 1, Voter: h.account("member").Address.String(), Option: govv1.OptionYes}}
		},
	},
	{
		name:        "market_sell",
		description: "The trader sells NOAH for ausd at the oracle rate with no minimum; settlement lands in the block's EndBlock events.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 400_000, blockEvents: true,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&markettypes.MsgSell{Trader: h.account("trader").Address.String(), OfferCoin: noah(100), AskDenom: chain.USDBaseDenom}}
		},
	},
	{
		name:        "bank_send_taxed",
		description: "The trader sends ausd and pays the fee in ausd: the declared fee carries the transfer tax beside the gas leg.",
		signer:      "trader", feeDenom: chain.USDBaseDenom, gas: 250_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&banktypes.MsgSend{FromAddress: h.account("trader").Address.String(), ToAddress: h.account("member").Address.String(), Amount: sdk.NewCoins(usd(10))}}
		},
	},
	{
		name:        "market_buy",
		description: "The trader buys one NOAH with ausd under a maximum offer, the cap a buy must carry.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 400_000, blockEvents: true,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&markettypes.MsgBuy{Trader: h.account("trader").Address.String(), AskCoin: noah(1), OfferDenom: chain.USDBaseDenom, MaximumOffer: usd(2)}}
		},
	},
	{
		name:        "disbursement_register_members",
		description: "The registrar registers two members within the open tranche under its term; each grant's creation is a typed journal event.",
		signer:      "registrar", feeDenom: chain.NoahBaseDenom, gas: 400_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&disbursementtypes.MsgRegisterMembers{
				Registrar: h.account("registrar").Address.String(), ExpectedTerm: 1,
				Addresses: []string{h.account("member2").Address.String(), h.account("member3").Address.String()},
				Reference: "admission batch fixtures-1",
			}}
		},
	},
	{
		name:        "disbursement_set_payee",
		description: "A registered member moves the payee of their own grant to another address.",
		signer:      "member2", feeDenom: chain.NoahBaseDenom, gas: 250_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&disbursementtypes.MsgSetPayee{Sender: h.account("member2").Address.String(), GrantId: 1, Payee: h.account("other").Address.String()}}
		},
	},
	{
		name:        "multi_message",
		description: "Two messages in one transaction, a send and a delegation, with a memo.",
		signer:      "member", feeDenom: chain.NoahBaseDenom, gas: 400_000, memo: "fixture memo",
		msgs: func(h *harness) []sdk.Msg {
			member := h.account("member").Address.String()
			return []sdk.Msg{
				&banktypes.MsgSend{FromAddress: member, ToAddress: h.account("other").Address.String(), Amount: sdk.NewCoins(noah(1))},
				&stakingtypes.MsgDelegate{DelegatorAddress: member, ValidatorAddress: h.validatorOperator().String(), Amount: noah(1)},
			}
		},
	},
	{
		name:        "refused_fee_short_of_tax",
		description: "An ausd send whose fee carries no ausd leg: the ante refuses it before anything moves, with the fee contract's error.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 250_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&banktypes.MsgSend{FromAddress: h.account("trader").Address.String(), ToAddress: h.account("member").Address.String(), Amount: sdk.NewCoins(usd(5))}}
		},
		override: func(q feeQuote) sdk.Coins {
			return sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, q.Declared.AmountOf(chain.NoahBaseDenom)))
		},
	},
	{
		name:        "failed_not_registrar",
		description: "A registration signed by a member rather than the registrar: the ante passes, execution fails with the mandate's refusal.",
		signer:      "member", feeDenom: chain.NoahBaseDenom, gas: 250_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&disbursementtypes.MsgRegisterMembers{
				Registrar: h.account("member").Address.String(), ExpectedTerm: 1,
				Addresses: []string{h.account("other").Address.String()}, Reference: "admission batch fixtures-2",
			}}
		},
	},
}

// committee holds the key-signed committee and contributor messages, every
// committee the same key under its module's mandate, each at term one.
var committee = []step{
	{
		name:        "market_committee_update_policy",
		description: "The conversion committee moves the live policy inside its mandate's bounds.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			policy := markettypes.DefaultConversionPolicy()
			policy.BasePool = sdk.NewDecCoinFromDec(policy.BasePool.Denom, policy.BasePool.Amount.MulInt64(3).QuoInt64(2))
			return []sdk.Msg{&markettypes.MsgCommitteeUpdatePolicy{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, Policy: policy}}
		},
	},
	{
		name:        "market_committee_set_tobin_tax",
		description: "The conversion committee sets one denomination's Tobin tax under the mandate's cap.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&markettypes.MsgCommitteeSetTobinTax{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, Denom: chain.USDBaseDenom, TobinTax: math.LegacyMustNewDecFromStr("0.005")}}
		},
	},
	{
		name:        "market_settle_refused",
		description: "A settlement redemption with no settlement open: execution refuses it, which pins the message and the refusal.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&markettypes.MsgSettle{Trader: h.account("trader").Address.String(), OfferCoin: usd(1)}}
		},
	},
	{
		name:        "treasury_committee_update_policy",
		description: "The economic committee moves the economic policy inside its mandate's bounds.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			policy := treasurytypes.DefaultEconomicPolicy()
			policy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.4")
			policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.1")
			return []sdk.Msg{&treasurytypes.MsgCommitteeUpdatePolicy{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, Policy: policy}}
		},
	},
	{
		name:        "claims_committee_submit_claim",
		description: "The claims committee submits a stablecoin claim: Insurance buys the ausd it lacks with NOAH under the offer cap, and the claim waits out its cancellation period.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 500_000, blockEvents: true,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&claimstypes.MsgCommitteeSubmitClaim{
				Committee: h.account("committee").Address.String(), ExpectedTerm: 1, Reference: "claim case fixtures-1",
				Recipient: h.account("other").Address.String(), Amount: usd(10), MaxOffer: chain.NativeBaseAmount(100),
			}}
		},
	},
	{
		name:        "claims_committee_cancel_claim",
		description: "The claims committee cancels the claim it submitted, releasing the Insurance reservation.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&claimstypes.MsgCommitteeCancelClaim{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, ClaimId: 1}}
		},
	},
	{
		name:        "reserve_committee_deploy",
		description: "The reserve committee deploys NOAH to a mandated destination and attests the external holding it bought, opening position 1.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 400_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&reservetypes.MsgCommitteeDeploy{
				Committee: h.account("committee").Address.String(), ExpectedTerm: 1, PositionId: 0,
				Destination: h.account("other").Address.String(), Amount: noah(500), Acquired: venue(300),
				VenueReference: "custodian account 1", Reference: "deployment 1",
			}}
		},
	},
	{
		name:        "reserve_committee_record_update",
		description: "The reserve committee restates position 1's attested holding.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&reservetypes.MsgCommitteeRecordUpdate{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, PositionId: 1, Quantity: venue(310), Reference: "attestation 1"}}
		},
	},
	{
		name:        "reserve_committee_attribute_return",
		description: "The reserve committee attributes NOAH already in the Reserve account to position 1 and states the remaining holding.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&reservetypes.MsgCommitteeAttributeReturn{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, PositionId: 1, ReturnedCoin: noah(50), RemainingQuantity: venue(280), Reference: "return 1"}}
		},
	},
	{
		name:        "reserve_committee_mark_impaired",
		description: "The reserve committee marks position 1 impaired, zeroing its credit without erasing the claim.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&reservetypes.MsgCommitteeMarkImpaired{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, PositionId: 1, Reference: "impairment 1"}}
		},
	},
	{
		name:        "reserve_committee_clear_impairment",
		description: "The reserve committee clears the impairment it marked.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&reservetypes.MsgCommitteeClearImpairment{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, PositionId: 1, Reference: "impairment 1 cleared"}}
		},
	},
	{
		name:        "reserve_committee_correct_position",
		description: "The reserve committee corrects the restatement in entry 2 with a new quantity; history keeps both.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&reservetypes.MsgCommitteeCorrectPosition{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, PositionId: 1, Corrects: 2, Quantity: venue(305), VenueReference: "custodian account 1", Reference: "correction of entry 2"}}
		},
	},
	{
		name:        "reserve_committee_reverse_return",
		description: "The reserve committee reverses the return it attributed in entry 3.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&reservetypes.MsgCommitteeReverseReturn{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, PositionId: 1, Reverses: 3, Reference: "reversal of entry 3"}}
		},
	},
	{
		name:        "reserve_committee_close_position",
		description: "The reserve committee closes position 1, crystallising its result.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&reservetypes.MsgCommitteeClosePosition{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, PositionId: 1, Reference: "closure"}}
		},
	},
	{
		name:        "reserve_committee_burn_paper",
		description: "The reserve committee burns registry paper the Reserve holds that carries no credit.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&reservetypes.MsgCommitteeBurnPaper{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, Amounts: sdk.NewCoins(usd(10))}}
		},
	},
	{
		name:        "reserve_committee_burn_surplus",
		description: "The reserve committee burns NOAH surplus within the action bound.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&reservetypes.MsgCommitteeBurnSurplus{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, Amount: noah(1)}}
		},
	},
	{
		name:        "reserve_committee_fund_buffer",
		description: "The reserve committee moves NOAH from the Reserve to the redemption Buffer.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&reservetypes.MsgCommitteeFundBuffer{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, Amount: noah(10)}}
		},
	},
	{
		name:        "reserve_committee_fund_insurance",
		description: "The reserve committee moves NOAH from the Reserve to Insurance.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&reservetypes.MsgCommitteeFundInsurance{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, Amount: noah(10)}}
		},
	},
	{
		name:        "security_committee_plan_upgrade",
		description: "The security committee schedules a software upgrade through x/upgrade.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&securitytypes.MsgCommitteePlanUpgrade{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, Name: "v2", Height: 1_000_000, Info: "{}"}}
		},
	},
	{
		name:        "security_committee_cancel_upgrade",
		description: "The security committee cancels the upgrade it scheduled.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&securitytypes.MsgCommitteeCancelUpgrade{Committee: h.account("committee").Address.String(), ExpectedTerm: 1}}
		},
	},
	{
		name:        "asset_emergency_suspend",
		description: "The emergency committee suspends an asset, closing conversion in both directions.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&assettypes.MsgEmergencySuspendAsset{Committee: h.account("committee").Address.String(), Denom: chain.KRWBaseDenom, ExpectedTerm: 1}}
		},
	},
	{
		name:        "disbursement_committee_suspend",
		description: "The grants committee suspends a member found fake within the hold; the grant stops paying and moves nothing.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&disbursementtypes.MsgCommitteeSuspend{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, Addresses: []string{h.account("member3").Address.String()}}}
		},
	},
	{
		name:        "disbursement_committee_reinstate",
		description: "The grants committee reinstates the member it suspended.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&disbursementtypes.MsgCommitteeReinstate{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, Addresses: []string{h.account("member3").Address.String()}}}
		},
	},
	{
		name:        "disbursement_committee_compensate",
		description: "The grants committee awards NOAH compensation within its term's allowance, clocked from the block.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 400_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&disbursementtypes.MsgCommitteeCompensate{
				Committee: h.account("committee").Address.String(), ExpectedTerm: 1, Beneficiary: h.account("other").Address.String(),
				Amount: noah(100), Reference: "bounty fixtures-1", StartTime: uint64(h.blockTime.Unix()) + 1, Periods: 2, MaxOffer: math.ZeroInt(),
			}}
		},
	},
	{
		name:        "disbursement_set_controller",
		description: "A contributor rotates the controller of their beneficiary record.",
		signer:      "other", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&disbursementtypes.MsgSetController{Sender: h.account("other").Address.String(), Beneficiary: h.account("other").Address.String(), Controller: h.account("member").Address.String()}}
		},
	},
	{
		name:        "disbursement_committee_registrar",
		description: "The grants committee reappoints the registrar for a window inside its own, advancing the registrar term.",
		signer:      "committee", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&disbursementtypes.MsgCommitteeRegistrar{Committee: h.account("committee").Address.String(), ExpectedTerm: 1, Registrar: h.account("registrar").Address.String(), ActivationTime: 1, ExpiryTime: windowExpiry}}
		},
	},
}

// govAuthority is the address every authority message names.
var govAuthority = authtypes.NewModuleAddress(govtypes.ModuleName).String()

// proposal wraps msgs in a governance proposal from the trader at the minimum
// deposit. Submission pins the messages' encoding inside the proposal, which
// is the only shape an authority message ever has on chain; the proposal is
// not voted on, so its execution is not part of the fixture.
func proposal(h *harness, module string, msgs ...sdk.Msg) []sdk.Msg {
	anys := make([]*codectypes.Any, 0, len(msgs))
	for _, m := range msgs {
		a, err := codectypes.NewAnyWithValue(m)
		require.NoError(h.t, err)
		anys = append(anys, a)
	}
	return []sdk.Msg{&govv1.MsgSubmitProposal{
		Messages: anys, InitialDeposit: sdk.NewCoins(noah(1_000)), Proposer: h.account("trader").Address.String(),
		Metadata: "ipfs://bafyfixture" + module, Title: "Every " + module + " authority message",
		Summary: "A fixture proposal carrying each governance-only message of the " + module + " module.",
	}}
}

// economicPolicyMax is the economic mandate's upper bound, wide enough for one
// move of every field.
func economicPolicyMax() treasurytypes.EconomicPolicy {
	return treasurytypes.EconomicPolicy{
		ValidatorBlockRewardTarget: chain.NativeBaseAmount(1_000), OracleBlockRewardTarget: chain.NativeBaseAmount(1_000),
		RedemptionBufferTargetRatio: half, StrategicReserveTargetRatio: half, InsuranceTargetRatio: half,
		LiabilityRatioWeight: math.LegacyOneDec(), VolatilityWeight: math.LegacyOneDec(), FlowWeight: math.LegacyOneDec(),
	}
}

// wideConversionPolicy is the conversion mandate's upper bound, the default
// policy doubled.
func wideConversionPolicy() markettypes.ConversionPolicy {
	wide := markettypes.DefaultConversionPolicy()
	wide.BasePool = sdk.NewDecCoinFromDec(wide.BasePool.Denom, wide.BasePool.Amount.MulInt64(2))
	wide.PoolRecoveryPeriod *= 2
	wide.MinStabilitySpread = wide.MinStabilitySpread.MulInt64(2)
	return wide
}

// proposals holds one governance proposal per Ark module carrying every
// authority message the module has.
var proposals = []step{
	{
		name:        "gov_proposal_asset",
		description: "A proposal carrying every asset authority message: params, registration, each lifecycle step, retirement, and the emergency mandate.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 600_000,
		msgs: func(h *harness) []sdk.Msg {
			committee := h.account("committee").Address.String()
			return proposal(h, "asset",
				&assettypes.MsgUpdateParams{Authority: govAuthority, Params: assettypes.DefaultParams()},
				&assettypes.MsgRegisterAsset{Authority: govAuthority, Denom: "achf"},
				&assettypes.MsgHaltIssuance{Authority: govAuthority, Denom: chain.JPYBaseDenom, ExpectedVersion: 1},
				&assettypes.MsgResumeIssuance{Authority: govAuthority, Denom: chain.JPYBaseDenom, ExpectedVersion: 1},
				&assettypes.MsgSuspendAsset{Authority: govAuthority, Denom: chain.JPYBaseDenom, ExpectedVersion: 1},
				&assettypes.MsgOpenSettlement{Authority: govAuthority, Denom: chain.JPYBaseDenom, ExpectedVersion: 1, RedemptionRate: math.LegacyOneDec(), SettlementWindow: 14_400},
				&assettypes.MsgCancelSettlement{Authority: govAuthority, Denom: chain.JPYBaseDenom, ExpectedVersion: 1},
				&assettypes.MsgRecoverAsset{Authority: govAuthority, Denom: chain.JPYBaseDenom, ExpectedVersion: 1},
				&assettypes.MsgWriteOffAsset{Authority: govAuthority, Denom: chain.JPYBaseDenom, ExpectedVersion: 1},
				&assettypes.MsgFinaliseRetirement{Authority: govAuthority, Denom: chain.JPYBaseDenom, ExpectedVersion: 1, MaxResidualSupply: math.ZeroInt()},
				&assettypes.MsgSetEmergencyMandate{Authority: govAuthority, Committee: committee, ActivationTime: 1, ExpiryTime: windowExpiry},
			)
		},
	},
	{
		name:        "gov_proposal_claims",
		description: "A proposal carrying every claims authority message: params, the claims mandate, a governance claim, and a cancellation.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 500_000,
		msgs: func(h *harness) []sdk.Msg {
			return proposal(h, "claims",
				&claimstypes.MsgUpdateParams{Authority: govAuthority, Params: claimstypes.DefaultParams()},
				&claimstypes.MsgSetClaimsMandate{Authority: govAuthority, Committee: h.account("committee").Address.String(), ActivationTime: 1, ExpiryTime: windowExpiry, CommitteeClaimLimit: chain.NativeBaseAmount(1_000)},
				&claimstypes.MsgSubmitClaim{Authority: govAuthority, Reference: "claim case gov-1", Recipient: h.account("other").Address.String(), Amount: usd(10), MaxOffer: chain.NativeBaseAmount(100)},
				&claimstypes.MsgCancelClaim{Authority: govAuthority, ClaimId: 1},
			)
		},
	},
	{
		name:        "gov_proposal_disbursement",
		description: "A proposal carrying every disbursement authority message: params, the grants mandate, a tranche, a return, an ownership grant, a compensation award, a cancellation, and a reinstatement.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 600_000,
		msgs: func(h *harness) []sdk.Msg {
			other := h.account("other").Address.String()
			now := uint64(h.blockTime.Unix()) + 1
			return proposal(h, "disbursement",
				&disbursementtypes.MsgUpdateParams{Authority: govAuthority, Params: disbursementtypes.DefaultParams()},
				&disbursementtypes.MsgSetGrantsMandate{Authority: govAuthority, Committee: h.account("committee").Address.String(), ActivationTime: 1, ExpiryTime: windowExpiry, CompensationAllowance: chain.NativeBaseAmount(1_000_000)},
				&disbursementtypes.MsgOpenTranche{Authority: govAuthority, Amount: disbursementtypes.DefaultParams().MemberAmount.MulRaw(10)},
				&disbursementtypes.MsgReturnUnallocated{Authority: govAuthority, Amount: noah(1_000)},
				&disbursementtypes.MsgGrantOwnership{Authority: govAuthority, Beneficiary: other, Amount: chain.NativeBaseAmount(100_000), Reference: "ownership grant fixtures-1", StartTime: now},
				&disbursementtypes.MsgCompensate{Authority: govAuthority, Beneficiary: other, Amount: noah(100), Reference: "bounty gov-1", StartTime: now, Periods: 2, MaxOffer: math.ZeroInt()},
				&disbursementtypes.MsgCancelGrants{Authority: govAuthority, GrantIds: []uint64{3}},
				&disbursementtypes.MsgReinstateMembers{Authority: govAuthority, Addresses: []string{h.account("member3").Address.String()}},
			)
		},
	},
	{
		name:        "gov_proposal_market",
		description: "A proposal carrying every market authority message: params, a Tobin tax override and its removal, the conversion mandate, and a policy update.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 500_000,
		msgs: func(h *harness) []sdk.Msg {
			return proposal(h, "market",
				&markettypes.MsgUpdateParams{Authority: govAuthority, Params: markettypes.DefaultParams()},
				&markettypes.MsgSetTobinTaxOverride{Authority: govAuthority, Denom: chain.USDBaseDenom, TobinTax: math.LegacyMustNewDecFromStr("0.005")},
				&markettypes.MsgRemoveTobinTaxOverride{Authority: govAuthority, Denom: chain.USDBaseDenom},
				&markettypes.MsgSetConversionMandate{Authority: govAuthority, Committee: h.account("committee").Address.String(), ActivationTime: 1, ExpiryTime: windowExpiry, MinimumPolicy: markettypes.DefaultConversionPolicy(), MaximumPolicy: wideConversionPolicy(), MaxTobinTax: math.LegacyMustNewDecFromStr("0.01")},
				&markettypes.MsgUpdatePolicy{Authority: govAuthority, Policy: markettypes.DefaultConversionPolicy()},
			)
		},
	},
	{
		name:        "gov_proposal_oracle",
		description: "A proposal carrying every oracle authority message: params, a feed added and one removed, and the reference denomination.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 500_000,
		msgs: func(h *harness) []sdk.Msg {
			return proposal(h, "oracle",
				&oracletypes.MsgUpdateParams{Authority: govAuthority, Params: oracletypes.DefaultParams()},
				&oracletypes.MsgAddFeed{Authority: govAuthority, Denom: "achf"},
				&oracletypes.MsgRemoveFeed{Authority: govAuthority, Denom: chain.MXNBaseDenom},
				&oracletypes.MsgSetReferenceDenom{Authority: govAuthority, ReferenceDenom: chain.XDRBaseDenom, OutgoingRate: math.LegacyOneDec()},
			)
		},
	},
	{
		name:        "gov_proposal_reserve",
		description: "A proposal carrying every reserve authority message: the mandate, the recognition policy, funding, each position correction, and a burn.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 600_000,
		msgs: func(h *harness) []sdk.Msg {
			floor := noah(100)
			return proposal(h, "reserve",
				&reservetypes.MsgSetReserveMandate{Authority: govAuthority, Committee: h.account("committee").Address.String(), ActivationTime: 1, ExpiryTime: windowExpiry, DeploymentAllowance: noah(1_000), MinimumNoahBalance: floor, Destinations: []string{h.account("other").Address.String()}},
				&reservetypes.MsgSetRecognitionPolicy{Authority: govAuthority, Entries: []reservetypes.EligibilityEntry{{Denom: chain.USDBaseDenom + "-venue", HaircutFactor: math.LegacyMustNewDecFromStr("0.9"), MaxCreditPerNoah: half, MaxRateAge: 3_600, MaxAttestationAge: 30 * 86_400}}},
				&reservetypes.MsgFundBuffer{Authority: govAuthority, Amount: noah(10), MinimumReserveBalance: floor},
				&reservetypes.MsgFundInsurance{Authority: govAuthority, Amount: noah(10), MinimumReserveBalance: floor},
				&reservetypes.MsgCorrectPosition{Authority: govAuthority, PositionId: 1, Corrects: 2, Quantity: venue(305), VenueReference: "custodian account 1", Reference: "governance correction of entry 2"},
				&reservetypes.MsgClearImpairment{Authority: govAuthority, PositionId: 1, Reference: "governance clearance"},
				&reservetypes.MsgMarkImpaired{Authority: govAuthority, PositionId: 1, Reference: "governance impairment"},
				&reservetypes.MsgClosePosition{Authority: govAuthority, PositionId: 1, Reference: "governance closure"},
				&reservetypes.MsgReverseReturn{Authority: govAuthority, PositionId: 1, Reverses: 3, Reference: "governance reversal of entry 3"},
				&reservetypes.MsgBurnReserveAssets{Authority: govAuthority, Amounts: sdk.NewCoins(usd(10)), MinimumReserveBalance: floor},
			)
		},
	},
	{
		name:        "gov_proposal_security",
		description: "A proposal carrying the security module's one authority message, the security mandate.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 400_000,
		msgs: func(h *harness) []sdk.Msg {
			return proposal(h, "security",
				&securitytypes.MsgSetSecurityMandate{Authority: govAuthority, Committee: h.account("committee").Address.String(), ActivationTime: 1, ExpiryTime: windowExpiry},
			)
		},
	},
	{
		name:        "gov_proposal_treasury",
		description: "A proposal carrying every treasury authority message: params, the economic mandate, a policy update, and a subsidy return.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 500_000,
		msgs: func(h *harness) []sdk.Msg {
			policy := treasurytypes.DefaultEconomicPolicy()
			policy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.05")
			return proposal(h, "treasury",
				&treasurytypes.MsgUpdateParams{Authority: govAuthority, Params: treasurytypes.DefaultParams()},
				&treasurytypes.MsgSetEconomicMandate{Authority: govAuthority, Committee: h.account("committee").Address.String(), ActivationTime: 1, ExpiryTime: windowExpiry, MinimumPolicy: treasurytypes.DefaultEconomicPolicy(), MaximumPolicy: economicPolicyMax()},
				&treasurytypes.MsgUpdatePolicy{Authority: govAuthority, Policy: policy},
				&treasurytypes.MsgReturnSubsidy{Authority: govAuthority, Amount: noah(1), MinimumSubsidyBalance: chain.NoahCoin(math.ZeroInt())},
			)
		},
	},
}

// counterWasm is the e2e tier's counter contract, gzipped once and checked in
// beside the fixtures: the chain accepts gzip, the CLI sends it, and the
// smaller payload keeps the store fixture's five encodings of it readable.
var counterWasm = sync.OnceValue(func() []byte {
	code, err := os.ReadFile(filepath.Join("testdata", "counter.wasm.gz"))
	if err != nil {
		panic(err)
	}
	return code
})

// stubWasm is a valid-header, empty module: enough for a proposal's basic
// validation, which is all a proposal fixture exercises.
var stubWasm = []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

func stubHash() []byte {
	sum := sha256.Sum256(stubWasm)
	return sum[:]
}

// counterContract is the first instance of code 1, the address the classic
// scheme assigns it.
func counterContract() string { return wasmkeeper.BuildContractAddressClassic(1, 1).String() }

func lastProposalID(h *harness) uint64 {
	next, err := h.app.GovKeeper.ProposalID.Peek(h.readCtx())
	require.NoError(h.t, err)
	return next - 1
}

// fresh is an address no account exists at, for the vesting fixtures.
func fresh(name string) sdk.AccAddress { return newAccount(name).Address }

// cosmosSet holds the Cosmos module messages a key signs.
var cosmosSet = []step{
	{
		name:        "authz_grant",
		description: "A member grants another account a send authorisation with a spend limit and an expiry.",
		signer:      "member", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			expiry := h.blockTime.Add(365 * 24 * time.Hour)
			msg, err := authz.NewMsgGrant(h.account("member").Address, h.account("other").Address, banktypes.NewSendAuthorization(sdk.NewCoins(noah(5)), nil), &expiry)
			require.NoError(h.t, err)
			return []sdk.Msg{msg}
		},
	},
	{
		name:        "authz_exec",
		description: "The grantee executes a send from the granter under the authorisation.",
		signer:      "other", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			exec := authz.NewMsgExec(h.account("other").Address, []sdk.Msg{&banktypes.MsgSend{FromAddress: h.account("member").Address.String(), ToAddress: h.account("other").Address.String(), Amount: sdk.NewCoins(noah(1))}})
			return []sdk.Msg{&exec}
		},
	},
	{
		name:        "authz_revoke",
		description: "The granter revokes the send authorisation.",
		signer:      "member", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			revoke := authz.NewMsgRevoke(h.account("member").Address, h.account("other").Address, sdk.MsgTypeURL(&banktypes.MsgSend{}))
			return []sdk.Msg{&revoke}
		},
	},
	{
		name:        "bank_multi_send",
		description: "One input paying two outputs.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&banktypes.MsgMultiSend{
				Inputs:  []banktypes.Input{{Address: h.account("trader").Address.String(), Coins: sdk.NewCoins(noah(3))}},
				Outputs: []banktypes.Output{{Address: h.account("member").Address.String(), Coins: sdk.NewCoins(noah(1))}, {Address: h.account("other").Address.String(), Coins: sdk.NewCoins(noah(2))}},
			}}
		},
	},
	{
		name:        "feegrant_grant_allowance",
		description: "The trader grants a member a basic fee allowance with a spend limit and an expiry.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			expiry := h.blockTime.Add(365 * 24 * time.Hour)
			msg, err := feegrant.NewMsgGrantAllowance(&feegrant.BasicAllowance{SpendLimit: sdk.NewCoins(noah(10)), Expiration: &expiry}, h.account("trader").Address, h.account("member").Address)
			require.NoError(h.t, err)
			return []sdk.Msg{msg}
		},
	},
	{
		name:        "feegrant_revoke_allowance",
		description: "The granter revokes the fee allowance.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			revoke := feegrant.NewMsgRevokeAllowance(h.account("trader").Address, h.account("member").Address)
			return []sdk.Msg{&revoke}
		},
	},
	{
		name:        "feegrant_prune_allowances",
		description: "Anyone prunes expired allowances.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&feegrant.MsgPruneAllowances{Pruner: h.account("trader").Address.String()}}
		},
	},
	{
		name:        "distribution_set_withdraw_address",
		description: "A delegator directs their rewards to another address.",
		signer:      "member", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&distrtypes.MsgSetWithdrawAddress{DelegatorAddress: h.account("member").Address.String(), WithdrawAddress: h.account("other").Address.String()}}
		},
	},
	{
		name:        "distribution_withdraw_delegator_reward",
		description: "A delegator withdraws the rewards accrued on one validator.",
		signer:      "member", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&distrtypes.MsgWithdrawDelegatorReward{DelegatorAddress: h.account("member").Address.String(), ValidatorAddress: h.validatorOperator().String()}}
		},
	},
	{
		name:        "distribution_withdraw_validator_commission_refused",
		description: "The operator withdraws commission a zero-rate validator never earned: execution refuses it.",
		signer:      "validator", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&distrtypes.MsgWithdrawValidatorCommission{ValidatorAddress: h.validatorOperator().String()}}
		},
	},
	{
		name:        "distribution_fund_community_pool",
		description: "The trader funds the community pool.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&distrtypes.MsgFundCommunityPool{Amount: sdk.NewCoins(noah(10)), Depositor: h.account("trader").Address.String()}}
		},
	},
	{
		name:        "distribution_deposit_validator_rewards_pool",
		description: "The trader deposits into one validator's rewards pool.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&distrtypes.MsgDepositValidatorRewardsPool{Depositor: h.account("trader").Address.String(), ValidatorAddress: h.validatorOperator().String(), Amount: sdk.NewCoins(noah(1))}}
		},
	},
	{
		name:        "gov_deposit",
		description: "A deposit on the text proposal.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&govv1.MsgDeposit{ProposalId: 1, Depositor: h.account("trader").Address.String(), Amount: sdk.NewCoins(noah(100))}}
		},
	},
	{
		name:        "gov_vote_weighted",
		description: "A member splits a vote on the text proposal across options.",
		signer:      "member", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&govv1.MsgVoteWeighted{ProposalId: 1, Voter: h.account("member").Address.String(), Options: []*govv1.WeightedVoteOption{{Option: govv1.OptionYes, Weight: "0.700000000000000000"}, {Option: govv1.OptionAbstain, Weight: "0.300000000000000000"}}}}
		},
	},
	{
		name:        "gov_cancel_proposal",
		description: "The proposer cancels the asset proposal while it is in its voting period.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&govv1.MsgCancelProposal{ProposalId: 2, Proposer: h.account("trader").Address.String()}}
		},
	},
	{
		name:        "gov_legacy_submit_proposal",
		description: "A v1beta1 text proposal, the legacy shape older tooling still produces.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			msg, err := govv1beta1.NewMsgSubmitProposal(govv1beta1.NewTextProposal("Legacy text proposal", "A v1beta1 text proposal for the legacy fixtures."), sdk.NewCoins(noah(1_000)), h.account("trader").Address)
			require.NoError(h.t, err)
			return []sdk.Msg{msg}
		},
	},
	{
		name:        "gov_legacy_deposit",
		description: "A v1beta1 deposit on the legacy proposal.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&govv1beta1.MsgDeposit{ProposalId: lastProposalID(h), Depositor: h.account("trader").Address.String(), Amount: sdk.NewCoins(noah(10))}}
		},
	},
	{
		name:        "gov_legacy_vote",
		description: "A v1beta1 vote on the legacy proposal.",
		signer:      "member", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&govv1beta1.MsgVote{ProposalId: lastProposalID(h), Voter: h.account("member").Address.String(), Option: govv1beta1.OptionYes}}
		},
	},
	{
		name:        "gov_legacy_vote_weighted",
		description: "A v1beta1 weighted vote on the legacy proposal.",
		signer:      "member", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&govv1beta1.MsgVoteWeighted{ProposalId: lastProposalID(h), Voter: h.account("member").Address.String(), Options: []govv1beta1.WeightedVoteOption{{Option: govv1beta1.OptionYes, Weight: math.LegacyOneDec()}}}}
		},
	},
	{
		name:        "staking_create_validator",
		description: "An account creates a second validator with a fixed consensus key and a self-delegation.",
		signer:      "other", feeDenom: chain.NoahBaseDenom, gas: 400_000,
		msgs: func(h *harness) []sdk.Msg {
			msg, err := stakingtypes.NewMsgCreateValidator(
				sdk.ValAddress(h.account("other").Address).String(), ed25519.GenPrivKeyFromSecret([]byte("ark decoding fixtures: second validator")).PubKey(),
				noah(100), stakingtypes.NewDescription("fixture-validator", "", "https://example.invalid", "", "the second validator"),
				stakingtypes.NewCommissionRates(math.LegacyMustNewDecFromStr("0.1"), math.LegacyMustNewDecFromStr("0.2"), math.LegacyMustNewDecFromStr("0.01")), math.OneInt(),
			)
			require.NoError(h.t, err)
			return []sdk.Msg{msg}
		},
	},
	{
		name:        "staking_edit_validator",
		description: "The operator edits the second validator's description.",
		signer:      "other", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{stakingtypes.NewMsgEditValidator(sdk.ValAddress(h.account("other").Address).String(), stakingtypes.NewDescription("fixture-validator", stakingtypes.DoNotModifyDesc, stakingtypes.DoNotModifyDesc, stakingtypes.DoNotModifyDesc, "edited"), nil, nil)}
		},
	},
	{
		name:        "staking_undelegate",
		description: "A member starts unbonding part of a delegation.",
		signer:      "member", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&stakingtypes.MsgUndelegate{DelegatorAddress: h.account("member").Address.String(), ValidatorAddress: h.validatorOperator().String(), Amount: noah(1)}}
		},
	},
	{
		name:        "staking_begin_redelegate",
		description: "A member moves part of a delegation to the second validator.",
		signer:      "member", feeDenom: chain.NoahBaseDenom, gas: 400_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&stakingtypes.MsgBeginRedelegate{DelegatorAddress: h.account("member").Address.String(), ValidatorSrcAddress: h.validatorOperator().String(), ValidatorDstAddress: sdk.ValAddress(h.account("other").Address).String(), Amount: noah(1)}}
		},
	},
	{
		name:        "staking_cancel_unbonding_delegation",
		description: "A member cancels the unbonding started two blocks earlier, naming its creation height.",
		signer:      "member", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&stakingtypes.MsgCancelUnbondingDelegation{DelegatorAddress: h.account("member").Address.String(), ValidatorAddress: h.validatorOperator().String(), Amount: noah(1), CreationHeight: h.height - 1}}
		},
	},
	{
		name:        "slashing_unjail_refused",
		description: "The operator unjails a validator that is neither jailed nor self-delegated: execution refuses it.",
		signer:      "validator", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&slashingtypes.MsgUnjail{ValidatorAddr: h.validatorOperator().String()}}
		},
	},
	{
		name:        "vesting_create_vesting_account",
		description: "A continuous vesting account funded at a fresh address.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{vestingtypes.NewMsgCreateVestingAccount(h.account("trader").Address, fresh("vesting continuous"), sdk.NewCoins(noah(10)), h.blockTime.Add(365*24*time.Hour).Unix(), false)}
		},
	},
	{
		name:        "vesting_create_permanent_locked_account",
		description: "A permanently locked account funded at a fresh address.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{vestingtypes.NewMsgCreatePermanentLockedAccount(h.account("trader").Address, fresh("vesting locked"), sdk.NewCoins(noah(10)))}
		},
	},
	{
		name:        "vesting_create_periodic_vesting_account",
		description: "A periodic vesting account with two periods funded at a fresh address.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			periods := []vestingtypes.Period{{Length: 30 * 86_400, Amount: sdk.NewCoins(noah(5))}, {Length: 30 * 86_400, Amount: sdk.NewCoins(noah(5))}}
			return []sdk.Msg{vestingtypes.NewMsgCreatePeriodicVestingAccount(h.account("trader").Address, fresh("vesting periodic"), h.blockTime.Unix()+1, periods)}
		},
	},
}

// wasmSet holds the contract messages a key signs, on the e2e tier's counter.
var wasmSet = []step{
	{
		name:        "wasm_store_code",
		description: "The trader stores the counter contract's code, gzipped as the CLI sends it, permissionless at launch; the file carries the whole payload.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 20_000_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&wasmtypes.MsgStoreCode{Sender: h.account("trader").Address.String(), WASMByteCode: counterWasm()}}
		},
	},
	{
		name:        "wasm_instantiate_contract",
		description: "The trader instantiates code 1 with an admin; the instance is the counter contract the later fixtures act on.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 1_000_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&wasmtypes.MsgInstantiateContract{Sender: h.account("trader").Address.String(), Admin: h.account("trader").Address.String(), CodeID: 1, Label: "counter", Msg: []byte(`{"count":0}`)}}
		},
	},
	{
		name:        "wasm_instantiate_contract2",
		description: "A second instance at a salted, predictable address.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 1_000_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&wasmtypes.MsgInstantiateContract2{Sender: h.account("trader").Address.String(), Admin: h.account("trader").Address.String(), CodeID: 1, Label: "counter-2", Msg: []byte(`{"count":0}`), Salt: []byte("fixture salt")}}
		},
	},
	{
		name:        "wasm_execute_contract",
		description: "The trader increments the counter.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 1_000_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&wasmtypes.MsgExecuteContract{Sender: h.account("trader").Address.String(), Contract: counterContract(), Msg: []byte(`{"increment":{}}`)}}
		},
	},
	{
		name:        "wasm_migrate_contract_refused",
		description: "The admin migrates the contract to the same code; the counter exports no migrate entry point, so the VM refuses it.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 1_000_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&wasmtypes.MsgMigrateContract{Sender: h.account("trader").Address.String(), Contract: counterContract(), CodeID: 1, Msg: []byte(`{}`)}}
		},
	},
	{
		name:        "wasm_update_admin",
		description: "The admin hands the contract to another admin.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&wasmtypes.MsgUpdateAdmin{Sender: h.account("trader").Address.String(), NewAdmin: h.account("other").Address.String(), Contract: counterContract()}}
		},
	},
	{
		name:        "wasm_update_contract_label",
		description: "The new admin relabels the contract.",
		signer:      "other", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&wasmtypes.MsgUpdateContractLabel{Sender: h.account("other").Address.String(), NewLabel: "counter-renamed", Contract: counterContract()}}
		},
	},
	{
		name:        "wasm_clear_admin",
		description: "The admin clears the contract's admin for good.",
		signer:      "other", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&wasmtypes.MsgClearAdmin{Sender: h.account("other").Address.String(), Contract: counterContract()}}
		},
	},
	{
		name:        "wasm_update_instantiate_config",
		description: "The code's creator restricts who may instantiate it.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 300_000,
		msgs: func(h *harness) []sdk.Msg {
			return []sdk.Msg{&wasmtypes.MsgUpdateInstantiateConfig{Sender: h.account("trader").Address.String(), CodeID: 1, NewInstantiatePermission: &wasmtypes.AccessConfig{Permission: wasmtypes.AccessTypeAnyOfAddresses, Addresses: []string{h.account("trader").Address.String()}}}}
		},
	},
}

// cosmosProposals carries every Cosmos and Wasm authority message, split so no
// proposal exceeds the block's transaction size.
var cosmosProposals = []step{
	{
		name:        "gov_proposal_cosmos",
		description: "A proposal carrying every Cosmos SDK authority message: each module's params, send-enabled, consensus params, a community pool spend, legacy content, and an upgrade with its cancellation.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 600_000,
		msgs: func(h *harness) []sdk.Msg {
			content, err := codectypes.NewAnyWithValue(&govv1beta1.TextProposal{Title: "Legacy content", Description: "Executed through MsgExecLegacyContent."})
			require.NoError(h.t, err)
			return proposal(h, "cosmos",
				&authtypes.MsgUpdateParams{Authority: govAuthority, Params: authtypes.DefaultParams()},
				&banktypes.MsgUpdateParams{Authority: govAuthority, Params: banktypes.DefaultParams()},
				&banktypes.MsgSetSendEnabled{Authority: govAuthority, SendEnabled: []*banktypes.SendEnabled{{Denom: chain.USDBaseDenom, Enabled: true}}, UseDefaultFor: []string{chain.KRWBaseDenom}},
				&consensustypes.MsgUpdateParams{Authority: govAuthority, Block: &cmtproto.BlockParams{MaxBytes: 22_020_096, MaxGas: 100_000_000}, Evidence: simtestutil.DefaultConsensusParams.Evidence, Validator: simtestutil.DefaultConsensusParams.Validator, Abci: &cmtproto.ABCIParams{VoteExtensionsEnableHeight: 1}},
				&distrtypes.MsgUpdateParams{Authority: govAuthority, Params: distrtypes.DefaultParams()},
				&distrtypes.MsgCommunityPoolSpend{Authority: govAuthority, Recipient: h.account("other").Address.String(), Amount: sdk.NewCoins(noah(1))},
				&govv1.MsgUpdateParams{Authority: govAuthority, Params: govv1.DefaultParams()},
				&govv1.MsgExecLegacyContent{Content: content, Authority: govAuthority},
				&slashingtypes.MsgUpdateParams{Authority: govAuthority, Params: slashingtypes.DefaultParams()},
				&stakingtypes.MsgUpdateParams{Authority: govAuthority, Params: stakingtypes.DefaultParams()},
				&upgradetypes.MsgSoftwareUpgrade{Authority: govAuthority, Plan: upgradetypes.Plan{Name: "v3", Height: 2_000_000, Info: "{}"}},
				&upgradetypes.MsgCancelUpgrade{Authority: govAuthority},
			)
		},
	},
	{
		name:        "gov_proposal_wasm",
		description: "A proposal carrying the Wasm authority messages that fit one transaction: params, pinning, a sudo, the upload allow-list, and a store-and-instantiate with a stub binary.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 600_000,
		msgs: func(h *harness) []sdk.Msg {
			return proposal(h, "wasm",
				&wasmtypes.MsgUpdateParams{Authority: govAuthority, Params: wasmtypes.DefaultParams()},
				&wasmtypes.MsgPinCodes{Authority: govAuthority, CodeIDs: []uint64{1}},
				&wasmtypes.MsgUnpinCodes{Authority: govAuthority, CodeIDs: []uint64{1}},
				&wasmtypes.MsgSudoContract{Authority: govAuthority, Contract: counterContract(), Msg: []byte(`{}`)},
				&wasmtypes.MsgAddCodeUploadParamsAddresses{Authority: govAuthority, Addresses: []string{h.account("trader").Address.String()}},
				&wasmtypes.MsgRemoveCodeUploadParamsAddresses{Authority: govAuthority, Addresses: []string{h.account("trader").Address.String()}},
				&wasmtypes.MsgStoreAndInstantiateContract{Authority: govAuthority, WASMByteCode: stubWasm, Admin: h.account("trader").Address.String(), Label: "stub", Msg: []byte(`{}`), Source: "https://example.invalid/stub", Builder: "cosmwasm/optimizer:0.16.0", CodeHash: stubHash()},
			)
		},
	},
	{
		name:        "gov_proposal_wasm_migrate",
		description: "A proposal carrying a store-and-migrate with a stub binary, kept apart so the two binaries never share a transaction.",
		signer:      "trader", feeDenom: chain.NoahBaseDenom, gas: 400_000,
		msgs: func(h *harness) []sdk.Msg {
			return proposal(h, "wasm-migrate",
				&wasmtypes.MsgStoreAndMigrateContract{Authority: govAuthority, WASMByteCode: stubWasm, Contract: counterContract(), Msg: []byte(`{}`)},
			)
		},
	},
}

// paymentBlock closes every run: by then every member registered above is past
// the two-week hold.
var paymentBlock = step{
	name:        "disbursement_payment_block",
	description: "Two weeks and a day after registration the block pays each member's tenth from custody: no transaction, only EndBlock events.",
	txFree:      true, jump: 14*24*time.Hour + 24*time.Hour, blockEvents: true,
}

// venue is a holding at an external venue, the shape an attested external
// custody symbol takes.
func venue(n int64) sdk.Coin {
	return sdk.NewCoin(chain.USDBaseDenom+"-venue", chain.NativeBaseAmount(n))
}

func allSteps() []step {
	var out []step
	out = append(out, firstTranche...)
	out = append(out, committee...)
	out = append(out, proposals...)
	out = append(out, cosmosSet...)
	out = append(out, wasmSet...)
	out = append(out, cosmosProposals...)
	return append(out, paymentBlock)
}

// blockEventKept selects the block events a fixture records: Ark's typed
// events and the bank movements beside them, not every module's housekeeping.
func blockEventKept(event cmtabci.Event) bool {
	switch event.Type {
	case banktypes.EventTypeTransfer, banktypes.EventTypeCoinSpent, banktypes.EventTypeCoinReceived:
		return true
	}
	return strings.HasPrefix(event.Type, "ark.")
}

func TestDecodingFixtures(t *testing.T) {
	h := newHarness(t)

	files := map[string][]byte{}
	keys := keysFile{
		Notes:   "Test keys for the decoding fixtures; never fund them on a public network. Private keys are secp256k1 scalars, public keys compressed, addresses Bech32 ark.",
		ChainID: chainID,
		Validator: validatorRec{
			Operator:        h.validatorOperator().String(),
			ConsensusPubKey: base64.StdEncoding.EncodeToString(h.validators.Keys[0].PubKey().Bytes()),
		},
	}
	for _, name := range append(slices.Clone(accountNames), "validator") {
		a := h.account(name)
		keys.Accounts = append(keys.Accounts, accountRec{
			Name: name, PrivateKeyHex: hex.EncodeToString(a.Key.Key),
			PublicKey: base64.StdEncoding.EncodeToString(a.Key.PubKey().Bytes()), Address: a.Address.String(),
		})
	}
	files["keys.json"] = marshal(t, keys)

	events := func(in []cmtabci.Event, keep func(cmtabci.Event) bool) []eventRec {
		var out []eventRec
		for _, e := range in {
			if keep != nil && !keep(e) {
				continue
			}
			rec := eventRec{Type: e.Type, Typed: h.typedJSON(e)}
			for _, a := range e.Attributes {
				rec.Attributes = append(rec.Attributes, attributeRec{Key: a.Key, Value: a.Value, Index: a.Index})
			}
			out = append(out, rec)
		}
		return out
	}

	for _, s := range allSteps() {
		if s.jump > 0 {
			h.jump(s.jump)
		}
		file := fixtureFile{Name: s.name, Description: s.description, ChainID: chainID}
		if s.txFree {
			res := h.block(nil)
			file.Height, file.BlockTime = h.height, strconv.FormatInt(h.blockTime.Unix(), 10)
			file.BlockEvents = events(res.Events, blockEventKept)
			require.NotEmpty(t, file.BlockEvents, "%s: no block events", s.name)
			files[s.name+".json"] = marshal(t, file)
			continue
		}

		signer := h.account(s.signer)
		msgs := s.msgs(h)
		quote := h.quoteFee(msgs, s.feeDenom, s.gas)
		declared := quote.Declared
		if s.override != nil {
			declared = s.override(quote)
		}
		tx := h.sign(signer, msgs, declared, s.gas, s.memo)
		res := h.block([][]byte{tx.txBytes})
		require.Len(t, res.TxResults, 1, "%s: one result per transaction", s.name)
		result := res.TxResults[0]
		t.Logf("%-32s %s", s.name, describeResult(result))

		txJSON, err := h.app.TxConfig().TxJSONEncoder()(tx.tx)
		require.NoError(t, err)

		file.Height, file.BlockTime = h.height, strconv.FormatInt(h.blockTime.Unix(), 10)
		file.Signer = &signerRec{
			Key: signer.Name, Address: signer.Address.String(),
			PublicKey:     base64.StdEncoding.EncodeToString(signer.Key.PubKey().Bytes()),
			AccountNumber: strconv.FormatUint(tx.accountNumber, 10), Sequence: strconv.FormatUint(tx.sequence, 10),
		}
		file.Fee = &feeRec{
			FeeDenom: quote.FeeDenom, GasLimit: strconv.FormatUint(quote.GasLimit, 10), Tax: quote.Tax,
			GasPrice: quote.GasPriceJSON, Required: quote.Required, PadStable: quote.PadStable, PadNoah: quote.PadNoah, Declared: declared,
		}
		file.SignDirect = &signDirectRec{
			SignDoc:       base64.StdEncoding.EncodeToString(tx.signDoc),
			BodyBytes:     base64.StdEncoding.EncodeToString(tx.bodyBytes),
			AuthInfoBytes: base64.StdEncoding.EncodeToString(tx.authInfoBytes),
		}
		file.SignAmino = &signAminoRec{SignBytes: string(tx.aminoBytes)}
		file.Signature = base64.StdEncoding.EncodeToString(tx.signature)
		file.TxBytes = base64.StdEncoding.EncodeToString(tx.txBytes)
		file.TxHash = txHash(tx.txBytes)
		file.Tx = txJSON
		file.Result = &resultRec{
			Code: result.Code, Codespace: result.Codespace, Log: result.Log,
			GasWanted: strconv.FormatInt(result.GasWanted, 10), GasUsed: strconv.FormatInt(result.GasUsed, 10),
		}
		file.Events = events(result.Events, nil)
		if s.blockEvents {
			file.BlockEvents = events(res.Events, blockEventKept)
		}
		files[s.name+".json"] = marshal(t, file)
	}

	if *update {
		require.NoError(t, os.MkdirAll(fixtureDir, 0o755))
		for name, content := range files {
			require.NoError(t, os.WriteFile(filepath.Join(fixtureDir, name), content, 0o600))
		}
		// A renamed or removed step leaves no file behind.
		entries, err := os.ReadDir(fixtureDir)
		require.NoError(t, err)
		for _, e := range entries {
			if _, known := files[e.Name()]; !known {
				require.NoError(t, os.Remove(filepath.Join(fixtureDir, e.Name())))
			}
		}
		return
	}
	for name, content := range files {
		have, err := os.ReadFile(filepath.Join(fixtureDir, name))
		require.NoError(t, err, "%s: missing; run with -update-fixtures", name)
		if !bytes.Equal(have, content) {
			t.Errorf("%s differs from the chain as built at %s; regenerate with -update-fixtures if the change is intended", name, firstDifference(have, content))
		}
	}
	entries, err := os.ReadDir(fixtureDir)
	require.NoError(t, err)
	for _, e := range entries {
		_, known := files[e.Name()]
		require.True(t, known, "%s is not produced by any step; remove it", e.Name())
	}
}

func marshal(t *testing.T, v any) []byte {
	t.Helper()
	bz, err := json.MarshalIndent(v, "", "  ")
	require.NoError(t, err)
	return append(bz, '\n')
}

// firstDifference names the first line that differs, one-based.
func firstDifference(have, want []byte) string {
	a, b := bytes.Split(have, []byte("\n")), bytes.Split(want, []byte("\n"))
	for i := range min(len(a), len(b)) {
		if !bytes.Equal(a[i], b[i]) {
			return "line " + strconv.Itoa(i+1)
		}
	}
	return "line " + strconv.Itoa(min(len(a), len(b))+1)
}
