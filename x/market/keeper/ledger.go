// SPDX-License-Identifier: Apache-2.0
// Originates from Ark's Terra Classic port of x/market/keeper/swap.go.
// Modified for Ark: conversion quoting, eligibility, and settlement.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/market/types"
)

// book applies a quote to the ledger: the pool gap, the offer into the module account, the record
// and the burn its kind calls for, the mint, and the output back to trader. Expansion retains the whole NOAH
// offer for block settlement; a redemption burns the offer and mints the quoted NOAH at once, and
// block settlement later burns the Buffer-funded share.
func (k Keeper) book(ctx context.Context, trader sdk.AccAddress, q quote) error {
	// A nil delta is a stablecoin pair, which moves no pool. Every other quote already computed and
	// validated its post-swap gap, so booking writes it without recomputing.
	if !q.delta.IsNil() {
		if err := k.ArkPoolDelta.Set(ctx, q.delta); err != nil {
			return errorsmod.Wrapf(err, "applying swap to pool for offer %s and receive %s", q.offer, q.output)
		}
	}

	offer := sdk.NewCoins(q.offer)
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, trader, types.ModuleName, offer); err != nil {
		return errorsmod.Wrapf(err, "sending offer coins %s from trader %s to module", offer, trader)
	}

	burn := true
	switch q.kind {
	case expansion:
		if err := k.recordExpansion(ctx, q.offer, q.value); err != nil {
			return errorsmod.Wrapf(err, "recording expansion of offer %s into output %s", q.offer, q.output)
		}
		burn = false
	case redemption, planRedemption:
		if err := k.recordRedemption(ctx, q.value, q.output.Amount); err != nil {
			return errorsmod.Wrapf(err, "recording redemption of offer %s into output %s", q.offer, q.output)
		}
	case cross:
		// No NOAH moved, so there is nothing for block settlement to value.
	}
	if burn {
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, offer); err != nil {
			return errorsmod.Wrapf(err, "burning offer %s in module", offer)
		}
	}
	output := sdk.NewCoins(q.output)
	if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, output); err != nil {
		return errorsmod.Wrapf(err, "minting output %s in module", output)
	}
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, trader, output); err != nil {
		return errorsmod.Wrapf(err, "sending swap coins %s from module to trader %s", output, trader)
	}
	return nil
}
