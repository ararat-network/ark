package client

import (
	"bytes"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	sdkclient "github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"

	chain "github.com/ararat-network/ark/pkg/chain"
)

// TipFlagUsage is the help arkd gives the SDK's --tip flag, which the v0.54
// factory defines on every transaction command and never reads.
const TipFlagUsage = "NOAH paid above the base fee for priority, e.g. 1000anoah"

// DefaultGasAdjustment is what arkd stamps on every command carrying
// --gas-adjustment, and the only correction any client applies. An estimate
// tracks execution closely: Ark's fee decorator meters its gate under
// simulation and charges a stand-in for the transfer a fee-less estimate
// cannot make — which --gas auto always is, its gas limit being zero when
// the estimate is built — and what remains is wasmd's counter, which skips
// simulation, against the SDK's size decorator, which overcharges it.
// TestGasEstimateMatchesExecution pins the worst ratio at just under one
// and fails if this drifts short or loose. The margin above is for what no
// estimate can see: state moving between the estimate and inclusion, a
// vesting payer's dearer transfer, a wider balance. An explicit
// --gas-adjustment overrides it.
const DefaultGasAdjustment = 1.15

// PriceTransactions wraps a transaction command so its fee is built for it — the
// chain's tax on the messages, gas from --gas-prices or the sheet, headroom
// on every stable leg, a NOAH tip from --tip — without the command knowing.
// The command runs once, as written; what is replaced is the TxConfig it
// builds through. Every transaction the SDK's factory builds comes from
// TxConfig.NewTxBuilder, and a builder holds all the fee needs: the
// messages, the settled gas, the payer. The parts of the fee are printed to
// stderr ahead of the SDK's confirmation, which shows the fee as one figure.
// An explicit --fees is left as declared, with the tip added.
func PriceTransactions(cmd *cobra.Command) {
	run := cmd.RunE
	if run == nil {
		return
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		stored := sdkclient.GetClientContextFromCmd(cmd)
		pricing, err := newPricer(cmd, stored)
		if err != nil {
			return err
		}
		completing := stored.WithTxConfig(pricingTxConfig{TxConfig: stored.TxConfig, pricer: pricing})
		if err := sdkclient.SetCmdClientContext(cmd, completing); err != nil {
			return err
		}
		defer func() { _ = sdkclient.SetCmdClientContext(cmd, stored) }()
		return pricing.settling(func() error { return run(cmd, args) })
	}
}

// pricer is the policy one command's transactions are priced under, read
// from its flags: --fees leaves the declaration as it is and adds the tip,
// --gas-prices makes the gas leg the SDK's, --offline has no chain to ask.
// The queries go over the stored context, which knows the node and needs no
// keyring: a transaction names its own payer.
type pricer struct {
	clientCtx sdkclient.Context
	out       io.Writer
	tip       sdk.Coins
	explicit  bool
	fromSheet bool
}

func newPricer(cmd *cobra.Command, stored sdkclient.Context) (*pricer, error) {
	flagSet := cmd.Flags()
	tip, err := tipFlag(flagSet)
	if err != nil {
		return nil, err
	}
	fees, _ := flagSet.GetString(flags.FlagFees)
	gasPrices, _ := flagSet.GetString(flags.FlagGasPrices)
	offline, _ := flagSet.GetBool(flags.FlagOffline)
	return &pricer{
		clientCtx: stored.WithOffline(offline || stored.Offline),
		out:       cmd.ErrOrStderr(),
		tip:       tip,
		explicit:  fees != "",
		fromSheet: gasPrices == "",
	}, nil
}

// refusal carries the one error settling raises, past signatures that have
// none to return.
type refusal struct{ err error }

// settling runs the command with that refusal turned back into an error. A
// fee is settled where a built transaction is first read — the confirmation,
// the sign bytes, the generate-only print — and each of those precedes
// signing and broadcast, so unwinding through them cannot leave a
// transaction half sent.
func (p *pricer) settling(run func() error) (err error) {
	defer func() {
		raised := recover()
		if raised == nil {
			return
		}
		refused, ok := raised.(refusal)
		if !ok {
			panic(raised)
		}
		err = refused.err
	}()
	return run()
}

// price is the declaration for a built transaction, printed as the signer
// reads it above the SDK's confirmation.
func (p *pricer) price(tx authsigning.Tx) (sdk.Coins, error) {
	if p.explicit {
		return tx.GetFee().Add(p.tip...), nil
	}
	payer, sends := payerOf(tx)
	quote, err := quoteFee(p.clientCtx, tx.GetMsgs(), payer, sends, p.fromSheet)
	if err != nil {
		return nil, err
	}
	priced, err := quote.price(tx.GetGas(), tx.GetFee(), p.tip)
	if err != nil {
		return nil, err
	}
	fmt.Fprintln(p.out, priced)
	return priced.Fee, nil
}

// payerOf is the account the fee is drawn on — the granter when there is
// one, else the payer the transaction names or its first signer — and
// whether that is the first signer, the account the messages draw on, so the
// transfer counts against the same balance.
func payerOf(tx authsigning.Tx) (payer sdk.AccAddress, sends bool) {
	if granter := tx.FeeGranter(); len(granter) > 0 {
		return granter, false
	}
	payer = tx.FeePayer()
	signers, err := tx.GetSigners()
	return payer, err == nil && len(signers) > 0 && bytes.Equal(payer, signers[0])
}

// pricingTxConfig is the command's TxConfig for the length of one run: the
// builders it hands out complete their own fee. Only NewTxBuilder is its own.
// WrapTxBuilder carries a transaction someone else built and finished — what
// tx sign is given — and stays the SDK's.
type pricingTxConfig struct {
	sdkclient.TxConfig
	pricer *pricer
}

func (c pricingTxConfig) NewTxBuilder() sdkclient.TxBuilder {
	return &pricingTxBuilder{TxBuilder: c.TxConfig.NewTxBuilder(), pricer: c.pricer}
}

// pricingTxBuilder settles the fee of what it built when that is first read.
// Reading is the seam rather than any one setter: everything is set before
// anything reads, so the order the setters arrive in is not relied on, and
// the transaction read carries all the pricing needs.
type pricingTxBuilder struct {
	sdkclient.TxBuilder
	pricer  *pricer
	settled bool
}

func (b *pricingTxBuilder) GetTx() authsigning.Tx {
	b.settle()
	return b.TxBuilder.GetTx()
}

func (b *pricingTxBuilder) SetSignatures(signatures ...signingtypes.SignatureV2) error {
	b.settle()
	return b.TxBuilder.SetSignatures(signatures...)
}

// SetExtensionOptions forwards what an embedded interface cannot: the factory
// asks for ExtendedTxBuilder by assertion, which this type has to satisfy
// itself or the options are dropped in silence.
func (b *pricingTxBuilder) SetExtensionOptions(options ...*codectypes.Any) {
	if extended, ok := b.TxBuilder.(sdkclient.ExtendedTxBuilder); ok {
		extended.SetExtensionOptions(options...)
	}
}

// settle fixes the fee once. A builder without gas is one the factory is
// simulating for an estimate — --gas auto builds with none — and has no fee
// to settle; the build that follows the estimate settles it.
func (b *pricingTxBuilder) settle() {
	tx := b.TxBuilder.GetTx()
	if b.settled || tx.GetGas() == 0 {
		return
	}
	b.settled = true
	fee, err := b.pricer.price(tx)
	if err != nil {
		panic(refusal{err})
	}
	b.SetFeeAmount(fee)
}

// tipFlag reads --tip: NOAH alone, none when the flag is absent or blank.
func tipFlag(flagSet *pflag.FlagSet) (sdk.Coins, error) {
	if flagSet.Lookup(flags.FlagTip) == nil {
		return nil, nil
	}
	value, err := flagSet.GetString(flags.FlagTip)
	if err != nil || value == "" {
		return nil, err
	}
	tip, err := sdk.ParseCoinNormalized(value)
	if err != nil {
		return nil, fmt.Errorf("parsing --%s: %w", flags.FlagTip, err)
	}
	if tip.Denom != chain.NoahBaseDenom {
		return nil, fmt.Errorf("tips are paid in %s, got %s", chain.NoahBaseDenom, tip)
	}
	if !tip.IsPositive() {
		return nil, nil
	}
	return sdk.NewCoins(tip), nil
}
