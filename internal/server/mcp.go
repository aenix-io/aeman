package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aenix-org/aeman/internal/boardservice"
	"github.com/aenix-org/aeman/internal/mcpserver"
)

// mcpTokenCtxKey carries the caller's GitHub token down to the tool handlers'
// ResolveToken seam. The streamable transport delivers TokenInfo on the
// per-request Extra (not on the handler context), so a receiving middleware
// bridges it onto the context the tools actually run with.
type mcpTokenCtxKey struct{}

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
		// Route MCP writes through the shared board store, so agent edits update
		// the cache and reach the UI's watch stream like every other write.
		WrapBackend: func(b boardservice.Backend) boardservice.Backend {
			return &storeBackend{inner: b, store: s.store}
		},
	})
	srv.AddReceivingMiddleware(injectGitHubToken)
	return srv
}

// injectGitHubToken copies the GitHub token from the request's verified
// TokenInfo onto the handler context, where ResolveToken can read it.
func injectGitHubToken(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if extra := req.GetExtra(); extra != nil && extra.TokenInfo != nil {
			if tok, ok := extra.TokenInfo.Extra[githubTokenExtraKey].(string); ok && tok != "" {
				ctx = context.WithValue(ctx, mcpTokenCtxKey{}, tok)
			}
		}
		return next(ctx, method, req)
	}
}

// resolveTokenFromContext is the MCP ResolveToken seam for the HTTP transport.
func resolveTokenFromContext(ctx context.Context) (string, error) {
	tok, _ := ctx.Value(mcpTokenCtxKey{}).(string)
	if tok == "" {
		return "", errors.New("no authenticated GitHub token for this MCP request")
	}
	return tok, nil
}
