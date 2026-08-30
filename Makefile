# aubade — build and verification.
#
# `make check` is the gate. It is the single definition of "green": the
# pre-commit hook, the Stop hook, and CI all call it and nothing else, so
# there is exactly one place where the bar can be raised or (visibly) lowered.
# See VERIFICATION.md.

SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c

GO      ?= go
BIN     := bin
PKGS    := ./...
BINARIES := aubade aubade-lab

.DEFAULT_GOAL := check
.PHONY: all check vet build build-all test e2e clean fmt fmt-check hooks help

all: build

## check: the gate — vet, build, unit tests, then the end-to-end regression run.
check: vet build-all test e2e
	@printf '\n\033[32m==> make check: GREEN\033[0m\n'

## vet: static analysis over every package.
vet:
	@printf '\033[1m==> go vet\033[0m\n'
	$(GO) vet $(PKGS)

## build-all: compile every package (catches breakage in packages with no binary).
build-all:
	@printf '\033[1m==> go build\033[0m\n'
	$(GO) build $(PKGS)

## build: produce both binaries in ./bin.
build: $(BIN)
	@printf '\033[1m==> building binaries into $(BIN)/\033[0m\n'
	@for b in $(BINARIES); do \
		$(GO) build -o $(BIN)/$$b ./cmd/$$b && echo "    $(BIN)/$$b"; \
	done

$(BIN):
	@mkdir -p $(BIN)

## test: unit tests.
test:
	@printf '\033[1m==> go test\033[0m\n'
	$(GO) test $(PKGS)

## e2e: the end-to-end regression run (generate -> digest --no-llm -> eval).
e2e: build
	@printf '\033[1m==> end-to-end regression\033[0m\n'
	@./scripts/e2e-regression.sh

## fmt: rewrite sources with gofmt.
fmt:
	$(GO) fmt $(PKGS)

## fmt-check: fail if anything is unformatted (advisory; not in the gate yet).
fmt-check:
	@unformatted=$$(gofmt -l . | grep -v '^vendor/' || true); \
	if [ -n "$$unformatted" ]; then \
		echo "unformatted files:"; echo "$$unformatted"; exit 1; \
	fi

## hooks: install the local git hooks into this clone.
hooks:
	@./scripts/install-hooks.sh

## clean: remove build and run artifacts.
clean:
	rm -rf $(BIN) out

## help: list targets.
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'
