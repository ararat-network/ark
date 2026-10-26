// Command registry writes Ark's network registry: for every network, the
// community chain-registry files, chain.json and assetlist.json, and ark.json
// for what that schema has no field for. The chain-derived half comes from
// pkg/chain and the genesis artefacts, so it cannot drift from the chain; the
// endpoints come from a file the registry keeps by hand.
//
//	go run ./contrib/registry --out <dir> [--endpoints <file>] [--version <ref>]
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	// The app's init seals the SDK config with the chain's prefix and coin
	// type; the registry reads them from there, as arkd does.
	_ "github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/pkg/chain"
)

const (
	gitRepo        = "https://github.com/ararat-network/ark"
	registryRepo   = "https://raw.githubusercontent.com/ararat-network/ark-registry/main"
	chainSchema    = "../chain.schema.json"
	assetSchema    = "../assetlist.schema.json"
	feeContractDoc = "docs/clients/CLIENT_FEES.md"
)

// options are the generator's inputs.
type options struct {
	repo      string // the ark checkout the artefacts and go.mod are read from
	out       string
	endpoints string
	version   string // the ark ref the files describe: a tag, or main
	imageBase string
}

func main() {
	var o options
	flag.StringVar(&o.repo, "repo", ".", "the ark checkout to read")
	flag.StringVar(&o.out, "out", "", "the directory to write the registry into")
	flag.StringVar(&o.endpoints, "endpoints", "", "a JSON file of endpoints per network, kept by the registry")
	flag.StringVar(&o.version, "version", "main", "the ark ref the files describe")
	flag.StringVar(&o.imageBase, "image-base", registryRepo, "the URL the registry's images are served from")
	flag.Parse()
	if o.out == "" {
		fmt.Fprintln(os.Stderr, "--out is required")
		os.Exit(2)
	}
	if err := generate(o); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// generate writes every network's files and the shared images under o.out.
func generate(o options) error {
	versions, err := readVersions(filepath.Join(o.repo, "go.mod"))
	if err != nil {
		return err
	}
	endpoints, err := readEndpoints(o.endpoints)
	if err != nil {
		return err
	}
	for _, n := range networks {
		artefact, err := readArtefact(filepath.Join(o.repo, n.artefact))
		if err != nil {
			return fmt.Errorf("%s: %w", n.name, err)
		}
		if artefact.chainID != n.artefactChainID {
			return fmt.Errorf("%s: artefact %s carries chain ID %s, expected %s", n.name, n.artefact, artefact.chainID, n.artefactChainID)
		}
		ep := endpoints[n.name]
		if n.local {
			ep = localEndpoints()
		}
		dir := filepath.Join(o.out, n.name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		files := []struct {
			name string
			v    any
		}{
			{"chain.json", chainFile(n, artefact, versions, ep, o.version)},
			{"assetlist.json", assetFile(n, artefact)},
			{"ark.json", arkFile(n, artefact, ep, o.version, o.imageBase)},
		}
		for _, f := range files {
			if err := writeJSON(filepath.Join(dir, f.name), f.v); err != nil {
				return err
			}
		}
	}
	return copyImages(o.repo, filepath.Join(o.out, "images"))
}

// network is one entry of the registry. The localnet runs the testnet
// artefact under its own chain ID, so its assets come from that artefact and
// it carries no genesis hash: its genesis is made on every init.
type network struct {
	name, chainID, networkType, prettyName string
	artefact, artefactChainID              string
	local                                  bool
}

var networks = []network{
	{name: "ark", chainID: "ark-1", networkType: "mainnet", prettyName: "Ark", artefact: "app/genesis/genesis.json", artefactChainID: "ark-1"},
	{name: "arktestnet", chainID: "ark-testnet-1", networkType: "testnet", prettyName: "Ark Testnet", artefact: "app/genesis/testnet.json", artefactChainID: "ark-testnet-1"},
	{name: "arklocal", chainID: "localark", networkType: "devnet", prettyName: "Ark Localnet", artefact: "app/genesis/testnet.json", artefactChainID: "ark-testnet-1", local: true},
}

// artefact is what the generator reads from a genesis file.
type artefact struct {
	chainID         string
	sha256          string
	assets          []string // active registry assets, in file order
	minBaseGasPrice string
	referenceDenom  string
	feeds           []string
}

func readArtefact(path string) (artefact, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return artefact{}, err
	}
	var g struct {
		ChainID  string `json:"chain_id"`
		AppState struct {
			Asset struct {
				Assets []struct {
					Denom  string `json:"denom"`
					Status string `json:"status"`
				} `json:"assets"`
			} `json:"asset"`
			Treasury struct {
				Params struct {
					MinBaseGasPrice string `json:"min_base_gas_price"`
				} `json:"params"`
			} `json:"treasury"`
			Oracle struct {
				ReferenceDenom string `json:"reference_denom"`
				Feeds          struct {
					Denoms []string `json:"denoms"`
				} `json:"feeds"`
			} `json:"oracle"`
		} `json:"app_state"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		return artefact{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	sum := sha256.Sum256(raw)
	a := artefact{
		chainID:         g.ChainID,
		sha256:          hex.EncodeToString(sum[:]),
		minBaseGasPrice: g.AppState.Treasury.Params.MinBaseGasPrice,
		referenceDenom:  g.AppState.Oracle.ReferenceDenom,
		feeds:           g.AppState.Oracle.Feeds.Denoms,
	}
	for _, asset := range g.AppState.Asset.Assets {
		if asset.Status == "ASSET_STATUS_ACTIVE" {
			a.assets = append(a.assets, asset.Denom)
		}
	}
	return a, nil
}

// versions are the dependency versions go.mod pins, read as text so the
// generator adds no dependency of its own.
type versions struct {
	sdk, cometbft, wasmvm string
}

func readVersions(goMod string) (versions, error) {
	raw, err := os.ReadFile(goMod)
	if err != nil {
		return versions{}, err
	}
	find := func(module string) (string, error) {
		m := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(module) + `\s+(v\S+)`).FindStringSubmatch(string(raw))
		if m == nil {
			return "", fmt.Errorf("%s: %s is not required", goMod, module)
		}
		return m[1], nil
	}
	var v versions
	if v.sdk, err = find("github.com/cosmos/cosmos-sdk"); err != nil {
		return versions{}, err
	}
	if v.cometbft, err = find("github.com/cometbft/cometbft"); err != nil {
		return versions{}, err
	}
	if v.wasmvm, err = find("github.com/CosmWasm/wasmvm/v3"); err != nil {
		return versions{}, err
	}
	return v, nil
}

// endpoint is one API endpoint in the chain-registry shape.
type endpoint struct {
	Address  string `json:"address"`
	Provider string `json:"provider,omitempty"`
}

// explorer is one explorer in the chain-registry shape.
type explorer struct {
	Kind        string `json:"kind,omitempty"`
	URL         string `json:"url,omitempty"`
	TxPage      string `json:"tx_page,omitempty"`
	AccountPage string `json:"account_page,omitempty"`
}

// networkEndpoints is the hand-kept half of one network's entry.
type networkEndpoints struct {
	RPC       []endpoint `json:"rpc"`
	REST      []endpoint `json:"rest"`
	GRPC      []endpoint `json:"grpc"`
	WSS       []endpoint `json:"wss,omitempty"`
	Explorers []explorer `json:"explorers"`
	Indexer   []endpoint `json:"indexer"`
}

func readEndpoints(path string) (map[string]networkEndpoints, error) {
	if path == "" {
		return map[string]networkEndpoints{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out map[string]networkEndpoints
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	for _, name := range slices.Sorted(maps.Keys(out)) {
		if !slices.ContainsFunc(networks, func(n network) bool { return n.name == name }) {
			return nil, fmt.Errorf("%s: %s is not a network", path, name)
		}
	}
	return out, nil
}

// localEndpoints are the Docker localnet's node 0 ports, contrib/localnet.
func localEndpoints() networkEndpoints {
	return networkEndpoints{
		RPC:       []endpoint{{Address: "http://localhost:26657", Provider: "localnet"}},
		REST:      []endpoint{{Address: "http://localhost:1317", Provider: "localnet"}},
		GRPC:      []endpoint{{Address: "localhost:9090", Provider: "localnet"}},
		Explorers: []explorer{},
		Indexer:   []endpoint{},
	}
}

// chainJSON is chain.json, the chain-registry schema's fields Ark fills. Logos
// are left out: the schema admits only the community registry's own URLs.
type chainJSON struct {
	Schema       string     `json:"$schema"`
	ChainName    string     `json:"chain_name"`
	ChainType    string     `json:"chain_type"`
	ChainID      string     `json:"chain_id"`
	Status       string     `json:"status"`
	NetworkType  string     `json:"network_type"`
	PrettyName   string     `json:"pretty_name"`
	Website      string     `json:"website"`
	Bech32Prefix string     `json:"bech32_prefix"`
	Slip44       uint32     `json:"slip44"`
	Fees         fees       `json:"fees"`
	Staking      staking    `json:"staking"`
	Codebase     codebase   `json:"codebase"`
	APIs         apis       `json:"apis"`
	Explorers    []explorer `json:"explorers"`
	Keywords     []string   `json:"keywords"`
}

type (
	fees struct {
		FeeTokens []feeToken `json:"fee_tokens"`
	}
	feeToken struct {
		Denom            string  `json:"denom"`
		FixedMinGasPrice float64 `json:"fixed_min_gas_price"`
	}
	staking struct {
		StakingTokens []stakingToken `json:"staking_tokens"`
	}
	stakingToken struct {
		Denom string `json:"denom"`
	}
	codebase struct {
		GitRepo            string   `json:"git_repo"`
		RecommendedVersion string   `json:"recommended_version,omitempty"`
		CompatibleVersions []string `json:"compatible_versions,omitempty"`
		SDK                typed    `json:"sdk"`
		Consensus          typed    `json:"consensus"`
		Cosmwasm           cosmwasm `json:"cosmwasm"`
		Genesis            genesis  `json:"genesis"`
	}
	typed    struct{ Type, Version string }
	cosmwasm struct {
		Version string `json:"version"`
		Enabled bool   `json:"enabled"`
	}
	genesis struct {
		GenesisURL string `json:"genesis_url"`
	}
	apis struct {
		RPC  []endpoint `json:"rpc"`
		REST []endpoint `json:"rest"`
		GRPC []endpoint `json:"grpc"`
		WSS  []endpoint `json:"wss,omitempty"`
	}
)

func (t typed) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type    string `json:"type"`
		Version string `json:"version,omitempty"`
	}{t.Type, t.Version})
}

func chainFile(n network, a artefact, v versions, ep networkEndpoints, version string) chainJSON {
	minGas, err := math.LegacyNewDecFromStr(a.minBaseGasPrice)
	if err != nil {
		panic(fmt.Sprintf("%s: min base gas price %q: %v", n.name, a.minBaseGasPrice, err))
	}
	cb := codebase{
		GitRepo:   gitRepo,
		SDK:       typed{Type: "cosmos", Version: v.sdk},
		Consensus: typed{Type: "cometbft", Version: v.cometbft},
		Cosmwasm:  cosmwasm{Version: v.wasmvm, Enabled: true},
		Genesis:   genesis{GenesisURL: fmt.Sprintf("https://raw.githubusercontent.com/ararat-network/ark/%s/%s", version, n.artefact)},
	}
	if version != "main" {
		cb.RecommendedVersion = version
		cb.CompatibleVersions = []string{version}
	}
	return chainJSON{
		Schema:       chainSchema,
		ChainName:    n.name,
		ChainType:    "cosmos",
		ChainID:      n.chainID,
		Status:       "upcoming",
		NetworkType:  n.networkType,
		PrettyName:   n.prettyName,
		Website:      "https://ararat.network",
		Bech32Prefix: sdk.GetConfig().GetBech32AccountAddrPrefix(),
		Slip44:       sdk.GetConfig().GetCoinType(),
		Fees:         fees{FeeTokens: []feeToken{{Denom: chain.NoahBaseDenom, FixedMinGasPrice: minGas.MustFloat64()}}},
		Staking:      staking{StakingTokens: []stakingToken{{Denom: chain.NoahBaseDenom}}},
		Codebase:     cb,
		APIs:         apis{RPC: orEmpty(ep.RPC), REST: orEmpty(ep.REST), GRPC: orEmpty(ep.GRPC), WSS: ep.WSS},
		Explorers:    orEmptyExplorers(ep.Explorers),
		Keywords:     []string{"stablecoin", "oracle", "cosmwasm"},
	}
}

// assetlistJSON is assetlist.json: NOAH and every registry asset the
// artefact lists as active, each from the chain's own Bank metadata.
type assetlistJSON struct {
	Schema    string  `json:"$schema"`
	ChainName string  `json:"chain_name"`
	Assets    []asset `json:"assets"`
}

type asset struct {
	Description string      `json:"description"`
	DenomUnits  []denomUnit `json:"denom_units"`
	Base        string      `json:"base"`
	Name        string      `json:"name"`
	Display     string      `json:"display"`
	Symbol      string      `json:"symbol"`
	TypeAsset   string      `json:"type_asset"`
}

type denomUnit struct {
	Denom    string   `json:"denom"`
	Exponent uint32   `json:"exponent"`
	Aliases  []string `json:"aliases,omitempty"`
}

func assetFile(n network, a artefact) assetlistJSON {
	out := assetlistJSON{Schema: assetSchema, ChainName: n.name}
	out.Assets = append(out.Assets, fromMetadata(chain.NoahMetadata()))
	for _, denom := range a.assets {
		out.Assets = append(out.Assets, fromMetadata(chain.NativeAssetMetadata(denom)))
	}
	return out
}

func fromMetadata(m banktypes.Metadata) asset {
	units := make([]denomUnit, 0, len(m.DenomUnits))
	for _, u := range m.DenomUnits {
		units = append(units, denomUnit{Denom: u.Denom, Exponent: u.Exponent, Aliases: u.Aliases})
	}
	return asset{
		Description: m.Description, DenomUnits: units, Base: m.Base,
		Name: m.Name, Display: m.Display, Symbol: m.Symbol, TypeAsset: "sdk.coin",
	}
}

// arkJSON is ark.json: what the chain-registry schema has no field for.
type arkJSON struct {
	ChainID        string     `json:"chain_id"`
	ArkVersion     string     `json:"ark_version"`
	GenesisSHA256  string     `json:"genesis_sha256,omitempty"`
	Bech32Prefix   string     `json:"bech32_prefix"`
	Slip44         uint32     `json:"slip44"`
	Features       features   `json:"features"`
	ReferenceDenom string     `json:"reference_denom"`
	Feeds          []string   `json:"feeds"`
	FeeDenoms      []string   `json:"fee_denoms"`
	FeeContract    string     `json:"fee_contract"`
	LogoURIs       logoURIs   `json:"logo_URIs"`
	Indexer        []endpoint `json:"indexer"`
}

type features struct {
	Cosmwasm       bool `json:"cosmwasm"`
	VoteExtensions bool `json:"vote_extensions"`
	ConversionOpen bool `json:"conversion_open"`
}

type logoURIs struct {
	PNG string `json:"png"`
	SVG string `json:"svg"`
}

func arkFile(n network, a artefact, ep networkEndpoints, version, imageBase string) arkJSON {
	out := arkJSON{
		ChainID:        n.chainID,
		ArkVersion:     version,
		Bech32Prefix:   sdk.GetConfig().GetBech32AccountAddrPrefix(),
		Slip44:         sdk.GetConfig().GetCoinType(),
		Features:       features{Cosmwasm: true, VoteExtensions: true, ConversionOpen: len(a.assets) > 0},
		ReferenceDenom: a.referenceDenom,
		Feeds:          orEmptyStrings(a.feeds),
		FeeDenoms:      append([]string{chain.NoahBaseDenom}, a.assets...),
		FeeContract:    fmt.Sprintf("%s/blob/%s/%s", gitRepo, version, feeContractDoc),
		LogoURIs:       logoURIs{PNG: imageBase + "/images/" + imagePNG, SVG: imageBase + "/images/" + imageSVG},
		Indexer:        orEmpty(ep.Indexer),
	}
	if !n.local {
		out.GenesisSHA256 = a.sha256
	}
	return out
}

// The brand kit files the registry serves, copied beside the networks.
const (
	imagePNG = "ark-symbol-navy-512.png"
	imageSVG = "ark-symbol-navy.svg"
)

func copyImages(repo, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range []struct{ from, to string }{
		{filepath.Join(repo, "docs", "brand", "png", imagePNG), imagePNG},
		{filepath.Join(repo, "docs", "brand", "svg", imageSVG), imageSVG},
	} {
		raw, err := os.ReadFile(f.from)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, f.to), raw, 0o600); err != nil { //nolint:gosec // the output directory is the operator's flag
			return err
		}
	}
	return nil
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

func orEmpty(in []endpoint) []endpoint {
	if in == nil {
		return []endpoint{}
	}
	return in
}

func orEmptyExplorers(in []explorer) []explorer {
	if in == nil {
		return []explorer{}
	}
	return in
}

func orEmptyStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
