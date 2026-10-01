GO ?= go
BINARY := spotify-playlist-filler
CONFIG ?= config.yml

.DEFAULT_GOAL := help
.PHONY: help run exec build build-all test race vet fmt check clean

help:
	@printf '%s\n' 'Targets: run, build, build-all, test, race, vet, fmt, check, clean' 'Override GO or CONFIG as needed.'

run:
	$(GO) run ./cmd/$(BINARY) -config "$(CONFIG)"

# Compatibility alias for the previous Makefile.
exec: run

build:
	mkdir -p bin
	$(GO) build -trimpath -o bin/$(BINARY) ./cmd/$(BINARY)

build-all:
	GO="$(GO)" bash ./build.sh

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

check: test vet
	@test -z "$$(gofmt -l cmd internal)" || { printf '%s\n' 'Run make fmt'; exit 1; }

clean:
	rm -rf bin build coverage.out
