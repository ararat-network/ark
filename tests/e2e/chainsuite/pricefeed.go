package chainsuite

import (
	"context"
	"fmt"
	"strings"

	"github.com/cosmos/interchaintest/v10/chain/cosmos"
	"github.com/cosmos/interchaintest/v10/dockerutil"
	"github.com/cosmos/interchaintest/v10/ibc"
	"github.com/cosmos/interchaintest/v10/testutil"
	"go.uber.org/zap"
)

// PinnedPrices are the route legs a pinned sidecar reports besides the default config's USD/NOAH
// bootstrap leg, in USD per unit. XDR/USD is chain.BootstrapUSDPerXDR, so axdr stays at one NOAH.
var PinnedPrices = []struct{ Pair, Price string }{
	{"AUD/USD", "0.65"},
	{"CAD/USD", "0.73"},
	{"CNY/USD", "0.14"},
	{"EUR/USD", "1.08"},
	{"GBP/USD", "1.27"},
	{"JPY/USD", "0.0067"},
	{"KRW/USD", "0.00075"},
	{"MXN/USD", "0.055"},
	{"SGD/USD", "0.74"},
	{"XDR/USD", "1.371"},
}

const (
	// pinnedEndpoint never resolves: .invalid is reserved for that, so a pinned provider fails
	// fast and every leg falls back to its bootstrap price.
	pinnedEndpoint = "https://offline.invalid/"
	// pinnedUntil outlives any run, including the default USD/NOAH leg's own expiry.
	pinnedUntil    = "2100-01-01T00:00:00Z"
	bootstrapTable = "[[resolver.bootstrap_prices]]"
)

const (
	SidecarProcess    = "pricefeed"
	SidecarPort       = "8080"
	SidecarHome       = "/home/sidecar"
	SidecarConfigFile = "pricefeed.toml"
	nodeGRPCPort      = "9090"
)

// PriceFeedSidecar is one price-feed sidecar per validator, from the same
// image as the node, started before it. Container-to-container transport is
// plaintext, as on the Compose localnet; production terminates TLS.
func PriceFeedSidecar(image ibc.DockerImage) ibc.SidecarConfig {
	return ibc.SidecarConfig{
		ProcessName: SidecarProcess,
		Image:       image,
		HomeDir:     SidecarHome,
		Ports:       []string{SidecarPort + "/tcp"},
		StartCmd: []string{
			"pricefeed", "start",
			"--config", SidecarHome + "/" + SidecarConfigFile,
			"--address", "0.0.0.0:" + SidecarPort,
			"--tls-mode", "plaintext",
		},
		ValidatorProcess: true,
		PreStart:         true,
	}
}

// WirePriceFeed runs before genesis: it writes each sidecar's runtime config,
// pointed at its own validator's gRPC, and points the validator's app.toml at
// the sidecar. Neither address is known until interchaintest has named the
// containers, which is why this is a PreGenesis hook rather than config.
func WirePriceFeed(ctx context.Context, log *zap.Logger, chain *cosmos.CosmosChain) error {
	live := GetEnvironment().PriceFeedLive
	for _, val := range chain.Validators {
		sidecar := priceFeedSidecar(val)
		if sidecar == nil {
			return fmt.Errorf("validator %s has no %s sidecar", val.Name(), SidecarProcess)
		}
		cfg, err := sidecarConfig(ctx, val)
		if err != nil {
			return err
		}
		if !live {
			if cfg, err = pinPrices(cfg); err != nil {
				return err
			}
		}
		fw := dockerutil.NewFileWriter(log, val.DockerClient, val.TestName)
		if err := fw.WriteFile(ctx, sidecar.VolumeName, SidecarConfigFile, cfg); err != nil {
			return fmt.Errorf("writing %s for %s: %w", SidecarConfigFile, val.Name(), err)
		}
		if err := testutil.ModifyTomlConfigFile(
			ctx, log, val.DockerClient, val.TestName, val.VolumeName, "config/app.toml",
			testutil.Toml{
				"pricefeed": testutil.Toml{
					"enabled":           true,
					"sidecar_addresses": []string{sidecar.HostName() + ":" + SidecarPort},
					"tls":               testutil.Toml{"mode": "plaintext"},
				},
			},
		); err != nil {
			return fmt.Errorf("pointing %s at its sidecar: %w", val.Name(), err)
		}
	}
	return nil
}

func priceFeedSidecar(val *cosmos.ChainNode) *cosmos.SidecarProcess {
	for _, s := range val.Sidecars {
		if s.ProcessName == SidecarProcess {
			return s
		}
	}
	return nil
}

// sidecarConfig is the sidecar's own default config, as `pricefeed init`
// writes it, with the client section pointed at the validator: the same
// rewrite contrib/localnet/init.sh applies with sed.
func sidecarConfig(ctx context.Context, val *cosmos.ChainNode) ([]byte, error) {
	stdout, stderr, err := val.Exec(ctx, []string{
		"sh", "-c", "pricefeed init --config /tmp/" + SidecarConfigFile + " >/dev/null && cat /tmp/" + SidecarConfigFile,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("pricefeed init: %w: %s", err, stderr)
	}
	return rewriteClient(stdout, val.HostName()+":"+nodeGRPCPort), nil
}

func rewriteClient(cfg []byte, address string) []byte {
	lines := strings.Split(string(cfg), "\n")
	section := ""
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			section = trimmed
			continue
		}
		switch {
		case section == "[client]" && strings.HasPrefix(trimmed, "addresses ="):
			lines[i] = fmt.Sprintf("addresses = [%q]", address)
		case section == "[client.tls]" && strings.HasPrefix(trimmed, "mode ="):
			lines[i] = `mode = "plaintext"`
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// pinPrices takes the sidecar off the internet: every provider endpoint points nowhere, every
// bootstrap leg outlives the run, and the legs in PinnedPrices join the default USD/NOAH one.
func pinPrices(cfg []byte) ([]byte, error) {
	lines := strings.Split(string(cfg), "\n")
	out := make([]string, 0, len(lines)+4*len(PinnedPrices))
	section, pinned := "", false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			section = trimmed
			if section == bootstrapTable && !pinned {
				for _, leg := range PinnedPrices {
					out = append(out, bootstrapTable,
						fmt.Sprintf("pair = %q", leg.Pair),
						fmt.Sprintf("price = %q", leg.Price),
						fmt.Sprintf("valid_until = %q", pinnedUntil),
						"")
				}
				pinned = true
			}
		}
		switch {
		case strings.HasPrefix(section, "[[providers.") && strings.HasSuffix(section, ".endpoints]]") &&
			strings.HasPrefix(trimmed, "url ="):
			line = fmt.Sprintf("url = %q", pinnedEndpoint)
		case section == bootstrapTable && strings.HasPrefix(trimmed, "valid_until ="):
			line = fmt.Sprintf("valid_until = %q", pinnedUntil)
		}
		out = append(out, line)
	}
	if !pinned {
		return nil, fmt.Errorf("pricefeed config has no %s table to pin", bootstrapTable)
	}
	return []byte(strings.Join(out, "\n")), nil
}
