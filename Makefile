.PHONY: build install uninstall test vet check fmt clean status doctor hook-log ci help

BINARY := gochecks
GOBIN  := $(shell go env GOPATH)/bin

help: ## List available targets
	@awk 'BEGIN{FS=":.*##"; printf "Targets:\n"} /^[a-zA-Z_-]+:.*?##/ {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the binary into ./gochecks
	go build -o $(BINARY) .

install: ## Install gochecks to $(GOPATH)/bin, where the hook expects it
	go install .
	@echo "installed to $(GOBIN)/$(BINARY)"

uninstall: ## Remove installed gochecks
	rm -f $(GOBIN)/$(BINARY)

test: ## Run go test on the package
	go test ./...

vet: ## Run go vet
	go vet ./...

fmt: ## gofmt -s -w on the tree
	gofmt -s -w .

check: build ## Dogfood: run gochecks check against its own sources
	./$(BINARY) check --changed "$$(git ls-files '*.go' | tr '\n' ' ')" --text

status: build ## Dogfood: show gochecks's own status
	./$(BINARY) status

doctor: build ## Toolchain and tool health
	./$(BINARY) doctor

hook-log: ## Tail the hook invocation log
	tail -f $$(./$(BINARY) status | awk '/^last/{gsub(/[():]/,""); print $$NF; exit}')

clean: ## Remove built binary
	rm -f $(BINARY)

ci: fmt vet test build check ## What CI should run
