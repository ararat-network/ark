# Testing Ark

Most tests live beside the implementation. This directory holds two tiers above them: [integration](integration/)
runs cross-module scenarios in-process against the real application, with [fixture_test.go](integration/fixture_test.go)
constructing funded validators, driving blocks, and supplying oracle reports; [e2e](e2e/README.md) runs the built
`arkd` image on a Docker network under interchaintest, driven through the CLI, REST, and RPC, with Hermes between two
chains. The e2e tree is its own Go module and needs Docker; see its README for the packages and the variables.

## Choose the scope

Run commands from the repository root using the toolchain in [mise.toml](../mise.toml).

| Change | Starting verification |
| --- | --- |
| One module | `go test ./x/<module>/...` |
| Ante or pending pool | `go test ./app/ante/... ./app/mempool/...`, then relevant `./app` tests |
| Application wiring or cross-module behaviour | `go test ./app/... ./tests/integration/...` |
| CLI, node configuration, relayer, export, or anything a validator or integrator runs against a live node | `make test-e2e-vet`, then the matching package under `make test-e2e E2E_PACKAGES=./<package>/...` |
| Pricefeed runtime or transport | Focused packages under `./pricefeed/...` and affected `./cmd/pricefeed/cmd` tests |
| Shared primitive | Its `./pkg/<name>/...` tests and affected callers |
| Repository-wide verification | `make test`, `make lint`, and `go build ./...` |

`make test-race` and `make test-cover` run the full tree with race instrumentation or coverage. Prefer focused race runs
for a narrow concurrency change. Some tests bind loopback sockets, start in-memory SDK apps, or use Wasm native libraries;
a sandbox bind/cache failure is an environment issue, not by itself a failing product assertion.

## Fixtures and conventions

Keeper tests use module-local fixtures; type validation/parsing tests use table-driven functions. Start validation cases
from default params/genesis and mutate one field; test accepted boundaries as well as errors. Shared application fixtures
live in [app/testutil](../app/testutil/). [Embedded Wasm fixtures](../app/testdata/contracts.go) support application tests.
Metric golden fixtures and their intentional update workflow are owned by [telemetry](../pkg/telemetry/README.md).

## Simulation and live rehearsals

The [Makefile](../Makefile) exposes `test-sim`, `test-sim-nondeterminism`, `test-sim-import-export`,
`test-sim-after-import`, `test-sim-fuzz`, and `test-sim-benchmark`. They select the `sims` build tag, target tests,
block counts, and timeouts; inspect the target before starting a long run. Module factories live in `x/*/simulation`.

The [Docker localnet](../contrib/localnet/README.md) and [rehearsal scripts](../contrib/scripts/README.md) exercise
process/network behaviour: rates and block liveness, private emergency submission, saturation, state sync, and upgrades.
These are separate from in-memory Go tests and require their stated disposable environment.

## CI map

[Go tests](../.github/workflows/test.yml), [lint](../.github/workflows/lint.yml),
[simulations](../.github/workflows/sims.yml), [localnet](../.github/workflows/localnet.yml),
[e2e](../.github/workflows/e2e.yml), and [upgrades](../.github/workflows/upgrade.yml) define their current triggers and
gates. The e2e suites run per pull request on the working tree; nightly they start on the latest release image and
upgrade to the working tree first, so a release-to-release migration is rehearsed against every suite. The nightly
workflows file one issue per workflow on failure through [nightly-failure.yml](../.github/workflows/nightly-failure.yml).
[CodeQL](../.github/workflows/codeql.yml) and [vulnerability scanning](../.github/workflows/vulncheck.yml) provide security
checks; `make vulncheck` runs the reachable-call scanner locally. Image publication is configured separately in
[docker-push.yml](../.github/workflows/docker-push.yml); [THREAT_MODEL.md](../docs/design/THREAT_MODEL.md) owns release trust.
## Height-sensitive fixtures

Apply keeper writes through `NewNextBlockContext` before driving `FinalizeBlock`/`Commit` in app scenarios. The SDK
flushes the finalise branch inside `FinalizeBlock`; a keeper write after that call can be discarded and can make a
fixture imply state was visible earlier than it was on chain. Height-sensitive query assertions should call a query
server with the intended context; `baseapp.NewQueryServerTestHelper` captures its context at construction.
