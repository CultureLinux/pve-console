APP := pve-console
GO ?= go
BIN_DIR := bin
VERSION ?= dev
COMMIT ?= none
DATE ?= unknown
LDFLAGS := -s -w -X pvc/internal/version.Version=$(VERSION) -X pvc/internal/version.Commit=$(COMMIT) -X pvc/internal/version.Date=$(DATE)

.PHONY: build test test-race cover fmt vet clean screens

build:
	mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP) ./cmd/pvm

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

cover:
	mkdir -p $(BIN_DIR)
	$(GO) test -coverprofile=$(BIN_DIR)/coverage.out ./...
	$(GO) tool cover -func=$(BIN_DIR)/coverage.out | tail -n 1

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

clean:
	rm -rf $(BIN_DIR)

# Regenerates the animated captures of the README (docs/media/*.svg) from the
# demo UI. Needs no terminal, no recorder: see internal/ui/screens_test.go.
screens:
	PVC_SCREENS_DIR=$(CURDIR)/docs/media $(GO) test ./internal/ui -run TestGenerateScreens -count=1
