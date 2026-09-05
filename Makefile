###############################################################################
###                                 Build                                   ###
###############################################################################

VERSION := $(shell git describe --tags --always --dirty)
COMMIT := $(shell git rev-parse HEAD)

# Comma-separated go build tags; none by default.
BUILD_TAGS ?=

# version.Name and version.AppName are set in app/config.go rather than here.
# They are chain identity, and Name decides the keyring service name, so they
# have to hold under `go build` and `go test` too — neither passes ldflags.
# Only the halves that genuinely vary per build are stamped.
ldflags = -X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
	-X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT) \
	-X "github.com/cosmos/cosmos-sdk/version.BuildTags=$(BUILD_TAGS)"
ldflags += $(LDFLAGS)

BUILD_FLAGS := -tags "$(BUILD_TAGS)" -mod=readonly -trimpath -ldflags '$(strip $(ldflags))'

build:
	@go build $(BUILD_FLAGS) -o build/arkd ./cmd/arkd

install:
	@go install $(BUILD_FLAGS) ./cmd/arkd

clean:
	@rm -rf build/ coverage.out

###############################################################################
###                                 Tests                                   ###
###############################################################################

test:
	@go test -mod=readonly ./...

test-race:
	@go test -mod=readonly -race ./...

test-cover:
	@go test -mod=readonly -covermode=atomic -coverprofile=coverage.out ./...

###############################################################################
###                               Simulation                                ###
###############################################################################

test-sim:
	@go test ./app -failfast -mod=readonly -timeout 30m -tags=sims -run TestFullAppSimulation -NumBlocks=50

test-sim-nondeterminism:
	@go test ./app -failfast -mod=readonly -timeout 30m -tags=sims -run TestAppStateDeterminism -NumBlocks=100 -BlockSize=200

test-sim-import-export:
	@go test ./app -failfast -mod=readonly -timeout 20m -tags=sims -run TestAppImportExport -NumBlocks=50

test-sim-after-import:
	@go test ./app -failfast -mod=readonly -timeout 30m -tags=sims -run TestAppSimulationAfterImport -NumBlocks=50

# Small blocks on purpose: the fuzzer wants many short runs, and it kills a
# worker that does not report an iteration promptly. At the default simulation
# size one iteration takes the better part of a minute and every run dies as
# "hung or terminated unexpectedly".
test-sim-fuzz:
	@go test ./app -failfast -mod=readonly -timeout 3m -tags=sims -run ^$$ -fuzz FuzzFullAppSimulation -fuzztime 2m -NumBlocks=10 -BlockSize=50

test-sim-benchmark:
	@go test ./app -failfast -mod=readonly -timeout 24h -tags=sims -benchmem -run ^$$ -bench BenchmarkFullAppSimulation -NumBlocks=100 -BlockSize=200

###############################################################################
###                                Linting                                  ###
###############################################################################

lint:
	@golangci-lint run ./...

lint-fix:
	@golangci-lint run ./... --fix

format:
	@golangci-lint fmt

###############################################################################
###                                Security                                 ###
###############################################################################

# govulncheck reports only vulnerabilities on reachable call paths, so its
# findings are ones this code can actually hit rather than every advisory
# touching a module in go.mod. It is not a merge gate: some findings have no
# fixed version upstream, and a permanently red gate stops being read.
#
# The binary comes from the pinned toolchain, like golangci-lint above: an
# unpinned scanner can turn the nightly red on its own release.
vulncheck:
	@govulncheck ./...

###############################################################################
###                                Protobuf                                 ###
###############################################################################

HTTPS_GIT := https://github.com/ararat-network/ark.git

DOCKER := $(shell which docker)
protoVer=0.18.1
protoImageName=ghcr.io/cosmos/proto-builder:$(protoVer)
# expanded per-recipe so only the proto targets require docker
protoImage=$(if $(DOCKER),,$(error docker is required for the proto targets))$(DOCKER) run --rm -v $(CURDIR):/workspace -v ark-proto-cache:/root/.cache --workdir /workspace $(protoImageName)

proto-all: proto-format proto-lint proto-gen

proto-gen:
	@echo "Generating Protobuf files"
	@$(protoImage) sh ./proto/scripts/protocgen.sh
	@go mod tidy

proto-format:
	@$(protoImage) buf format -w proto

proto-lint:
	@$(protoImage) buf lint proto --error-format=json

proto-check-breaking:
	@$(protoImage) buf breaking proto --against $(HTTPS_GIT)#branch=main

proto-update-deps:
	@echo "Updating Protobuf dependencies"
	@$(protoImage) buf mod update proto

###############################################################################
###                                Localnet                                 ###
###############################################################################

# VALIDATORS picks the shape: 4 (default) or 1. The compose file has fixed
# services, so any other count has no containers to land on. node1..3 and
# their sidecars sit behind the "multi" profile.
VALIDATORS ?= 4
localnetImage=ark/arkd
localnetCompose=$(DOCKER) compose -f contrib/localnet/docker-compose.yml
localnetProfile=$(if $(filter 1,$(VALIDATORS)),,--profile multi)

localnet-check:
	@$(if $(filter 1 4,$(VALIDATORS)),:,$(error VALIDATORS must be 1 or 4, got $(VALIDATORS)))

# Build ark/arkd from the working tree: static musl arkd and pricefeed on Alpine.
localnet-build-env:
	$(MAKE) -C contrib/images arkd-env

# Generate .testnets/: one validator home per node, each paired with a sidecar
# config, plus the emergency committee keyring. The directory is disposable;
# init wipes it from inside the container, and the mode lets the container's
# uid write to it on a Linux host.
localnet-init: localnet-check
	@mkdir -p $(CURDIR)/.testnets && chmod 0777 $(CURDIR)/.testnets
	$(DOCKER) run --rm -e VALIDATORS=$(VALIDATORS) -v $(CURDIR)/.testnets:/data \
		-v $(CURDIR)/contrib/localnet/init.sh:/init.sh:ro \
		--entrypoint sh $(localnetImage) /init.sh

localnet-up: localnet-check
	$(localnetCompose) $(localnetProfile) up -d

# Validators and price-feed sidecars, RPC on 26657, REST on 1317.
localnet-start: localnet-stop localnet-build-env localnet-init localnet-up

# Every profile is named so their services come down too: a service in a
# profile that is not enabled is not an orphan, and down would skip it.
localnet-stop:
	$(localnetCompose) --profile multi --profile statesync down --remove-orphans

# Wait for node0 to pass five blocks and hold an oracle exchange rate.
localnet-liveness:
	@contrib/scripts/localnet-liveness.sh 90 2 5 http://localhost:26657 http://localhost:1317

# Drive docs/EMERGENCY_SUBMISSION_RUNBOOK.md against the running localnet:
# offline multisig ceremony, dark-carrier submission, leak and inclusion
# checks. Needs the four-validator shape: the carrier is the last node and
# the leak check reads the others' mempools.
localnet-runbook: localnet-check
	$(DOCKER) run --rm --network ark-localnet -e VALIDATORS=$(VALIDATORS) -v $(CURDIR)/.testnets:/data \
		-v $(CURDIR)/contrib/scripts/runbook-emergency-suspend.sh:/runbook.sh:ro \
		--entrypoint sh $(localnetImage) /runbook.sh

# Bring up the state-sync client and hold it to a snapshot restore. Needs the
# four-validator shape: sync0's light client wants two RPC servers, and the
# snapshots it restores from are node0's.
localnet-statesync:
	@$(if $(filter 4,$(VALIDATORS)),:,$(error localnet-statesync needs VALIDATORS=4, got $(VALIDATORS)))
	$(localnetCompose) --profile statesync up -d sync0
	@contrib/scripts/localnet-statesync.sh 120 2 http://localhost:26697

# Coordinated upgrade on one host node under cosmovisor; see the script's
# header for the knobs.
upgrade-rehearsal:
	@contrib/scripts/upgrade-rehearsal.sh

.PHONY: build install clean test test-race test-cover \
	test-sim test-sim-nondeterminism test-sim-import-export test-sim-after-import test-sim-fuzz test-sim-benchmark \
	lint lint-fix format vulncheck \
	proto-all proto-gen proto-format proto-lint proto-check-breaking proto-update-deps \
	localnet-check localnet-build-env localnet-init localnet-up localnet-start localnet-stop localnet-liveness localnet-runbook localnet-statesync upgrade-rehearsal
