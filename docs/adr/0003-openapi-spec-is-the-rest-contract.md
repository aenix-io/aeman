# ADR 0003 — The OpenAPI document is the REST contract

**Status:** accepted · 2026-09-18

## Decision

`api/openapi.yaml` is the description of `/api/v1`. It is written by hand, it ships in the binary as `api.Spec`, and the server hands it out as JSON at `GET /api/v1/openapi.json`. Clients are generated from it; `GET /api/v1` no longer carries a catalog of its own.

The resource types stay hand-written. `pkg/apiserver` is a public importable package and an embedder's contract, so nothing generates it; the document MIRRORS those Go types — `x-go-type` names each one, `additionalProperties: false` closes it — and a contract test is what keeps the mirror true. The server's tests hold the exchanges they make against the document: the response always, the request when the server accepted it, statuses included — the watch, which is an upgrade rather than a response, and the document's own route excepted. A route the document does not describe fails, and so does an operation nothing serves.

Errors are RFC 9457 problems whose `code` is a CLOSED enum, checked in both directions against the codes the package can emit.

What the document cannot say stays prose. The WATCH protocol is an upgrade with no response body to describe, so its frames are declared as schemas nothing references and the protocol itself is in `docs/api.md`; so are the board rules that span routes — one action is one commit, what a capacity means, the domain closure.

## Why

The surface was described in four places that drifted: the mux, a doc comment above it, a literal endpoint catalog served at `GET /api/v1`, and `docs/api.md`. A door could exist in one and not the others — `PATCH /people/{login}` was wired and missing from the catalog — and a client had four answers to choose between. No response shape was checked: the Go tests decoded into the resource types, which take a missing or a surplus field without a word, and the frontend cast each payload to the type it hoped for.

Generating the server from the document would have inverted the problem: the types under `pkg/` are what embedders import, and a generator owning them would make a wire decision a breaking change for them. Generating nothing and writing the document by hand would have left the same drift in a new place. Mirroring plus a test that fails on drift keeps the hand-written types and still makes the document true.

Writing the document ahead of any generation also proves it against the server that exists, so the next change — a generated Go server — starts from a description already known to match rather than from a table nobody ran.

## Consequences

- The response schemas are a second copy of the Go structs. Nothing enforces them at compile time; a test does, and a field added in Go without a line of YAML fails the first test that sends it. That cost is the price of `pkg/apiserver` staying hand-written, and it is paid on every field.
- kin-openapi is a test-only dependency. It must stay that way: the binary keeps its short dependency list, which is why the document is served through the YAML parser already linked in rather than through the validator.
- A route added to `registerAPI` lands with its operation or the tests fail, in both directions. So does a new problem code.
- The document describes what the server does, not what it should do. Where the two were found to differ, the server won and the document says the true thing — a reader looking for the intended behaviour reads `docs/api.md` beside it.
- Frames on the watch stream are described but unvalidated: no test drives a WebSocket through the document, so those schemas are kept true by reading, like any prose.
