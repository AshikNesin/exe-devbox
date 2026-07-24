# exe-devbox Makefile
# Common dev workflows: build, install (binary + bash completion), test, run.

BINARY   := devbox
BIN_DIR  := $(HOME)/.local/bin
DOTFILES := $(HOME)/dotfiles/bash/devbox-completion.bash
PKG      := ./cmd/devbox
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: all build install completion run test vet fmt clean

all: build

build:
	@echo "→ building $(BINARY) $(VERSION)"
	go build -ldflags "-X main.Version=$(VERSION)" -o $(BINARY) $(PKG)

# install: build to ~/.local/bin (already on PATH via ~/.bashrc) + refresh bash
# completion into ~/dotfiles (sourced from ~/.bashrc). Re-run after adding cmds.
install: build
	@echo "→ installing to $(BIN_DIR)/$(BINARY)"
	@mkdir -p $(BIN_DIR)
	@cp $(BINARY) $(BIN_DIR)/$(BINARY)
	@$(MAKE) --no-print-directory completion
	@echo "✓ installed. Open a new shell or: source ~/.bashrc"

completion:
	@echo "→ refreshing bash completion -> $(DOTFILES)"
	@mkdir -p $(dir $(DOTFILES))
	@if [ -x "$(BIN_DIR)/$(BINARY)" ]; then \
		$(BIN_DIR)/$(BINARY) completion bash > $(DOTFILES); \
	else ./$(BINARY) completion bash > $(DOTFILES); fi

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
