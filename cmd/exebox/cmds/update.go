package cmds

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ashiknesin/exebox/internal/output"
	"github.com/spf13/cobra"
)

const githubLatestURL = "https://api.github.com/repos/AshikNesin/exebox/releases/latest"
const rawBinaryURL = "https://raw.githubusercontent.com/AshikNesin/exebox/releases/releases/%s/exebox-linux-amd64"

// appVersion is injected from root.go at NewRoot time.
var appVersion string

type ghRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
}

func newUpdateCmd(version string) *cobra.Command {
	appVersion = version
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update exebox to the latest GitHub release",
		RunE:  runUpdate,
	}
	cmd.Flags().Bool("check", false, "only check for updates, don't install")
	return cmd
}

func runUpdate(cmd *cobra.Command, args []string) error {
	checkOnly, _ := cmd.Flags().GetBool("check")

	current := strings.TrimPrefix(appVersion, "v")
	if current == "" || current == "dev" {
		output.Global.Warn("running a dev build (version unknown) — will attempt update anyway")
	}

	// Fetch latest release info
	resp, err := http.Get(githubLatestURL)
	if err != nil {
		return fmt.Errorf("checking latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("github API returned %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return fmt.Errorf("parsing release response: %w", err)
	}
	latest := strings.TrimPrefix(rel.TagName, "v")

	output.Global.Info("current: %s", current)
	output.Global.Info("latest:  %s", rel.TagName)

	if current == latest {
		output.Global.OK("already up to date")
		return nil
	}

	if checkOnly {
		output.Global.Info("update available: %s → %s", current, rel.TagName)
		output.Global.Info("run 'exebox update' to install")
		return nil
	}

	// Resolve current binary path (follow symlinks).
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding current binary: %w", err)
	}
	exePath, _ = filepath.EvalSymlinks(exePath)

	output.Global.Info("downloading %s ...", rel.TagName)

	// Download to a temp file in the same directory (atomic rename).
	dir := filepath.Dir(exePath)
	tmp, err := os.CreateTemp(dir, ".exebox-update-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	url := fmt.Sprintf(rawBinaryURL, rel.TagName)
	dlResp, err := http.Get(url)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("downloading binary: %w", err)
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != 200 {
		tmp.Close()
		return fmt.Errorf("download returned %d for %s", dlResp.StatusCode, url)
	}
	if _, err := io.Copy(tmp, dlResp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("writing binary: %w", err)
	}
	tmp.Close()

	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	// Atomic swap. On Linux the running process holds the inode, so
	// renaming over it is safe — the old binary stays mapped until exit.
	if err := os.Rename(tmpPath, exePath); err != nil {
		// Cross-device fallback (e.g. tmpfs → disk).
		data, readErr := os.ReadFile(tmpPath)
		if readErr != nil {
			return fmt.Errorf("rename failed (%w) and readback failed (%v)", err, readErr)
		}
		if err := os.WriteFile(exePath, data, 0755); err != nil {
			return fmt.Errorf("write binary: %w", err)
		}
	}

	output.Global.OK("updated to %s", rel.TagName)
	return nil
}
