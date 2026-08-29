package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/aenix-io/aeman/internal/forge"
	"github.com/aenix-io/aeman/pkg/apiserver"
	"github.com/aenix-io/aeman/pkg/board"
)

// The personal board's own endpoints: what the visitor linked, linking a
// repository, unlinking it. Cards on the personal board go through the
// ordinary card endpoints (POST /cards with personal=true, ?view=personal).

// personalOf is the visitor's linked repository, or ok = false.
func (s *Server) personalOf(r *http.Request) (login string, info apiserver.PersonalInfo, ok bool) {
	_, login, err := s.apiTokens(r)
	if err != nil || login == "" || s.gitBE == nil {
		return login, apiserver.PersonalInfo{}, false
	}
	url, linked := s.gitBE.personalLink(login)
	if !linked {
		return login, apiserver.PersonalInfo{}, false
	}
	info = apiserver.PersonalInfo{Domain: board.PersonalDomain(login), URL: url}
	// A linked board that would not attach is a state the UI draws — the
	// reason and the page that fixes it — not an error found on the first
	// write.
	info.Problem, info.ActionURL = s.personalUnavailable(r.Context(), login)
	return login, info, true
}

func (s *Server) handleGetPersonal(w http.ResponseWriter, r *http.Request) {
	_, info, ok := s.personalOf(r)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "no personal board linked")
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// handleLinkPersonal links the visitor's repository as their personal board:
// the URL is checked, the visitor's credential must push to it (in the
// self-hosted mode the forge is asked), the link is committed to the primary
// and the repository attached — initialised as a board if it is empty.
func (s *Server) handleLinkPersonal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL string `json:"url"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.URL) == "" {
		writeJSONError(w, http.StatusBadRequest, "a repository URL is required")
		return
	}
	tok, login, err := s.apiTokens(r)
	if err != nil || login == "" {
		writeJSONError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if s.gitBE == nil {
		writeJSONError(w, http.StatusNotFound, "this server has no git store")
		return
	}
	if s.access != nil {
		// Self-hosted: the forge says whether the visitor may push there —
		// which needs a repository the forge knows how to name from the URL.
		// A single-user server says yes and lets the push itself decide.
		write, err := s.access.canPush(r.Context(), tok, in.URL)
		if errors.Is(err, errNotARepository) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, "could not check the repository: "+err.Error())
			return
		}
		if !write {
			msg, action := s.personalRefusal(r.Context(), tok)
			writeJSONErrorAction(w, http.StatusForbidden, msg, action)
			return
		}
	}
	// An explicit link is a person saying "try again": whatever went wrong
	// last time is forgotten, so the attach below really goes to the forge.
	s.gitBE.forgetPersonalProblem(login)
	if err := s.gitBE.linkPersonal(r.Context(), login, in.URL); err != nil {
		writeJSONError(w, http.StatusBadGateway, "record the link: "+err.Error())
		return
	}
	if err := s.gitBE.attachPersonal(r.Context(), login, tok); err != nil {
		writeJSONError(w, http.StatusBadGateway, "attach the repository: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apiserver.PersonalInfo{Domain: board.PersonalDomain(login), URL: in.URL})
}

// personalUnavailable explains why the login's personal board is not there
// to write to; "" when it is attached, or when nothing was ever linked (a
// card for a board that does not exist is a different mistake, and the
// service says so). A GitHub App that was never installed on the repository
// is the likely cause on an app-signed deployment, so the link comes along.
func (s *Server) personalUnavailable(ctx context.Context, login string) (msg, actionURL string) {
	if s.gitBE == nil || s.gitBE.hasPersonal(login) {
		return "", ""
	}
	why := s.gitBE.personalProblem(login)
	if why == "" {
		return "", ""
	}
	msg = "your personal board could not be attached: " + why
	if s.gitCfg != nil && s.gitCfg.App != nil {
		actionURL = s.gitCfg.App.InstallURL(ctx)
		msg += " — this board's GitHub App must be installed on that repository"
	}
	return msg, actionURL
}

// personalRefusal explains why the forge would not vouch for a personal
// repository. Signed in through a GitHub App, a person's token reaches only
// the repositories the app is installed on — so their own repository is
// refused until they install it there, which "you need push access" would
// neither say nor let them act on. A token that carries the whole account
// (an OAuth App's, a classic one) has no such excuse, and is told plainly.
func (s *Server) personalRefusal(ctx context.Context, token string) (msg, actionURL string) {
	const base = "you need push access to your personal repository"
	if !forge.IsUserToServerToken(token) || s.gitCfg == nil || s.gitCfg.App == nil {
		return base, ""
	}
	return base + ", and this board's GitHub App must be installed on it", s.gitCfg.App.InstallURL(ctx)
}

// handleAppSetup is where GitHub redirects after the board's GitHub App is
// installed or updated (the app's Setup URL). The remembered attach failure
// is dropped and the attach retried with the visitor's token; then home. A
// visitor without a session just goes home — there is nothing to retry for.
func (s *Server) handleAppSetup(w http.ResponseWriter, r *http.Request) {
	// A server still waiting for the installation retries the board itself.
	if _, waiting := s.inSetup(); waiting {
		s.retrySetup()
	}
	if tok, login, err := s.apiTokens(r); err == nil && login != "" && s.gitBE != nil {
		s.gitBE.forgetPersonalProblem(login)
		if err := s.gitBE.attachPersonal(r.Context(), login, tok); err != nil {
			s.log.Warn("personal board after app setup", "login", login, "err", err)
		}
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// handleAuthCallback tells GitHub's post-install redirect apart from the
// sign-in flow. When the app asks for user authorization during install,
// GitHub lands here — with an installation signature (installation_id,
// setup_action) and no state of ours — right after someone did the right
// thing; greeting that with "invalid OAuth state" is wrong twice over. It
// is the same event as /auth/setup and is handled as one. A callback
// without the installation signature is the sign-in flow, CSRF check and
// all.
func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("installation_id") != "" || q.Get("setup_action") != "" {
		s.handleAppSetup(w, r)
		return
	}
	s.auth.handleCallback(w, r)
}

// handleUnlinkPersonal removes the link; the repository is left as it is.
func (s *Server) handleUnlinkPersonal(w http.ResponseWriter, r *http.Request) {
	tok, login, err := s.apiTokens(r)
	if err != nil || login == "" {
		writeJSONError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if s.gitBE == nil {
		writeJSONError(w, http.StatusNotFound, "this server has no git store")
		return
	}
	if _, linked := s.gitBE.personalLink(login); !linked {
		writeJSONError(w, http.StatusNotFound, "no personal board linked")
		return
	}
	if err := s.gitBE.unlinkPersonal(r.Context(), login); err != nil {
		writeJSONError(w, http.StatusBadGateway, "remove the link: "+err.Error())
		return
	}
	if err := s.gitBE.attachPersonal(r.Context(), login, tok); err != nil { // no link any more: this detaches
		s.log.Warn("detach personal board", "login", login, "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
