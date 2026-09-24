# Licensing and corresponding source

Ark-authored source is licensed under Apache 2.0 (`LICENSE`). Third-party material
retains its original licence and copyright notices. This does not grant ownership
of upstream code to Ark or replace upstream licence terms.

The combined `arkd` executable includes Prysm's GPLv3-covered BLS implementation,
through IBC Wasm's verifier, and is distributed under GPLv3 (`COPYING`). Recipients
may modify and redistribute that combined work under GPLv3. Its Apache-licensed
components retain their notices and remain available under Apache 2.0 separately.
The independently built `pricefeed` executable does not import Prysm; including
`COPYING` with a distribution does not by itself relicense that separate program.

## Upstream attribution

Ark builds on the following projects. This table identifies major upstreams; it
is not a substitute for the dependency notices and sources included with releases.

| Project | Relationship | Licence |
| --- | --- | --- |
| [Terra Classic](https://github.com/terra-money/classic-core) | Original chain implementation adapted by Ark | Apache 2.0 |
| [Cosmos SDK](https://github.com/cosmos/cosmos-sdk) and its separately versioned modules | Application framework and adapted application/CLI scaffolding | Apache 2.0 |
| [Gaia](https://github.com/cosmos/gaia) | Adapted application export and end-to-end test helpers | Apache 2.0 |
| [Connect / Slinky](https://github.com/skip-mev/connect) | Adapted oracle validation, metrics, and provider code | Apache 2.0 |
| [MEXC websocket-proto](https://github.com/mexcdevelop/websocket-proto) | Wire schema the MEXC sidecar adapter decodes against; no code is adapted | Apache 2.0 |
| [Terra oracle-feeder](https://github.com/terra-money/oracle-feeder) | Oracle sidecar reference | Apache 2.0 |
| [CosmWasm / wasmvm](https://github.com/CosmWasm/wasmvm) | Contract runtime and native Rust library | Apache 2.0, with separately licensed dependencies |
| [Prysm](https://github.com/OffchainLabs/prysm) | BLS verifier imported by the node | GPLv3 |
| [go-ethereum](https://github.com/ethereum/go-ethereum) | Ethereum library packages imported by node dependencies | LGPLv3 or later for those library files |

`NOTICE` preserves Terra Classic, Gaia, and Cosmos SDK copyright statements and
reproduces the Cosmos SDK and wasmvm notices. Dependency versions come
from the release revision's `go.mod`, `go.sum`, and wasmvm `Cargo.lock`. The source
bundle retains complete Go module archives, including their original notices,
and includes native dependencies and Rust crates with their upstream notices.
The extracted `notices/` directory is a convenience index, not an exhaustive
replacement for notices embedded in source files. Preserve those embedded notices
when modifying or redistributing source, and identify modifications and dates as
required by the applicable licence. Dependency changes require renewed review;
an offline build checks source availability, not legal compatibility or provenance.

## Adapted source files

The following files contain adaptations identified by source comparison. Each
carries an Apache-2.0 identifier and a notice describing Ark's modifications.
Preserve these notices when moving or modifying the files. This inventory covers
confirmed adaptations and traced Terra port lineage, including extensively
rewritten files and code moved into new packages. It does not establish that every
current line came from upstream or identify every possible unrecorded source.

The upstream links pin the revisions compared during review. They are evidence
snapshots, not claims that those revisions were the original import versions.
Ark's changes include subsequent refactoring and replacement of upstream logic.

### Cosmos SDK

Comparison revision: `486b89779f22760e3282202fb08e29cce292b87d`.

| Ark file | Upstream source |
| --- | --- |
| [app/app.go](app/app.go) | [simapp/app.go](https://github.com/cosmos/cosmos-sdk/blob/486b89779f22760e3282202fb08e29cce292b87d/simapp/app.go) |
| [app/app_config.go](app/app_config.go) | [tests/e2e/distribution/config.go](https://github.com/cosmos/cosmos-sdk/blob/486b89779f22760e3282202fb08e29cce292b87d/tests/e2e/distribution/config.go) |
| [app/app_test.go](app/app_test.go) | [simapp/app_test.go](https://github.com/cosmos/cosmos-sdk/blob/486b89779f22760e3282202fb08e29cce292b87d/simapp/app_test.go) and [simapp/testutil_network_test.go](https://github.com/cosmos/cosmos-sdk/blob/486b89779f22760e3282202fb08e29cce292b87d/simapp/testutil_network_test.go) |
| [app/sim_bench_test.go](app/sim_bench_test.go) | [simapp/sim_bench_test.go](https://github.com/cosmos/cosmos-sdk/blob/486b89779f22760e3282202fb08e29cce292b87d/simapp/sim_bench_test.go) |
| [app/sim_test.go](app/sim_test.go) | [simapp/sim_test.go](https://github.com/cosmos/cosmos-sdk/blob/486b89779f22760e3282202fb08e29cce292b87d/simapp/sim_test.go) |
| [cmd/arkd/cmd/commands.go](cmd/arkd/cmd/commands.go) | [simapp/simd/cmd/commands.go](https://github.com/cosmos/cosmos-sdk/blob/486b89779f22760e3282202fb08e29cce292b87d/simapp/simd/cmd/commands.go) |
| [cmd/arkd/cmd/export.go](cmd/arkd/cmd/export.go) | [server/export.go](https://github.com/cosmos/cosmos-sdk/blob/486b89779f22760e3282202fb08e29cce292b87d/server/export.go) |
| [cmd/arkd/cmd/in_place_testnet.go](cmd/arkd/cmd/in_place_testnet.go) | [server/start.go](https://github.com/cosmos/cosmos-sdk/blob/486b89779f22760e3282202fb08e29cce292b87d/server/start.go) |
| [cmd/arkd/cmd/testnet.go](cmd/arkd/cmd/testnet.go) | [simapp/simd/cmd/testnet.go](https://github.com/cosmos/cosmos-sdk/blob/486b89779f22760e3282202fb08e29cce292b87d/simapp/simd/cmd/testnet.go) |
| [cmd/arkd/cmd/testnet_test.go](cmd/arkd/cmd/testnet_test.go) | [simapp/simd/cmd/testnet_test.go](https://github.com/cosmos/cosmos-sdk/blob/486b89779f22760e3282202fb08e29cce292b87d/simapp/simd/cmd/testnet_test.go) |
| [proto/scripts/protocgen.sh](proto/scripts/protocgen.sh) | [scripts/protocgen.sh](https://github.com/cosmos/cosmos-sdk/blob/486b89779f22760e3282202fb08e29cce292b87d/scripts/protocgen.sh) |

### Gaia

Comparison revision: `aa5f96b6534f973458b7ccb2dbced281d384b825`.

| Ark file | Upstream source |
| --- | --- |
| [app/export.go](app/export.go) | [app/export.go](https://github.com/cosmos/gaia/blob/aa5f96b6534f973458b7ccb2dbced281d384b825/app/export.go) |
| [app/testnet.go](app/testnet.go) | [cmd/gaiad/cmd/testnet_set_local_validator.go](https://github.com/cosmos/gaia/blob/aa5f96b6534f973458b7ccb2dbced281d384b825/cmd/gaiad/cmd/testnet_set_local_validator.go) |
| [tests/e2e/chainsuite/chain.go](tests/e2e/chainsuite/chain.go) | [tests/interchain/chainsuite/chain.go](https://github.com/cosmos/gaia/blob/aa5f96b6534f973458b7ccb2dbced281d384b825/tests/interchain/chainsuite/chain.go) |
| [tests/e2e/chainsuite/config.go](tests/e2e/chainsuite/config.go) | [tests/interchain/chainsuite/config.go](https://github.com/cosmos/gaia/blob/aa5f96b6534f973458b7ccb2dbced281d384b825/tests/interchain/chainsuite/config.go) |
| [tests/e2e/chainsuite/context.go](tests/e2e/chainsuite/context.go) | [tests/interchain/chainsuite/context.go](https://github.com/cosmos/gaia/blob/aa5f96b6534f973458b7ccb2dbced281d384b825/tests/interchain/chainsuite/context.go) |
| [tests/e2e/chainsuite/relayer.go](tests/e2e/chainsuite/relayer.go) | [tests/interchain/chainsuite/relayer.go](https://github.com/cosmos/gaia/blob/aa5f96b6534f973458b7ccb2dbced281d384b825/tests/interchain/chainsuite/relayer.go) |
| [tests/e2e/chainsuite/suite.go](tests/e2e/chainsuite/suite.go) | [tests/interchain/chainsuite/suite.go](https://github.com/cosmos/gaia/blob/aa5f96b6534f973458b7ccb2dbced281d384b825/tests/interchain/chainsuite/suite.go) |
| [tests/e2e/integrator/endpoints_test.go](tests/e2e/integrator/endpoints_test.go) | [tests/interchain/integrator/endpoints_test.go](https://github.com/cosmos/gaia/blob/aa5f96b6534f973458b7ccb2dbced281d384b825/tests/interchain/integrator/endpoints_test.go) |
| [tests/e2e/validator/config_test.go](tests/e2e/validator/config_test.go) | [tests/interchain/validator/config_test.go](https://github.com/cosmos/gaia/blob/aa5f96b6534f973458b7ccb2dbced281d384b825/tests/interchain/validator/config_test.go) |

### Connect

Comparison revision: `9f85605735ddea817886fe8a26d309d761d242fe`.

| Ark file | Upstream source |
| --- | --- |
| [abci/metrics/types.go](abci/metrics/types.go) | [service/metrics/types.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/service/metrics/types.go) |
| [abci/voteextension/validation.go](abci/voteextension/validation.go) | [abci/ve/utils.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/abci/ve/utils.go) |
| [pricefeed/sidecar/providers/api/binance/handler.go](pricefeed/sidecar/providers/api/binance/handler.go) | [providers/apis/binance/api_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/apis/binance/api_handler.go) |
| [pricefeed/sidecar/providers/api/binance/response.go](pricefeed/sidecar/providers/api/binance/response.go) | [providers/apis/binance/utils.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/apis/binance/utils.go) |
| [pricefeed/sidecar/providers/api/bitstamp/handler.go](pricefeed/sidecar/providers/api/bitstamp/handler.go) | [providers/apis/bitstamp/api_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/apis/bitstamp/api_handler.go) |
| [pricefeed/sidecar/providers/api/bitstamp/response.go](pricefeed/sidecar/providers/api/bitstamp/response.go) | [providers/apis/bitstamp/utils.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/apis/bitstamp/utils.go) |
| [pricefeed/sidecar/providers/api/coinbase/handler.go](pricefeed/sidecar/providers/api/coinbase/handler.go) | [providers/apis/coinbase/api_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/apis/coinbase/api_handler.go) |
| [pricefeed/sidecar/providers/api/coinbase/response.go](pricefeed/sidecar/providers/api/coinbase/response.go) | [providers/apis/coinbase/utils.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/apis/coinbase/utils.go) |
| [pricefeed/sidecar/providers/api/coingecko/handler.go](pricefeed/sidecar/providers/api/coingecko/handler.go) | [providers/apis/coingecko/api_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/apis/coingecko/api_handler.go) |
| [pricefeed/sidecar/providers/api/coingecko/response.go](pricefeed/sidecar/providers/api/coingecko/response.go) | [providers/apis/coingecko/utils.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/apis/coingecko/utils.go) |
| [pricefeed/sidecar/providers/api/coinmarketcap/handler.go](pricefeed/sidecar/providers/api/coinmarketcap/handler.go) | [providers/apis/coinmarketcap/api_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/apis/coinmarketcap/api_handler.go) |
| [pricefeed/sidecar/providers/api/coinmarketcap/response.go](pricefeed/sidecar/providers/api/coinmarketcap/response.go) | [providers/apis/coinmarketcap/utils.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/apis/coinmarketcap/utils.go) |
| [pricefeed/sidecar/providers/api/geckoterminal/handler.go](pricefeed/sidecar/providers/api/geckoterminal/handler.go) | [providers/apis/geckoterminal/api_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/apis/geckoterminal/api_handler.go) |
| [pricefeed/sidecar/providers/api/geckoterminal/response.go](pricefeed/sidecar/providers/api/geckoterminal/response.go) | [providers/apis/geckoterminal/utils.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/apis/geckoterminal/utils.go) |
| [pricefeed/sidecar/providers/api/kraken/handler.go](pricefeed/sidecar/providers/api/kraken/handler.go) | [providers/apis/kraken/api_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/apis/kraken/api_handler.go) |
| [pricefeed/sidecar/providers/api/kraken/response.go](pricefeed/sidecar/providers/api/kraken/response.go) | [providers/apis/kraken/utils.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/apis/kraken/utils.go) |
| [pricefeed/sidecar/providers/api/polymarket/handler.go](pricefeed/sidecar/providers/api/polymarket/handler.go) | [providers/apis/polymarket/api_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/apis/polymarket/api_handler.go) |
| [pricefeed/sidecar/providers/api/polymarket/response.go](pricefeed/sidecar/providers/api/polymarket/response.go) | [providers/apis/polymarket/api_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/apis/polymarket/api_handler.go) |
| [pricefeed/sidecar/providers/base/websocket/errors.go](pricefeed/sidecar/providers/base/websocket/errors.go) | [providers/base/websocket/errors/ws_query_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/base/websocket/errors/ws_query_handler.go) |
| [pricefeed/sidecar/providers/types/errors.go](pricefeed/sidecar/providers/types/errors.go) | [providers/types/errors.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/types/errors.go) |
| [pricefeed/sidecar/providers/websocket/binance/handler.go](pricefeed/sidecar/providers/websocket/binance/handler.go) | [providers/websockets/binance/ws_data_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/binance/ws_data_handler.go) |
| [pricefeed/sidecar/providers/websocket/binance/messages.go](pricefeed/sidecar/providers/websocket/binance/messages.go) | [providers/websockets/binance/messages.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/binance/messages.go) |
| [pricefeed/sidecar/providers/websocket/bitfinex/handler.go](pricefeed/sidecar/providers/websocket/bitfinex/handler.go) | [providers/websockets/bitfinex/ws_data_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/bitfinex/ws_data_handler.go) |
| [pricefeed/sidecar/providers/websocket/bitfinex/messages.go](pricefeed/sidecar/providers/websocket/bitfinex/messages.go) | [providers/websockets/bitfinex/messages.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/bitfinex/messages.go) |
| [pricefeed/sidecar/providers/websocket/bitfinex/parse.go](pricefeed/sidecar/providers/websocket/bitfinex/parse.go) | [providers/websockets/bitfinex/parse.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/bitfinex/parse.go) |
| [pricefeed/sidecar/providers/websocket/bitstamp/handler.go](pricefeed/sidecar/providers/websocket/bitstamp/handler.go) | [providers/websockets/bitstamp/ws_data_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/bitstamp/ws_data_handler.go) |
| [pricefeed/sidecar/providers/websocket/bitstamp/messages.go](pricefeed/sidecar/providers/websocket/bitstamp/messages.go) | [providers/websockets/bitstamp/messages.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/bitstamp/messages.go) |
| [pricefeed/sidecar/providers/websocket/bitstamp/parse.go](pricefeed/sidecar/providers/websocket/bitstamp/parse.go) | [providers/websockets/bitstamp/parse.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/bitstamp/parse.go) |
| [pricefeed/sidecar/providers/websocket/bybit/handler.go](pricefeed/sidecar/providers/websocket/bybit/handler.go) | [providers/websockets/bybit/ws_data_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/bybit/ws_data_handler.go) |
| [pricefeed/sidecar/providers/websocket/bybit/messages.go](pricefeed/sidecar/providers/websocket/bybit/messages.go) | [providers/websockets/bybit/messages.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/bybit/messages.go) |
| [pricefeed/sidecar/providers/websocket/bybit/parse.go](pricefeed/sidecar/providers/websocket/bybit/parse.go) | [providers/websockets/bybit/parse.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/bybit/parse.go) |
| [pricefeed/sidecar/providers/websocket/coinbase/handler.go](pricefeed/sidecar/providers/websocket/coinbase/handler.go) | [providers/websockets/coinbase/ws_data_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/coinbase/ws_data_handler.go) |
| [pricefeed/sidecar/providers/websocket/coinbase/messages.go](pricefeed/sidecar/providers/websocket/coinbase/messages.go) | [providers/websockets/coinbase/messages.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/coinbase/messages.go) |
| [pricefeed/sidecar/providers/websocket/coinbase/parse.go](pricefeed/sidecar/providers/websocket/coinbase/parse.go) | [providers/websockets/coinbase/parse.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/coinbase/parse.go) |
| [pricefeed/sidecar/providers/websocket/cryptodotcom/handler.go](pricefeed/sidecar/providers/websocket/cryptodotcom/handler.go) | [providers/websockets/cryptodotcom/ws_data_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/cryptodotcom/ws_data_handler.go) |
| [pricefeed/sidecar/providers/websocket/cryptodotcom/messages.go](pricefeed/sidecar/providers/websocket/cryptodotcom/messages.go) | [providers/websockets/cryptodotcom/messages.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/cryptodotcom/messages.go) |
| [pricefeed/sidecar/providers/websocket/cryptodotcom/parse.go](pricefeed/sidecar/providers/websocket/cryptodotcom/parse.go) | [providers/websockets/cryptodotcom/parse.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/cryptodotcom/parse.go) |
| [pricefeed/sidecar/providers/websocket/gate/handler.go](pricefeed/sidecar/providers/websocket/gate/handler.go) | [providers/websockets/gate/ws_data_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/gate/ws_data_handler.go) |
| [pricefeed/sidecar/providers/websocket/gate/messages.go](pricefeed/sidecar/providers/websocket/gate/messages.go) | [providers/websockets/gate/messages.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/gate/messages.go) |
| [pricefeed/sidecar/providers/websocket/gate/parse.go](pricefeed/sidecar/providers/websocket/gate/parse.go) | [providers/websockets/gate/parse.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/gate/parse.go) |
| [pricefeed/sidecar/providers/websocket/huobi/handler.go](pricefeed/sidecar/providers/websocket/huobi/handler.go) | [providers/websockets/huobi/ws_data_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/huobi/ws_data_handler.go) |
| [pricefeed/sidecar/providers/websocket/huobi/messages.go](pricefeed/sidecar/providers/websocket/huobi/messages.go) | [providers/websockets/huobi/messages.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/huobi/messages.go) |
| [pricefeed/sidecar/providers/websocket/huobi/parse.go](pricefeed/sidecar/providers/websocket/huobi/parse.go) | [providers/websockets/huobi/parse.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/huobi/parse.go) |
| [pricefeed/sidecar/providers/websocket/kraken/handler.go](pricefeed/sidecar/providers/websocket/kraken/handler.go) | [providers/websockets/kraken/ws_data_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/kraken/ws_data_handler.go) |
| [pricefeed/sidecar/providers/websocket/kraken/messages.go](pricefeed/sidecar/providers/websocket/kraken/messages.go) | [providers/websockets/kraken/messages.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/kraken/messages.go) |
| [pricefeed/sidecar/providers/websocket/kraken/parse.go](pricefeed/sidecar/providers/websocket/kraken/parse.go) | [providers/websockets/kraken/parse.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/kraken/parse.go) |
| [pricefeed/sidecar/providers/websocket/kucoin/dial.go](pricefeed/sidecar/providers/websocket/kucoin/dial.go) | [providers/websockets/kucoin/hooks.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/kucoin/hooks.go) |
| [pricefeed/sidecar/providers/websocket/kucoin/handler.go](pricefeed/sidecar/providers/websocket/kucoin/handler.go) | [providers/websockets/kucoin/ws_data_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/kucoin/ws_data_handler.go) |
| [pricefeed/sidecar/providers/websocket/kucoin/messages.go](pricefeed/sidecar/providers/websocket/kucoin/messages.go) | [providers/websockets/kucoin/messages.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/kucoin/messages.go) |
| [pricefeed/sidecar/providers/websocket/kucoin/parse.go](pricefeed/sidecar/providers/websocket/kucoin/parse.go) | [providers/websockets/kucoin/parse.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/kucoin/parse.go) |
| [pricefeed/sidecar/providers/websocket/mexc/handler.go](pricefeed/sidecar/providers/websocket/mexc/handler.go) | [providers/websockets/mexc/ws_data_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/mexc/ws_data_handler.go) |
| [pricefeed/sidecar/providers/websocket/mexc/messages.go](pricefeed/sidecar/providers/websocket/mexc/messages.go) | [providers/websockets/mexc/messages.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/mexc/messages.go) |
| [pricefeed/sidecar/providers/websocket/mexc/parse.go](pricefeed/sidecar/providers/websocket/mexc/parse.go) | [providers/websockets/mexc/parse.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/mexc/parse.go) |
| [pricefeed/sidecar/providers/websocket/okx/handler.go](pricefeed/sidecar/providers/websocket/okx/handler.go) | [providers/websockets/okx/ws_data_handler.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/okx/ws_data_handler.go) |
| [pricefeed/sidecar/providers/websocket/okx/messages.go](pricefeed/sidecar/providers/websocket/okx/messages.go) | [providers/websockets/okx/messages.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/okx/messages.go) |
| [pricefeed/sidecar/providers/websocket/okx/parse.go](pricefeed/sidecar/providers/websocket/okx/parse.go) | [providers/websockets/okx/parse.go](https://github.com/skip-mev/connect/blob/9f85605735ddea817886fe8a26d309d761d242fe/providers/websockets/okx/parse.go) |

### MEXC websocket-proto

Comparison revision: `0c9c4f35dd0fadc3a46a350e909a93379d81e811`.

| Ark file | Upstream source |
| --- | --- |
| [pricefeed/sidecar/providers/websocket/mexc/wire.go](pricefeed/sidecar/providers/websocket/mexc/wire.go) | [PushDataV3ApiWrapper.proto](https://github.com/mexcdevelop/websocket-proto/blob/0c9c4f35dd0fadc3a46a350e909a93379d81e811/PushDataV3ApiWrapper.proto), [PublicMiniTickerV3Api.proto](https://github.com/mexcdevelop/websocket-proto/blob/0c9c4f35dd0fadc3a46a350e909a93379d81e811/PublicMiniTickerV3Api.proto) |

### Terra Classic

Comparison revision: `59a5b744c2dade15983907f05e00b4b2d07ffc67`.

| Ark file | Upstream source |
| --- | --- |
| [pkg/chain/blocks.go](pkg/chain/blocks.go) | [types/util/blocks.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/types/util/blocks.go) |
| [x/market/simulation/genesis.go](x/market/simulation/genesis.go) | [x/market/simulation/genesis.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/market/simulation/genesis.go) |
| [x/oracle/simulation/genesis.go](x/oracle/simulation/genesis.go) | [x/oracle/simulation/genesis.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/oracle/simulation/genesis.go) |

### Terra port lineage and generated source

The following files trace back to the Terra port, including subsequent rewrites
and splits. Market conversion policy came from the original parameter handling;
oracle ballot processing moved into the ABCI pipeline. Their notices record that
lineage and Ark's modifications without assigning upstream ownership to new code.
The Terra comparison revision above also applies to these source links.

| Ark file | Terra source lineage |
| --- | --- |
| [abci/oracle/aggregation.go](abci/oracle/aggregation.go) | [x/oracle/types/ballot.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/oracle/types/ballot.go) |
| [abci/oracle/ballot.go](abci/oracle/ballot.go) | [x/oracle/types/ballot.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/oracle/types/ballot.go) |
| [x/market/keeper/grpc_query.go](x/market/keeper/grpc_query.go) | [x/market/keeper/querier.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/market/keeper/querier.go) |
| [x/market/keeper/keeper.go](x/market/keeper/keeper.go) | [x/market/keeper/keeper.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/market/keeper/keeper.go) |
| [x/market/keeper/msg_server.go](x/market/keeper/msg_server.go) | [x/market/keeper/msg_server.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/market/keeper/msg_server.go) |
| [x/market/keeper/swap.go](x/market/keeper/swap.go) | [x/market/keeper/swap.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/market/keeper/swap.go) |
| [x/market/module/module.go](x/market/module/module.go) | [x/market/module.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/market/module.go) |
| [x/market/types/conversion_policy.go](x/market/types/conversion_policy.go) | [x/market/types/params.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/market/types/params.go) |
| [x/market/types/expected_keepers.go](x/market/types/expected_keepers.go) | [x/market/types/expected_keepers.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/market/types/expected_keepers.go) |
| [x/market/types/genesis.go](x/market/types/genesis.go) | [x/market/types/genesis.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/market/types/genesis.go) |
| [x/market/types/params.go](x/market/types/params.go) | [x/market/types/params.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/market/types/params.go) |
| [x/oracle/keeper/genesis.go](x/oracle/keeper/genesis.go) | [x/oracle/genesis.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/oracle/genesis.go) |
| [x/oracle/keeper/grpc_query.go](x/oracle/keeper/grpc_query.go) | [x/oracle/keeper/querier.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/oracle/keeper/querier.go) |
| [x/oracle/keeper/keeper.go](x/oracle/keeper/keeper.go) | [x/oracle/keeper/keeper.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/oracle/keeper/keeper.go) |
| [x/oracle/keeper/msg_server.go](x/oracle/keeper/msg_server.go) | [x/oracle/keeper/msg_server.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/oracle/keeper/msg_server.go) |
| [x/oracle/module/module.go](x/oracle/module/module.go) | [x/oracle/module.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/oracle/module.go) |
| [x/oracle/types/codec.go](x/oracle/types/codec.go) | [x/oracle/types/codec.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/oracle/types/codec.go) |
| [x/oracle/types/errors.go](x/oracle/types/errors.go) | [x/oracle/types/errors.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/oracle/types/errors.go) |
| [x/oracle/types/expected_keepers.go](x/oracle/types/expected_keepers.go) | [x/oracle/types/expected_keeper.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/oracle/types/expected_keeper.go) |
| [x/oracle/types/genesis.go](x/oracle/types/genesis.go) | [x/oracle/types/genesis.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/oracle/types/genesis.go) |
| [x/oracle/types/params.go](x/oracle/types/params.go) | [x/oracle/types/params.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/oracle/types/params.go) |
| [x/treasury/keeper/abci.go](x/treasury/keeper/abci.go) | [x/treasury/abci.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/treasury/abci.go) |
| [x/treasury/keeper/grpc_query.go](x/treasury/keeper/grpc_query.go) | [x/treasury/keeper/querier.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/treasury/keeper/querier.go) |
| [x/treasury/keeper/keeper.go](x/treasury/keeper/keeper.go) | [x/treasury/keeper/keeper.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/treasury/keeper/keeper.go) |
| [x/treasury/module/module.go](x/treasury/module/module.go) | [x/treasury/module.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/treasury/module.go) |
| [x/treasury/simulation/genesis.go](x/treasury/simulation/genesis.go) | [x/treasury/simulation/genesis.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/treasury/simulation/genesis.go) |
| [x/treasury/types/expected_keepers.go](x/treasury/types/expected_keepers.go) | [x/treasury/types/exptected_keepers.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/treasury/types/exptected_keepers.go) |
| [x/treasury/types/genesis.go](x/treasury/types/genesis.go) | [x/treasury/types/genesis.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/treasury/types/genesis.go) |
| [x/treasury/types/params.go](x/treasury/types/params.go) | [x/treasury/types/params.go](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/x/treasury/types/params.go) |
| [proto/ark/market/v1/genesis.proto](proto/ark/market/v1/genesis.proto) | [proto/terra/market/v1beta1/genesis.proto](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/proto/terra/market/v1beta1/genesis.proto) |
| [proto/ark/market/v1/market.proto](proto/ark/market/v1/market.proto) | [proto/terra/market/v1beta1/market.proto](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/proto/terra/market/v1beta1/market.proto) |
| [proto/ark/market/v1/query.proto](proto/ark/market/v1/query.proto) | [proto/terra/market/v1beta1/query.proto](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/proto/terra/market/v1beta1/query.proto) |
| [proto/ark/market/v1/tx.proto](proto/ark/market/v1/tx.proto) | [proto/terra/market/v1beta1/tx.proto](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/proto/terra/market/v1beta1/tx.proto) |
| [proto/ark/oracle/v1/genesis.proto](proto/ark/oracle/v1/genesis.proto) | [proto/terra/oracle/v1beta1/genesis.proto](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/proto/terra/oracle/v1beta1/genesis.proto) |
| [proto/ark/oracle/v1/oracle.proto](proto/ark/oracle/v1/oracle.proto) | [proto/terra/oracle/v1beta1/oracle.proto](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/proto/terra/oracle/v1beta1/oracle.proto) |
| [proto/ark/oracle/v1/query.proto](proto/ark/oracle/v1/query.proto) | [proto/terra/oracle/v1beta1/query.proto](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/proto/terra/oracle/v1beta1/query.proto) |
| [proto/ark/oracle/v1/tx.proto](proto/ark/oracle/v1/tx.proto) | [proto/terra/oracle/v1beta1/tx.proto](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/proto/terra/oracle/v1beta1/tx.proto) |
| [proto/ark/treasury/v1/genesis.proto](proto/ark/treasury/v1/genesis.proto) | [proto/terra/treasury/v1beta1/genesis.proto](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/proto/terra/treasury/v1beta1/genesis.proto) |
| [proto/ark/treasury/v1/query.proto](proto/ark/treasury/v1/query.proto) | [proto/terra/treasury/v1beta1/query.proto](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/proto/terra/treasury/v1beta1/query.proto) |
| [proto/ark/treasury/v1/treasury.proto](proto/ark/treasury/v1/treasury.proto) | [proto/terra/treasury/v1beta1/treasury.proto](https://github.com/terra-money/classic-core/blob/59a5b744c2dade15983907f05e00b4b2d07ffc67/proto/terra/treasury/v1beta1/treasury.proto) |

The originating `.proto` owns its four-line licence, lineage, modification, and
attribution notice, attached immediately before `package`. Gogo and gRPC retain
that notice. `proto/scripts/protocgen-notices.sh` also places it immediately after
the generated-file marker in Pulsar and gateway output, where generators otherwise
bury or omit it. `proto/scripts/protocgen-pulsar.sh` invokes this helper for both
full and standalone Pulsar generation. Preserve the schema notice when moving or
regenerating source; do not maintain a separate handwritten generated-file notice.
The helper rejects missing source metadata or malformed notices. A new schema with
upstream lineage needs a notice in the same format.

## Obtaining release source

Each binary release has a matching `*-source-<commit>.tar.gz` asset and SHA-256
file. Download that asset, rather than GitHub's automatically generated source
archive, which does not include dependency sources. The release also includes a
`*-notices.tar.gz` archive. Sources must remain available alongside binaries for
as long as required by their licences; do not rely on expiring CI artifacts.

Published container images contain `/usr/share/ark/corresponding-source.tar.gz`,
licence documents, and `/usr/share/ark/third-party-notices.tar.gz`. To extract
them without starting the node:

```sh
container=$(docker create IMAGE_REFERENCE)
docker cp "$container:/usr/share/ark/." ./ark-source
docker rm "$container"
```

This covers each published image, including nightly and manual builds. The local
development image target is not a redistribution artifact; publish the Dockerfile's
`distribution` target through the source-packaging workflow.

### Container operating-system sources

The distribution target also contains `/usr/share/ark/os/`. Its
`corresponding-source.tar.gz` supplies Alpine source archives, the exact aports
recipes, patches, configurations and installation scripts. `MANIFEST.json` maps
every installed runtime package to its version, architecture, source origin and
aports commit. It separately records the system archives actually linked into
`arkd` and their builder package versions; the builder and runtime can use different
Alpine versions. The application bundle covers Go, Rust, wasmvm and Herumi sources.
The OS bundle covers the linked Alpine runtimes as well as the separate OS tools.

OS programs retain their individual licences. For example, BusyBox is GPLv2-only;
Ark's GPLv3 `COPYING` does not replace that licence. Refer to the per-image manifest
and preserved source notices for the actual component terms, including compiler
runtime exceptions. `os/notices/` provides readable extracted notices; keep the
complete source archives and embedded notices too. Its `licence-texts/` directory
also includes standard SPDX texts, checked against the bundled SPDX source data.
Template placeholders in those standard texts do not identify copyright holders;
the original sources and recipe attributions remain authoritative.

The extraction command above includes this directory. Check and unpack it with:

```sh
cd ark-source/os
sha256sum -c corresponding-source.tar.gz.sha256
mkdir unpacked
tar -xzf corresponding-source.tar.gz -C unpacked
python3 /path/to/ark/contrib/scripts/package-os-source.py verify unpacked
```

The collector evaluates official aports recipes in a separate unprivileged build
stage, checks their source hashes, and fails on missing coverage. Its inventory
comes from the final runtime stage and the node's linker map, not a second package
installation. Collection requires network access; verification of the extracted
bundle does not. The bundle's README describes rebuilding with Alpine's `abuild`.
Matching build tools and build dependencies are prerequisites; this check does not
rebuild every OS package or establish bit-identical binaries.

Retain each image's matching OS and application sources whenever retaining or
redistributing that image. A later image's bundle cannot substitute for an older
package version. Historical images and standalone binary toolchains need their own
source and notice review; this container inventory does not establish their coverage.

### Standalone node runtime sources

Node releases also attach `arkd-runtime-source-<commit>-linux-<arch>.tar.gz`, its
SHA-256 file, a package/source manifest, a build record and a readable notices
archive. Download the runtime sources for your binary's architecture together with
the application source bundle. Each binary archive includes its runtime manifest,
build record and notices. The pure-Go sidecar does not use this C/C++ runtime bundle.

GoReleaser's release adapter builds the committed application export with each
Linux architecture's compiler in the shared container builder. It records actual
linker inputs and retrieves corresponding Alpine recipes, sources and notices for
the linked runtimes, including musl and applicable GCC Runtime Library Exception
text. The standalone bundle excludes container programs that are not shipped with
the executable. Installed compiler packages may supply runtime object files; their
presence in this inventory does not mean the whole compiler is inside the node.

The build record ties the target, normalised build arguments, source revision and
application-source hash to the binary and runtime-source hashes. The adapter
checks this pairing and the source inventory before returning the binary to
GoReleaser. A missing or mismatched bundle fails the build. Base image digests are
pinned; APK versions resolved during the build are recorded with exact source
commits. This records provenance without claiming that future APK resolution is
frozen or that the build is bit-for-bit reproducible.

Extract runtime sources and verify them using `package-os-source.py verify` as
above. Retain application and runtime sources with the matching standalone
release; a later compiler's source archive is not a substitute. General-purpose
build tools remain prerequisites. Historical binaries distributed through other
paths need their own review.

## Preparing and checking a source bundle

Commit the intended release changes first. Packaging exports only `HEAD` and
refuses tracked changes or a moving `HEAD`; untracked local files and Git history
are excluded. Run from the root:

```sh
make source-bundle SOURCE_KIND=arkd
make verify-source SOURCE_KIND=arkd
# Use SOURCE_KIND=pricefeed for the separate sidecar release.
```

Packaging needs Python 3.12 or later, Git, the Go version in `go.mod`, and network
access to download checksum-verified Go dependencies. Node/image bundles also
need Cargo or Docker to vendor the locked Rust dependencies, and Git to retrieve
Herumi's native source submodules. The Herumi checkout is compared with its
checksum-verified Go module before its submodules are included.

Verification requires Docker, including support for Linux amd64 and arm64.
Preparing the verifier images downloads general-purpose compiler and OS build
tools. The verifier pins Rust 1.82.0 to match wasmvm v3.0.7's Alpine builder;
newer Rust releases removed a stack-probe symbol required by its Wasmer version.
Compilation then runs with networking disabled and empty Go caches. Both
node architectures rebuild wasmvm and Herumi from source; sidecar verification
cross-compiles its four published OS/architecture combinations. A successful
check proves the bundle supplies buildable source, not bit-for-bit reproducibility
of upstream prebuilt native libraries or the final release executable. This gate
does not execute the resulting node: runtime and CPU compatibility need native
hardware testing, because CPU probes in cryptographic libraries can fail under
foreign-architecture emulation.

For a manual build, extract the bundle, set `GOPROXY=file:///absolute/path/to/go-proxy`,
`GOSUMDB=off`, `GOTOOLCHAIN=local`, and `GOWORK=off`, then build from `ark/`.
`ark/contrib/scripts/verify-source.sh` records the native rebuild commands.
`SOURCE-MANIFEST.json` records the source revision, Go dependency versions and
checksums, target matrix, wasmvm lockfile hash, and Herumi submodule revisions.
General-purpose toolchains and OS build tools are prerequisites. Operational
credentials, validator keys, local data, and prior Git history are not build inputs.

## Maintainer release obligations

Keep licence and attribution documents with every distribution, preserve notices
in copied or modified upstream files, and provide the corresponding source for
the actual shipped version. Do not add restrictions that prevent recipients from
exercising their GPLv3 rights in the combined node. Original Ark source remains
available under Apache 2.0. A source archive alone does not discharge every
third-party obligation: inspect new dependencies, embedded binaries, and native
libraries whenever the dependency graph or release toolchain changes.
