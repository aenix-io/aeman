package server

import "github.com/aenix-io/aeman/internal/forge"

// cliName is the forge's command-line tool, for the copy that tells a
// person how to sign in on a single-user server.
func cliName(f forge.Forge) string {
	if f != nil && f.Kind() == forge.GitLab {
		return "glab"
	}
	return "gh"
}
