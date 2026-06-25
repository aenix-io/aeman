// Command aeman is a backend-less project management UI. It serves an embedded
// single-page application and uses GitHub Projects v2 as its data backend,
// authenticating through the local gh CLI or a GitHub OAuth web flow.
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
	"syscall"
	"time"

	"github.com/aenix-org/aeman/internal/server"
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
  aeman version         Print the version
  aeman help            Show this help

Run 'aeman serve --help' for serve flags.
`)
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8765", "address to listen on")
	owner := fs.String("owner", "", "default GitHub org/user to load projects from")
	project := fs.Int("project", 0, "default GitHub Project number to open")
	open := fs.Bool("open", true, "open the UI in a browser on start")
	verbose := fs.Bool("verbose", false, "enable debug logging")
	if err := fs.Parse(args); err != nil {
		return err
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

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
		}
	}

	srv, err := server.New(server.Options{
		Addr:           *addr,
		DefaultOwner:   *owner,
		DefaultProject: *project,
		Version:        version,
		Logger:         logger,
		Auth:           auth,
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
