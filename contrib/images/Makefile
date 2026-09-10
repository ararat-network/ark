# Images built from the working tree. The version stamps are passed in so the
# build context can leave .git behind; each binary describes its own tag line,
# as in the root Makefile.
ROOT              := $(shell git rev-parse --show-toplevel)
VERSION           := $(shell git describe --tags --match 'v[0-9]*' --always --dirty)
PRICEFEED_VERSION := $(shell git describe --tags --match 'pricefeed/v[0-9]*' --always --dirty | sed 's|^pricefeed/||')
COMMIT            := $(shell git rev-parse HEAD)

all: arkd-env

arkd-env:
	docker build --tag ark/arkd -f arkd-env/Dockerfile \
		--build-arg VERSION=$(VERSION) --build-arg PRICEFEED_VERSION=$(PRICEFEED_VERSION) \
		--build-arg COMMIT=$(COMMIT) $(ROOT)

.PHONY: all arkd-env
