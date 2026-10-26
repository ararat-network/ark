package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/pkg/chain"
)

const repo = "../.."

func generateInto(t *testing.T, endpoints string) string {
	t.Helper()
	out := t.TempDir()
	require.NoError(t, generate(options{repo: repo, out: out, endpoints: endpoints, version: "main", imageBase: registryRepo}))
	return out
}

func readFile[T any](t *testing.T, path string) T {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var v T
	require.NoError(t, json.Unmarshal(raw, &v))
	return v
}

func TestNetworksAndAssets(t *testing.T) {
	out := generateInto(t, "")
	tests := []struct {
		name, chainID, networkType string
		assets                     int
		hashed, local              bool
	}{
		{"ark", "ark-1", "mainnet", 1, true, false},
		{"arktestnet", "ark-testnet-1", "testnet", 11, true, false},
		{"arklocal", "localark", "devnet", 11, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := readFile[chainJSON](t, filepath.Join(out, tt.name, "chain.json"))
			require.Equal(t, tt.chainID, c.ChainID)
			require.Equal(t, tt.networkType, c.NetworkType)
			require.Equal(t, "ark", c.Bech32Prefix)
			require.Equal(t, uint32(330), c.Slip44)
			require.Equal(t, "cosmos", c.ChainType)
			require.Equal(t, []feeToken{{Denom: chain.NoahBaseDenom, FixedMinGasPrice: 1e11}}, c.Fees.FeeTokens)
			require.Equal(t, "v0.54.4", c.Codebase.SDK.Version)
			require.Equal(t, tt.local, len(c.APIs.RPC) > 0, "only the localnet has endpoints without an endpoints file")

			a := readFile[assetlistJSON](t, filepath.Join(out, tt.name, "assetlist.json"))
			require.Len(t, a.Assets, tt.assets)
			require.Equal(t, chain.NoahBaseDenom, a.Assets[0].Base)
			require.Equal(t, "noah", a.Assets[0].Display)
			require.Equal(t, []denomUnit{{Denom: chain.NoahBaseDenom, Exponent: 0}, {Denom: "noah", Exponent: 18}}, a.Assets[0].DenomUnits)
			for _, as := range a.Assets {
				require.Equal(t, "sdk.coin", as.TypeAsset)
				require.NotEmpty(t, as.Symbol)
			}

			k := readFile[arkJSON](t, filepath.Join(out, tt.name, "ark.json"))
			require.Equal(t, tt.chainID, k.ChainID)
			require.Equal(t, tt.assets > 1, k.Features.ConversionOpen)
			require.Equal(t, tt.assets, len(k.FeeDenoms))
			require.Equal(t, chain.XDRBaseDenom, k.ReferenceDenom)
			require.Contains(t, k.Feeds, chain.XDRBaseDenom)
			require.Equal(t, tt.hashed, k.GenesisSHA256 != "")
		})
	}
	for _, image := range []string{imagePNG, imageSVG} {
		_, err := os.Stat(filepath.Join(out, "images", image))
		require.NoError(t, err)
	}
}

func TestGenesisHashIsTheArtefacts(t *testing.T) {
	out := generateInto(t, "")
	raw, err := os.ReadFile(filepath.Join(repo, "app", "genesis", "testnet.json"))
	require.NoError(t, err)
	sum := sha256.Sum256(raw)
	k := readFile[arkJSON](t, filepath.Join(out, "arktestnet", "ark.json"))
	require.Equal(t, hex.EncodeToString(sum[:]), k.GenesisSHA256)
}

func TestEndpointsMergeAndRefuseUnknownNetworks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "endpoints.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"arktestnet":{"rpc":[{"address":"https://rpc.testnet.example","provider":"ararat"}],"rest":[],"grpc":[],"explorers":[{"kind":"ark","url":"https://explorer.testnet.example"}],"indexer":[]}}`), 0o600))
	out := generateInto(t, path)
	c := readFile[chainJSON](t, filepath.Join(out, "arktestnet", "chain.json"))
	require.Equal(t, []endpoint{{Address: "https://rpc.testnet.example", Provider: "ararat"}}, c.APIs.RPC)
	require.Len(t, c.Explorers, 1)
	main := readFile[chainJSON](t, filepath.Join(out, "ark", "chain.json"))
	require.Empty(t, main.APIs.RPC)

	require.NoError(t, os.WriteFile(path, []byte(`{"nowhere":{}}`), 0o600))
	err := generate(options{repo: repo, out: t.TempDir(), endpoints: path, version: "main", imageBase: registryRepo})
	require.ErrorContains(t, err, "nowhere is not a network")
}

func TestOutputIsDeterministic(t *testing.T) {
	first, second := generateInto(t, ""), generateInto(t, "")
	for _, n := range networks {
		for _, name := range []string{"chain.json", "assetlist.json", "ark.json"} {
			a, err := os.ReadFile(filepath.Join(first, n.name, name))
			require.NoError(t, err)
			b, err := os.ReadFile(filepath.Join(second, n.name, name))
			require.NoError(t, err)
			require.Equal(t, string(a), string(b), "%s/%s", n.name, name)
		}
	}
}

func TestVersionNamesTheRef(t *testing.T) {
	out := t.TempDir()
	require.NoError(t, generate(options{repo: repo, out: out, version: "v1.2.3", imageBase: registryRepo}))
	c := readFile[chainJSON](t, filepath.Join(out, "ark", "chain.json"))
	require.Equal(t, "v1.2.3", c.Codebase.RecommendedVersion)
	require.Equal(t, []string{"v1.2.3"}, c.Codebase.CompatibleVersions)
	require.Contains(t, c.Codebase.Genesis.GenesisURL, "/v1.2.3/app/genesis/genesis.json")
	k := readFile[arkJSON](t, filepath.Join(out, "ark", "ark.json"))
	require.Equal(t, "v1.2.3", k.ArkVersion)
}
