package proposals

import abcimetrics "github.com/ararat-network/ark/abci/metrics"

// ErrExtendedCommitValidation is returned when an extended commit cannot be validated.
var ErrExtendedCommitValidation = abcimetrics.NewError(abcimetrics.StatusExtendedCommitValidation, "extended commit validation failed")
