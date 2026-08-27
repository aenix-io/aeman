// Command aeman is a backend-less project management UI. It serves an embedded
// single-page application and uses GitHub Projects v2 as its data backend,
// authenticating through the local gh CLI or a GitHub OAuth web flow. It also
// exposes a native JSON API and an MCP server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // board timezone by name works even in scratch containers

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aenix-io/aeman/internal/ghcli"
	"github.com/aenix-io/aeman/internal/migrate"
	"github.com/aenix-io/aeman/internal/migrate/ghsource"
	"github.com/aenix-io/aeman/internal/server"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/pkg/gitstore"
	"github.com/aenix-io/aeman/pkg/mcpserver"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

// boardEnv reads the board number's environment variable. AEMAN_PROJECT is
// the pre-rename name and is still honoured: "project" became aeman's own
// planning entity, and a rename that silently stops a deployment from booting
// is not a rename anyone should have to read release notes to survive.
func boardEnv() string {
	if v := os.Getenv("AEMAN_BOARD"); v != "" {
		return v
	}
	return os.Getenv("AEMAN_PROJECT")
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "aeman:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// The board's days live in ONE time zone for every user (AEMAN_TZ, IANA
	// name): without it a teammate east of the server crosses their local
	// midnight and starts seeing deferred "tomorrow" cards on today's board.
	if tz := os.Getenv("AEMAN_TZ"); tz != "" {
		if err := board.SetLocation(tz); err != nil {
			return fmt.Errorf("AEMAN_TZ: %w", err)
		}
	}
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "mcp":
		return runMCP(args[1:])
	case "init":
		return runInit(args[1:])
	case "migrate":
		return runMigrate(args[1:])
	case "version", "--version", "-v":
		fmt.Println("aeman", version)
		return nil
	case "help", "--help", "-h":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q (run 'aeman help')", args[0])
	}
}

func usage() {
	fmt.Print(`aeman - short-term planning for engineering teams

Usage:
  aeman serve [flags]   Start the server and open the UI
  aeman mcp [flags]     Start the MCP server on stdio
  aeman init --repo URL Bootstrap an empty repository as a board
  aeman migrate [flags] Copy a GitHub Projects v2 board into a repository
  aeman version         Print the version
  aeman help            Show this help

The board's storage is a git repository: pass --repo name=url (or
AEMAN_REPOS) to serve and mcp. Without it the GitHub Projects v2 board
named by --owner/--board is served.

Run 'aeman serve --help', 'aeman mcp --help' or 'aeman init --help' for flags.
`)
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8765", "address to listen on")
	owner := fs.String("owner", os.Getenv("AEMAN_OWNER"), "default GitHub org/user to load projects from")
	projectDefault, _ := strconv.Atoi(boardEnv())
	project := fs.Int("board", projectDefault, "GitHub Project number of the board to open")
	lockBoard := fs.Bool("lock-board", os.Getenv("AEMAN_LOCK_BOARD") == "true", "pin the UI to --owner/--project and hide the board picker")
	open := fs.Bool("open", true, "open the UI in a browser on start")
	verbose := fs.Bool("verbose", false, "enable debug logging")
	gf := addGitFlags(fs, os.Getenv)
	if err := fs.Parse(args); err != nil {
		return err
	}
	gitCfg, err := gf.config()
	if err != nil {
		return err
	}
	if gitCfg == nil && *lockBoard && (*owner == "" || *project <= 0) {
		return fmt.Errorf("--lock-board requires --owner and --board (or AEMAN_OWNER/AEMAN_BOARD)")
	}
	fillGitToken(context.Background(), gitCfg)

	logger := newLogger(*verbose)

	// Multi-user GitHub OAuth mode is enabled when client credentials are set
	// in the environment (kept out of flags so secrets stay out of `ps`).
	var auth *server.OAuthConfig
	if id, secret := os.Getenv("AEMAN_GITHUB_CLIENT_ID"), os.Getenv("AEMAN_GITHUB_CLIENT_SECRET"); id != "" && secret != "" {
		baseURL := os.Getenv("AEMAN_BASE_URL")
		if baseURL == "" {
			return fmt.Errorf("AEMAN_BASE_URL is required when GitHub OAuth is configured")
		}
		auth = &server.OAuthConfig{
			ClientID:     id,
			ClientSecret: secret,
			BaseURL:      baseURL,
			Scopes:       os.Getenv("AEMAN_SCOPES"),
			SessionFile:  os.Getenv("AEMAN_SESSION_FILE"),
			SessionKey:   os.Getenv("AEMAN_SESSION_KEY"),
		}
	}

	srv, err := server.New(server.Options{
		Addr:           *addr,
		DefaultOwner:   *owner,
		DefaultProject: *project,
		Version:        version,
		Logger:         logger,
		Auth:           auth,
		LockBoard:      *lockBoard,
		Git:            gitCfg,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *open {
		go openBrowser(srv.URL())
	}

	return srv.Run(ctx)
}

func runMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	owner := fs.String("owner", os.Getenv("AEMAN_OWNER"), "default GitHub org/user")
	projectDefault, _ := strconv.Atoi(boardEnv())
	project := fs.Int("board", projectDefault, "GitHub Project number of the board")
	lockBoard := fs.Bool("lock-board", os.Getenv("AEMAN_LOCK_BOARD") == "true", "pin owner/project, ignoring per-tool overrides")
	verbose := fs.Bool("verbose", false, "enable debug logging")
	gf := addGitFlags(fs, os.Getenv)
	if err := fs.Parse(args); err != nil {
		return err
	}
	gitCfg, err := gf.config()
	if err != nil {
		return err
	}

	logger := newLogger(*verbose)

	cfg := mcpserver.Config{
		Owner:        *owner,
		Project:      *project,
		Lock:         *lockBoard,
		Version:      version,
		ResolveToken: resolveGitHubToken,
		// Scope the default (unspecified-view) list to the local user's own Me
		// board; best-effort via the gh CLI, else the list stays sprint-scoped.
		ResolveLogin: ghcli.Login,
	}
	var drain func(context.Context) error
	if gitCfg != nil {
		// Git mode: this process owns its own clone, cache and push; the
		// board is the configured repository, whatever owner/board a tool
		// passes; no GitHub token is needed for the board itself.
		fillGitToken(context.Background(), gitCfg)
		gb, err := server.OpenGitBackend(gitCfg, logger)
		if err != nil {
			return err
		}
		cfg.Owner, cfg.Project, cfg.Lock = "git", 1, true
		cfg.ResolveToken = func(context.Context) (string, error) { return "", nil }
		cfg.WrapBackend = func(boardservice.Backend) boardservice.Backend { return gb.Backend() }
		drain = gb.Drain
	}
	srv := mcpserver.New(cfg)

	// Attribute activity events to the local gh identity (cached per process).
	srv.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if login, err := ghcli.Login(ctx); err == nil {
				ctx = boardservice.WithActor(ctx, login)
			}
			return next(ctx, method, req)
		}
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("aeman MCP server ready on stdio", "owner", cfg.Owner, "board", cfg.Project, "locked", cfg.Lock)
	err = mcpserver.Serve(ctx, srv)
	if drain != nil {
		// The client may close the pipe right after a mutation: wait for the
		// queue and push before exiting, so nothing is left only on disk.
		dctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if derr := drain(dctx); derr != nil {
			logger.Warn("final push failed; unpushed commits stay in the clone", "err", derr)
		}
	}
	return err
}

// resolveGitHubToken returns a token from GITHUB_TOKEN/GH_TOKEN, falling back to
// the local gh CLI (mirroring aeman's local run mode).
func resolveGitHubToken(ctx context.Context) (string, error) {
	for _, env := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v, nil
		}
	}
	out, err := ghcli.Run(ctx, "auth", "token")
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(out)
	if tok == "" {
		return "", fmt.Errorf("no GitHub token; set GITHUB_TOKEN or run `gh auth login`")
	}
	return tok, nil
}

// runInit bootstraps an empty repository as a board: board.yaml and the
// no-team group in one commit, pushed. Safe to run twice; a repository that
// already holds a board is left alone.
func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	title := fs.String("title", "aeman board", "the board's title")
	gf := addGitFlags(fs, os.Getenv)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := gf.config()
	if err != nil {
		return err
	}
	if cfg == nil {
		return fmt.Errorf("init needs --repo URL (or AEMAN_REPOS)")
	}
	fillGitToken(context.Background(), cfg)
	remote := gitstore.Remote{URL: cfg.Repos[0].URL}
	if cfg.Token != "" {
		remote.Auth = &githttp.BasicAuth{Username: "x-access-token", Password: cfg.Token}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := gitstore.InitBoard(ctx, memory.NewStorage(), remote, gitstore.Options{Committer: cfg.Committer}, *title); err != nil {
		return err
	}
	fmt.Printf("board initialised in %s\n", remote.URL)
	return nil
}

// runMigrate copies a GitHub Projects v2 board into a repository: snapshot
// as truth, events as history, verified, idempotent. The Projects board is
// never written.
func runMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	owner := fs.String("owner", os.Getenv("AEMAN_OWNER"), "GitHub org/user that owns the Projects v2 board")
	boardDefault, _ := strconv.Atoi(boardEnv())
	number := fs.Int("board", boardDefault, "GitHub Project number of the board to copy")
	title := fs.String("title", "aeman board", "the migrated board's title")
	dryRun := fs.Bool("dry-run", false, "build and verify everything, push nothing")
	force := fs.Bool("force", false, "write over a repository that already holds commits")
	reportPath := fs.String("report", "", "write the report (and the id table) to this file")
	gf := addGitFlags(fs, os.Getenv)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := gf.config()
	if err != nil {
		return err
	}
	if cfg == nil || *owner == "" || *number <= 0 {
		return fmt.Errorf("migrate needs --owner, --board and --repo URL")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	tok, err := resolveGitHubToken(ctx)
	if err != nil {
		return err
	}
	fillGitToken(ctx, cfg)
	remote := gitstore.Remote{URL: cfg.Repos[0].URL}
	if cfg.Token != "" {
		remote.Auth = &githttp.BasicAuth{Username: "x-access-token", Password: cfg.Token}
	}
	rep, err := migrate.Run(ctx, ghsource.New(tok), memory.NewStorage(), remote, migrate.Options{
		Owner: *owner, Board: *number, Title: *title, Committer: cfg.Committer, DryRun: *dryRun, Force: *force,
	})
	if err != nil {
		return err
	}
	fmt.Print(rep.String())
	if *reportPath != "" {
		var b strings.Builder
		b.WriteString(rep.String())
		b.WriteString("\nid table (old → new):\n")
		keys := make([]string, 0, len(rep.IDMap))
		for k := range rep.IDMap {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "%s %s\n", k, rep.IDMap[k])
		}
		if err := os.WriteFile(*reportPath, []byte(b.String()), 0o600); err != nil {
			return fmt.Errorf("write report: %w", err)
		}
	}
	return nil
}

// newLogger builds a stderr logger. The MCP server speaks JSON-RPC on stdout, so
// logs must never go there.
func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// openBrowser tries to open url in the default browser. Failure is non-fatal.
func openBrowser(url string) {
	time.Sleep(300 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
