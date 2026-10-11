package keeper

import (
	"context"
	"errors"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/types"
)

// quote is what a look at a grant pays now and when the module looks next.
type quote struct {
	amount sdk.Coin
	next   uint64
}

// payable quotes a grant: what its schedule has accrued and it has not been paid.
func (k *Keeper) payable(ctx context.Context, g types.Grant) (quote, error) {
	at := chain.BlockSeconds(ctx)
	earned, next, err := g.Accrued(at)
	if err != nil {
		return quote{}, err
	}
	// Unreachable: a payment pays at most what had accrued, accrual never falls and a cancel freezes
	// it, and import refuses principal paid past genesis time.
	if earned.LT(g.Paid) {
		return quote{}, errors.New("grant paid beyond accrued entitlement")
	}
	q := quote{
		amount: sdk.NewCoin(g.Amount.Denom, earned.Sub(g.Paid)),
		next:   next,
	}
	if g.Suspended() {
		q.amount.Amount = math.ZeroInt()
	}
	return q, nil
}

// settle pays a quote and queues the grant for its next boundary. A send Bank refuses returns
// ErrPaymentRefused with nothing written.
func (k *Keeper) settle(ctx context.Context, actor string, g types.Grant, q quote) error {
	amount := q.amount.Amount
	account := types.Account(g.Kind)
	if amount.IsPositive() {
		payee, err := chain.ParseCanonicalAccountAddress("payee", g.Payee)
		if err != nil {
			return err
		}
		// Unreachable: import holds custody to the books and every send releases what it pays. The
		// check keeps a shortfall an invariant failure, so a send error below is a payee refusal.
		if k.bankKeeper.GetBalance(ctx, k.accountKeeper.GetModuleAddress(account), q.amount.Denom).Amount.LT(amount) {
			return fmt.Errorf("%s custody is below the %s payment", account, q.amount)
		}
		// Bank refuses a blocked payee and applies send restrictions before it moves a coin, so a
		// refusal leaves nothing written: the block defers it, a transaction fails with it.
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, account, payee, sdk.NewCoins(q.amount)); err != nil {
			return errorsmod.Wrapf(types.ErrPaymentRefused, "%s to %s: %v", q.amount, g.Payee, err)
		}
		g.Paid = g.Paid.Add(amount) // Payment cannot exceed the validated remaining principal.
		g.Remaining = g.Remaining.Sub(amount)
		b, err := k.getBook(ctx, account)
		if err != nil {
			return err
		}
		// Unreachable: the book reserves at least the grant's remaining principal, which covers the payment.
		if err := b.Release(q.amount); err != nil {
			return err
		}
		if err := k.Books.Set(ctx, account, b); err != nil {
			return err
		}
	}
	next := q.next
	if g.Suspended() || g.Remaining.IsZero() {
		next = 0
	}
	if err := k.setDueTime(ctx, &g, next); err != nil {
		return err
	}
	if err := g.Validate(); err != nil {
		return err
	}
	if err := k.Grants.Set(ctx, g.Id, g); err != nil {
		return err
	}
	if amount.IsZero() {
		return nil
	}
	return k.recordGrant(ctx, types.GrantEntry{
		GrantId: g.Id,
		Action:  types.GrantAction_GRANT_ACTION_PAID,
		Actor:   actor,
		Amount:  q.amount,
		Subject: g.Payee,
	})
}
