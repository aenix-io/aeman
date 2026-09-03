package forge

import "context"

// CLI is a source of the person's credential and identity on a
// single-user server — the forge's own tool (gh, glab), the environment,
// or the OS keychain. Its token is what the server reads the forge and
// pushes with, and its login is who that token belongs to; the two come
// from ONE source, or the board pushes as one account and signs the work
// with another's name.
type CLI interface {
	// Token is the tool's stored credential; an error names how to log in.
	Token(ctx context.Context) (string, error)
	// Login is the login of the person the tool is signed in as.
	Login(ctx context.Context) (string, error)
}
