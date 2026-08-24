package server

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/aenix-io/aeman/pkg/board"
)

// The warm roster: which boards were being kept warm, and on whose session.
// It exists so a RESTART does not leave a cold-cache pit — before it, every
// deploy meant the first person on each board personally paid the minute-long
// load while the UI looked broken. The file sits next to the session file and
// holds no secrets: board keys and logins only, the tokens stay in the
// session store.

// recordWarm notes that a board is warmed on some login's session and saves
// the roster when that changed.
func (s *boardStore) recordWarm(key, login string) {
	if s.warmFile == "" || key == "" || login == "" {
		return
	}
	s.mu.Lock()
	if s.warmRoster == nil {
		s.warmRoster = map[string]string{}
	}
	if s.warmRoster[key] == login {
		s.mu.Unlock()
		return
	}
	s.warmRoster[key] = login
	data, err := json.Marshal(s.warmRoster)
	path := s.warmFile
	s.mu.Unlock()
	if err != nil {
		return
	}
	// Best-effort, atomic: a torn write must not eat the roster.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		s.logger().Warn("warm roster not saved", "err", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		s.logger().Warn("warm roster not saved", "err", err)
	}
}

// loadWarmRoster reads the persisted roster (missing file = empty roster).
func (s *boardStore) loadWarmRoster() map[string]string {
	if s.warmFile == "" {
		return nil
	}
	data, err := os.ReadFile(s.warmFile)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	if err := json.Unmarshal(data, &out); err != nil {
		s.logger().Warn("warm roster unreadable; starting empty", "err", err)
		return nil
	}
	s.mu.Lock()
	s.warmRoster = out
	s.mu.Unlock()
	return out
}

// startupWarm brings the cache up right after start instead of waiting for
// the first request to pay the cold load.
func (s *Server) startupWarm() {
	if s.auth == nil {
		// Local mode: one user, one token — warm the default board with it.
		if s.opts.DefaultOwner == "" || s.opts.DefaultProject == 0 {
			return
		}
		s.warmOne(s.opts.DefaultOwner, s.opts.DefaultProject, "")
		return
	}
	for key, login := range s.store.loadWarmRoster() {
		owner, numStr, ok := strings.Cut(key, "/")
		num, err := strconv.Atoi(numStr)
		if !ok || err != nil || owner == "" || login == "" {
			continue
		}
		go s.warmOne(owner, num, login)
	}
}

// warmOne loads one board into the cache on the given login's freshest live
// session (or the local token when login is empty), seeding the warmer.
func (s *Server) warmOne(owner string, num int, login string) {
	ctx := context.Background()
	var be *storeBackend
	if s.auth == nil {
		tok, err := s.tokens.Token(ctx)
		if err != nil {
			s.log.Warn("startup warm skipped: no local token", "err", err)
			return
		}
		be = &storeBackend{inner: &resolvingBackend{inner: s.newGHClient(tok), store: s.store}, store: s.store}
	} else {
		sid, sess, ok := s.auth.newestSessionFor(ctx, login)
		if !ok {
			s.log.Info("startup warm skipped: no live session", "board", storeKey(owner, num), "login", login)
			return
		}
		client := s.newGHClient(sess.token)
		auth := s.auth
		be = &storeBackend{
			inner: &resolvingBackend{inner: client, store: s.store}, store: s.store,
			multiUser: true,
			warmAlive: func() bool { return auth.sessionAlive(sid) },
		}
		ctx = board.WithActor(ctx, login)
	}
	s.log.Info("startup warm began", "board", storeKey(owner, num), "login", login)
	if _, err := be.LoadBoard(ctx, owner, num); err != nil {
		s.log.Warn("startup warm failed", "board", storeKey(owner, num), "err", err)
		return
	}
	s.log.Info("startup warm done", "board", storeKey(owner, num))
}
