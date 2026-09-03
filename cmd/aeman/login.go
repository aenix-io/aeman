package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/term"

	"github.com/aenix-io/aeman/internal/forge"
	"github.com/aenix-io/aeman/internal/tokenstore"
)

// tokenInput is where a token is read from: a terminal, where it is asked
// for with the echo off, or standard input, whose first line is the token —
// `gh auth token | aeman login` has to work with no terminal at all.
type tokenInput struct {
	r        io.Reader
	tty      bool
	readPass func() (string, error)
	// prompt is where the question goes — stderr, like every other tool's,
	// so `aeman login > file` still shows it and the file holds only what
	// the command produced.
	prompt io.Writer
}

// stdinToken is the real input, hidden when there is a terminal to hide it
// on.
func stdinToken() tokenInput {
	fd := int(os.Stdin.Fd())
	return tokenInput{
		r:      os.Stdin,
		prompt: os.Stderr,
		tty:    term.IsTerminal(fd),
		readPass: func() (string, error) {
			b, err := term.ReadPassword(fd)
			return string(b), err
		},
	}
}

// restoreTerminalOnInterrupt puts the terminal back the way it was if the
// person presses Ctrl-C at the hidden prompt, and then lets the signal do
// what it was going to do. term.ReadPassword turns the echo off and
// restores it on the way out, which a killed process skips — leaving a
// shell that types nothing back until `stty sane`. The returned func stops
// watching. It does nothing where stdin is not a terminal, which is every
// piped run.
//
// Restoring WITHOUT handing the signal on is worse than not catching it.
// x/term keeps ISIG set, so Ctrl-C really does raise; Go installs handlers
// with SA_RESTART, so the read the person is sitting in front of resumes
// rather than failing. Catching and stopping there would leave `aeman
// login` unkillable by Ctrl-C — and, since the restore has just put the
// echo back on under that live read, the next thing typed goes to the
// scrollback in clear. Which is the token.
func restoreTerminalOnInterrupt(fd int) func() {
	if !term.IsTerminal(fd) {
		return func() {}
	}
	state, err := term.GetState(fd)
	if err != nil {
		return func() {}
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	stop := onFirstSignal(sigs, func() { _ = term.Restore(fd, state) }, resendToSelf)
	return func() {
		signal.Stop(sigs)
		stop()
	}
}

// onFirstSignal runs restore when one arrives and then passes it on. Split
// out from its caller so both halves can be checked without a terminal or
// a real signal: the bug this replaced ran only the first half.
func onFirstSignal(sigs <-chan os.Signal, restore func(), pass func(os.Signal)) func() {
	done := make(chan struct{})
	go func() {
		select {
		case sig := <-sigs:
			restore()
			pass(sig)
		case <-done:
		}
	}()
	// Once, because the caller defers it and a test calls it twice to say
	// so: closing a closed channel panics, and a panic on the way out of
	// `aeman login` would be a worse ending than the one being prevented.
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

// resendToSelf gives the signal back to the default disposition, which is
// what ends the command.
func resendToSelf(sig os.Signal) {
	signal.Reset(sig)
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		return
	}
	_ = p.Signal(sig)
}

// openStore is how every command here reaches the OS secret store. It is
// a variable so the package's tests can replace it with one that fails:
// an entry point that opens the real store reads and writes the keychain
// of whoever runs `go test`, and `logout` deletes without asking.
var openStore = tokenstore.Open

const (
	loginHint  = "the token is typed at the prompt or piped in: `gh auth token | aeman login`"
	logoutHint = "it removes the stored token for the forge the flags name"
)

// forgeTargetFor parses the flags login and logout share and refuses a
// positional argument. `aeman login ghp_...` is the natural first attempt
// and the one thing this command exists to prevent: an argument is in the
// shell's history and in every ps listing before the command runs. The
// refusal never repeats it — printing it back would put it in the
// terminal a second time.
//
// Split from the commands so the refusal can be tested without opening a
// store: the entry points are the only place the real keychain is built.
func forgeTargetFor(name, hint string, env func(string) string, args []string) (forge.Forge, string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	gf := addGitFlags(fs, env)
	if err := fs.Parse(args); err != nil {
		return nil, "", err
	}
	if fs.NArg() > 0 {
		return nil, "", fmt.Errorf("`aeman %s` takes no arguments; %s", name, hint)
	}
	return gf.forgeTarget()
}

func runLogin(args []string) error {
	f, host, err := forgeTargetFor("login", loginHint, os.Getenv, args)
	if err != nil {
		return err
	}
	defer restoreTerminalOnInterrupt(int(os.Stdin.Fd()))()
	log := newLogger(false)
	// Bounded like every other forge call here: this is the one path with a
	// person waiting at a prompt, so an unreachable forge must fail rather
	// than hang until they interrupt it.
	client := &http.Client{Timeout: forgeTimeout}
	return login(context.Background(), f, client, openStore(log), host, stdinToken(), os.Stdout)
}

func runLogout(args []string) error {
	_, host, err := forgeTargetFor("logout", logoutHint, os.Getenv, args)
	if err != nil {
		return err
	}
	return logout(openStore(newLogger(false)), host, os.Stdout)
}

// login stores a token for one forge instance, but only after the forge has
// said whose it is. An item holding a typo is worse than no item: every
// later command finds it, stops looking, and fails on a credential nobody
// can see.
func login(ctx context.Context, f forge.Forge, client *http.Client, store tokenstore.Store, host string, in tokenInput, out io.Writer) error {
	tok, err := readToken(host, in)
	if err != nil {
		return err
	}
	if tok == "" {
		return errors.New("no token given; type it at the prompt, or pipe it in: `gh auth token | aeman login`")
	}
	user, err := f.User(ctx, client, tok)
	switch {
	case errors.Is(err, forge.ErrBadToken):
		return fmt.Errorf("%s rejected the token", f.Label())
	case err != nil:
		return fmt.Errorf("ask %s whose token this is: %w", f.Label(), err)
	}
	if err := store.Set(host, tok); err != nil {
		// Every failure to store gets the same advice, because the store
		// cannot tell them apart: on macOS a locked keychain and a denied
		// item both arrive as whatever /usr/bin/security printed. So the
		// guidance comes first and the tool's own words after it — the
		// person on a locked CI runner needs the exit status to work out
		// why, and the variable to get moving meanwhile.
		//
		// Not AEMAN_GIT_TOKEN: that one is the server's push credential
		// and is read by nothing that resolves an identity, so a person
		// sent there would push fine and have no name on the commits.
		return fmt.Errorf("no usable secret store here; set %s in the environment instead: %w",
			strings.Join(tokenEnv(f), " or "), err)
	}
	fmt.Fprintf(out, "Signed in to %s as %s; the token is stored in the OS keychain (service %q, account %q).\n",
		host, user.Login, tokenstore.Service, host)
	return nil
}

// logout removes one forge instance's token. The removal runs whatever the
// read said: an item holding nothing but whitespace reads as absent, and
// skipping the delete on that answer would leave it in the store with no
// command able to remove it. Delete is idempotent, so a host with nothing
// stored costs one call and reports the state it is already in.
func logout(store tokenstore.Store, host string, out io.Writer) error {
	_, readErr := store.Get(host)
	if err := store.Delete(host); err != nil {
		return err
	}
	if errors.Is(readErr, tokenstore.ErrNotFound) {
		// Not "no item": a blank one reads the same way, and the delete
		// above has just removed it either way.
		fmt.Fprintf(out, "No usable token was stored for %s.\n", host)
		return nil
	}
	fmt.Fprintf(out, "Signed out of %s; the keychain item (service %q, account %q) is gone.\n",
		host, tokenstore.Service, host)
	return nil
}

// readToken asks the terminal with the echo off, or takes the first line of
// standard input and ignores the rest, so a tool that prints more than the
// token still works as a source.
func readToken(host string, in tokenInput) (string, error) {
	if in.tty {
		prompt := in.prompt
		if prompt == nil {
			prompt = io.Discard
		}
		fmt.Fprintf(prompt, "Token for %s (input hidden): ", host)
		tok, err := in.readPass()
		fmt.Fprintln(prompt)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(tok), nil
	}
	line, err := bufio.NewReader(in.r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
