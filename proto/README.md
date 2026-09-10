# Protocol schemas and generation

This tree owns handwritten `.proto` sources and the dual Go generation workflow. Fields, messages, RPCs, and wire contracts
are changed here; generated outputs are reviewed but not edited directly.

## Source and output map

| Source | gogo output | Pulsar / standard gRPC output |
| --- | --- | --- |
| `ark/<module>/v1` | `x/<module>/types` | `api/ark/<module>/v1` |
| `ark/<module>/module/v1` | As selected by `go_package` | `api/ark/<module>/module/v1` |
| `ark/abci/v1` | `abci/voteextension/types` | `api/ark/abci/v1` |
| `ark/pricefeed/v1` | `pricefeed/api` | `api/ark/pricefeed/v1` |
| `ark/mandate/v1` | `pkg/mandate` | `api/ark/mandate/v1` |

[buf.gen.gogo.yaml](buf.gen.gogo.yaml) runs gocosmos and grpc-gateway. [buf.gen.yaml](buf.gen.yaml) uses managed mode,
go-pulsar, and go-grpc. The module `go_package` selects typed gogo output; runtime/depinject imports use generated
`api/` types. Module implementation imports its `x/<module>/types` package rather than the Pulsar runtime representation.

## Editing and regeneration

Follow the [repository protobuf conventions](../AGENTS.md#protobuf-generation) and
[annotation rules](../AGENTS.md#project-context), including signer/address/scalar annotations, Amino handling, and
non-nullable custom math fields. Propose schema changes before editing as required by the repository guidelines.

From the repository root, with Docker running and the pinned Go toolchain available:

```sh
make proto-format
make proto-lint
make proto-gen
```

The [Makefile](../Makefile) pins the proto-builder image. It mounts the workspace and the `ark-proto-cache` named volume;
[protocgen.sh](scripts/protocgen.sh) generates and relocates gogo output, then invokes
[protocgen-pulsar.sh](scripts/protocgen-pulsar.sh) to refresh generated API files. Scripts run under the image's shell.
`make proto-gen` also runs `go mod tidy` on the host, so inspect dependency changes as well as generated files.

Run focused module/transport tests, `git diff --check`, and a build appropriate to the changed surface. Do not run
generation for a prose-only edit. `make proto-check-breaking` compares schemas against the configured main branch and
requires access to that source. [Pricefeed compatibility](../docs/PRICEFEED_OPERATIONS.md#nodesidecar-compatibility)
constrains changes to the independently released node–sidecar service.

Protocol semantics live in [docs/](../docs/README.md); fields and local contracts belong in schema comments. The
[application map](../app/README.md) and module READMEs explain how generated services are wired.
