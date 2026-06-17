package aggregator

import (
	"fmt"
)

// OracleKeeperError is returned when oracle keeper state access or mutation fails.
type OracleKeeperError struct {
	Err error
}

func (e OracleKeeperError) Error() string {
	return fmt.Sprintf("oracle keeper error: %s", e.Err.Error())
}

func (e OracleKeeperError) Unwrap() error {
	return e.Err
}

func (e OracleKeeperError) Label() string {
	return "OracleKeeperError"
}

// PriceAggregationError is an error that is returned when there is a failure in aggregating the prices.
type PriceAggregationError struct {
	Err error
}

func (e PriceAggregationError) Error() string {
	return fmt.Sprintf("price aggregation error: %s", e.Err.Error())
}

func (e PriceAggregationError) Unwrap() error {
	return e.Err
}

func (e PriceAggregationError) Label() string {
	return "PriceAggregationError"
}
