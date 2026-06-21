SHELL := bash

.PHONY: help build test coverage lint format
.DEFAULT_GOAL := help

# Run tasks through the nix dev shell. Inside the shell (tools already on PATH)
# run directly; outside it, enter the shell for each command.
ifeq (,$(shell command -v dprint 2>/dev/null))
NIX := nix develop -c
endif

define exec
	@printf '\033[1;36m%s\033[0m\n' "$(1)"
	@$(NIX) bash -c '$(1)'
endef

help:
	@printf '\033[1;32mAvailable targets:\033[0m\n'
	@grep -E '^[a-zA-Z_-]+:.*# ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*# "}{printf "  \033[1;36m%-12s\033[0m %s\n", $$1, $$2}'

build:  # Build the acron binary
	$(call exec,go build -o acron .)

test:  # Run tests
	$(call exec,go test ./...)

coverage:  # Run tests with coverage
	$(call exec,go test -cover ./...)

lint:  # Lint code and check formatting
	$(call exec,! gofumpt -l . | grep -q .)
	$(call exec,go vet ./...)
	$(call exec,golangci-lint run)
	$(call exec,dprint check)

format:  # Format code
	$(call exec,gofumpt -w .)
	$(call exec,dprint fmt)
	$(call exec,nix fmt)
