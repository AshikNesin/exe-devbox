// Package shell handles shell detection and auto-installation of completion
// scripts. It detects which shell the user is running (bash or zsh), generates
// the appropriate completion script via cobra, and injects a source line into
// the user's rc file (~/.bashrc or ~/.zshrc).
package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// CompletionResult holds what happened during auto-completion setup.
type CompletionResult struct {
	Shell     string // "bash" or "zsh"
	ScriptPath string // where the completion script was written
	RCFile    string // which rc file was patched
	RCLine    string // the source line added
	AlreadyOK bool   // rc file already had the source line
}

// DetectedShell returns the user's shell type: "bash", "zsh", or "".
// Checks $SHELL first, then $0, then falls back to bash.
func DetectedShell() string {
	for _, check := range []string{os.Getenv("SHELL"), os.Getenv("0")} {
		switch {
		case strings.Contains(check, "zsh"):
			return "zsh"
		case strings.Contains(check, "bash"):
			return "bash"
		}
	}
	// Fallback: check if ~/.bashrc exists and ~/.zshrc doesn't.
	if home, err := os.UserHomeDir(); err == nil {
		_, zshErr := os.Stat(filepath.Join(home, ".zshrc"))
		_, bashErr := os.Stat(filepath.Join(home, ".bashrc"))
		if zshErr == nil && bashErr != nil {
			return "zsh"
		}
	}
	return "bash" // default assumption
}

// InstallCompletion generates and installs shell completion for the given
// cobra root command. It:
//  1. Generates the completion script for the detected shell.
//  2. Writes it to ~/.local/share/exebox/completion.{sh,zsh}.
//  3. Adds a source line to ~/.bashrc or ~/.zshrc (idempotent).
func InstallCompletion(root *cobra.Command) (*CompletionResult, error) {
	sh := DetectedShell()
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("find home dir: %w", err)
	}

	// 1. Determine paths
	var scriptPath, rcFile, rcLine string
	switch sh {
	case "zsh":
		scriptPath = filepath.Join(home, ".local", "share", "exebox", "_exebox")
		rcFile = filepath.Join(home, ".zshrc")
		rcLine = fmt.Sprintf("[ -f %s ] && source %s", scriptPath, scriptPath)
	default: // bash
		scriptPath = filepath.Join(home, ".local", "share", "exebox", "completion.bash")
		rcFile = filepath.Join(home, ".bashrc")
		rcLine = fmt.Sprintf("[ -f %s ] && source %s", scriptPath, scriptPath)
	}

	// 2. Generate + write completion script
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		return nil, fmt.Errorf("create completion dir: %w", err)
	}
	f, err := os.Create(scriptPath)
	if err != nil {
		return nil, fmt.Errorf("create completion script: %w", err)
	}
	defer f.Close()
	switch sh {
	case "zsh":
		if err := root.GenZshCompletion(f); err != nil {
			return nil, fmt.Errorf("generate zsh completion: %w", err)
		}
	default:
		if err := root.GenBashCompletionV2(f, false); err != nil {
			return nil, fmt.Errorf("generate bash completion: %w", err)
		}
	}

	// 3. Add source line to rc file (idempotent)
	alreadyOK := false
	content, _ := os.ReadFile(rcFile)
	// Check if we've already added it (match the script path, not the exact line,
	// since the user might have edited it).
	if strings.Contains(string(content), scriptPath) {
		alreadyOK = true
	}
	if !alreadyOK {
		// Append with a newline if the file doesn't end with one.
		if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
			content = append(content, '\n')
		}
		header := "# exebox shell completion\n"
		content = append(content, []byte(header+rcLine+"\n")...)
		if err := os.WriteFile(rcFile, content, 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", filepath.Base(rcFile), err)
		}
	}

	return &CompletionResult{
		Shell:     sh,
		ScriptPath: scriptPath,
		RCFile:    rcFile,
		RCLine:    rcLine,
		AlreadyOK: alreadyOK,
	}, nil
}