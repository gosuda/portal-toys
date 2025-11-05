.PHONY: tunnel install run help clean

BIN_DIR   := $(CURDIR)/bin
BIN       := $(BIN_DIR)/portal-tunnel$(if $(filter Windows_NT,$(OS)),.exe,)
PKG       := gosuda.org/portal/cmd/portal-tunnel
VERSION   := v1.3.3
GOINSTALL := $(if $(filter Windows_NT,$(OS)),set "GOBIN=$(BIN_DIR)" &&,GOBIN="$(BIN_DIR)") go install $(PKG)@$(VERSION)
RELAY_URL := wss://portal.gosuda.org/relay
PORT ?= 8080

tunnel: tunnel-install tunnel-run

tunnel-install:
	@$(GOINSTALL)

tunnel-run:
	"$(BIN)" expose --port $(PORT) --host 127.0.0.1 --relay "$(RELAY_URL)"

tunnel-help:
	"$(BIN)" --help

clean:
	rm -f "$(BIN)"
