# Code-quality review: `feat/initial`

Fixed point `main` (the empty scaffold). `git diff main...feat/initial` is non-empty: 155 files, 19,802
insertions. Every Go file, test, doc and workflow was read against the two rubrics below, plus the
project's own documentation (`AGENTS.md`, `README.md`, `docs/cli-spec.md`, `docs/adr/`) and the site
notes in `docs/obramat-api-discovery.md`.

Line references are to the review point (`cc586c3`), before the fixes.

## Size check first

No Go file reaches 1000 lines; the largest is `internal/cli/watch.go` at 580 (see T1). Nothing in the
diff pushes a file across the threshold, so the 1k rule is not violated.

## Rubric 1: Thermo-Nuclear Code Quality

| id | sev | finding | where | disposition |
|---|---|---|---|---|
| T1 | M | **Divergent change.** `watch.go` hosts the whole `sink` noun (`sinkRow`, `sinkList`, `sinkCmd`, ~75 lines) beside the watch commands. It is edited for two unrelated reasons, and it contradicts the repo's own layout rule ("one file per noun", cli-spec §12). | `internal/cli/watch.go:507-580` | Fixed: moved to `internal/cli/sink.go`. |
| T2 | M | **Duplicated code.** `expandHome` and `home` are the same `~/` expansion in two packages. | `internal/cli/auth.go:168`, `internal/sink/sink.go:192` | Fixed: one `config.ExpandHome`. |
| T3 | M | **Duplicated vocabulary / repeated switch.** Event-type strings are constants in `watch` and bare literals in the `sink` switch; the canonical home is `state`, which owns `Event`. | `internal/watch/watch.go:18-25`, `internal/sink/sink.go:88-105` | Fixed: constants moved to `internal/state`, every site uses them. |
| T4 | M | **Duplicated state.** A search watch stores the in-stock flag twice: `SearchTarget.InStock` and `Params.InStock`. The check reads the first, the fetch uses the second. | `internal/watch/watch.go:30,89`, `internal/cli/watch.go:175` | Fixed: one field, `Params.InStock`. |
| T5 | L | **Speculative generality / dead surface.** `harvest.Options.UserAgent` is set by the CLI and never read by the package; the CLI applies the override itself. | `internal/harvest/harvest.go:41`, `internal/cli/harvest.go` | Fixed: field deleted. |
| T6 | L | **Mysterious name.** `Event.Item` holds a Product, and the project vocabulary explicitly avoids "item". | `internal/state/state.go:56` | Fixed: `Event.Product` (JSON key unchanged). |
| T7 | L | **Magic coupling.** `Kind.ExitCode` is `int(k) + 1`, so inserting a kind renumbers every exit code. | `internal/api/errors.go:32-37` | Fixed: explicit switch. |
| T8 | L | **Bespoke hot-path work.** `jsonIsland` compiles a fresh regexp on every call, on every page parse. | `internal/api/html.go:45-48` | Fixed: cached per id. |
| T9 | L | **Bespoke helper.** `mcp.contains` duplicates `slices.Contains`. | `internal/mcp/mcp.go:266` | Fixed: `slices.Contains`, helper deleted. |
| T10 | M | **Shotgun surgery.** `needsSite` hardcodes command paths, so every new site command must remember to edit it; the fact belongs beside the command. | `internal/cli/root.go:49-57` | Deferred (see below). |
| T11 | L | **Large, mixed module.** `listing.go` holds orchestration, the SSR models, card parsing and price parsing. | `internal/api/listing.go` | Deferred (see below). |
| T12 | L | **String-matched control flow.** `isCobraUsage` matches cobra's error text. | `internal/cli/root.go:160-168` | Deferred (see below). |
| T13 | L | **Thin constructor.** `updater.Client()` returns a fresh `http.Client` per call. | `internal/updater/updater.go:58` | Deferred (see below). |

No spaghetti growth, feature checks in shared paths, cast-heavy contracts or unnecessary `any` were
found: `any` is confined to JSON boundaries (`output`, `mcp`, `state`) and the two `interface{ Scan }`
helpers are the right abstractions. No file was pushed past 1k lines.

## Rubric 2: Pocock — Standards

| id | sev | finding | rule | where |
|---|---|---|---|---|
| S1 | M | `Event.Item` for a Product is a documented vocabulary violation. | AGENTS.md: _Product … Avoid: item_ | `internal/state/state.go:56` (fixed with T6) |
| S2 | M | The same `~/` expansion twice is Duplicated Code. | baseline | `internal/cli/auth.go:168`, `internal/sink/sink.go:192` (fixed with T2) |
| S3 | M | Event names are duplicated between the producer and the sink switch; a changed literal would silently stop notifications. | baseline: Duplicated Code, Repeated Switches | `internal/watch/watch.go:18-25`, `internal/sink/sink.go:88-105` (fixed with T3) |
| S4 | M | `watch.go` contains the sink noun: Divergent Change, and a breach of cli-spec §12 ("one file per noun"). | baseline + cli-spec §12 | `internal/cli/watch.go:507-580` (fixed with T1) |
| S5 | L | `harvest.Options.UserAgent` is never read: Speculative Generality. | baseline | `internal/harvest/harvest.go:41` (fixed with T5) |
| S6 | L | Two copies of the in-stock flag in one stored target: Data Clumps / duplicated state. | baseline | `internal/watch/watch.go:30` (fixed with T4) |
| S7 | L | `mcp.contains` where `slices.Contains` exists. | baseline: bespoke helper | `internal/mcp/mcp.go:266` (fixed with T9) |

Checked and deliberately not flagged: `state.Seen` (honest name for "what the watch last saw"), the
`mcp` `str`/`num`/`boolean` constructors (they remove a lot of struct-literal noise), `installView`
(a rendering adapter, not a pass-through), and the `interface{ Scan(...any) error }` helper (the
canonical way to scan both `Row` and `Rows`, and not tooling-enforced).

## Rubric 2: Pocock — Spec

Spec source: `docs/cli-spec.md`, with `docs/obramat-api-discovery.md` for the site facts.

| id | sev | finding | spec line | disposition |
|---|---|---|---|---|
| P1 | M | **Implemented narrower than written.** The spec says a command that needs the storefront exits 3 with no session, but only `auth status --check` calls `requireSession`; `search`, `product`, `store` etc. run without a session and let the site answer (which is the intended first-run behaviour). | cli-spec §7 "a command that needs the storefront was run with no browser session"; §auth "A command that needs the storefront with no session exits 3" | Fixed the spec wording (behaviour unchanged): only `auth status --check` exits 3. |
| P2 | L | The same overstatement is repeated in the ADR. | `docs/adr/0004-browser-session-and-chrome-tls.md` | Fixed with P1. |
| P3 | L | The layout rule the sink commands break is documented. | cli-spec §12 "one file per noun" | Fixed with T1. |

No missing command, flag or JSON field was found: every item in cli-spec §4–§10 is implemented
(`search`/`filters`, `product show` + `item`, `category list`, `store list|near|show|stock`,
`watch add|list|remove|check|run|events|service`, `sink list|test`, `auth login|harvest|status|logout`,
`mcp` + `install`, `config path|list|get|set`, `doctor`, `skills`, `update`, `completion`). The JSON
contracts match the Product/Price/Stock/Listing/Store types field for field. The site notes match the
parsers: pagination and the dependency-error page, the store cookie minimum, lazy card groups, the
stock drawer, the category sitemap and the store sitemap.

No scope creep worth flagging: the only behaviour beyond the letter of the spec is the "no stock answer
for N stores" notice on `store stock --all`, which serves the spec's "until every store has answered".

## Deferred, with reasons

- **T10** (`needsSite`). Moving the fact onto the commands (a cobra annotation) is the right shape, but
  it touches every site noun and the 10-minute pause gate is safety-critical; the list is small,
  covered by `TestNeedsSite`, and changing it now risks more than it buys. Revisit if a fourth site
  noun appears.
- **T11** (`listing.go`). 534 lines, one subject (reading a listing page), well under the 1k rule. A
  split into `cards.go`/`filters.go` would move code without deleting a concept — rearrangement, not
  the simplification the rubric asks for.
- **T12** (`isCobraUsage`). Cobra does not expose a typed usage error; matching its text is the least-bad
  option short of vendoring a fork.
- **T13** (`updater.Client()`). A one-line constructor, called twice, on a cold path; replacing it with
  a package var would save nothing real.
- **Primitive Obsession** (store codes as `int`, references as `string`): these are the wire formats and
  the spec's own vocabulary, already wrapped where it matters (`StoreRef`, `IsRef`).

## Summary

- Thermo-Nuclear: 13 findings — 0 high, 5 medium (T1–T4, T10), 8 low. Fixed 9, deferred 4.
- Standards: 7 findings — 0 high, 4 medium, 3 low. All fixed.
- Spec: 3 findings — 0 high, 1 medium (P1), 2 low. All fixed (docs only).
- No file over 1000 lines; the largest (`watch.go`, 580) drops to ~490 with the sink split.
- Behaviour unchanged throughout: the fixes rename, move, or delete dead/duplicated surface.
