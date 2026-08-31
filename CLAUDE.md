# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Status

Iteration 1 (the walking skeleton) is built and passing. `project_description.md` holds the domain
framing and vocabulary; `devnotes/design.md` holds the architecture and its open decisions;
`devnotes/iteration1-artifacts.md` holds the concrete artifact formats and a §5 recording what the
build changed about them.

The earlier standing instruction not to write code has been lifted by the user.

Decisions in `devnotes/design.md` are marked `[DECIDED]` / `[LEANING]` / `[OPEN]` — keep those
markers and its §11 decision queue current as things are settled. Constraints read out of
Qniform's OpenAPI spec are recorded in its §1 and are expensive to rediscover.

## Commands

- `bash build.sh` — format, vet, test, build both halves. The full pass.
- `bash run.sh` — build and start the server (does **not** run tests). Needs `DATABASE_URL`.
- `go test .` — the Go suite. It loads the target spec from `targets/qniform/openapi.yaml` and
  fails rather than skipping if absent, deliberately: a silently skipped spec check is how
  "Qniform-shaped" output drifts into being a mock.
- `bash spec-check.sh` — reports drift between OnRamp's copy of the target contract and Qniform's
  own. Out of band on purpose; nothing in a Run reads the Qniform repository.
- `go test -run TestApplyFanOutAndCascade .` — one test.
- `cd web && npm run dev` — Vite on :5173, proxying `/api` to :8090. Gate frontend type checking on
  `npm run build` (`tsc -b`), not `tsc --noEmit`.

Environment: `DATABASE_URL` (required), `ONRAMP_ADDR` (default `:8090`), `ONRAMP_STORE` (default
`data/`), `ONRAMP_TARGET_SPEC` (override the vendored spec; tests only), `QNIFORM_SPEC` (upstream
location, read by `spec-check.sh` alone).

## Dev servers: the default ports are the user's

**The user runs their own backend and frontend, always on the defaults** — `bash run.sh` on
**:8090** and `npm run dev` on **:5173**. Assume anything listening there is theirs and in use,
even when the process args are identical to what you would have started (`run.sh` execs the same
`./onramp` binary, so args cannot tell them apart).

Never free those ports. To check something yourself, start your own on a different one:

```
ONRAMP_ADDR=:8091 DATABASE_URL="$DATABASE_URL" ./onramp &     # your backend
(cd web && npm run dev -- --port 5174)                        # your frontend, if needed
```

Kill what you start and **only** what you start — by the PID you launched, not by a name pattern:

```
lsof -nP -iTCP:8091 -sTCP:LISTEN     # confirm it is yours before killing
```

For read-only checks, using the user's running server is fine — prefer that to starting anything.

## Architecture

The system is one pure function, and everything else serves it:

```
Output = Run(SourceSchemas, TargetSpecs, Xforms, SourceDatasets)
```

Four inputs — what the source looks like, what the target looks like, how to map between them, and
the data itself — all content-addressed, so a Run's identity is their **digest** and any Run is
exactly reproducible. `run.go` holds it; read that first.

**The target spec is an input, not a dependency**: spec validation *withholds* payloads, so a
changed contract changes the output. It is hashed per target (`Spec.HashFor`), because only the
endpoints a transform writes are inputs to it. Do not move it out of `InputVersions` —
`TestTargetSpecIsPartOfTheContract` pins this.

**The spec is OnRamp's own file** (`targets/qniform/openapi.yaml`), checked in. It is a description
of a data format, in the same category as a SourceSchema — one describes the source's shape, the
other the target's — and is OpenAPI only because that is what Qniform emits. Never read it from
`~/Code/qniform6` at run time: that made a foreign, unpinnable file part of the deterministic
contract and left Runs recording a spec hash nothing could resolve. Drift is `spec-check.sh`, and
adopting a change is a decision, not a refresh.

**Storage is split three ways** and this is deliberate (`design.md` §5): Postgres holds metadata
only (`db.go`, `migrations/001_initial_schema.sql`); dataset content lands as Parquet under
`ONRAMP_STORE`, never rewritten; DuckDB (`engine.go`, in-process) is the execution engine.

**The pipeline**, in the order data moves:

1. `schema.go` + `encoding.go` — a Source Schema's fields carry a **logical type** *and* a
   separate **physical encoding**. Keeping them apart is what stops the type vocabulary exploding
   per legacy platform: `overpunch_trailing` and `cyymmdd` are encodings of `decimal` and `date`,
   not types. Null sentinels (`0000000`, `N/A`) are per-field schema data, because legacy systems
   have no NULL and which sentinel means absent is discovered knowledge.
2. `dataset.go` — coerces every cell, lands typed rows as Parquet, withholds rows that fail.
   **Ingest decodes and types; it never derives.** No joins, no enrichment, no minting — anything
   that needs to know about the *target* is a rule. The typed table on the far side of ingest **is**
   the Dataset; the CSV was only transport, and there is no staging layer to name.
3. `transform.go` — **a Rule owns its whole chain**: the schemas it reads (`from`), the target endpoint
   it writes (`to`), the identity it mints (`mints`), the SQL that flattens (`model`), and the
   payload shape (`payload`). There is no Output, no entities section and no `after:` — a rule names
   Qniform's endpoint directly, conflict policy lives on the mint, and load order is Qniform's own
   (`qniform.go`), recorded once rather than declared per project. **The mint is declared, not left
   in SQL**: identity drives dedupe, conflicts, references, the cascade, run diffing and slice
   closure, and all of those need it without executing it. Two rules minting one entity must have
   disjoint prefixes (checked at load); `checkMintedPrefix` verifies the SQL agrees with the
   declaration. **Every SQL file belongs to exactly one Rule** (`Validate` rejects an unclaimed one),
   and an **entity** is the generated union of what its rules produce (`Engine.UnionInto`), never a
   hand-written model. Both rules exist because an unclaimed "preparation" model is where rule logic
   hides while the rule page shows a thin projection — which is exactly what happened with the
   `stg_accounts` this replaced. An **Output** is one target API call; a **Rule** is a named group binding source
   schemas to one Output, carrying the prose that makes the mapping legible without reading SQL.
   Rules are declared, not inferred. Models no Rule claims are shared preparation and are
   implementation. Do not reintroduce "stage" or "emission" for these — "stage" collided with a
   Reject's `phase`, which is a different axis entirely. The rules are a *directory* (`migration_types/FAS/transforms/fas-accounts/`): a YAML
   manifest and one SQL file per rule under `sql/`. Model order is inferred from the SQL; entity identity is
   *declared*.
4. `binding.go` — the target binding, mechanics only and deliberately incapable of more. **There is no intermediate
   model**: a rule produces the target's own fields, named as the target names them, and
   `checkColumnsAgainstTarget` in `apply.go` fails any run whose columns are neither fields of the
   target endpoint, nor read for a nesting key, nor `__`-prefixed. The emitter has **no expression
   syntax** — no `from` (one name end to end) and no `omit_when` (absence is a NULL in the model,
   which the emitter omits by a fixed rule). Do not reintroduce either: they are what let a second
   transformation layer grow. What is left is the field list, `map`/`key_from` nesting,
   `references`, and batch config. See `devnotes/design.md` §3b.
5. `run.go` — one story in order: what a Run *is*, what it takes to ask for one (`RunRequest`), and
   `ExecuteRun` — which collapses on the declared identity, runs the quarantine cascade, and emits.
   Identity is declared per **entity**; minting is per **Rule**, so two rules can feed one entity
   with different keys under one shared conflict policy.
6. `openapi.go` — validates every emitted batch against Qniform's real spec before reporting, and
   (`output_api.go`) joins that spec against the emitter bindings to give each Output a **coverage**
   view: every field the target accepts, marked with whether a rule binds it. `components:` is not
   parsed, so a `$ref` property shows its component name only.

**Three ideas do most of the work.** Each is easy to break by accident:

- **There is no 1:1 mapping from source to target.** FAS has no party file, so every party is
  *derived* from account records. One flat file fans out into several emissions in dependency
  order. Any change that assumes one dataset means one endpoint is wrong.
- **Minting must be a pure function of source values** — never a sequence, never `random()` or
  `now()`. A sequence makes output depend on insertion order, which breaks reproducibility and
  makes run diffs meaningless.
- **Conflicts quarantine, they do not abort**, and quarantine is **transitive**. A withheld party
  withholds every row naming it, and OnRamp applies that cascade itself *before* emitting so the
  error stays causally accurate ("withheld because its rep conflicts") rather than arriving from
  Qniform as twenty unresolvable-code errors.

Rejects carry three orthogonal axes (`reject.go`): `class` (row vs set-level `conflict`), `stage`
(`coercion` / `validation` / `target` — the extract's fault, ours, or Qniform's), and `cause`
(linking a withheld row to the upstream reject that caused it).

**Transport is split from domain by file**, following qniform6: every file with an HTTP surface
has an `_api.go` sibling, and `api.go` is only the route index.

## Conventions

- A Run's `reason` is **required**, refused if blank. The audit obligation is about *why*.
- Tests call `t.Parallel()`.
- After changing Go code run `gofmt -w .` and `deadcode .`.
- `runs.body` is `JSON`, **never `JSONB`** — JSONB normalises object key order and silently
  discards the field order the payload declares.
- Determinism is a test, not an aspiration: `TestRunIsDeterministic` compares two runs byte for
  byte. Two ordering bugs that no functional test would have caught were found by it.

## What OnRamp is

A tool for migrating large financial firms (brokers, banks) from a legacy books-and-records
system onto **Qniform**. A migration runs for months or years. The user-facing loop is:

get client data → define a schema for it → write transformation rules that load it into Qniform
→ run regression tests against that Qniform instance and inspect the result → revise schema,
rules and tests → repeat.

Making that loop fast and observable is the core product problem — the link between source data
and resulting Qniform state is the insight the user is paying for, and the Qniform API is the
likely bottleneck.

## Domain vocabulary (use these terms exactly)

- **Migration Type** — which of the big four legacy COBOL/mainframe systems a project migrates
  from. Keys the project and determines the out-of-the-box base schemas.
- **Source System** — any system data can arrive from, including bespoke client systems outside
  the Migration Type.
- **Source Schema** — a named "table" from a Source System: per-field name, type, description,
  required flag.
- **Dataset** — an uploaded body of data keyed to a Source Schema, validated against it.
- **Xforms** — the transformation rules mapping source data to Qniform API calls.
- **Run** — one execution, capturing the versions of all inputs plus run number, timestamp and
  user. A project is a sequence of Runs.
- **Milestone / Outcome** — project-side structure. 4–5 Milestones per project (3 weeks to
  2 months each), each containing Outcomes stated as expected Qniform behaviour
  ("Equity Trades can create trade confirms, but not when the customer has opted out"). Outcomes
  are the anchor that regression tests and the rule changes serving them stay linked to.

## The invariant the architecture hangs off

```
Output = Apply(SourceSchemas, SourceDatasets, Xforms)
```

All three inputs are versioned, so any past Run is exactly reproducible from its recorded input
versions. Every change to any of them is audited with who, when, and **why** — the "why" matters
as much as the change, because a decision made six months earlier must still be explicable.
Any design proposal that breaks determinism or drops the rationale trail is wrong for this system.

## Constraints that shape every design decision

- **Volume**: tens of millions of accounts, hundreds of millions of transactions — assumed from
  day one, in tension with the seconds-scale transform/observe/tweak loop.
- **Schema variance**: base schemas per Migration Type are a starting point only. Standardized,
  semi-standardized and wholly novel schemas all coexist in one project.
- **Structural mismatch**: source data is flat and near-uniformly string-typed; Qniform's API is
  mostly flat but not entirely. One Qniform call typically draws from several Datasets.
- **Concurrency**: many people work one migration over months.
- **Knowledge base**: what's learned about legacy-system behaviour must outlive the project and
  be available across the whole OnRamp userbase, not just the team that learned it.
- **AI assistance**: the product goal is a "Data Engineer in a Box" so a non-technical user can
  drive the loop — tracing the implications of a schema or rule change, with examples. Combined
  with the knowledge base (legacy vendor documentation, RAG) this extends to business judgement:
  how to map something, what regression tests to write.

## Technology

Decided: Go backend exposing an API to a React frontend; Postgres unless a specific purpose
justifies otherwise. Everything else is open — the user has flagged **how transformation rules
are encoded** as the decision needing to come early.

## The target system: Qniform

Qniform lives at `~/Code/qniform6` (a separate repo, not a dependency of this one). It is the
migration target and the shape OnRamp must produce.

- `~/Code/qniform6/openapi.yaml` — the full REST surface OnRamp's Xforms target (~10.5k lines;
  grep it, don't read it whole).
- `~/Code/qniform6/CLAUDE.md` — its architecture and conventions; useful context for what
  "loaded into Qniform" means, and a reference for house style.
- `~/Code/qniform6/devnotes/` — domain notes on regulatory and accounting behaviour.

## Vocabulary that has already been settled

These were renamed deliberately; do not reintroduce the old words.

| Gone | Use | Why |
|---|---|---|
| Xform | **Transform** | no need to abbreviate until it hurts |
| Emitter, PayloadSpec | **TargetBinding** | it binds flat rows to the target's shape |
| Model | **the rule's SQL**, at `sql/<rule name>.sql` by convention | a rule has exactly one; "model" implied a separate layer, and naming the file only gave the two a way to disagree |
| Stage, staging | **Dataset** / `DataPath` | the coerced table *is* the Dataset; the CSV was transport |
| Output (declared) | **target endpoint** | no OnRamp object between a rule and Qniform |
| stage (on a Reject) | **phase** | collided with the emissions, which were also "stages" |
| Prior | **WithheldAtIngest** | says what it holds |
| triple | **digest** | there are four inputs, not three |
| record_version | *nothing* | premature: no run predates this week, and versioning six shapes protected only throwaway data |
