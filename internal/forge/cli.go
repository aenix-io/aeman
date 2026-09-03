package forge

import "context"

// CLI is a source of the person's credential and identity on a
// single-user server — the forge's own tool (gh, glab), the environment,
// or the OS keychain. Its token is what the server reads the forge and
// pushes with, and its login is who that token belongs to; the two come
// from ONE source, or the board pushes as one account and signs the work
// with another's name.
type CLI interface {
	// Token is this source's credential. A source with nothing to give
	// may answer either way — an error, or the empty string — and a
	// caller standing for SEVERAL sources owes the difference: it must
	// return an error when none of them answered, never a silent empty
	// string, because the server decides "have we got a credential at
	// all" on that error alone.
	Token(ctx context.Context) (string, error)
	// Login is who the forge says this source's token belongs to, which
	// need not be whoever ran a tool on the machine: a stored bot token
	// belongs to the bot. It must describe the token Token would hand
	// back right now, or the push is made with one account's credential
	// and the work signed with another's name.
	Login(ctx context.Context) (string, error)
}

// Credential is a CLI that can answer both questions at once. A caller
// that needs the token AND the person it belongs to must not ask twice: a
// CLI standing for several sources re-decides between the calls when one
// of them empties, and the caller is then holding one source's token
// beside another's name — which is the split the pairing exists to
// prevent. A CLI that is a single source has nothing to add and need not
// implement it; ask separately when the assertion fails.
type Credential interface {
	CLI
	TokenAndLogin(ctx context.Context) (token, login string, err error)
}
