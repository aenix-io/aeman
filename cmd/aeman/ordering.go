package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/pkg/gitstore"
)

// runOrderingReport prints what the schema-4 migration (the ordering model,
// docs/design/ordering.md) would do to a board, and writes nothing: every
// repository is cloned into memory, brought to schema 3 there, and the plan
// is computed over the merged board — the files that would be re-keyed and
// every Team cell whose in-work order would change. The owner reads it
// before the production run; `aeman serve` performs the migration itself.
func runOrderingReport(args []string) error {
	fs := flag.NewFlagSet("ordering-report", flag.ContinueOnError)
	gf := addGitFlags(fs, os.Getenv)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := gf.config()
	if err != nil {
		return err
	}
	if cfg == nil {
		return fmt.Errorf("ordering-report needs --repo name=url (or AEMAN_REPOS), the primary first")
	}
	log := newLogger(false)
	fillGitToken(context.Background(), cfg, cliFor(cfg.Forge, openStore(log), osEnv, log))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	domains := make([]gitstore.Domain, 0, len(cfg.Repos))
	for _, spec := range cfg.Repos {
		remote := gitstore.Remote{URL: spec.URL}
		if tok := spec.Token; tok != "" {
			remote.Auth = cfg.Forge.GitAuth(tok)
		} else if cfg.Token != "" {
			remote.Auth = cfg.Forge.GitAuth(cfg.Token)
		}
		repo, err := gitstore.Clone(ctx, memory.NewStorage(), remote, gitstore.Options{Committer: cfg.Committer}, 0)
		if err != nil {
			return fmt.Errorf("clone %s: %w", spec.URL, err)
		}
		if _, err := gitstore.MigrateSchema(repo); err != nil {
			return fmt.Errorf("schema of %s: %w", spec.URL, err)
		}
		domains = append(domains, gitstore.Domain{Name: spec.Name, Repo: repo})
	}
	report, err := gitstore.MigrateOrdering(domains, true)
	if err != nil {
		return err
	}
	fmt.Print(report.String())
	return nil
}
