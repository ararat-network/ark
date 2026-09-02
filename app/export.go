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

// ExportAppStateAndValidators exports the state of the application for a
// genesis file. The export continues the chain: heights stay absolute and the
// genesis starts at the next height, so every height-anchored record — mandate
// windows, claim schedules, settlement plans, oracle windows — resumes where
// it was.
//
// Zero-height export is refused. It would re-express every stored height for
// a chain restarting at one, which CometBFT has not required since genesis
// gained an initial height, and a partial re-expression imports cleanly while
// silently reopening windows the old chain had closed.
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

// trimValidators jails every validator off the allow list and applies the
// resulting set change, so a relaunch seats only the validators committed to
// it. This is the one relaunch problem no in-place fork can solve: producing
// the block that would run the fork needs the two-thirds that are missing.
// Jailing is not slashing — nothing sets a jailed-until time, so a trimmed
// validator that returns unjails at once, and its delegations stay in place.
// An entry naming no validator is refused rather than ignored, because a typo
// there would jail the validator it meant to keep.
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
