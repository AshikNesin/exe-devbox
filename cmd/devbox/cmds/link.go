package cmds

import (
	"fmt"
	"net/url"
)

// exe.dev lobby commands run via `ssh exe.dev <lobby-cmd>`. Suggest links take
// the lobby command (NOT the ssh form): https://exe.dev/docs/suggest-links.md
// e.g. suggest?command=share+set-public+mybox

// suggestLink wraps a lobby command in an exe.dev click-to-run suggest link.
// The command is the lobby form ("share port vm 8080"), without "ssh exe.dev".
func suggestLink(lobbyCmd string) string {
	return "https://exe.dev/suggest?command=" + url.QueryEscape(lobbyCmd)
}

// shellCommand is the verbatim string to paste at https://exe.dev/shell for
// commands that aren't suggestable (e.g. `domain add` isn't in the supported
// suggest set as of writing). It's the full `ssh exe.dev …` form.
func shellCommand(lobbyCmd string) string {
	return "ssh exe.dev " + lobbyCmd
}

// --- typed builders so callers don't assemble strings by hand ---

func suggestSharePort(vm string, port int) string {
	return suggestLink(fmt.Sprintf("share port %s %d", vm, port))
}

func suggestSetPublic(vm string) string {
	return suggestLink(fmt.Sprintf("share set-public %s", vm))
}

// domainAdd registers a custom domain with exe.dev. `domain add` is NOT a
// supported suggest command (only ls, resize, and share-* are), so there is no
// suggest link — the user must run it at https://exe.dev/shell. Returns the
// full ssh command and a deep link to the shell page.
// Ref: https://exe.dev/docs/suggest-links.md
func domainAdd(vm, domain string) (shell string) {
	return shellCommand(fmt.Sprintf("domain add %s %s", vm, domain))
}

// domainRemove unregisters a custom domain from exe.dev. Same note as
// domainAdd: not suggestable, so returns the shell command.
func domainRemove(vm, domain string) string {
	return shellCommand(fmt.Sprintf("domain remove %s %s", vm, domain))
}
