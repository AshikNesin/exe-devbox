# exebox Makefile
# Common dev workflows: build, install (binary + bash completion), test, run.

BINARY   := exebox
BIN_DIR  := $(HOME)/.local/bin
PKG      := ./cmd/exebox
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: all build install completion run test vet fmt clean

all: build

build:
	@echo "→ building $(BINARY) $(VERSION)"
	go build -ldflags "-X main.Version=$(VERSION)" -o $(BINARY) $(PKG)

# install: build to ~/.local/bin (already on PATH via ~/.bashrc) + auto-install
# shell completion (detects bash/zsh). Re-run after adding cmds.
install: build
	@echo "→ installing to $(BIN_DIR)/$(BINARY)"
	@mkdir -p $(BIN_DIR)
	@cp $(BINARY) $(BIN_DIR)/$(BINARY)
	@echo "→ auto-installing shell completion"
	@mkdir -p $(HOME)/.local/share/exebox
	@$(BIN_DIR)/$(BINARY) completion bash > $(HOME)/.local/share/exebox/completion.bash
	@grep -q 'exebox/completion.bash' $(HOME)/.bashrc 2>/dev/null || \
		printf '\n# exebox shell completion\n[ -f $$HOME/.local/share/exebox/completion.bash ] && source $$HOME/.local/share/exebox/completion.bash\n' >> $(HOME)/.bashrc
	@echo "✓ installed. Open a new shell or: source ~/.bashrc"

# completion: regenerate the completion script (for development use).
# setup auto-installs completion; this is a manual refresh shortcut.
completion: build
	@$(MAKE) --no-print-directory install

run: build
	./$(BINARY) $(filter-out $@,$(MAKECMDGOALS))

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -f $(BINARY)

# release: build cross-compiled binaries, tag, and upload to GitHub releases.
# Usage: make release VERSION=v0.2.0
VERSION ?= 
RELEASE_DIR := release-build
GITHUB_API ?= https://api.github.com
GITHUB_REPO ?= AshikNesin/exebox

release:
	@if [ -z "$(VERSION)" ]; then echo "Usage: make release VERSION=v0.2.0"; exit 1; fi
	@echo "→ building $(VERSION)"
	@mkdir -p $(RELEASE_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w -X main.Version=$(VERSION)" -o $(RELEASE_DIR)/exebox-$(VERSION)-linux-amd64 $(PKG)
	@chmod +x $(RELEASE_DIR)/exebox-$(VERSION)-linux-amd64
	@echo "→ uploading binary to releases/$(VERSION)/ via Contents API"
	@python3 -c " \
import json, base64; \
v = '$(VERSION)'; \
with open('$(RELEASE_DIR)/exebox-' + v + '-linux-amd64', 'rb') as f: \
    b64 = base64.b64encode(f.read()).decode(); \
payload = json.dumps({'message': 'Add exebox ' + v + ' binary', 'content': b64}); \
open('$(RELEASE_DIR)/upload.json', 'w').write(payload) \
"
	@curl -sf -X PUT \
	  "$(GITHUB_API)/repos/$(GITHUB_REPO)/contents/releases/$(VERSION)/exebox-linux-amd64" \
	  -H 'Content-Type: application/json' \
	  -d @$(RELEASE_DIR)/upload.json | python3 -c "import sys,json;print('  uploaded:', json.load(sys.stdin)['content']['path'])"
	@git tag -a $(VERSION) -m "$(VERSION)"
	@git push origin $(VERSION)
	@echo "✓ release $(VERSION) binary uploaded + tag pushed"
	@echo "  download: https://github.com/$(GITHUB_REPO)/raw/main/releases/$(VERSION)/exebox-linux-amd64"
