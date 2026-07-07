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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aenix-org/aeman/internal/ghcli"
	"github.com/aenix-org/aeman/internal/server"
	"github.com/aenix-org/aeman/pkg/boardservice"
	"github.com/aenix-org/aeman/pkg/mcpserver"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "aeman:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "mcp":
		return runMCP(args[1:])
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
	fmt.Print(`aeman - backend-less project management on top of GitHub Projects v2

Usage:
  aeman serve [flags]   Start the local server and open the UI
  aeman mcp [flags]     Start the MCP server on stdio
  aeman version         Print the version
  aeman help            Show this help

Run 'aeman serve --help' or 'aeman mcp --help' for flags.
`)
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8765", "address to listen on")
	owner := fs.String("owner", os.Getenv("AEMAN_OWNER"), "default GitHub org/user to load projects from")
	projectDefault, _ := strconv.Atoi(os.Getenv("AEMAN_PROJECT"))
	project := fs.Int("project", projectDefault, "default GitHub Project number to open")
	lockBoard := fs.Bool("lock-board", os.Getenv("AEMAN_LOCK_BOARD") == "true", "pin the UI to --owner/--project and hide the board picker")
	open := fs.Bool("open", true, "open the UI in a browser on start")
	verbose := fs.Bool("verbose", false, "enable debug logging")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *lockBoard && (*owner == "" || *project <= 0) {
		return fmt.Errorf("--lock-board requires --owner and --project (or AEMAN_OWNER/AEMAN_PROJECT)")
	}

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
	projectDefault, _ := strconv.Atoi(os.Getenv("AEMAN_PROJECT"))
	project := fs.Int("project", projectDefault, "default GitHub Project number")
	lockBoard := fs.Bool("lock-board", os.Getenv("AEMAN_LOCK_BOARD") == "true", "pin owner/project, ignoring per-tool overrides")
	verbose := fs.Bool("verbose", false, "enable debug logging")
	if err := fs.Parse(args); err != nil {
		return err
	}

	logger := newLogger(*verbose)

	srv := mcpserver.New(mcpserver.Config{
		Owner:        *owner,
		Project:      *project,
		Lock:         *lockBoard,
		Version:      version,
		ResolveToken: resolveGitHubToken,
		// Scope the default (unspecified-view) list to the local user's own Me
		// board; best-effort via the gh CLI, else the list stays sprint-scoped.
		ResolveLogin: ghcli.Login,
	})

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

	logger.Info("aeman MCP server ready on stdio", "owner", *owner, "project", *project, "locked", *lockBoard)
	return mcpserver.Serve(ctx, srv)
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
