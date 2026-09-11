// SPDX-License-Identifier: Apache-2.0
// Adapted from Gaia, tests/interchain/chainsuite/relayer.go.
// Modified for Ark: relayer setup and configuration.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package chainsuite

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/ibc"
	"github.com/cosmos/interchaintest/v10/relayer"
)

// Relayer is Hermes, the relayer validators and integrators run against ark.
type Relayer struct {
	ibc.Relayer
}

func NewRelayer(ctx context.Context, testName interchaintest.TestName) (*Relayer, error) {
	dockerClient, dockerNetwork, err := GetDockerContext(ctx)
	if err != nil {
		return nil, err
	}
	rly := interchaintest.NewBuiltinRelayerFactory(
		ibc.Hermes,
		GetLogger(ctx),
		relayer.CustomDockerImage(HermesRepository, HermesVersion, HermesUIDGID),
	).Build(testName, dockerClient, dockerNetwork)
	return &Relayer{Relayer: rly}, nil
}

// SetupChainKeys tells Hermes about chain and restores its funded wallet.
// Hermes prices its own transactions, so it gets RelayerGasPrices where the
// chain's CLI configuration carries none.
func (r *Relayer) SetupChainKeys(ctx context.Context, chain *Chain) error {
	rep := GetRelayerExecReporter(ctx)
	cfg := chain.Config()
	cfg.GasPrices = RelayerGasPrices
	rpcAddr, grpcAddr := chain.GetRPCAddress(), chain.GetGRPCAddress()
	if !r.UseDockerNetwork() {
		rpcAddr, grpcAddr = chain.GetHostRPCAddress(), chain.GetHostGRPCAddress()
	}
	chainID := cfg.ChainID
	if err := r.AddChainConfiguration(ctx, rep, cfg, chainID, rpcAddr, grpcAddr); err != nil {
		return err
	}
	return r.RestoreKey(ctx, rep, cfg, chainID, chain.RelayerWallet.Mnemonic())
}

// GetTransferChannel is the open transfer channel from chain to counterparty.
func (r *Relayer) GetTransferChannel(ctx context.Context, chain, counterparty *Chain) (*ibc.ChannelOutput, error) {
	return r.GetChannelWithPort(ctx, chain, counterparty, TransferPortID)
}

func (r *Relayer) GetChannelWithPort(ctx context.Context, chain, counterparty *Chain, portID string) (*ibc.ChannelOutput, error) {
	// The chain's own view: Hermes reports channel states in its own words.
	stdout, _, err := chain.GetNode().ExecQuery(ctx, "ibc", "channel", "channels")
	if err != nil {
		return nil, err
	}
	var channels struct {
		Channels []ibc.ChannelOutput `json:"channels"`
	}
	if err := json.Unmarshal(stdout, &channels); err != nil {
		return nil, fmt.Errorf("decoding channels %s: %w", stdout, err)
	}
	for _, channel := range channels.Channels {
		if channel.PortID == portID && channel.State == "STATE_OPEN" {
			return &channel, nil
		}
	}
	return nil, fmt.Errorf("no open channel on port %s from %s to %s", portID, chain.Config().ChainID, counterparty.Config().ChainID)
}

// Flush relays whatever is pending on the transfer path.
func (r *Relayer) Flush(ctx context.Context, pathName, channelID string) error {
	return r.Relayer.Flush(ctx, GetRelayerExecReporter(ctx), pathName, channelID)
}
