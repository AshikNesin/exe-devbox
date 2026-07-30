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

// domainAdd is NOT reliably suggestable (not in the supported list), so we give
// the user both: a suggest link (works if exe.dev added it) + the shell command.
func domainAdd(vm, domain string) (suggest, shell string) {
	cmd := fmt.Sprintf("domain add %s %s", vm, domain)
	return suggestLink(cmd), shellCommand(cmd)
}
