# Git backend for aeman — research report (phase 1)

Date: 2026-08-27. Client under test: go-git v5.19.2 (v6.0.0-alpha.5 checked for the same gaps — identical). Servers: real `git-upload-pack` locally over the file transport, and GitHub over SSH and HTTPS. Data: the live board `aenix-org` #37 — 2488 items, exported in full.

The spike code lives in the job's scratch directory and is not merged. The measured history is on `aenix-org/aeman-db` branches `spike/shallow-gate` and `spike/board37-replay`; `main` is untouched.

## Verdict

**The gate passes.** go-git can shallow-clone, deepen an already-shallow clone to a *time* horizon in one round-trip, and push from the shallow clone — against GitHub. Every gap found is in go-git's convenience layer, not in the protocol or the storage, and each is closed by 15–80 lines of code over its public plumbing API. The design can proceed on go-git.

**The numbers justify the move.** Cold start drops from ~50–110 s (GraphQL, full board) to **2.6 s** (shallow clone from GitHub). A full board read from the clone is **~100 ms**. A card write as a commit is **1.5 ms**. Memory for a warm board is single-digit MB.

## 1. Gate: shallow / deepen / push

| step | local git-upload-pack | GitHub, SSH | GitHub, HTTPS+token |
|---|---|---|---|
| push 300 commits | — | 2.64 s | — |
| clone `--depth 1` | 25 ms | 1.47 s | 0.46–0.87 s |
| deepen to a date (`deepen-since`, one round-trip) | 40 ms | 1.76 s | — |
| `Fetch{Depth:N}` on a shallow clone + unshallow fix | 24 ms | 1.93 s | — |
| commit + push from the shallow clone | 56 ms | 2.28 s | 1.23 s |
| no-op fetch / advertise refs | — | 1.40 s | 0.17–0.21 s |

GitHub advertises `shallow`, `deepen-since` and `filter` on both transports.

### Gaps in go-git and how each is closed

1. **`repository.Log` ignores the shallow list.** Only `Pull`'s fast-forward check reads it. On a shallow repo the log walker runs into the missing parent and fails with `object not found`. → Our history walker stops *at* the boundary: visit the shallow commit, do not cross it. (`NewCommitPreorderIter`'s `ignore` set means "do not visit" — off by one for this.)
2. **`Fetch` applies the server's `shallow` lines but not its `unshallow` lines.** They are parsed (`resp.Unshallows`) and never used, so after deepening the old boundary stays marked and the log is still cut at depth 1 although the objects arrived. → Either drop every shallow entry whose parents are all present (~15 lines), or use the plumbing path below, which applies both lists.
3. **`FetchOptions` carries only a commit count.** `DepthSince` exists in `packp` but is not reachable from the public API. → Drive one upload-pack session by hand: `transport.Client → NewUploadPackSession → UploadPackRequest{Depth: DepthSince(t), Shallows, Haves} → sideband demux → packfile.UpdateObjectStorage → SetShallow(old + shallows − unshallows)`. ~80 lines, exact to the day, one round-trip, works against GitHub. (The alternative — a doubling loop on `Depth` with a date check — took 4 rounds for a 120-day horizon and overshot by 2×.)
4. **A rejected push from a shallow clone surfaces as `object not found`, not `ErrNonFastForwardUpdate`.** The pre-push fast-forward walk falls into the boundary's missing parent. → The retry loop must not classify by error type: on *any* push error, fetch; if nothing new arrived the push really failed, otherwise re-apply and retry. Verified in three fetch flavours.
5. **SSH host keys.** go-git reads `~/.ssh/known_hosts` but does not pin host-key algorithms to the stored ones → `knownhosts: key mismatch` against GitHub. Production is distroless with no `~/.ssh` anyway. → Default to HTTPS + token (faster on every op, forge-agnostic: basic auth on GitHub/GitLab/Gitea). SSH stays possible with an explicit `known_hosts`.
6. **Plain `Fetch` (no `Depth`) is the everyday sync.** It brings new commits down to what we have without adding shallow entries. Repeated `Fetch{Depth:1}` piles up boundaries (harmless for push, untidy).

## 2. Speed on the real board

Board 37 exported in full: 2481 draft cards, 7 issues, 0 PRs; 100 hidden state cards (37 epic, 7 sprint, 7 project, 14 deadline, 9 process, 26 process-task); bodies 3.1 MB total (median 475 B, p90 3.3 KB, max 30 KB); **20 169 event lines** in 2101 card logs and 725 notes.

Baseline: paging the whole board out of GraphQL took **1 min 51 s** (50 pages × 50 items via `gh`); the server's own cold load was observed at ~52 s in production.

History replayed as **one commit per event** (20 169 commits, dated and authored from the log), through plumbing only — blob → leaf tree → `cards` tree → root → commit, no worktree:

| | |
|---|---|
| initial commit, 2488 files | 0.58 s |
| replay, per commit (16-shard layout) | **1.46 ms** |
| push the 20 170-commit history to a local bare | 40 s (go-git pack: 34 MiB) |
| the same after real `git gc --aggressive` | 11.4 MiB |
| push the 20 170-commit history to GitHub (HTTPS) | 14.8 s |

The server's hot paths, against that history on GitHub (`spike/board37-replay`):

| op | result |
|---|---|
| **cold start**: clone `--depth 1`, filesystem storer, bare | **2.59 s**, heap +2.2 MB, 1.7 MB on disk |
| **full board read**: tree walk + YAML front-matter of 2388 cards (3.2 MB) | **95–154 ms** (40 ms from the memory storer) |
| **one card write as a commit** | **1.45–1.64 ms** |
| push 5 queued commits in one push | 1.23 s |
| deepen 4 weeks back (`deepen-since`) | 3.76 s → 11 653 commits reachable, heap +32 MB, 9.5 MB on disk |
| deepen to the full history (20 175 commits) | +3.31 s, heap +52 MB total, 15.8 MB on disk |

## 3. Layout: sharding is arithmetic, not taste

Every commit rewrites the tree of each directory on the changed file's path. Three shardings of the same 20 169-commit replay:

| shard | dirs under `cards/` | `cards` tree object | per commit | loose store | go-git push pack | push to bare |
|---|---|---|---|---|---|---|
| flat | 1 (2388 files) | **140 KB** | 4.79 ms | 1.3 GB | 189 MiB | 4 m 32 s |
| 2 tail chars | 589 | 17 KB | 2.33 ms | 566 MB | 170 MiB | 1 m 46 s |
| 1 tail char | 16 | 448 B | **1.46 ms** | 402 MB | **34 MiB** | 40 s |

The rewritten intermediate tree is the whole cost. Rule for the design: keep every directory that gets rewritten per commit at a few hundred entries at most. Shard by the id's *tail*: ULID heads are time-ordered and near-constant, head-sharding degenerates into one directory.

The spike's ids are GitHub item ids, whose tail alphabet is narrow — that is why "1 tail char" gave 16 directories, not 32. The two-level layout the design prescribes was measured separately on the same replay, with a Crockford-base32 pair derived from a hash of the id (the distribution a ULID tail gives):

| shard | dirs | `cards` tree | one leaf tree | objects in history | go-git push pack |
|---|---|---|---|---|---|
| 1 level, 16 dirs | 16 | 448 B | ~150 files, ~9 KB | 91 544 | 34 MiB |
| 2 levels, 32 × 32 | 925 realized | 896 B | ~5 files, ~300 B | 109 562 | 40 MiB |

Two levels rewrite ~1.2 KB of trees per commit instead of ~9 KB, but add one object per commit: +20 % objects and +18 % pre-gc pack at this board size. The trade is bought for scale — at 10 000 cards a single level of 32 has 19 KB leaf trees, the size that produced the 170 MiB history above, while two levels stay flat. (The spike's per-commit milliseconds are not comparable between these two runs: its tree builder walks every directory per commit and dominates the 925-directory case.)

## 4. Memory and growth with nobody packing

go-git writes every object loose and never packs on its own.

| | filesystem storer | memory storer |
|---|---|---|
| clone `--depth 1` (2388 cards) | heap +2.2 MB, 1.7 MB disk | heap +7.1 MB |
| 2000 commits (commit-per-action) | 1.21 ms each, **+16.6 MB disk** (~8 KB/commit loose), heap +7.1 MB | 74 µs each, heap +1.7 MB |
| after deepening to 20k commits | heap +52 MB (pack index + LRU cache), 15.8 MB disk | — |

At ~100 actions a day that is ~0.8 MB/day of loose objects, ~300 MB/year, in ~4 files per commit. **`Repository.RepackObjects` + `Prune` handle it in-process**: 91 544 loose files (104 MB) → 35.5 MB in 2 files in 44.7 s (a nightly run over one day's objects would be well under a second). No git binary needed.

The memory storer is fast (74 µs/commit, 40 ms full read) and smaller than expected, but it loses everything on restart — the filesystem storer on the existing `/data` volume keeps unpushed commits across a container restart and is inspectable with `git log`. Filesystem is the recommendation; the storer is a one-line swap if that ever changes.

## 5. Concurrency: two writers, one remote

Loop (go-git has no rebase): commit → push → rejected → fetch → hard-reset onto the new tip → re-apply OUR change → push. 200 cards, 2 writers × 40 actions each, on depth-1 clones.

| scenario | pushes | rejected | same-card conflicts | history | per action |
|---|---|---|---|---|---|
| disjoint cards | 120 | 40 | 0 | 81 = 80 + seed ✓ | 48 ms |
| both writers on the same 3 cards | 120 | 40 | 14, resolved last-write-wins by re-apply | 161 = 81 + 80 ✓ | 64 ms |

`fsck` clean apart from dangling commits (a rejected push has already uploaded its objects; the remote's gc collects them). Re-applying a queued change is a file-level "write this card's final state" — the DeltaFIFO queue already holds exactly that, so re-apply *is* the conflict resolution: last write wins per card, today's semantics.

**Starvation**: in a tight retry loop one writer lost every race until the other had pushed all 40 of its commits (`max-retries-for-one-action = 40`). Fix: backoff with jitter on reject, and push the whole local queue in one push so a replica that wins once flushes everything it has.

## 6. What the event log is — and is not

Replaying the 20 169 logged events on top of a rewound snapshot and comparing the end state with the real snapshot: **1282 of 2488 cards differ**. By field: `stage` 943 (the log records *derived* states — `in-progress`, `done` — that the snapshot never stores), `zone` 404 and `progress` 293 (moves and clamps that never produced an event), `parent` 178 (the log stores the parent's *title*, the field its *id*), `sprint`/`start`/`day` 60–73 (carry-over and reseed writes without events), `plan`/`week` 10–15, `assignees` 3, `epic` 1.

So the log is an annotation, not a journal: today's state is *not* reconstructible from it. Two consequences for the design:

- **Migration**: the snapshot is the truth; events become synthetic commits whose *messages* carry the event and whose file changes are best-effort. The final commit of the migration must be the exact snapshot, and the migration must verify that.
- **The git backend fixes this by construction**: every write is a commit, so from the switch-over on, history is complete — the property the current log was supposed to give and cannot.

## 7. Side findings

- `Status` is `Todo` on all 2487 cards that have it — a vestigial field; it does not migrate.
- The three cards titled `aeman: …` (with a space) are ordinary cards, not state cards.
- Pushing over SSH costs ~1.4 s per operation in handshake + advertisement alone; HTTPS ~0.2 s. Nothing in the design puts a network op on a request path, but background push cadence and the deepen path benefit either way.
- Spike branches to remove when done (destructive, left to the maintainer): `git push git@github.com:aenix-org/aeman-db.git --delete spike/shallow-gate spike/board37-replay`.
