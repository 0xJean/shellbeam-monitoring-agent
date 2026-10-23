GO ?= go
PYTHON ?= python3
DIST ?= dist
AGENT_VERSION ?= 1.5.12
VERSION_HASH ?= $(shell git rev-parse --verify HEAD 2>/dev/null || printf unknown)
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
MODULE := github.com/komari-monitor/komari-agent
LDFLAGS := -s -w -X $(MODULE)/update.CurrentVersion=$(AGENT_VERSION) -X $(MODULE)/update.VersionHash=$(VERSION_HASH)
EXE := $(if $(filter windows,$(GOOS)),.exe,)
ASSET := komari-agent-$(GOOS)-$(GOARCH)$(EXE)
BRANDED_ASSET := shellbeam-monitoring-agent-$(GOOS)-$(GOARCH)$(EXE)

.PHONY: build check notices artifacts clean

build:
	mkdir -p "$(DIST)"
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -trimpath -ldflags='$(LDFLAGS)' -o "$(DIST)/shellbeam-monitoring-agent$(EXE)" .

check:
	$(GO) test ./...
	$(GO) vet ./...

notices:
	$(GO) mod download all
	rm -rf "$(DIST)/THIRD_PARTY_NOTICES"
	$(PYTHON) scripts/collect-third-party-notices.py --go "$(GO)" --output "$(DIST)/THIRD_PARTY_NOTICES"
	cp LICENSE NOTICE UPSTREAM.md "$(DIST)/"

# Both raw filenames contain the same bytes. The legacy filename is part of the
# existing updater/App distribution contract, not the product display name.
artifacts: build notices
	cp "$(DIST)/shellbeam-monitoring-agent$(EXE)" "$(DIST)/$(ASSET)"
	cp "$(DIST)/shellbeam-monitoring-agent$(EXE)" "$(DIST)/$(BRANDED_ASSET)"
	cp readme.md README_zh-cn.md "$(DIST)/"
	mkdir -p "$(DIST)/docs"
	cp docs/integration.md docs/releases.md docs/management.md docs/validation.md "$(DIST)/docs/"

clean:
	rm -rf "$(DIST)"
