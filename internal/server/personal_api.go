package server

import (
	"net/http"
	"strings"

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
	return login, apiserver.PersonalInfo{Domain: board.PersonalDomain(login), URL: url}, true
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
	if fa, ok := s.access.(*forgeAccess); ok {
		// Self-hosted: the forge says whether the visitor may push there —
		// which needs a repository the forge knows (owner/repo in the URL).
		if _, err := repoSlug(in.URL); err != nil {
			writeJSONError(w, http.StatusBadRequest, "not a repository URL: "+err.Error())
			return
		}
		_, write, err := fa.probe(r.Context(), tok, in.URL)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, "could not check the repository: "+err.Error())
			return
		}
		if !write {
			writeJSONError(w, http.StatusForbidden, "you need push access to your personal repository")
			return
		}
	}
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
