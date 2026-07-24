package proposals

import "errors"

// ErrExtendedCommitValidation is returned when an extended commit cannot be validated.
var ErrExtendedCommitValidation = errors.New("extended commit validation failed")
