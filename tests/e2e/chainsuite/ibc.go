package chainsuite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/chain/cosmos"
	"github.com/cosmos/interchaintest/v10/ibc"
	"github.com/cosmos/interchaintest/v10/testutil"

	sdkmath "cosmossdk.io/math"
)

// TransferPath names the relayer path between two linked chains.
const TransferPath = "transfer"

// CreateLinkedChains starts two chains and Hermes. beforeLink runs once both
// chains produce blocks and before the relayer creates clients: the launch
// genesis admits no client type, so the hub has to be opened first. The
// relayer is running over a transfer channel when this returns.
func CreateLinkedChains(
	ctx context.Context,
	testName interchaintest.TestName,
	specA, specB *interchaintest.ChainSpec,
	beforeLink func(a, b *Chain) error,
) (*Chain, *Chain, *Relayer, error) {
	cosmosA, cosmosB := NewCosmosChain(ctx, testName, specA), NewCosmosChain(ctx, testName, specB)

	// The relayer is configured by hand rather than through an interchain
	// link: it needs a numeric gas price the CLI transactions must not carry.
	ic := interchaintest.NewInterchain()
	wallets := make([]ibc.Wallet, 2)
	for i, c := range []*cosmos.CosmosChain{cosmosA, cosmosB} {
		wallet, err := c.BuildRelayerWallet(ctx, "relayer-"+c.Config().ChainID)
		if err != nil {
			return nil, nil, nil, err
		}
		wallets[i] = wallet
		ic.AddChain(c, ibc.WalletAmount{
			Address: wallet.FormattedAddress(),
			Denom:   c.Config().Denom,
			Amount:  NOAH(ValidatorFunds),
		})
	}
	dockerClient, dockerNetwork, err := GetDockerContext(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	rep := GetRelayerExecReporter(ctx)
	if err := ic.Build(ctx, rep, interchaintest.InterchainBuildOptions{
		Client:    dockerClient,
		NetworkID: dockerNetwork,
		TestName:  testName.Name(),
	}); err != nil {
		return nil, nil, nil, err
	}

	chainA, err := chainFromCosmosChain(cosmosA, wallets[0])
	if err != nil {
		return nil, nil, nil, err
	}
	chainB, err := chainFromCosmosChain(cosmosB, wallets[1])
	if err != nil {
		return nil, nil, nil, err
	}
	if beforeLink != nil {
		if err := beforeLink(chainA, chainB); err != nil {
			return nil, nil, nil, err
		}
	}

	rly, err := NewRelayer(ctx, testName)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, c := range []*Chain{chainA, chainB} {
		if err := rly.SetupChainKeys(ctx, c); err != nil {
			return nil, nil, nil, err
		}
	}
	if err := rly.GeneratePath(ctx, rep, cosmosA.Config().ChainID, cosmosB.Config().ChainID, TransferPath); err != nil {
		return nil, nil, nil, err
	}
	// interchaintest pins Hermes's config to 14days; pass the period per client.
	if err := rly.LinkPath(ctx, rep, TransferPath, ibc.CreateChannelOptions{
		SourcePortName: TransferPortID,
		DestPortName:   TransferPortID,
		Order:          ibc.Unordered,
	}, ibc.CreateClientOptions{TrustingPeriod: TrustingPeriod}); err != nil {
		return nil, nil, nil, err
	}
	if err := rly.StartRelayer(ctx, rep, TransferPath); err != nil {
		return nil, nil, nil, err
	}
	return chainA, chainB, rly, nil
}

// OpenHubMessages is the governance act the launch genesis waits for: it
// admits the tendermint and wasm client types, turns ICS-20 both ways, and
// enables both interchain-account roles with the host allowing a bank send.
func OpenHubMessages(gov string) []json.RawMessage {
	quote := func(format string, args ...any) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(format, args...))
	}
	return []json.RawMessage{
		quote(`{"@type":"/ibc.core.client.v1.MsgUpdateParams","signer":%q,"params":{"allowed_clients":["07-tendermint","08-wasm"]}}`, gov),
		quote(`{"@type":"/ibc.applications.transfer.v1.MsgUpdateParams","signer":%q,"params":{"send_enabled":true,"receive_enabled":true}}`, gov),
		quote(`{"@type":"/ibc.applications.interchain_accounts.controller.v1.MsgUpdateParams","signer":%q,"params":{"controller_enabled":true}}`, gov),
		quote(`{"@type":"/ibc.applications.interchain_accounts.host.v1.MsgUpdateParams","signer":%q,"params":{"host_enabled":true,"allow_messages":["/cosmos.bank.v1beta1.MsgSend"]}}`, gov),
	}
}

// OpenHub runs OpenHubMessages through governance on chain.
func OpenHub(ctx context.Context, chain *Chain) error {
	gov, err := chain.GovAuthority(ctx)
	if err != nil {
		return err
	}
	_, err = chain.SubmitAndPassProposal(ctx, FaucetKeyName, "open the hub", OpenHubMessages(gov)...)
	return err
}

// IBCDenom is the voucher denomination a transfer of baseDenom arrives as
// over the destination's channelID.
func IBCDenom(channelID, baseDenom string) string {
	sum := sha256.Sum256([]byte(TransferPortID + "/" + channelID + "/" + baseDenom))
	return "ibc/" + strings.ToUpper(hex.EncodeToString(sum[:]))
}

// WaitForBalance polls until address holds want of denom on chain.
func WaitForBalance(ctx context.Context, chain *Chain, address, denom string, want sdkmath.Int, timeout time.Duration) error {
	var got sdkmath.Int
	err := testutil.WaitForCondition(timeout, BlockTime, func() (bool, error) {
		balance, err := chain.GetBalance(ctx, address, denom)
		if err != nil {
			return false, err
		}
		got = balance
		return balance.Equal(want), nil
	})
	if err != nil {
		return fmt.Errorf("%s balance of %s: want %s, got %s: %w", denom, address, want, got, err)
	}
	return nil
}

// Transfer sends amount of denom from keyName on src to address on dst and
// waits for the voucher to land. It returns the denomination it arrived as.
func Transfer(ctx context.Context, rly *Relayer, src, dst *Chain, keyName, address, denom string, amount sdkmath.Int) (string, error) {
	channel, err := rly.GetTransferChannel(ctx, src, dst)
	if err != nil {
		return "", err
	}
	// A voucher going home unwraps to its base denomination.
	arrivesAs := Denom
	if !strings.HasPrefix(denom, "ibc/") {
		arrivesAs = IBCDenom(channel.Counterparty.ChannelID, denom)
	}
	before, err := dst.GetBalance(ctx, address, arrivesAs)
	if err != nil {
		return "", err
	}
	if _, err := src.GetNode().SendIBCTransfer(ctx, channel.ChannelID, keyName, ibc.WalletAmount{
		Address: address,
		Denom:   denom,
		Amount:  amount,
	}, ibc.TransferOptions{}); err != nil {
		return "", err
	}
	if err := WaitForBalance(ctx, dst, address, arrivesAs, before.Add(amount), 120*time.Second); err != nil {
		return "", err
	}
	return arrivesAs, nil
}
