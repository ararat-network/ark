package proposals

import abcimetrics "github.com/ararat-network/ark/abci/metrics"

// ErrExtendedCommitValidation is returned when an extended commit cannot be validated.
var ErrExtendedCommitValidation = abcimetrics.NewError(abcimetrics.StatusExtendedCommitValidation, "extended commit validation failed")

// errCommitOnly records a PrepareProposal that carried the commit alone under the stale-price rule.
// The response is a success, so the status, not an error return, is what makes the fallback visible.
var errCommitOnly = abcimetrics.NewError(abcimetrics.StatusCommitOnly, "commit-only proposal")
