package validation

import (
	"errors"
	"math"
	"time"
)

const (
	DefaultNumChecks                    = 1000
	DefaultRequiredPriceLivenessPercent = 99.0
	DefaultValidationPeriod             = 10 * time.Minute
	DefaultBurnInPeriod                 = time.Minute
	DefaultMaxResponseAge               = time.Minute
	DefaultMaxFutureSkew                = 5 * time.Second
	DefaultRequestTimeout               = 5 * time.Second
	DefaultDenomRefreshInterval         = 5 * time.Second
)

// Config configures a validation run against the public oracle price API.
type Config struct {
	// BurnInPeriod is how long to let the oracle run before validation checks begin.
	BurnInPeriod time.Duration
	// ValidationPeriod is how long to sample the price API.
	ValidationPeriod time.Duration
	// NumChecks is the number of price API checks to run over ValidationPeriod.
	NumChecks int
	// RequiredPriceLivenessPercent is the minimum successful response percentage per denom.
	RequiredPriceLivenessPercent float64
	// MaxResponseAge is the maximum accepted age of a price API response timestamp.
	MaxResponseAge time.Duration
	// MaxFutureSkew is the maximum amount a response timestamp may be ahead of the local clock.
	MaxFutureSkew time.Duration
	// RequestTimeout bounds each individual price API check.
	RequestTimeout time.Duration
	// DenomRefreshInterval is how often to refresh active denoms from the chain.
	DenomRefreshInterval time.Duration
}

// DefaultConfig returns a validation config with default timings and thresholds.
func DefaultConfig() Config {
	return Config{
		BurnInPeriod:                 DefaultBurnInPeriod,
		ValidationPeriod:             DefaultValidationPeriod,
		NumChecks:                    DefaultNumChecks,
		RequiredPriceLivenessPercent: DefaultRequiredPriceLivenessPercent,
		MaxResponseAge:               DefaultMaxResponseAge,
		MaxFutureSkew:                DefaultMaxFutureSkew,
		RequestTimeout:               DefaultRequestTimeout,
		DenomRefreshInterval:         DefaultDenomRefreshInterval,
	}
}

// Validate checks whether the validation config can be used.
func (c Config) Validate() error {
	if c.BurnInPeriod < 0 {
		return errors.New("burn in period cannot be negative")
	}
	if c.ValidationPeriod <= 0 {
		return errors.New("validation period must be greater than zero")
	}
	if c.NumChecks <= 0 {
		return errors.New("num checks must be greater than zero")
	}
	if c.ValidationPeriod/time.Duration(c.NumChecks) <= 0 {
		return errors.New("validation period must be long enough for num checks")
	}
	if math.IsNaN(c.RequiredPriceLivenessPercent) ||
		c.RequiredPriceLivenessPercent <= 0 ||
		c.RequiredPriceLivenessPercent > 100 {
		return errors.New("required price liveness percent must be between 0 and 100")
	}
	if c.MaxResponseAge <= 0 {
		return errors.New("max response age must be greater than zero")
	}
	if c.MaxFutureSkew < 0 {
		return errors.New("max future skew cannot be negative")
	}
	if c.RequestTimeout <= 0 {
		return errors.New("request timeout must be greater than zero")
	}
	if c.DenomRefreshInterval <= 0 {
		return errors.New("denom refresh interval must be greater than zero")
	}

	return nil
}
