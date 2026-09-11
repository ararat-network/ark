// SPDX-License-Identifier: Apache-2.0
// Adapted from Cosmos SDK, server/start.go (InPlaceTestnetCreator and testnetify).
// Modified for Ark: the app applies its own state change, the extended commit
// vote extensions restart from is rewritten, the commit is signed with the key
// directly, and the signing state is realigned.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/gogoproto/proto"
	"github.com/spf13/cobra"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtcfg "github.com/cometbft/cometbft/config"
	cmtcrypto "github.com/cometbft/cometbft/crypto"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/privval"
	cmtstate "github.com/cometbft/cometbft/proto/tendermint/state"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
	cmttypes "github.com/cometbft/cometbft/types"
	cmttime "github.com/cometbft/cometbft/types/time"

	"cosmossdk.io/core/address"
	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/ararat-network/ark/app"
)

const (
	flagTriggerTestnetUpgrade = "trigger-testnet-upgrade"
	flagSkipConfirmation      = "skip-confirmation"
)

// inPlaceTestnetArgs is what the app creator needs from the command line.
// It travels through startRun: the SDK's AppCreator carries app options only.
type inPlaceTestnetArgs struct {
	chainID  string
	operator sdk.AccAddress
	upgrade  string
}

// newInPlaceTestnetCmd is the start command running as in-place-testnet:
// start's checks and hooks, then an app creator that rewrites consensus and
// application state before the node starts.
func newInPlaceTestnetCmd(run *startRun, addressCodec address.Codec) *cobra.Command {
	cmd := server.StartCmdWithOptions(run.createTestnetApp, app.DefaultNodeHome, server.StartCmdOptions{
		PostSetup:           run.postSetup,
		PostSetupStandalone: run.postSetup,
	})
	cmd.Use = "in-place-testnet [new-chain-id] [operator-address]"
	cmd.Short = "Turn this node's state into a testnet it controls, and start it"
	cmd.Long = `Turn this node's committed state into a testnet under new-chain-id that this
node's validator key controls, and start it. Every validator is removed from
the application state and one is bonded at operator-address with the node's
consensus key and a seat's grant, minted for it. CometBFT's validator set, last
commit, and extended commit are rewritten to match, so the first block is this
node's alone. The signing state is realigned when it is ahead of the last
block. Once stopped, the node starts again with the plain start command.

Run this on a copy of a node's home, never a validator's own: the data folder
is rewritten and cannot be restored. The first block may take a while if much
pending state falls due. A dead price-feed client is tolerated; the node then
abstains from oracle votes.`
	cmd.Example = "  arkd in-place-testnet ark-fork-1 ark1... --home /tmp/fork --skip-confirmation"
	cmd.Args = cobra.ExactArgs(2)
	startRunE := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		operator, err := addressCodec.StringToBytes(args[1])
		if err != nil {
			return fmt.Errorf("parse operator address %q: %w", args[1], err)
		}
		if skip, _ := cmd.Flags().GetBool(flagSkipConfirmation); !skip {
			if err := confirmInPlaceTestnet(cmd); err != nil {
				return err
			}
		}
		upgrade, _ := cmd.Flags().GetString(flagTriggerTestnetUpgrade)
		run.testnet = &inPlaceTestnetArgs{chainID: args[0], operator: operator, upgrade: upgrade}
		// The app takes its chain ID from genesis when it is built, before
		// the creator rewrites the rest.
		if err := renameGenesis(run.svrCtx.Config.GenesisFile(), args[0]); err != nil {
			return err
		}
		return startRunE(cmd, args)
	}
	run.wrapPreRun(cmd)
	cmd.Flags().String(flagTriggerTestnetUpgrade, "", "Upgrade handler to run in the first block, e.g. \"v2\"")
	cmd.Flags().Bool(flagSkipConfirmation, false, "Skip the confirmation prompt")
	return cmd
}

// confirmInPlaceTestnet is the prompt before the data folder is rewritten.
func confirmInPlaceTestnet(cmd *cobra.Command) error {
	cmd.Println("This rewrites the data folder for a new chain and cannot be undone. Continue? (y/n)")
	text, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	switch strings.TrimSpace(strings.ToLower(text)) {
	case "y", "yes":
		return nil
	}
	return errors.New("cancelled")
}

// renameGenesis puts chainID on the genesis file.
func renameGenesis(path, chainID string) error {
	genesis, err := genutiltypes.AppGenesisFromFile(path)
	if err != nil {
		return err
	}
	genesis.ChainID = chainID
	if err := genesis.ValidateAndComplete(); err != nil {
		return err
	}
	return genesis.SaveAs(path)
}

// createTestnetApp is createApp followed by the rewrite of application and
// consensus state. The AppCreator signature returns no error, so a rewrite
// that fails stops the process here, before the node touches the state.
func (r *startRun) createTestnetApp(logger log.Logger, db dbm.DB, appOpts servertypes.AppOptions) servertypes.Application {
	arkApp := r.createApp(logger, db, appOpts).(*app.ArkApp)
	if err := r.testnetify(logger, arkApp); err != nil {
		panic(fmt.Errorf("in-place testnet: %w", err))
	}
	return arkApp
}

// testnetify hands the node's validator key to the app's state change, then
// aligns CometBFT's state with the validator it installed.
func (r *startRun) testnetify(logger log.Logger, arkApp *app.ArkApp) error {
	config := r.svrCtx.Config
	pv := privval.LoadOrGenFilePV(config.PrivValidatorKeyFile(), config.PrivValidatorStateFile())
	power, err := app.InitArkAppForTestnet(arkApp, pv.Key.PubKey, r.testnet.operator, r.testnet.upgrade)
	if err != nil {
		return err
	}
	return rewriteConsensusState(logger, config, arkApp, pv, r.testnet.chainID, power)
}

// rewriteConsensusState makes CometBFT's stores agree with the app: the
// validator set is pv alone at power, the last block's commit and extended
// commit carry pv's signatures under chainID, and the block store ends where
// the app does. The node starts from these as if it had produced them.
func rewriteConsensusState(logger log.Logger, config *cmtcfg.Config, arkApp *app.ArkApp, pv *privval.FilePV, chainID string, power int64) error {
	addrBookPath := filepath.Join(config.RootDir, "config", "addrbook.json")
	if err := os.WriteFile(addrBookPath, []byte("{}"), 0o600); err != nil {
		return fmt.Errorf("resetting the address book: %w", err)
	}

	blockStoreDB, err := cmtcfg.DefaultDBProvider(&cmtcfg.DBContext{ID: "blockstore", Config: config})
	if err != nil {
		return err
	}
	defer blockStoreDB.Close()
	blockStore := store.NewBlockStore(blockStoreDB)
	stateDB, err := cmtcfg.DefaultDBProvider(&cmtcfg.DBContext{ID: "state", Config: config})
	if err != nil {
		return err
	}
	defer stateDB.Close()
	stateStore := sm.NewStore(stateDB, sm.StoreOptions{DiscardABCIResponses: config.Storage.DiscardABCIResponses})
	state, genDoc, err := node.LoadStateFromDBOrGenesisDocProvider(stateDB, node.DefaultGenesisDocProviderFunc(config))
	if err != nil {
		return err
	}

	// The block store ends where the app does.
	info, err := arkApp.Info(&cmtabci.RequestInfo{})
	if err != nil {
		return err
	}
	switch {
	case info.LastBlockHeight == blockStore.Height():
		if state.LastBlockHeight != info.LastBlockHeight {
			state.LastBlockHeight = info.LastBlockHeight
			state.AppHash = info.LastBlockAppHash
		} else if err := blockStoreDB.Delete(fmt.Appendf(nil, "SC:%v", blockStore.Height()+1)); err != nil {
			return err
		}
	case blockStore.Height() > state.LastBlockHeight:
		if err := blockStore.DeleteLatestBlock(); err != nil {
			return err
		}
	}
	state.ChainID = chainID

	// The last commit, from the one validator.
	validatorAddress := pv.Key.PubKey.Address()
	commit, extendedCommit, err := forgeCommit(pv.Key.PrivKey, chainID, state.LastBlockHeight, state.LastBlockID, cmttime.Now())
	if err != nil {
		return err
	}
	if err := blockStore.SaveSeenCommit(state.LastBlockHeight, commit); err != nil {
		return err
	}
	if state.ConsensusParams.ABCI.VoteExtensionsEnabled(state.LastBlockHeight) {
		// With vote extensions on, the node rebuilds its last commit from the
		// extended commit, which the SDK's own rewrite leaves to the old set.
		bz, err := proto.Marshal(extendedCommit.ToProto())
		if err != nil {
			return err
		}
		if err := blockStoreDB.Set(fmt.Appendf(nil, "EC:%v", state.LastBlockHeight), bz); err != nil {
			return err
		}
	}

	newVal := &cmttypes.Validator{Address: validatorAddress, PubKey: pv.Key.PubKey, VotingPower: power}
	newValSet := cmttypes.NewValidatorSet([]*cmttypes.Validator{newVal})
	state.Validators = newValSet
	state.LastValidators = newValSet
	state.NextValidators = newValSet
	state.LastHeightValidatorsChanged = blockStore.Height()
	if err := stateStore.Save(state); err != nil {
		return err
	}
	valSet, err := newValSet.ToProto()
	if err != nil {
		return err
	}
	valInfo, err := (&cmtstate.ValidatorsInfo{ValidatorSet: valSet, LastHeightChanged: state.LastBlockHeight}).Marshal()
	if err != nil {
		return err
	}
	for _, height := range []int64{blockStore.Height() - 1, blockStore.Height(), blockStore.Height() + 1} {
		if err := stateDB.Set(fmt.Appendf(nil, "validatorsKey:%v", height), valInfo); err != nil {
			return err
		}
	}
	// The state DB's genesis document is the one node info reports; the
	// file has been renamed, the copy in the DB has not.
	genDoc.ChainID = chainID
	genDocBz, err := cmtjson.Marshal(genDoc)
	if err != nil {
		return err
	}
	if err := stateDB.SetSync([]byte("genesisDoc"), genDocBz); err != nil {
		return err
	}
	realignSignState(logger, pv, state.LastBlockHeight)
	logger.Info("rewrote consensus state for the testnet", "chain_id", chainID, "height", state.LastBlockHeight, "validator", validatorAddress)
	return nil
}

// forgeCommit is the last block's commit as privKey alone would have signed
// it under chainID at height: the precommit and, for the extended commit
// vote extensions restart from, its signature over an empty extension. The
// key signs directly; the file validator's own check would refuse a height it
// has signed on the old chain.
func forgeCommit(privKey cmtcrypto.PrivKey, chainID string, height int64, blockID cmttypes.BlockID, at time.Time) (*cmttypes.Commit, *cmttypes.ExtendedCommit, error) {
	vote := cmttypes.Vote{
		Type:             cmtproto.PrecommitType,
		Height:           height,
		Round:            0,
		BlockID:          blockID,
		Timestamp:        at,
		ValidatorAddress: privKey.PubKey().Address(),
		ValidatorIndex:   0,
	}
	pb := vote.ToProto()
	signature, err := privKey.Sign(cmttypes.VoteSignBytes(chainID, pb))
	if err != nil {
		return nil, nil, err
	}
	extensionSignature, err := privKey.Sign(cmttypes.VoteExtensionSignBytes(chainID, pb))
	if err != nil {
		return nil, nil, err
	}
	commitSig := cmttypes.CommitSig{
		BlockIDFlag:      cmttypes.BlockIDFlagCommit,
		ValidatorAddress: vote.ValidatorAddress,
		Timestamp:        at,
		Signature:        signature,
	}
	commit := &cmttypes.Commit{Height: height, Round: 0, BlockID: blockID, Signatures: []cmttypes.CommitSig{commitSig}}
	extended := &cmttypes.ExtendedCommit{
		Height:             height,
		Round:              0,
		BlockID:            blockID,
		ExtendedSignatures: []cmttypes.ExtendedCommitSig{{CommitSig: commitSig, ExtensionSignature: extensionSignature}},
	}
	return commit, extended, nil
}

// stepPrecommit is CometBFT's private validator step for a precommit.
const stepPrecommit int8 = 3

// realignSignState moves pv's last signed height back to height when the old
// chain took it further. The node signs this chain's next block at the height
// after the last one; a signing state past it refuses that as a regression,
// and one at it with the old chain's bytes as conflicting data.
func realignSignState(logger log.Logger, pv *privval.FilePV, height int64) {
	lss := &pv.LastSignState
	if lss.Height <= height {
		return
	}
	logger.Info("realigning the signing state to the last block", "from", lss.Height, "to", height)
	lss.Height = height
	lss.Round = 0
	lss.Step = stepPrecommit
	lss.Signature = nil
	lss.SignBytes = nil
	lss.Save()
}
