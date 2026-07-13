package proposals

import (
	cometabci "github.com/cometbft/cometbft/abci/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ValidateExtendedCommitInfo validates that the extended commit matches the
// consensus last commit and contains a super-majority of authenticated vote
// extensions. Oracle payload interpretation happens later in preblock.
func (h *Handler) ValidateExtendedCommitInfo(
	ctx sdk.Context,
	height int64,
	extendedCommitInfo cometabci.ExtendedCommitInfo,
) error {
	if err := h.validateVoteExtensionsFn(ctx, extendedCommitInfo); err != nil {
		h.logger.Error(
			"failed to validate vote extensions; vote extensions may not comprise a super-majority",
			"height", height,
			"err", err,
		)

		return err
	}

	return nil
}
