package client

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"cosmossdk.io/math"

	sdkclient "github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/pkg/chain"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// DefaultFeeHeadroom is declared above the chain's figure on every stable
// fee leg. The leg is a ceiling (D80): the chain charges the exact tax and
// base fee under it and the rest never leaves the account, so the headroom
// costs nothing and survives what moves between building and inclusion —
// the base fee by at most 2.5% a block, a capped tax with the oracle rate.
// NOAH gets none: its leg is charged whole, so headroom there is a tip.
var DefaultFeeHeadroom = math.LegacyMustNewDecFromStr("1.1")

// withHeadroom sizes the declaration for every leg of a priced fee.
func withHeadroom(fee sdk.Coins) sdk.Coins {
	lifted := sdk.NewCoins()
	for _, coin := range fee {
		coin.Amount = lift(coin.Denom, coin.Amount)
		lifted = lifted.Add(coin)
	}
	return lifted
}

// lift sizes one fee leg's declaration under DefaultFeeHeadroom: a stable
// leg ceils, NOAH stays exact. The declaration and the balance check that
// picks a denomination both price through here, so they cannot disagree.
func lift(denom string, amount math.Int) math.Int {
	if denom == chain.NoahBaseDenom {
		return amount
	}
	return DefaultFeeHeadroom.MulInt(amount).Ceil().TruncateInt()
}

// headroomPercent is DefaultFeeHeadroom as the whole percentage it adds.
func headroomPercent() math.Int {
	return DefaultFeeHeadroom.Sub(math.LegacyOneDec()).MulInt64(100).TruncateInt()
}

// FeeBreakdown is one priced fee and the parts it was built from: the base
// fee before the lift, the tax, the tip, and the declaration they became.
type FeeBreakdown struct {
	Gas sdk.Coins
	Tax sdk.Coins
	Tip sdk.Coins
	Fee sdk.Coins
}

// String is the one line a signer reads above the SDK's confirmation, which
// shows the fee as a single figure: what the declaration is made of.
func (b FeeBreakdown) String() string {
	if b.Fee.IsZero() {
		return "fee: none declared"
	}
	var parts []string
	if !b.Gas.IsZero() {
		parts = append(parts, "base fee "+b.Gas.String())
	}
	if !b.Tax.IsZero() {
		parts = append(parts, "transfer tax "+b.Tax.String())
	}
	line := "fee " + b.Fee.String()
	if len(parts) > 0 {
		line += ": " + strings.Join(parts, " + ")
		for _, coin := range b.Gas.Add(b.Tax...) {
			if coin.Denom != chain.NoahBaseDenom {
				line += fmt.Sprintf(", stable legs declared with %s%% headroom", headroomPercent())
				break
			}
		}
	}
	if !b.Tip.IsZero() {
		line += "; tip " + b.Tip.String()
	}
	return line
}

// feeQuote is what the chain says about one transaction: the tax its messages
// owe and the principal it was computed over, the posted gas prices, and what
// the payer can spend. Pricing against a gas figure is then arithmetic over
// figures already in hand.
type feeQuote struct {
	transferred sdk.Coins // what the messages move: the denominations to prefer
	spent       sdk.Coins // what of it leaves the fee account, netted from its balance
	tax         sdk.Coins
	sheet       []treasurytypes.GasPrice
	reference   treasurytypes.GasPrice
	spendable   sdk.Coins
	hasPayer    bool
}

// quoteFee asks the chain what msgs owe and what payer can spend; sends says
// whether payer is also the account the messages draw on, so the transfer
// counts against the same balance. fromSheet says whether the gas leg is the
// client's to price; a command that priced its own gas needs neither the
// sheet nor the balances. Offline there is no chain to ask, and a fee short
// of the tax is refused, so the fee has to be declared.
func quoteFee(clientCtx sdkclient.Context, msgs []sdk.Msg, payer sdk.AccAddress, sends, fromSheet bool) (feeQuote, error) {
	if clientCtx.Offline {
		return feeQuote{}, errors.New("offline, the transfer tax cannot be priced; provide --fees")
	}
	tax, transferred, err := computeTax(clientCtx, msgs)
	if err != nil {
		return feeQuote{}, err
	}
	quote := feeQuote{transferred: transferred, tax: tax}
	if sends {
		quote.spent = transferred
	}
	if !fromSheet {
		return quote, nil
	}
	sheet, err := treasurytypes.NewQueryClient(clientCtx).GasPrices(
		clientCtx.GetCmdContextWithFallback(),
		&treasurytypes.QueryGasPricesRequest{},
	)
	if err != nil {
		return feeQuote{}, fmt.Errorf("querying the gas price sheet: %w", err)
	}
	quote.sheet = sheet.GasPrices
	quote.reference = sheet.ReferenceGasPrice
	if payer.Empty() {
		return quote, nil
	}
	balances, err := banktypes.NewQueryClient(clientCtx).SpendableBalances(
		clientCtx.GetCmdContextWithFallback(),
		&banktypes.QuerySpendableBalancesRequest{Address: payer.String()},
	)
	if err != nil {
		return feeQuote{}, fmt.Errorf("querying spendable balances: %w", err)
	}
	quote.spendable = balances.Balances
	quote.hasPayer = true
	return quote, nil
}

// price is the declaration for a settled gas figure: the chain's tax, gas
// from gasFee when the command priced it or from the quote when not, every
// stable leg lifted, NOAH exact, tip added whole. The tax is the figure the
// ante holds the fee to, so the signer sees the whole charge in the fee they
// sign and is never taxed past it. The breakdown returned is the finished
// declaration: Fee always carries Tip. A zero gas prices nothing but the tip,
// as a factory without a gas figure declares no gas.
func (q feeQuote) price(gas uint64, gasFee, tip sdk.Coins) (FeeBreakdown, error) {
	if gas == 0 {
		return FeeBreakdown{Tip: tip, Fee: tip}, nil
	}
	// An unquoted sheet has no reference row: offline, or a command whose own
	// gas price priced the leg already.
	if gasFee.IsZero() && q.reference.Denom != "" {
		priced, err := q.gasFee(gas)
		if err != nil {
			return FeeBreakdown{}, err
		}
		gasFee = priced
	}
	return FeeBreakdown{
		Gas: gasFee,
		Tax: q.tax,
		Tip: tip,
		Fee: withHeadroom(gasFee.Add(q.tax...)).Add(tip...),
	}, nil
}

// computeTax is the chain's own figure for what msgs owe, from the same
// calculator the ante holds the fee to, beside the principal it taxed: the
// denominations the transaction is already moving, which the fee prefers to
// ride. The messages travel as the bytes the chain decodes for itself, so
// what the command built them as — autocli builds dynamic ones — is no
// concern here.
func computeTax(clientCtx sdkclient.Context, msgs []sdk.Msg) (tax, transferred sdk.Coins, err error) {
	packed := make([]*codectypes.Any, len(msgs))
	for i, msg := range msgs {
		anyMsg, err := codectypes.NewAnyWithValue(msg)
		if err != nil {
			return nil, nil, fmt.Errorf("packing message %d for the tax query: %w", i, err)
		}
		packed[i] = anyMsg
	}
	resp, err := treasurytypes.NewQueryClient(clientCtx).ComputeTax(
		clientCtx.GetCmdContextWithFallback(),
		&treasurytypes.QueryComputeTaxRequest{Messages: packed},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("querying the transfer tax: %w", err)
	}
	return resp.Tax, resp.TaxBase, nil
}

// gasFee prices gas from the quoted sheet: the reference row without a payer
// to inspect — the figure the gate quotes on refusal — and otherwise the
// first denomination in the recorded order whose spendable balance covers the
// declaration.
func (q feeQuote) gasFee(gas uint64) (sdk.Coins, error) {
	gasLimit := math.LegacyNewDecFromBigInt(new(big.Int).SetUint64(gas))
	if !q.hasPayer {
		return sdk.NewCoins(sdk.NewCoin(
			q.reference.Denom,
			q.reference.GasPrice.Mul(gasLimit).Ceil().RoundInt(),
		)), nil
	}
	fee, ok := q.pickFeeDenom(gasLimit)
	if !ok {
		return nil, fmt.Errorf(
			"no spendable balance covers the gas fee (%s%s at the posted prices)%s; provide --fees",
			q.reference.GasPrice.Mul(gasLimit).Ceil().RoundInt(),
			q.reference.Denom,
			besideTax(q.tax),
		)
	}
	return sdk.NewCoins(fee), nil
}

// pickFeeDenom walks the fee-denomination candidates in the recorded order —
// the denominations the transaction moves, NOAH, the reference, then the rest
// of the sheet — and takes the first that is priced with a spendable balance
// covering the declaration — gas fee and tax lifted by the headroom, NOAH
// exact — plus whatever of the transfer leaves the same account. The
// reference arrives as its own row beside the sheet and joins the price map,
// so a transferred reference amount still prices through the first tier. The
// coin returned is the gas portion alone, unlifted; the caller adds the tax
// and lifts.
func (q feeQuote) pickFeeDenom(gasLimit math.LegacyDec) (sdk.Coin, bool) {
	rows := make(map[string]treasurytypes.GasPrice, len(q.sheet)+1)
	for _, row := range q.sheet {
		rows[row.Denom] = row
	}
	rows[q.reference.Denom] = q.reference

	candidates := make([]string, 0, len(q.transferred)+len(q.sheet)+2)
	for _, coin := range q.transferred {
		candidates = append(candidates, coin.Denom)
	}
	candidates = append(candidates, chain.NoahBaseDenom, q.reference.Denom)
	for _, row := range q.sheet {
		candidates = append(candidates, row.Denom)
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, denom := range candidates {
		if _, done := seen[denom]; done {
			continue
		}
		seen[denom] = struct{}{}
		row, priced := rows[denom]
		if !priced {
			continue
		}
		amount := row.GasPrice.Mul(gasLimit).Ceil().RoundInt()
		need := lift(denom, amount.Add(q.tax.AmountOf(denom)))
		need = need.Add(q.spent.AmountOf(denom))
		if q.spendable.AmountOf(denom).GTE(need) {
			return sdk.NewCoin(denom, amount), true
		}
	}
	return sdk.Coin{}, false
}

// besideTax names the tax a fee also has to carry, when there is one.
func besideTax(tax sdk.Coins) string {
	if tax.IsZero() {
		return ""
	}
	return fmt.Sprintf(" beside the transfer tax %s", tax)
}
