package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"

	"ark/abci/codec"
	abcioracle "ark/abci/oracle"
	oracleencoding "ark/abci/oracle/encoding"
)

type voteExtensionsOutput struct {
	SourceBlockHeight int64                 `json:"source_block_height"`
	VoteHeight        int64                 `json:"vote_height"`
	Round             int32                 `json:"round"`
	Votes             []voteExtensionOutput `json:"votes"`
}

type voteExtensionOutput struct {
	ValidatorAddress string            `json:"validator_address"`
	ValidatorPower   int64             `json:"validator_power"`
	BlockIDFlag      string            `json:"block_id_flag"`
	Rates            map[string]string `json:"rates"`
}

func voteExtensionsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vote-extensions [block-height]",
		Short: "Inspect oracle vote extensions retained in a committed block",
		Long: `Inspect the oracle vote extensions retained in transaction zero of a
committed Ark block. Block H contains the extended commit for vote height H-1.
If no block height is provided, the latest committed block is queried.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runVoteExtensions,
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func runVoteExtensions(cmd *cobra.Command, args []string) error {
	clientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return err
	}

	height, err := voteExtensionsBlockHeight(cmd, args, clientCtx.Height)
	if err != nil {
		return err
	}

	node, err := clientCtx.GetNode()
	if err != nil {
		return err
	}

	result, err := node.Block(cmd.Context(), height)
	if err != nil {
		return fmt.Errorf("query committed block: %w", err)
	}
	if result == nil || result.Block == nil {
		return fmt.Errorf("query committed block: node returned an empty block response")
	}

	output, err := inspectVoteExtensions(result.Block)
	if err != nil {
		return err
	}

	bz, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("marshal vote extensions output: %w", err)
	}

	return clientCtx.PrintRaw(bz)
}

func voteExtensionsBlockHeight(cmd *cobra.Command, args []string, queryHeight int64) (*int64, error) {
	if len(args) == 1 && cmd.Flags().Changed(flags.FlagHeight) {
		return nil, fmt.Errorf("block height may be provided either as an argument or with --%s, not both", flags.FlagHeight)
	}

	height := queryHeight
	if len(args) == 1 {
		parsed, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse block height %q: %w", args[0], err)
		}
		if parsed <= 0 {
			return nil, fmt.Errorf("block height must be positive")
		}
		height = parsed
	}

	if height < 0 {
		return nil, fmt.Errorf("block height must be positive")
	}
	if height == 0 {
		return nil, nil
	}

	return &height, nil
}

func inspectVoteExtensions(block *cmttypes.Block) (voteExtensionsOutput, error) {
	if block.Height <= 0 {
		return voteExtensionsOutput{}, fmt.Errorf("source block height must be positive")
	}
	if len(block.Txs) == 0 {
		return voteExtensionsOutput{}, fmt.Errorf("source block %d contains no vote-extension metadata", block.Height)
	}

	extendedCommit, err := codec.NewExtendedCommitCodec().Decode(block.Txs[0])
	if err != nil {
		return voteExtensionsOutput{}, fmt.Errorf(
			"decode vote-extension metadata from source block %d: %w",
			block.Height,
			err,
		)
	}

	output := voteExtensionsOutput{
		SourceBlockHeight: block.Height,
		VoteHeight:        block.Height - 1,
		Round:             extendedCommit.Round,
		Votes:             make([]voteExtensionOutput, len(extendedCommit.Votes)),
	}
	voteExtensionCodec := codec.NewVoteExtensionCodec()

	for i, vote := range extendedCommit.Votes {
		voteExtension, err := voteExtensionCodec.Decode(vote.VoteExtension)
		if err != nil {
			return voteExtensionsOutput{}, fmt.Errorf(
				"decode vote extension for validator %X: %w",
				vote.Validator.Address,
				err,
			)
		}
		if err := abcioracle.ValidateVoteExtension(voteExtension); err != nil {
			return voteExtensionsOutput{}, fmt.Errorf(
				"validate vote extension for validator %X: %w",
				vote.Validator.Address,
				err,
			)
		}

		rates := make(map[string]string, len(voteExtension.Rates))
		for denom, rawRate := range voteExtension.Rates {
			rate, err := oracleencoding.DecodeRate(rawRate)
			if err != nil {
				return voteExtensionsOutput{}, fmt.Errorf(
					"decode rate %s for validator %X: %w",
					denom,
					vote.Validator.Address,
					err,
				)
			}
			rates[denom] = rate.String()
		}

		output.Votes[i] = voteExtensionOutput{
			ValidatorAddress: fmt.Sprintf("%X", vote.Validator.Address),
			ValidatorPower:   vote.Validator.Power,
			BlockIDFlag:      vote.BlockIdFlag.String(),
			Rates:            rates,
		}
	}

	return output, nil
}
