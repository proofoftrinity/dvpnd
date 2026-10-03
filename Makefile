.DEFAULT_GOAL := default
VERSION := $(shell git describe --tags --match 'v*' | sed 's/^v//' | rev | cut -d - -f 2- | rev)
COMMIT := $(shell git log -1 --format='%H')

TAGS := $(strip netgo)
LD_FLAGS := -s -w \
	-X github.com/cosmos/cosmos-sdk/version.Name=dvpnd \
	-X github.com/cosmos/cosmos-sdk/version.AppName=dvpnd \
	-X github.com/cosmos/cosmos-sdk/version.Version=${VERSION} \
	-X github.com/cosmos/cosmos-sdk/version.Commit=${COMMIT} \
	-X github.com/cosmos/cosmos-sdk/version.BuildTags=${TAGS}

.PHONY: check-architecture
check-architecture:
	@bash scripts/check-architecture-doc.sh

# The regression gate (test/README.md): every change passes it before it is
# committed. Formatting, vet (the integration tests included), every unit test
# with the race detector, SPDX headers, the licence rule and the provenance
# record. No network, no root, no Docker.
GO_FILES = $$(git ls-files --cached --others --exclude-standard '*.go')
.PHONY: check
check:
	@unformatted=$$(gofmt -l $(GO_FILES)); \
	  [ -z "$$unformatted" ] || { echo "gofmt: not formatted:"; echo "$$unformatted"; exit 1; }
	go vet ./...
	go vet -tags integration ./test/integration/
	go test -race -count=1 ./...
	@missing=$$(grep -L 'SPDX-License-Identifier: Apache-2.0' $(GO_FILES)); \
	  [ -z "$$missing" ] || { echo "missing SPDX header:"; echo "$$missing"; exit 1; }
	@! grep -q 'sentinel-official/sentinel-go-sdk' go.mod go.sum || \
	  { echo "sentinel-go-sdk carries no licence and must not be a dependency"; exit 1; }
	@out=$$(bash docs/provenance/verify-fork.sh 2>&1) || { echo "$$out"; exit 1; }; echo "$$out" | tail -1

# The integration suite (test/README.md): every protocol with its
# real daemon and a real client, in Docker. Run it for any change to a service,
# the egress policy, the runtime, the Dockerfile, the runner or the unit.
.PHONY: check-integration
check-integration:
	@bash test/integration/run.sh

.PHONY: check-all
check-all: check check-integration

.PHONY: benchmark
benchmark:
	@go test -bench -mod=readonly -v ./...

.PHONY: build
build:
	go build -ldflags="${LD_FLAGS}" -mod=readonly -tags="${TAGS}" -trimpath \
		-o ./bin/dvpnd main.go

.PHONY: clean
clean:
	rm -rf ./bin ./vendor

.PHONY: default
default: clean build

.PHONY: install
install:
	go build -ldflags="${LD_FLAGS}" -mod=readonly -tags="${TAGS}" -trimpath \
		-o "${GOPATH}/bin/dvpnd" main.go

.PHONY: build-image
build-image:
	@docker build --compress --file Dockerfile --force-rm --tag dvpnd .

.PHONY: go-lint
go-lint:
	@golangci-lint run --fix

.PHONY: test
test:
	@go test -cover -mod=readonly -v ./...

.PHONY: tools
tools:
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.50.1
