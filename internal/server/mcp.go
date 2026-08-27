package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aenix-io/aeman/pkg/boardservice"
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
		Owner:        gitBoardOwner,
		Project:      1,
		Lock:         true,
		Version:      s.opts.Version,
		ResolveLogin: resolveLoginFromContext,
		// The one shared store, as the caller may use it: identity and
		// rights ride the context, the push credential is the server's.
		Backend: s.visibleBE,
	})
	srv.AddReceivingMiddleware(injectGitHubToken, s.injectRights)
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

// resolveLoginFromContext is the MCP ResolveLogin seam for the HTTP transport:
// the caller's login is the verified session's UserID, injected above.
func resolveLoginFromContext(ctx context.Context) (string, error) {
	login, _ := ctx.Value(mcpLoginCtxKey{}).(string)
	if login == "" {
		return "", errors.New("no authenticated login for this MCP request")
	}
	return login, nil
}
