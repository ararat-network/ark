package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/pkg/chain"
)

// newDryRunCmd returns genesis dry-run: the file booted in memory as CometBFT
// starts a chain from it, InitChain and the first block, then the bonded set.
func newDryRunCmd(defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dry-run [genesis-file]",
		Short: "Boot a genesis file in memory through InitChain and its first block",
		Long: `Boot a genesis file in memory as CometBFT starts a chain from it: InitChain
with the file's chain ID, time, initial height, consensus parameters, and
validators, then the first block proposed, processed, finalised, and
committed. Collection and genesis validate check each gentx alone, so a gentx
the chain refuses, such as a commission under the staking floor, otherwise
surfaces only at genesis_time. Prints the bonded set: moniker, operator,
tokens in NOAH, and commission. Nothing is written; the file defaults to the
home's genesis.json.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			genesisFile := ""
			if len(args) == 1 {
				genesisFile = args[0]
			} else {
				config := server.GetServerContextFromCmd(cmd).Config
				config.SetRoot(client.GetClientContextFromCmd(cmd).HomeDir)
				genesisFile = config.GenesisFile()
			}
			return runDryRun(cmd.OutOrStdout(), genesisFile)
		},
	}
	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	return cmd
}

func runDryRun(out io.Writer, genesisFile string) (err error) {
	appGenesis, err := genutiltypes.AppGenesisFromFile(genesisFile)
	if err != nil {
		return err
	}
	doc, err := appGenesis.ToGenesisDoc()
	if err != nil {
		return err
	}
	if err := doc.ValidateAndComplete(); err != nil {
		return fmt.Errorf("genesis doc: %w", err)
	}

	// The app wants a home for its contract cache, never the node's own.
	home, err := os.MkdirTemp("", "arkd-dry-run-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(home) }()
	opts := viper.New()
	opts.Set(flags.FlagHome, home)
	arkApp := app.NewArkApp(log.NewNopLogger(), dbm.NewMemDB(), true, opts, baseapp.SetChainID(doc.ChainID))
	defer func() { err = errors.Join(err, arkApp.Close()) }()

	if err := bootGenesis(arkApp, doc); err != nil {
		return err
	}
	bonded, err := arkApp.StakingKeeper.GetBondedValidatorsByPower(arkApp.NewContext(true))
	if err != nil {
		return err
	}
	return printBondedSet(out, doc, bonded)
}

// bootGenesis sends what CometBFT sends at a chain's start: InitChain as its
// handshake builds it, then the first block at the genesis time. A panic is
// a chain that does not start, so it is returned as one.
func bootGenesis(arkApp *app.ArkApp, doc *cmttypes.GenesisDoc) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()

	genesisValidators := make([]*cmttypes.Validator, len(doc.Validators))
	for i, v := range doc.Validators {
		genesisValidators[i] = cmttypes.NewValidator(v.PubKey, v.Power)
	}
	set := cmttypes.NewValidatorSet(genesisValidators)
	params := doc.ConsensusParams.ToProto()
	res, err := arkApp.InitChain(&cmtabci.RequestInitChain{
		Time:            doc.GenesisTime,
		ChainId:         doc.ChainID,
		InitialHeight:   doc.InitialHeight,
		ConsensusParams: &params,
		Validators:      cmttypes.TM2PB.ValidatorUpdates(set),
		AppStateBytes:   doc.AppState,
	})
	if err != nil {
		return fmt.Errorf("InitChain: %w", err)
	}
	if len(res.Validators) > 0 {
		validators, err := cmttypes.PB2TM.ValidatorUpdates(res.Validators)
		if err != nil {
			return fmt.Errorf("InitChain validators: %w", err)
		}
		set = cmttypes.NewValidatorSet(validators)
	}
	// CometBFT's own refusal; the SDK's module manager refuses an empty set first.
	if set.IsNilOrEmpty() {
		return errors.New("validator set is nil in genesis and still empty after InitChain")
	}
	consensus := *doc.ConsensusParams
	if res.ConsensusParams != nil {
		consensus = consensus.Update(res.ConsensusParams)
	}
	maxBytes := consensus.Block.MaxBytes
	if maxBytes == -1 {
		maxBytes = cmttypes.MaxBlockSizeBytes
	}

	height, at, proposer, next := doc.InitialHeight, doc.GenesisTime, set.GetProposer().Address, set.Hash()
	proposal, err := arkApp.PrepareProposal(&cmtabci.RequestPrepareProposal{
		Height:             height,
		Time:               at,
		MaxTxBytes:         cmttypes.MaxDataBytesNoEvidence(maxBytes, set.Size()),
		NextValidatorsHash: next,
		ProposerAddress:    proposer,
	})
	if err != nil {
		return fmt.Errorf("first block PrepareProposal: %w", err)
	}
	processed, err := arkApp.ProcessProposal(&cmtabci.RequestProcessProposal{
		Height:             height,
		Time:               at,
		Txs:                proposal.Txs,
		NextValidatorsHash: next,
		ProposerAddress:    proposer,
	})
	if err != nil {
		return fmt.Errorf("first block ProcessProposal: %w", err)
	}
	if processed.Status != cmtabci.ResponseProcessProposal_ACCEPT {
		return fmt.Errorf("first block ProcessProposal: %s", processed.Status)
	}
	if _, err := arkApp.FinalizeBlock(&cmtabci.RequestFinalizeBlock{
		Height:             height,
		Time:               at,
		Txs:                proposal.Txs,
		NextValidatorsHash: next,
		ProposerAddress:    proposer,
	}); err != nil {
		return fmt.Errorf("first block FinalizeBlock: %w", err)
	}
	if _, err := arkApp.Commit(); err != nil {
		return fmt.Errorf("first block Commit: %w", err)
	}
	return nil
}

func printBondedSet(out io.Writer, doc *cmttypes.GenesisDoc, bonded []stakingtypes.Validator) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "%s boots at height %d, %s; bonded validators: %d\n",
		doc.ChainID, doc.InitialHeight, doc.GenesisTime.UTC().Format(time.RFC3339), len(bonded))
	fmt.Fprintln(w, "MONIKER\tOPERATOR\tNOAH\tCOMMISSION")
	for _, v := range bonded {
		noah := math.LegacyNewDecFromInt(v.Tokens).QuoInt(chain.NativeBaseAmount(1))
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", v.Description.Moniker, v.OperatorAddress, trimDecimal(noah), trimDecimal(v.Commission.Rate))
	}
	return w.Flush()
}

// trimDecimal renders d without trailing zeros: 5000000, 0.1.
func trimDecimal(d math.LegacyDec) string {
	s := d.String()
	if !strings.Contains(s, ".") {
		return s
	}
	return strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
}
