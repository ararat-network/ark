###############################################################################
###                                Protobuf                                 ###
###############################################################################

DOCKER := $(shell which docker)
protoVer=0.18.0
protoImageName=ghcr.io/cosmos/proto-builder:$(protoVer)
protoImage=$(DOCKER) run --rm -v $(CURDIR):/workspace -v noah-proto-cache:/root/.cache --workdir /workspace $(protoImageName)

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

.PHONY: proto-all proto-gen proto-format proto-lint proto-check-breaking proto-update-deps
