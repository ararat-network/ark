###############################################################################
###                                 Build                                   ###
###############################################################################

VERSION := $(shell git describe --tags --always --dirty)
COMMIT := $(shell git rev-parse HEAD)

# version.Name and version.AppName are set in app/config.go rather than here.
# They are chain identity, and Name decides the keyring service name, so they
# have to hold under `go build` and `go test` too — neither passes ldflags.
# Only the halves that genuinely vary per build are stamped.
ldflags = -X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
	-X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT)

BUILD_FLAGS := -ldflags '$(ldflags)'

build:
	@go build $(BUILD_FLAGS) -o build/arkd ./cmd/arkd

install:
	@go install $(BUILD_FLAGS) ./cmd/arkd

###############################################################################
###                                Protobuf                                 ###
###############################################################################

DOCKER := $(shell which docker)
protoVer=0.18.0
protoImageName=ghcr.io/cosmos/proto-builder:$(protoVer)
protoImage=$(DOCKER) run --rm -v $(CURDIR):/workspace -v ark-proto-cache:/root/.cache --workdir /workspace $(protoImageName)

proto-all: proto-format proto-lint proto-gen

proto-gen:
	@echo "Generating Protobuf files"
	@$(protoImage) sh ./scripts/protocgen.sh
	@go mod tidy

proto-format:
	@$(protoImage) find ./proto -name "*.proto" -exec clang-format -i {} \;

proto-lint:
	@$(protoImage) buf lint proto --error-format=json

proto-check-breaking:
	@$(protoImage) buf breaking proto --against $(HTTPS_GIT)#branch=main

proto-update-deps:
	@echo "Updating Protobuf dependencies"
	@$(protoImage) buf mod update proto

.PHONY: build install proto-all proto-gen proto-format proto-lint proto-check-breaking proto-update-deps
