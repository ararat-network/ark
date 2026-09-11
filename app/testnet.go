// SPDX-License-Identifier: Apache-2.0
// Adapted from Gaia, cmd/gaiad/cmd/testnet_set_local_validator.go.
// Modified for Ark: the validator set is replaced through the keepers, the
// bond is minted by the market module, oracle attendance is reset, and an
// upgrade can be scheduled for the first block.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package app

import (
	"fmt"

	cmtcrypto "github.com/cometbft/cometbft/crypto"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"github.com/ararat-network/ark/pkg/chain"
	markettypes "github.com/ararat-network/ark/x/market/types"
)

// TestnetValidatorTokens is the self-bond of the validator an in-place
// testnet installs, one seat's grant, and TestnetValidatorFloat the liquid
// balance minted beside it: a bond cannot pay fees.
var (
	TestnetValidatorTokens = chain.NativeBaseAmount(chain.SeatGrantNoah)
	TestnetValidatorFloat  = chain.NativeBaseAmount(chain.SeatFloatNoah)
)

// InitArkAppForTestnet turns committed state into a testnet the local node
// controls: every validator goes, and one bonded at operator with consPubKey
// takes the whole set. The changes sit in the working state until the next
// block commits them, which is why the caller must be the app the node
// starts with. upgradeToTrigger, when set, names an upgrade to run in that
// block. It returns the validator's consensus power, for CometBFT's set.
func InitArkAppForTestnet(arkApp *ArkApp, consPubKey cmtcrypto.PubKey, operator sdk.AccAddress, upgradeToTrigger string) (int64, error) {
	// Over the root store: the writes wait in its working state for the next
	// block to commit them, which a cached context would not give them.
	ctx := sdk.NewContext(arkApp.CommitMultiStore(), cmtproto.Header{Height: arkApp.LastBlockHeight()}, false, arkApp.Logger())
	staking := arkApp.StakingKeeper
	valCodec := staking.ValidatorAddressCodec()
	bondDenom, err := staking.BondDenom(ctx)
	if err != nil {
		return 0, err
	}
	powerReduction := staking.PowerReduction(ctx)

	// Every validator goes with its delegations, and the pools with them.
	validators, err := staking.GetAllValidators(ctx)
	if err != nil {
		return 0, err
	}
	bonded, notBonded := math.ZeroInt(), math.ZeroInt()
	store := ctx.KVStore(arkApp.GetKey(stakingtypes.ModuleName))
	for _, validator := range validators {
		valAddr, err := valCodec.StringToBytes(validator.GetOperator())
		if err != nil {
			return 0, err
		}
		consAddr, err := validator.GetConsAddr()
		if err != nil {
			return 0, err
		}
		delegations, err := staking.GetValidatorDelegations(ctx, valAddr)
		if err != nil {
			return 0, err
		}
		for _, delegation := range delegations {
			if err := staking.RemoveDelegation(ctx, delegation); err != nil {
				return 0, fmt.Errorf("removing delegation to %s: %w", validator.GetOperator(), err)
			}
		}
		if validator.IsBonded() {
			bonded = bonded.Add(validator.Tokens)
		} else {
			notBonded = notBonded.Add(validator.Tokens)
		}
		store.Delete(stakingtypes.GetValidatorKey(valAddr))
		store.Delete(stakingtypes.GetValidatorByConsAddrKey(consAddr))
		store.Delete(stakingtypes.GetValidatorsByPowerIndexKey(validator, powerReduction, valCodec))
		store.Delete(stakingtypes.GetLastValidatorPowerKey(valAddr))
		if validator.IsUnbonding() {
			if err := staking.DeleteValidatorQueue(ctx, validator); err != nil {
				return 0, err
			}
		}
	}
	if bonded.IsPositive() {
		if err := arkApp.BankKeeper.BurnCoins(ctx, stakingtypes.BondedPoolName, sdk.NewCoins(sdk.NewCoin(bondDenom, bonded))); err != nil {
			return 0, fmt.Errorf("burning the bonded pool: %w", err)
		}
	}
	if notBonded.IsPositive() {
		if err := arkApp.BankKeeper.BurnCoins(ctx, stakingtypes.NotBondedPoolName, sdk.NewCoins(sdk.NewCoin(bondDenom, notBonded))); err != nil {
			return 0, fmt.Errorf("burning the not-bonded pool: %w", err)
		}
	}

	// The one validator, bonded at the commission floor with a seat's grant.
	pubKey, err := cryptocodec.FromCmtPubKeyInterface(consPubKey)
	if err != nil {
		return 0, err
	}
	valAddr := sdk.ValAddress(operator)
	valAddrStr, err := valCodec.BytesToString(valAddr)
	if err != nil {
		return 0, err
	}
	validator, err := stakingtypes.NewValidator(valAddrStr, pubKey, stakingtypes.NewDescription("testnet", "", "", "", ""))
	if err != nil {
		return 0, err
	}
	params, err := staking.GetParams(ctx)
	if err != nil {
		return 0, err
	}
	rate := math.LegacyMaxDec(params.MinCommissionRate, math.LegacyMustNewDecFromStr("0.05"))
	validator.Commission = stakingtypes.NewCommission(rate, math.LegacyMaxDec(rate, math.LegacyMustNewDecFromStr("0.2")), math.LegacyMustNewDecFromStr("0.01"))
	validator.Status = stakingtypes.Bonded
	if err := staking.SetValidator(ctx, validator); err != nil {
		return 0, err
	}
	if err := staking.SetValidatorByConsAddr(ctx, validator); err != nil {
		return 0, err
	}
	if err := staking.SetValidatorByPowerIndex(ctx, validator); err != nil {
		return 0, err
	}
	if err := staking.Hooks().AfterValidatorCreated(ctx, valAddr); err != nil {
		return 0, err
	}
	minted := sdk.NewCoins(sdk.NewCoin(bondDenom, TestnetValidatorTokens.Add(TestnetValidatorFloat)))
	if err := arkApp.BankKeeper.MintCoins(ctx, markettypes.ModuleName, minted); err != nil {
		return 0, fmt.Errorf("minting the bond and float: %w", err)
	}
	if err := arkApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, markettypes.ModuleName, operator, minted); err != nil {
		return 0, err
	}
	validator, err = staking.GetValidator(ctx, valAddr)
	if err != nil {
		return 0, err
	}
	if _, err := staking.Delegate(ctx, operator, TestnetValidatorTokens, stakingtypes.Unbonded, validator, true); err != nil {
		return 0, fmt.Errorf("bonding the validator: %w", err)
	}
	power := sdk.TokensToConsensusPower(TestnetValidatorTokens, powerReduction)
	if err := staking.SetLastValidatorPower(ctx, valAddr, power); err != nil {
		return 0, err
	}
	if err := staking.SetLastTotalPower(ctx, math.NewInt(power)); err != nil {
		return 0, err
	}
	consAddr := sdk.ConsAddress(consPubKey.Address())
	if err := arkApp.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr, slashingtypes.ValidatorSigningInfo{
		Address:     consAddr.String(),
		StartHeight: arkApp.LastBlockHeight(),
	}); err != nil {
		return 0, err
	}

	// The oracle's per-validator window state names validators that are
	// gone; it restarts with the set, as it does at a window boundary.
	if err := arkApp.OracleKeeper.RewardWeight.Clear(ctx, nil); err != nil {
		return 0, err
	}
	if err := arkApp.OracleKeeper.Attendance.Clear(ctx, nil); err != nil {
		return 0, err
	}

	if upgradeToTrigger != "" {
		plan := upgradetypes.Plan{Name: upgradeToTrigger, Height: arkApp.LastBlockHeight() + 1}
		if err := arkApp.UpgradeKeeper.ScheduleUpgrade(ctx, plan); err != nil {
			return 0, fmt.Errorf("scheduling upgrade %q: %w", upgradeToTrigger, err)
		}
	}
	return power, nil
}
