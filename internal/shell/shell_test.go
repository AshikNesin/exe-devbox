package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestInstallCompletionBothShells verifies that when both .bashrc and .zshrc
// exist, both get the source line, even when the detected shell is bash.
func TestInstallCompletionBothShells(t *testing.T) {
	// Use a temp HOME so we don't touch the real rc files.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("SHELL", "/bin/bash")

	// Pre-create both rc files.
	for _, rc := range []string{".bashrc", ".zshrc"} {
		if err := os.WriteFile(filepath.Join(tmp, rc), []byte("# test\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	root := &cobra.Command{Use: "devbox"}
	root.AddCommand(&cobra.Command{Use: "new"})

	results, err := InstallCompletion(root)
	if err != nil {
		t.Fatalf("InstallCompletion: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}

	for _, rc := range []string{".bashrc", ".zshrc"} {
		b, err := os.ReadFile(filepath.Join(tmp, rc))
		if err != nil {
			t.Fatalf("read %s: %v", rc, err)
		}
		needle := "completion.bash"
		if rc == ".zshrc" {
			needle = "/_devbox"
		}
		if !strings.Contains(string(b), needle) {
			t.Errorf("%s does not contain %q\n%s", rc, needle, string(b))
		}
	}

	// Scripts should exist.
	for _, sh := range []string{"completion.bash", "_devbox"} {
		p := filepath.Join(tmp, ".local", "share", "devbox", sh)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("script %s not created: %v", sh, err)
		}
	}
}

// TestInstallCompletionIdempotent verifies re-running doesn't duplicate lines.
func TestInstallCompletionIdempotent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("SHELL", "/bin/bash")
	_ = os.WriteFile(filepath.Join(tmp, ".bashrc"), []byte("# test\n"), 0o644)

	root := &cobra.Command{Use: "devbox"}
	if _, err := InstallCompletion(root); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallCompletion(root); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(tmp, ".bashrc"))
	// The single source line references the path twice ([ -f X ] && source X),
	// so count header lines instead of path occurrences.
	if c := strings.Count(string(b), "# devbox shell completion"); c != 1 {
		t.Errorf("want 1 header line, got %d\n%s", c, string(b))
	}
}

// TestInstallCompletionSkipsMissingZshrc verifies that when .zshrc is absent and
// the detected shell is bash, zsh is skipped (no stray .zshrc created).
func TestInstallCompletionSkipsMissingZshrc(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("SHELL", "/bin/bash")
	_ = os.WriteFile(filepath.Join(tmp, ".bashrc"), []byte("# test\n"), 0o644)
	// no .zshrc

	root := &cobra.Command{Use: "devbox"}
	results, err := InstallCompletion(root)
	if err != nil {
		t.Fatal(err)
	}
	var zshRes *CompletionResult
	for i := range results {
		if results[i].Shell == "zsh" {
			zshRes = &results[i]
		}
	}
	if zshRes == nil || !zshRes.Skipped {
		t.Errorf("want zsh skipped, got %+v", zshRes)
	}
	if _, err := os.Stat(filepath.Join(tmp, ".zshrc")); err == nil {
		t.Error(".zshrc was created; should not have been")
	}
	// zsh completion script still generated.
	if _, err := os.Stat(filepath.Join(tmp, ".local", "share", "devbox", "_devbox")); err != nil {
		t.Errorf("zsh completion script not created: %v", err)
	}
}
