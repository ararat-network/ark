package types

import (
	"context"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	assettypes "ark/x/asset/types"
	oracletypes "ark/x/oracle/types"
	treasurytypes "ark/x/treasury/types"
)

// AccountKeeper is expected keeper for auth module
type AccountKeeper interface {
	GetModuleAddress(name string) sdk.AccAddress
	GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
}

// BankKeeper defines expected supply keeper
type BankKeeper interface {
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error

	BurnCoins(ctx context.Context, name string, amt sdk.Coins) error
	MintCoins(ctx context.Context, name string, amt sdk.Coins) error
}

// OracleKeeper defines expected oracle keeper. Market reads rates and nothing
// else: conversion policy is Market's own, and eligibility is the asset
// registry's.
type OracleKeeper interface {
	GetRateSet(ctx context.Context, denoms ...string) (oracletypes.RateSet, error)
}

// AssetKeeper defines the lifecycle authority Market derives eligibility from.
// Market stores rate policy only — never a membership set — so every question
// about whether a denomination may be offered, asked, or settled is answered
// here.
type AssetKeeper interface {
	GetAsset(ctx context.Context, denom string) (assettypes.Asset, error)
	ActiveSettlementPlan(ctx context.Context, denom string) (assettypes.SettlementPlan, bool, error)
	PricedLiveDenoms(ctx context.Context) ([]string, error)
	GetReference(ctx context.Context) (assettypes.ReferenceState, error)
}

// TreasuryKeeper defines the allocation and liability accounting required by
// Market settlement.
type TreasuryKeeper interface {
	RouteExpansion(ctx context.Context, grossOffer sdk.Coin, stableOutput sdk.Coin, quoteRates oracletypes.RateSet) (treasurytypes.ExpansionAllocation, error)
	DrawRedemptionBuffer(ctx context.Context, redeemedStable sdk.Coin, noahOutput math.Int, quoteRates oracletypes.RateSet) (treasurytypes.BufferDraw, error)
	RecordSupplyChange(ctx context.Context, burned sdk.Coin, minted sdk.Coin, quoteRates oracletypes.RateSet) error
}
