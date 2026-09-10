// Package cmd defines the Cobra command tree for the price-feed sidecar process.
//
// It owns process concerns such as config path selection, logging, telemetry
// endpoints, and short-lived administrative and inspection clients. The
// sidecar itself is pricefeed/sidecar, its config file pricefeed/config, and
// the liveness check pricefeed/validation.
package cmd
