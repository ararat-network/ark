package cmd

import (
	"github.com/spf13/pflag"

	"github.com/ararat-network/ark/pkg/tlsconfig"
)

const (
	flagTLSMode         = "tls-mode"
	flagTLSCAFile       = "tls-ca-file"
	flagTLSCertFile     = "tls-cert-file"
	flagTLSKeyFile      = "tls-key-file"
	flagTLSServerName   = "tls-server-name"
	flagTLSClientCAFile = "tls-client-ca-file"

	// chainFlagPrefix distinguishes the chain connection's TLS flags on a
	// command that also dials the sidecar.
	chainFlagPrefix = "chain-"
)

// addClientTLSFlags registers the files a command dials peer with, under
// prefix so a command with two peers can name both.
func addClientTLSFlags(flags *pflag.FlagSet, prefix, peer string, files *tlsconfig.Client) {
	flags.StringVar(
		&files.Mode,
		prefix+flagTLSMode,
		tlsconfig.Local,
		"Transport to the "+peer+": local permits loopback only, tls verifies its certificate, plaintext dials it unencrypted.",
	)
	flags.StringVar(
		&files.CAFile,
		prefix+flagTLSCAFile,
		files.CAFile,
		"CA bundle the "+peer+" certificate must chain to; empty uses system roots in TLS mode.",
	)
	flags.StringVar(
		&files.CertFile,
		prefix+flagTLSCertFile,
		files.CertFile,
		"Client certificate presented to the "+peer+".",
	)
	flags.StringVar(
		&files.KeyFile,
		prefix+flagTLSKeyFile,
		files.KeyFile,
		"Key of the client certificate presented to the "+peer+".",
	)
	flags.StringVar(
		&files.ServerName,
		prefix+flagTLSServerName,
		files.ServerName,
		"Name the "+peer+" certificate is verified against when it carries neither the dialled host nor its IP.",
	)
}

// addServerTLSFlags registers the files the public listener serves with.
func addServerTLSFlags(flags *pflag.FlagSet, files *tlsconfig.Server) {
	flags.StringVar(
		&files.Mode,
		flagTLSMode,
		tlsconfig.Local,
		"Public listener transport: local binds loopback only, tls serves the certificate below, plaintext serves unencrypted off loopback.",
	)
	flags.StringVar(
		&files.CertFile,
		flagTLSCertFile,
		files.CertFile,
		"Certificate the public listener serves with; required in TLS mode.",
	)
	flags.StringVar(&files.KeyFile, flagTLSKeyFile, files.KeyFile, "Key of the public listener's certificate.")
	flags.StringVar(
		&files.ClientCAFile,
		flagTLSClientCAFile,
		files.ClientCAFile,
		"CA bundle every client certificate must chain to; empty accepts any client.",
	)
}
