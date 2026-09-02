# Images built from the working tree. VERSION and COMMIT are passed in so the
# build context can leave .git behind.
ROOT    := $(shell git rev-parse --show-toplevel)
VERSION := $(shell git describe --tags --always --dirty)
COMMIT  := $(shell git rev-parse HEAD)

all: arkd-env

arkd-env:
	docker build --tag ark/arkd -f arkd-env/Dockerfile \
		--build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) $(ROOT)

.PHONY: all arkd-env
