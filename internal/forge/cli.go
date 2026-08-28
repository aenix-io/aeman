package forge

import "context"

// CLI is the code host's own command-line tool — gh for GitHub, glab for
// GitLab — standing in for a signed-in person on a single-user server: the
// person who ran the tool's login is the board's user, and the tool's token
// is what the server reads the forge and pushes with.
type CLI interface {
	// Token is the tool's stored credential; an error names how to log in.
	Token(ctx context.Context) (string, error)
	// Login is the login of the person the tool is signed in as.
	Login(ctx context.Context) (string, error)
}
