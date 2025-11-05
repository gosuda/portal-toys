.PHONY: tunnel install run help clean

PKG      := gosuda.org/portal/cmd/portal-tunnel
VERSION  := v1.3.2
BIN_DIR  := $(CURDIR)/bin
BIN      := $(BIN_DIR)/portal-tunnel$(if $(filter Windows_NT,$(OS)),.exe,)
GOINSTALL := $(if $(filter Windows_NT,$(OS)),set "GOBIN=$(BIN_DIR)" &&,GOBIN="$(BIN_DIR)") go install $(PKG)@$(VERSION)

tunnel: tunnel-install tunnel-run

tunnel-install:
	@$(GOINSTALL)

tunnel-run:
	"$(BIN)" expose --port 8080 --host localhost --relay "wss://portal.gosuda.org/relay"

tunnel-help:
	"$(BIN)" --help

clean:
	rm -f "$(BIN)"
