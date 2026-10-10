package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	cmttypes "github.com/cometbft/cometbft/types"

	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/ararat-network/ark/pkg/fsutil"
)

// saveGenesis writes a genesis document through an atomic replace, so a crash or a full disk
// leaves the file that was there rather than an empty one. The SDK's SaveAs truncates first.
func saveGenesis(genesis *genutiltypes.AppGenesis, path string) error {
	encoded, err := json.MarshalIndent(genesis, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding genesis %s: %w", path, err)
	}
	return fsutil.ReplaceFile(path, encoded, 0o600)
}

// exportGenesisFile is genutil.ExportGenesisFile over saveGenesis.
func exportGenesisFile(genesis *genutiltypes.AppGenesis, path string) error {
	if err := genesis.ValidateAndComplete(); err != nil {
		return err
	}
	return saveGenesis(genesis, path)
}

// exportGenesisFileWithTime is genutil.ExportGenesisFileWithTime over saveGenesis.
func exportGenesisFileWithTime(path, chainID string, validators []cmttypes.GenesisValidator, appState json.RawMessage, genTime time.Time) error {
	genesis := genutiltypes.NewAppGenesisWithVersion(chainID, appState)
	genesis.GenesisTime = genTime
	genesis.Consensus.Validators = validators
	return exportGenesisFile(genesis, path)
}
