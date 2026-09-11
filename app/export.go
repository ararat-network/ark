// SPDX-License-Identifier: Apache-2.0
// Adapted from Gaia, app/export.go.
// Modified for Ark: continuation export and validator allowlist handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package app

import (
	"encoding/json"
	"errors"
	"fmt"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
)

// ExportAppStateAndValidators exports a continuation genesis at the next absolute height,
// preserving height-anchored windows and schedules. Zero-height export is refused.
func (app *ArkApp) ExportAppStateAndValidators(forZeroHeight bool, jailAllowedAddrs, modulesToExport []string) (servertypes.ExportedApp, error) {
	if forZeroHeight {
		return servertypes.ExportedApp{}, errors.New(
			"zero-height export is not supported: export at a height and relaunch from the exported initial height",
		)
	}

	// as if they could withdraw from the start of the next block
	ctx := app.NewContextLegacy(true, cmtproto.Header{Height: app.LastBlockHeight()})

	if len(jailAllowedAddrs) > 0 {
		if err := app.trimValidators(ctx, jailAllowedAddrs); err != nil {
			return servertypes.ExportedApp{}, err
		}
	}

	genState, err := app.ModuleManager.ExportGenesisForModules(ctx, app.appCodec, modulesToExport)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}

	appState, err := json.MarshalIndent(genState, "", "  ")
	if err != nil {
		return servertypes.ExportedApp{}, err
	}

	validators, err := staking.WriteValidators(ctx, app.StakingKeeper)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}
	// Unreachable from a live chain, which always has a bonded set, and from
	// trimming, which refuses an allow list naming no validator. Kept because
	// a genesis with no validators fails at the relaunch's InitChain, far from
	// the operator who could fix it.
	if len(validators) == 0 {
		return servertypes.ExportedApp{}, errors.New("exported validator set is empty")
	}

	// We export at last height + 1, because that's the height at which
	// CometBFT will start InitChain.
	return servertypes.ExportedApp{
		AppState:        appState,
		Validators:      validators,
		Height:          app.LastBlockHeight() + 1,
		ConsensusParams: app.GetConsensusParams(ctx),
	}, nil
}

// trimValidators jails validators outside the relaunch allowlist and applies the set change.
// Delegations remain and returning validators can unjail; unknown allowlist entries are errors.
func (app *ArkApp) trimValidators(ctx sdk.Context, allowedAddrs []string) error {
	allowed := make(map[string]struct{}, len(allowedAddrs))
	for _, addr := range allowedAddrs {
		operator, err := sdk.ValAddressFromBech32(addr)
		if err != nil {
			return fmt.Errorf("parsing allowed validator %q: %w", addr, err)
		}
		if _, err := app.StakingKeeper.GetValidator(ctx, operator); err != nil {
			return fmt.Errorf("allowed validator %s: %w", addr, err)
		}
		allowed[operator.String()] = struct{}{}
	}

	validators, err := app.StakingKeeper.GetAllValidators(ctx)
	if err != nil {
		return fmt.Errorf("listing validators: %w", err)
	}
	for _, validator := range validators {
		if _, keep := allowed[validator.GetOperator()]; keep || validator.IsJailed() {
			continue
		}
		consAddr, err := validator.GetConsAddr()
		if err != nil {
			return fmt.Errorf("resolving consensus address of %s: %w", validator.GetOperator(), err)
		}
		if err := app.StakingKeeper.Jail(ctx, consAddr); err != nil {
			return fmt.Errorf("jailing %s: %w", validator.GetOperator(), err)
		}
	}
	if _, err := app.StakingKeeper.ApplyAndReturnValidatorSetUpdates(ctx); err != nil {
		return fmt.Errorf("applying trimmed validator set: %w", err)
	}

	return nil
}
