package oracle

import (
	"bytes"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oracleencoding "ark/abci/oracle/encoding"
	vetypes "ark/abci/ve/types"
	oracletypes "ark/x/oracle/types"
)

// ValidateVoteExtension validates a decoded oracle vote-extension payload.
func ValidateVoteExtension(voteExtension vetypes.OracleVoteExtension) error {
	if len(voteExtension.Rates) > oracletypes.MaxVoteTargets {
		return fmt.Errorf("number of oracle vote extension rates %d exceeds maximum %d", len(voteExtension.Rates), oracletypes.MaxVoteTargets)
	}

	for denom, rawRate := range voteExtension.Rates {
		if rawRate == nil {
			return errors.New("nil oracle vote extension rate")
		}
		rate, err := oracleencoding.DecodeRate(rawRate)
		if err != nil {
			return fmt.Errorf("invalid oracle vote extension rate for denom %s: %w", denom, err)
		}
		canonical, err := oracleencoding.EncodeRate(rate)
		if err != nil {
			return fmt.Errorf("canonicalize oracle vote extension rate for denom %s: %w", denom, err)
		}
		if !bytes.Equal(rawRate, canonical) {
			return fmt.Errorf("non-canonical oracle vote extension rate for denom %s", denom)
		}
		if err := sdk.ValidateDenom(denom); err != nil {
			return fmt.Errorf("invalid oracle vote extension denom %s: %w", denom, err)
		}
	}

	return nil
}
