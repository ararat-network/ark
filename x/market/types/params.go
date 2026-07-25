package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/pkg/decimal"
)

// Default parameter values
const (
	DefaultPoolRecoveryPeriod = chain.BlocksPerDay // 14,400
)

// Default parameter values
var (
	DefaultBasePool = sdk.NewDecCoin(
		chain.SDRBaseDenom,
		chain.NativeBaseAmount(1_000_000),
	) // 1,000,000 SDR = 1,000,000,000,000,000,000,000,000 asdr
	DefaultMinStabilitySpread = math.LegacyNewDecWithPrec(2, 2) // 2%
)

// DefaultParams creates default market module parameters
func DefaultParams() Params {
	return Params{
		BasePool:           DefaultBasePool,
		PoolRecoveryPeriod: DefaultPoolRecoveryPeriod,
		MinStabilitySpread: DefaultMinStabilitySpread,
	}
}

// Validate validates the set of params
func (p Params) Validate() error {
	if p.BasePool.Amount.IsNil() {
		return errors.New("base pool amount must be set")
	}
	if err := p.BasePool.Validate(); err != nil {
		return fmt.Errorf("invalid base pool: %w", err)
	}
	if !p.BasePool.IsPositive() {
		return fmt.Errorf("base pool must be positive, is %s", p.BasePool)
	}
	if _, err := decimal.Mul(p.BasePool.Amount, p.BasePool.Amount); err != nil {
		return fmt.Errorf("base pool square must be representable: %w", err)
	}
	if p.PoolRecoveryPeriod == 0 {
		return fmt.Errorf("pool recovery period must be positive, is %d", p.PoolRecoveryPeriod)
	}
	if p.MinStabilitySpread.IsNil() {
		return errors.New("min stability spread must be set")
	}
	if p.MinStabilitySpread.IsNegative() || p.MinStabilitySpread.GT(math.LegacyOneDec()) {
		return fmt.Errorf("min stability spread must be in [0, 1], is %s", p.MinStabilitySpread)
	}
	return nil
}
