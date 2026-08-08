# devbox Makefile
# Common dev workflows: build, install (binary + bash completion), test, run.

BINARY   := devbox
BIN_DIR  := $(HOME)/.local/bin
PKG      := ./cmd/devbox
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
	@mkdir -p $(HOME)/.local/share/devbox
	@$(BIN_DIR)/$(BINARY) completion bash > $(HOME)/.local/share/devbox/completion.bash
	@grep -q 'devbox/completion.bash' $(HOME)/.bashrc 2>/dev/null || \
		printf '\n# devbox shell completion\n[ -f $$HOME/.local/share/devbox/completion.bash ] && source $$HOME/.local/share/devbox/completion.bash\n' >> $(HOME)/.bashrc
	@echo "→ creating exe-devbox symlink alias"
	@ln -sf $(BINARY) $(BIN_DIR)/exe-devbox
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

# release: tag and push — CI (.github/workflows/release.yml) builds and publishes.
# Usage: make release VERSION=v0.4.0
VERSION ?= 

release:
	@if [ -z "$(VERSION)" ]; then echo "Usage: make release VERSION=v0.4.0"; exit 1; fi
	@echo "→ tagging $(VERSION) (CI will build + publish)"
	@git tag -a $(VERSION) -m "$(VERSION)"
	@git push origin $(VERSION)
	@echo "✓ tag pushed — watch CI: https://github.com/AshikNesin/exe-devbox/actions"
	@echo "  release will appear at: https://github.com/AshikNesin/exe-devbox/releases/tag/$(VERSION)"
