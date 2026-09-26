APP := pve-console
GO ?= go
BIN_DIR := bin
VERSION ?= dev
COMMIT ?= none
DATE ?= unknown
LDFLAGS := -s -w -X pvc/internal/version.Version=$(VERSION) -X pvc/internal/version.Commit=$(COMMIT) -X pvc/internal/version.Date=$(DATE)

.PHONY: build test fmt vet clean

build:
	mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP) ./cmd/pvm

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

clean:
	rm -rf $(BIN_DIR)
