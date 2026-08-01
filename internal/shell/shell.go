// Package shell handles shell detection and auto-installation of completion
// scripts. It generates completion scripts for all supported shells (bash and
// zsh), writes them to ~/.local/share/exebox/, and injects a source line into
// the corresponding rc file (~/.bashrc and ~/.zshrc) whenever that rc file
// already exists or is the detected login shell.
package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// supportedShells is the fixed list of shells exebox installs completion for.
var supportedShells = []string{"bash", "zsh"}

// CompletionResult holds what happened during auto-completion setup for one shell.
type CompletionResult struct {
	Shell      string // "bash" or "zsh"
	ScriptPath string // where the completion script was written
	RCFile     string // which rc file was patched ("" if skipped)
	RCLine     string // the source line added
	AlreadyOK  bool   // rc file already had the source line
	Skipped    bool   // true if the rc file did not exist and was not created
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

// InstallCompletion generates and installs shell completion for every supported
// shell (bash and zsh). For each shell it:
//  1. Generates the completion script via cobra.
//  2. Writes it to ~/.local/share/exebox/completion.bash (or _exebox for zsh).
//  3. Adds a source line to ~/.bashrc / ~/.zshrc (idempotent) when that rc file
//     already exists or is the detected login shell.
//
// This ensures both shells work regardless of which the user is currently in.
// The detected shell is always patched even if its rc file is missing (created).
func InstallCompletion(root *cobra.Command) ([]CompletionResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("find home dir: %w", err)
	}
	detected := DetectedShell()
	var results []CompletionResult
	for _, sh := range supportedShells {
		res, err := installForShell(root, sh, home, sh == detected)
		if err != nil {
			return results, err
		}
		results = append(results, *res)
	}
	return results, nil
}

// installForShell handles a single shell. If force is true (the detected login
// shell), the rc file is created if missing; otherwise a missing rc file means
// the shell is skipped.
func installForShell(root *cobra.Command, sh, home string, force bool) (*CompletionResult, error) {
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

	// 3. Decide whether to patch the rc file.
	res := &CompletionResult{
		Shell:      sh,
		ScriptPath: scriptPath,
		RCFile:     rcFile,
		RCLine:     rcLine,
	}
	content, _ := os.ReadFile(rcFile)
	if strings.Contains(string(content), scriptPath) {
		res.AlreadyOK = true
		return res, nil
	}
	// If the rc file does not exist and this is not the detected login shell,
	// skip patching rather than creating a stray rc file.
	if _, statErr := os.Stat(rcFile); statErr != nil && !force {
		res.Skipped = true
		res.RCFile = ""
		return res, nil
	}

	// Append the source line (idempotent).
	if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
		content = append(content, '\n')
	}
	header := "# exebox shell completion\n"
	content = append(content, []byte(header+rcLine+"\n")...)
	if err := os.WriteFile(rcFile, content, 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", filepath.Base(rcFile), err)
	}
	return res, nil
}