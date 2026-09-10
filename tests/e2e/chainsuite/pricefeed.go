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
	for _, val := range chain.Validators {
		sidecar := priceFeedSidecar(val)
		if sidecar == nil {
			return fmt.Errorf("validator %s has no %s sidecar", val.Name(), SidecarProcess)
		}
		cfg, err := sidecarConfig(ctx, val)
		if err != nil {
			return err
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
