# ADR 0004 — The Go server interface is generated from the document

**Status:** accepted · 2026-09-18

## Decision

`internal/server/apiv1` is generated from `api/openapi.yaml` by oapi-codegen (`std-http-server` + `strict-server` + `models`), and `registerAPI` registers what it emits. A handler is a method on one type taking the request already bound and answering with one of the response objects the document names for that operation; `var _ apiv1.StrictServerInterface = surface{}` is the compiler's proof that no described operation is unanswered.

The generated output is committed, because nothing that builds this repository runs a generate step. `make generate` rebuilds it together with `web/src/api/schema.d.ts` — one document, two generated artefacts — and CI regenerates both and fails on a difference.

Three patterns stay hand-registered, and only these three. The index cannot be a generated operation: under a server of `/api/v1` its path is `/`, which the generator registers as the subtree pattern `GET /api/v1/`, and that outranks the catch-all and answers every unknown GET beneath it. The watch upgrades the connection and needs the raw `http.ResponseWriter`, which a strict method is not handed. The catch-all is no operation at all.

## Why

[ADR 0003](0003-openapi-spec-is-the-rest-contract.md) made the document the contract and forecast this change. Until it landed the routes were written down twice — in the document and again as a list of `mux.HandleFunc` lines — and the only thing holding the two together was a test comparing them. Generating the server closes that by construction: a route exists because the document describes it, and there is no second list to drift.

The resource types stay hand-written for the reason ADR 0003 gave: `pkg/apiserver` is an embedder's contract, and a generator owning it would make a wire decision a breaking change for them. Only the surface — routes, parameters, request bodies, response objects — is generated.

## Consequences

- A route is added to the document, not to `registerAPI`. `TestEveryWiredRouteIsDescribed` asserts the hand-registered set is exactly the two exceptions, so an addition there fails even when its operation is described.
- Four answers changed, three of them pinned by tests; the fourth is the `charset=utf-8` the generated writers do not set. The behaviour matrix (G75) lists them.
- A `description` in the document becomes a doc comment in both generated artefacts. A false sentence there used to be prose nobody executed; it now propagates into Go and TypeScript.
- The binary links `oapi-codegen/runtime` and its transitive dependencies. kin-openapi stays test-only, as ADR 0003 requires. The `tool` directive also puts the generator in the module graph of anything that imports this module — see [embedding.md](../embedding.md).
- `api/openapi.yaml` carries three vendor extensions that are generation decisions rather than part of the contract it publishes: `x-go-name` on one schema, because a parameter of the same name would otherwise claim the Go type, and `x-go-type` on the two required integers, because a plain `int` cannot tell an absent field from a zero and zero means "take the set number back" there. The document is served verbatim at `GET /api/v1/openapi.json`; other tooling ignores `x-` keys and a reader should too.
- Whether an empty request body is tolerated is decided by `requestBody: required:` in the document, not by the handler: only `false` makes the generated code forgive `io.EOF`. One operation relies on it, and a test pins it.
