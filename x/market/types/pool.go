package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	"ark/pkg/decimal"
)

// EffectivePools contains the derived constant-product pools for one market
// state snapshot.
type EffectivePools struct {
	ConstantProduct math.LegacyDec
	ArkPool         math.LegacyDec
	NoahPool        math.LegacyDec
}

// NewEffectivePools validates and constructs all derived pool values from the
// configured base pool and current Ark pool delta.
func NewEffectivePools(basePool, arkPoolDelta math.LegacyDec) (EffectivePools, error) {
	if basePool.IsNil() {
		return EffectivePools{}, errors.New("base pool must be set")
	}
	if !basePool.IsInValidRange() {
		return EffectivePools{}, errors.New("base pool is out of range")
	}
	if !basePool.IsPositive() {
		return EffectivePools{}, fmt.Errorf("base pool must be positive, is %s", basePool)
	}
	if arkPoolDelta.IsNil() {
		return EffectivePools{}, errors.New("ark pool delta must be set")
	}
	if !arkPoolDelta.IsInValidRange() {
		return EffectivePools{}, errors.New("ark pool delta is out of range")
	}

	constantProduct, err := decimal.Mul(basePool, basePool)
	if err != nil {
		return EffectivePools{}, fmt.Errorf("constant product for base pool %s is not representable: %w", basePool, err)
	}
	arkPool, err := decimal.Add(basePool, arkPoolDelta)
	if err != nil {
		return EffectivePools{}, fmt.Errorf(
			"effective ark pool for base pool %s and delta %s is not representable: %w",
			basePool,
			arkPoolDelta,
			err,
		)
	}
	if !arkPool.IsPositive() {
		return EffectivePools{}, fmt.Errorf("effective ark pool must be positive: base pool %s, delta %s", basePool, arkPoolDelta)
	}

	noahPool, err := decimal.Quo(constantProduct, arkPool)
	if err != nil {
		return EffectivePools{}, fmt.Errorf(
			"effective noah pool for constant product %s and ark pool %s is not representable: %w",
			constantProduct,
			arkPool,
			err,
		)
	}
	if !noahPool.IsPositive() {
		return EffectivePools{}, fmt.Errorf(
			"effective noah pool must be positive: constant product %s, ark pool %s",
			constantProduct,
			arkPool,
		)
	}

	return EffectivePools{
		ConstantProduct: constantProduct,
		ArkPool:         arkPool,
		NoahPool:        noahPool,
	}, nil
}
