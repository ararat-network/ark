package chainsuite

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/cosmos/interchaintest/v10/chain/cosmos"
	"github.com/cosmos/interchaintest/v10/ibc"

	sdkmath "cosmossdk.io/math"
)

// ArtefactGenesis lays the testnet artefact, app/genesis/testnet.json, over
// the genesis interchaintest generated, then applies the suite's overrides.
// The artefact supplies the consensus block and every module's state except
// the three interchaintest wrote: auth and genutil carry the validators and
// their gentxs, and bank merges the generated accounts with the artefact's
// ledger so supply still equals the balances. Validators are therefore
// ordinary bonded accounts on the launch economics, not seats.
func ArtefactGenesis(overrides []cosmos.GenesisKV) func(ibc.ChainConfig, []byte) ([]byte, error) {
	return func(cfg ibc.ChainConfig, generated []byte) ([]byte, error) {
		merged, err := overlayArtefact(generated)
		if err != nil {
			return nil, err
		}
		return cosmos.ModifyGenesis(overrides)(cfg, merged)
	}
}

// artefactPath locates the artefact from this source file, so every suite
// reads the same one whatever its working directory.
func artefactPath() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("locating the chainsuite source directory")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "app", "genesis", "testnet.json"), nil
}

func overlayArtefact(generated []byte) ([]byte, error) {
	path, err := artefactPath()
	if err != nil {
		return nil, err
	}
	artefactBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading the testnet artefact: %w", err)
	}

	var artefact, genesis map[string]json.RawMessage
	if err := json.Unmarshal(artefactBytes, &artefact); err != nil {
		return nil, fmt.Errorf("unmarshal the testnet artefact: %w", err)
	}
	if err := json.Unmarshal(generated, &genesis); err != nil {
		return nil, fmt.Errorf("unmarshal the generated genesis: %w", err)
	}
	var artefactState, state map[string]json.RawMessage
	if err := json.Unmarshal(artefact["app_state"], &artefactState); err != nil {
		return nil, fmt.Errorf("unmarshal the artefact app state: %w", err)
	}
	if err := json.Unmarshal(genesis["app_state"], &state); err != nil {
		return nil, fmt.Errorf("unmarshal the generated app state: %w", err)
	}

	for module, raw := range artefactState {
		switch module {
		case "auth", "genutil":
			// interchaintest's accounts and gentxs.
		case "bank":
			merged, err := mergeBank(state[module], raw)
			if err != nil {
				return nil, err
			}
			state[module] = merged
		default:
			state[module] = raw
		}
	}
	if genesis["app_state"], err = json.Marshal(state); err != nil {
		return nil, fmt.Errorf("marshal the merged app state: %w", err)
	}
	genesis["consensus"] = artefact["consensus"]
	return json.Marshal(genesis)
}

type bankCoin struct {
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
}

type bankBalance struct {
	Address string     `json:"address"`
	Coins   []bankCoin `json:"coins"`
}

type bankGenesis struct {
	Params        json.RawMessage `json:"params"`
	Balances      []bankBalance   `json:"balances"`
	Supply        []bankCoin      `json:"supply"`
	DenomMetadata json.RawMessage `json:"denom_metadata"`
	SendEnabled   json.RawMessage `json:"send_enabled"`
}

// mergeBank keeps the artefact's params and metadata, appends the generated
// balances to the artefact's ledger, and recomputes supply as their sum.
func mergeBank(generatedRaw, artefactRaw json.RawMessage) (json.RawMessage, error) {
	var generated, artefact bankGenesis
	if err := json.Unmarshal(generatedRaw, &generated); err != nil {
		return nil, fmt.Errorf("unmarshal the generated bank genesis: %w", err)
	}
	if err := json.Unmarshal(artefactRaw, &artefact); err != nil {
		return nil, fmt.Errorf("unmarshal the artefact bank genesis: %w", err)
	}

	seen := make(map[string]struct{}, len(artefact.Balances))
	for _, balance := range artefact.Balances {
		seen[balance.Address] = struct{}{}
	}
	supply := make(map[string]sdkmath.Int)
	add := func(coins []bankCoin) error {
		for _, coin := range coins {
			amount, ok := sdkmath.NewIntFromString(coin.Amount)
			if !ok {
				return fmt.Errorf("bank amount %q is not an integer", coin.Amount)
			}
			total, present := supply[coin.Denom]
			if !present {
				total = sdkmath.ZeroInt()
			}
			supply[coin.Denom] = total.Add(amount)
		}
		return nil
	}
	merged := artefact
	for _, balance := range generated.Balances {
		if _, duplicate := seen[balance.Address]; duplicate {
			return nil, fmt.Errorf("generated account %s collides with an artefact balance", balance.Address)
		}
		merged.Balances = append(merged.Balances, balance)
	}
	for _, balance := range merged.Balances {
		if err := add(balance.Coins); err != nil {
			return nil, err
		}
	}
	merged.Supply = merged.Supply[:0]
	for denom, amount := range supply {
		merged.Supply = append(merged.Supply, bankCoin{Denom: denom, Amount: amount.String()})
	}
	sort.Slice(merged.Supply, func(i, j int) bool { return merged.Supply[i].Denom < merged.Supply[j].Denom })
	return json.Marshal(merged)
}
