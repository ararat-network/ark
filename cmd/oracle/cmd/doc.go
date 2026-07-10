// Package cmd defines the Cobra command tree for the standalone oracle process.
//
// It owns process concerns such as config path selection, signal handling,
// logging, telemetry endpoints, and short-lived administrative and inspection
// clients. Reusable oracle behavior remains under ark/oracle.
package cmd
