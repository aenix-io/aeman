package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/pkg/ghprojects"
	"github.com/aenix-io/aeman/pkg/mcpserver"
)

// mcpTokenCtxKey carries the caller's GitHub token down to the tool handlers'
// ResolveToken seam. The streamable transport delivers TokenInfo on the
// per-request Extra (not on the handler context), so a receiving middleware
// bridges it onto the context the tools actually run with.
type mcpTokenCtxKey struct{}

// mcpLoginCtxKey carries the caller's GitHub login (the verified TokenInfo's
// UserID) to the ResolveLogin seam, so the default list can scope to their own
// Me board without a round-trip.
type mcpLoginCtxKey struct{}

// registerMCP mounts the MCP streamable HTTP transport at /mcp, guarded by the
// bearer-token middleware so unauthenticated callers get a 401 carrying the
// protected-resource metadata URL. It is only mounted in OAuth mode.
func (s *Server) registerMCP(mux *http.ServeMux) {
	handler := mcp.NewStreamableHTTPHandler(s.mcpServerForRequest, nil)
	protected := auth.RequireBearerToken(s.auth.verifyToken, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: s.auth.baseURL() + "/.well-known/oauth-protected-resource",
	})(handler)
	mux.Handle("/mcp", protected)
	mux.Handle("/mcp/", protected)
}

// mcpServerForRequest builds the aeman MCP server for an HTTP MCP session. The
// ResolveToken seam pulls the caller's GitHub token from the request context, so
// every tool call runs against GitHub with that user's own token.
func (s *Server) mcpServerForRequest(*http.Request) *mcp.Server {
	srv := mcpserver.New(mcpserver.Config{
		Owner:        s.opts.DefaultOwner,
		Project:      s.opts.DefaultProject,
		Lock:         s.opts.LockBoard,
		Version:      s.opts.Version,
		Endpoint:     s.graphqlEndpoint,
		HTTPClient:   s.httpClient,
		ResolveToken: resolveTokenFromContext,
		ResolveLogin: resolveLoginFromContext,
		// Route MCP writes through the shared board store, so agent edits update
		// the cache and reach the UI's watch stream like every other write.
		WrapBackend: func(b boardservice.Backend) boardservice.Backend {
			if s.gitBE != nil {
				// Git mode: the one shared store; the caller's identity and
				// rights ride the context, the credential is the server's.
				return s.visibleBE
			}
			return &storeBackend{inner: b, store: s.store}
		},
	})
	srv.AddReceivingMiddleware(injectGitHubToken, s.injectRights, s.dropSessionOnBadCredentials)
	return srv
}

// injectRights resolves the MCP caller's domain rights from the token and
// login injectGitHubToken placed on the context — the same decision the
// HTTP accessMiddleware makes for a browser visitor.
func (s *Server) injectRights(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if s.access != nil {
			tok, _ := ctx.Value(mcpTokenCtxKey{}).(string)
			login, _ := ctx.Value(mcpLoginCtxKey{}).(string)
			if tok != "" || login != "" {
				rights, err := s.access.rights(ctx, tok, login)
				if err != nil {
					return nil, fmt.Errorf("access could not be decided: %w", err)
				}
				ctx = withRights(ctx, rights)
			}
		}
		return next(ctx, method, req)
	}
}

// injectGitHubToken copies the GitHub token from the request's verified
// TokenInfo onto the handler context, where ResolveToken can read it.
func injectGitHubToken(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if extra := req.GetExtra(); extra != nil && extra.TokenInfo != nil {
			if tok, ok := extra.TokenInfo.Extra[githubTokenExtraKey].(string); ok && tok != "" {
				ctx = context.WithValue(ctx, mcpTokenCtxKey{}, tok)
			}
			if extra.TokenInfo.UserID != "" {
				ctx = context.WithValue(ctx, mcpLoginCtxKey{}, extra.TokenInfo.UserID)
				// Attribute activity events recorded by this call's mutations.
				ctx = boardservice.WithActor(ctx, extra.TokenInfo.UserID)
			}
		}
		return next(ctx, method, req)
	}
}

// dropSessionOnBadCredentials turns "GitHub says this token is dead" into the
// only answer that helps: forget the session, so the next call fails
// authentication and the client runs the OAuth flow again. Without it the
// session stays valid for its full TTL while every tool call fails against a
// token that will never work — the client cannot tell whose credentials are
// at fault and keeps retrying (an agent reported it as "the server uses its
// own, invalid token"; the server holds no token of its own).
func (s *Server) dropSessionOnBadCredentials(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		res, err := next(ctx, method, req)
		if err == nil || !errors.Is(err, ghprojects.ErrBadCredentials) || s.auth == nil {
			return res, err
		}
		id, _ := extraString(req, sessionIDExtraKey)
		if login := s.auth.dropSession(id); login != "" {
			s.log.Warn("github rejected a session token; session dropped, re-authorization required",
				"login", login, "method", method)
		}
		return res, fmt.Errorf(
			"your GitHub authorization is no longer valid (GitHub rejected the token); "+
				"sign in again to aeman to reconnect: %w", err)
	}
}

// extraString reads one string value off a request's verified TokenInfo.
func extraString(req mcp.Request, key string) (string, bool) {
	extra := req.GetExtra()
	if extra == nil || extra.TokenInfo == nil {
		return "", false
	}
	v, ok := extra.TokenInfo.Extra[key].(string)
	return v, ok
}

// resolveTokenFromContext is the MCP ResolveToken seam for the HTTP transport.
func resolveTokenFromContext(ctx context.Context) (string, error) {
	tok, _ := ctx.Value(mcpTokenCtxKey{}).(string)
	if tok == "" {
		return "", errors.New("no authenticated GitHub token for this MCP request")
	}
	return tok, nil
}

// resolveLoginFromContext is the MCP ResolveLogin seam for the HTTP transport:
// the caller's login is the verified session's UserID, injected above.
func resolveLoginFromContext(ctx context.Context) (string, error) {
	login, _ := ctx.Value(mcpLoginCtxKey{}).(string)
	if login == "" {
		return "", errors.New("no authenticated login for this MCP request")
	}
	return login, nil
}
