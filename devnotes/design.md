# OnRamp — Architecture & Design

Status: **draft, under discussion.** Decisions are marked `[DECIDED]`, `[LEANING]` or `[OPEN]`.
Nothing here is built yet. Companion to `project_description.md`, which holds the domain framing
and is the source of truth for vocabulary.

---

## 1. Constraints discovered in the target system

Read from `~/Code/qniform6/openapi.yaml` (111 routes). These shape everything downstream.

### Event writes: batched, capped, all-or-nothing

`POST /firms/{firm}/events/<type>` — ~15 typed endpoints (`equity_trade`, `bond_trade`,
`customer_deposit`, `settlement`, `cash_transfer`, …). Each takes an **array, max 500 items**,
processed as **one all-or-nothing batch**: any invalid item rejects the whole request and nothing
is written. Response is always HTTP 200 with one result per input, in request order.

Consequences:

- 100M transactions is **≥200,000 requests**, minimum. Full loads are parallel batch jobs
  measured in hours, never interactive. This is the quantified form of "the API is probably the
  performance bottleneck".
- OnRamp needs **batch bisection**: on rejection, split the batch, retry, isolate the offending
  rows, quarantine them with their errors, and keep loading. One malformed row must never stall
  a load.
- Maker/checker matters: a caller with only `event:make` stages **exactly one event** for
  approval; arrays >1 item return 400. OnRamp's loader identity needs `event:self_post`.

### Reference writes: bulk, capped, per-item results

`PUT /firms/{firm}/accounts`, `/securities`, `/parties` — bulk upsert keyed on `code`, **max 500
items**, but **per-item status rather than all-or-nothing**. Partial success is normal and the
caller must inspect each item.

Two distinct failure tiers, which OnRamp must model separately:

- **Whole-request 400** — structural//policy violations (e.g. supplying a field the account
  `type` forbids).
- **Per-item `status: "error"`** — resolution failures (e.g. a `counterparty` code that doesn't
  resolve, or resolves to a party lacking the required role).

### `/simulate` — the fast-feedback primitive, half-built

Most event types have `POST /firms/{firm}/events/<type>/simulate`: runs the same conversion and
validation, **writes nothing**, returns the would-be ledger entries. Business-rule errors surface
identically.

But it takes a **single event object, not an array**. That makes it too chatty for a slice of
even a few hundred rows.

> **Action `[OPEN]`:** ask the Qniform team for a **batched `/simulate`**. Small change there,
> large one here — it is the difference between previewing a slice in one round trip and in 500.
> Also note: no `/simulate` exists for the reference upserts (accounts/parties/securities).

### The accounts endpoint is a good first target

`PUT /firms/{firm}/accounts` exercises more of the problem than its flatness suggests:

- **Nesting** — a `links` array of party references, merge-not-replace semantics.
- **Conditional requiredness by `type`** — `counterparty` is *required* for `custody`/`bank`/`pab`,
  *optional* for `customer` (omitting it means omnibus), and *rejected* for `inventory`/`control`.
  A naive column-to-column map cannot express this; it is a real test of the emitter layer.
- **Referential dependencies with role checks** — `counterparty`, `correspondent` and
  `responsible_party` are party *codes* that must resolve to a party holding a specific role
  (`customer`, `custodian`, `bank`, `rep`, `trader`, …).
- **Derived fields** — `correspondent` is normally *derived* from `responsible_party` and should
  usually be omitted. Sending it when it can be derived is not an error, but sending a wrong one
  fails the item.
- **Immutable-after-create fields** — `settlement_mode` is fixed once the account exists, and
  because upsert is a full replacement, *omitting* it on an account that has one fails the item.

`PUT /parties` carries the same hazard in its `roles` object: **omitted** leaves roles untouched,
**`{}`** retracts them all, and a role key asserts it. Omit-vs-empty is load-bearing in two places
now, so the emitter layer (§3, Layer 3) must distinguish "field absent" from "field null" — a
distinction most naive mapping layers lose.

Together these are a warning about idempotency: **re-running a Run against an already-loaded
Qniform instance is not automatically safe.** Reinforces the ephemeral-instance-per-run design
in §5.

---

## 1a. Vocabulary settled while building

`project_description.md` fixes the domain terms this system inherits (Migration Type, Source
System, Source Schema, Dataset, Xform, Run, Milestone, Outcome). Two more were needed and are now
fixed. `[DECIDED]`

- **Output** — one target API call that receives transformed data (`PUT /parties`). The code called
  these "stages" and the UI "emissions". Both read as implementation, and "stage" collided head-on
  with a Reject's *phase* — `run.stages[].rejects[].stage` was the same word for two unrelated
  things in one record.
- **Rule** — a named group of transformation logic binding one or more Source Schemas to one
  Output, carrying prose that says what it does and where it is not the obvious thing. This is the
  unit a person reasons in, and the level at which the mapping must be legible **without reading
  any SQL**.

Rules are *declared*, not inferred: the valuable part is the prose, and no analysis of the SQL
produces it. Two consequences worth keeping:

- **Models a Rule does not claim are shared preparation** — a principled definition of
  "implementation detail" rather than a judgement call about what to hide.
- **Identity is per-entity; minting is per-Rule.** Several Rules can feed one entity with different
  keys under one shared identity declaration and conflict policy.

A Reject's axis is now `phase` (`coercion` / `validation` / `target`), which is a different
question from *which Output* it happened on.

---

## 2. The central tension

Seconds-scale feedback over 100M rows is not achievable. Rather than pretend there is one loop,
stratify explicitly into three tiers. `[LEANING]`

| Tier       | Scope                         | Target latency | Qniform interaction                     |
|------------|-------------------------------|----------------|-----------------------------------------|
| **Slice**  | ~1k referentially-closed rows | < 1s           | `/simulate` + OpenAPI schema validation |
| **Sample** | ~1% stratified                | < 60s          | ephemeral instance                      |
| **Full**   | everything                    | hours          | ephemeral instance                      |

### The Slice is the key primitive

You cannot randomly sample 100M transactions: the rows reference accounts, securities and
parties absent from the sample, and everything fails on resolution errors for uninteresting
reasons. A **Slice** is a *referential graph closure* — pick N accounts, pull all their
transactions, all securities those touch, all parties referenced, transitively.

Make Slices first-class, named, versioned objects. Users will curate them ("the Puerto Rico muni
accounts", "the 12 accounts that broke on Tuesday"), pin them to Outcomes, and attach them to bug
reports. It is also what makes the AI loop viable (§6). **Closure semantics per Migration Type is
one of the first things to design.** `[OPEN]`

### Slice stability: narrow the fact side, not the dimension side

Because entities are *derived* (§3a), a naive Slice breaks a promise the product implicitly makes:
that a slice preview resembles the full run.

If a derived attribute involves aggregation — "party name = the most common spelling across this
rep's accounts" — then a Slice containing only account 401 derives `"John Smith"` while the full
run derives `"J Smith"`. **Slice results are not a subset of full-run results wherever derivation
aggregates.** Previewing a rule change then shows a difference that is an artefact of the slice
rather than of the change, which is worse than showing nothing.

Mitigation `[LEANING]`: a Slice narrows the **fact** side only and derives the **dimension** side
at full scope. The asymmetry is affordable — 100M transactions across 10M accounts still yields
only thousands of reps and branches, so deriving every party is cheap even in slice mode. Where
that is not affordable, the aggregation must be flagged in the UI as computed over partial data.

---

## 3. Transformation rule encoding

**`[LEANING]` Constrained SQL for logic + a non-Turing-complete declarative binding layer for the
flat→nested jump. Not JQ.**

Three layers:

### Layer 1 — Ingest (schema-on-read)

Datasets land **immutable, all-text, exactly as delivered**. Typed views are *derived* from the
Source Schema, not baked in at load.

**This layer decodes and types; it never derives.** `[DECIDED]` Trimming, encodings, null
sentinels, type coercion — and nothing that needs to know anything about the target. The typed
table on the far side is not a staging table standing in for the Dataset; it **is** the Dataset,
and the delivered file was only transport. See `iteration1-artifacts.md` §5.11 for what went wrong
when this was left implicit.

Therefore:

- Changing a column's type is a metadata edit, instantly re-runnable against a Slice.
- Coercion failures are first-class rejects, not crashes.
- The landed bytes are never rewritten — the audit trail depends on it.

### Layer 2 — Models: a DAG of named, versioned SQL SELECTs

dbt-shaped, without dbt. This is where "one Qniform call sources from many Datasets" is resolved:
joins, lookups, derivations, the judgement-heavy work.

### Layer 3 — Emitters: declarative binding to a Qniform request body

Generated from and validated against `openapi.yaml`. Nesting and arrays come from a grouping key
(group by `order_id` → array of allocations). Deliberately **not** a programming language, so it
can be checked at edit time and generated reliably.

**Why split 2 and 3?** The flat→nested step is mechanical and schema-driven — it should be
verifiable against the OpenAPI spec before Qniform is ever called. And keeping *all* logic in one
language means one lineage analyzer, one thing the AI must be good at, one thing to review.

### Why SQL over JQ

`project_description.md` names both. SQL wins on four counts:

1. **Set-based, so it scales.** JQ is row-at-a-time; at 100M rows that decides it.
2. **Statically analyzable for column lineage.** This is the hard requirement implied by "change
   a column type, you probably need to update a rule in sympathy". It cannot be delivered without
   parsing rules and extracting which source columns each reads. SQL has mature tooling; JQ
   effectively does not. See §7.
3. **LLM fluency.** The AI will write these. Orders of magnitude more SQL than JQ in training data.
4. **Client-readable.** A bank's own data people read SQL. Over a multi-month collaborative
   migration that is worth real money.

JQ's genuine advantage is nested-output construction, and Layer 3 takes that job away from it.

### The escape hatch

A registry of reviewed, versioned, **deterministic** UDFs for what SQL is bad at: COBOL
packed-decimal, EBCDIC, trailing-sign overpunch, `REDEFINES`, mainframe date formats. Ship as a
curated library per Migration Type — not ad-hoc user code. `[OPEN]`

---

## 3a. Derivation, dedupe and identity minting

**There is no 1:1 mapping from source datasets to target endpoints, and the architecture must
refuse to allow one to be assumed.** `[DECIDED]`

The legacy systems typically have **no party concept at all**, or not one that can be consumed.
An Account Master is often the only file received, and every party the target requires —
customers, reps, traders, branches — must be **derived from it**. This is the general case, not an
edge case, and designing around a source-row → target-row correspondence is the assumption that
"comes back around real hard".

Concretely, one flat FAS Account Master produces:

```
FAS Account Master  ──┬─→ distinct customers  ─→ PUT /parties      (roles: {customer:{}})
   (one flat file)    ├─→ distinct reps       ─→ PUT /parties      (roles: {rep:{}})
                      └─→ accounts            ─→ PUT /firms/{firm}/accounts
```

Four primitives fall out, all of which must exist from iteration 1:

### 3a.1 Deterministic key minting

Derived entities need codes that do not exist in the source. The minting function **must be a
pure function of the source values** — `FAS_REP_0042` from `REP-ID = '0042'`, or a hash of the
identifying tuple.

**Never a sequence number.** A sequence makes output depend on insertion order, which breaks
`Output = Apply(…)` outright and makes run diffs meaningless — Run 48 would re-mint different
codes for the same entities and every downstream reference would appear changed. This is the same
content-addressing discipline as §4, applied to entity identity.

### 3a.2 Dedupe is set-based

One rep appears on 500 accounts and must become one party. `SELECT DISTINCT` plus a deterministic
key function is the natural expression — and this is another point for SQL over JQ in §3, since
row-at-a-time processing is structurally bad at deduplication.

### 3a.3 Set-level conflicts are a new failure class

Accounts 1–400 say `REP-0042` is `"J Smith"`; account 401 says `"John Smith"`. **No individual row
is invalid** — the *set* is inconsistent. This does not fit the row-reject model in §10 and needs
its own category: a derived-entity conflict, reported against the derived entity with the
conflicting source rows attached.

This is a genuine migration problem, not a hypothetical, and FAS should seed it deliberately.

**Resolution is by quarantine, not abort.** `[DECIDED]` The conflicted entity is withheld; the
rest of the stage emits. Because a withheld identity cannot be referenced, quarantine **cascades
transitively** to every downstream row naming it, and OnRamp applies that cascade *before*
emitting rather than letting the target discover it — which keeps the error causally accurate and
saves the round trip. Quarantine is a pure function of the inputs, so a partially-quarantined Run
is still reproducible and still diffable. See `iteration1-artifacts.md` §2.3.1–2.3.2.

### 3a.4 Output ordering comes from the target, not the source

Accounts cannot load before the parties they reference. A Run is therefore a **DAG of emission
stages with ordering constraints**, not a single emit step.

Critically, that ordering is a property of **Qniform**, not of the project or Migration Type — it
is the same for every migration and is in principle derivable from `openapi.yaml`. Worth keeping
as a separate, reusable **target dependency graph** rather than something each project's xforms
restate. For iteration 1, declare it explicitly in the xform artifact and note it for later
extraction. `[OPEN]`

### 3a.5 Consequence for Layer 2

Layer 2 models produce **canonical target-shaped entities**, not "transformed source rows". The
intermediate layer is exactly where derivation, dedupe and minting live. The three-layer split in
§3 holds, but its middle layer should be understood in those terms.

---

## 4. Determinism as the performance mechanism

`Output = Apply(SourceSchemas, SourceDatasets, Xforms)` is presented in the project description as
an audit property. It is also the *speed* mechanism, and the design should lean on that reading.

**Content-address everything:** `[LEANING]`

- Dataset version = hash of content.
- Source Schema version = hash of canonical JSON.
- **Each model node** = hash of its SQL + the hashes of its inputs.
- A Run's identity = hash of the input triple.

Consequences:

- Reproducing a historical Run is free if its outputs are still resident.
- A changed rule invalidates **only its downstream cone**; everything else is a cache hit.

Per-*node* hashing rather than per-*run* is the part that matters. Get it wrong and every edit
recomputes the world.

---

## 5. Storage and execution

`[LEANING]` This is the "valid use case for a different database technology" the project
description allows for.

- **Postgres** — projects, schemas, the rule DAG, versions, Runs, audit, comments, knowledge
  base, Milestones/Outcomes. Small, relational, transactional.
- **Parquet on object storage** — Dataset content. Immutable, content-addressed, cheap.
- **DuckDB** — the execution engine. Vectorized, reads Parquet natively, sub-second over tens of
  millions of rows on one machine, Postgres-ish dialect that LLMs write well.

Honest cost: DuckDB in Go is cgo — build complexity and memory management. Fallback is a separate
worker process over gRPC, which is desirable anyway since runs should be isolated and killable.
`[OPEN]`

### Ephemeral Qniform instance per Run

Target a **throwaway Qniform schema per Run**, not a shared instance. Qniform's own test suite
already does this (`CREATE SCHEMA test_…`, migrate via `search_path`, drop `CASCADE`), so the
pattern is proven in that codebase. Buys isolated, parallel, disposable runs — three candidate
rule sets can execute concurrently and be diffed. Also sidesteps the upsert-idempotency hazard
in §1.

---

## 6. AI architecture

The obvious build — chatbot plus RAG over vendor PDFs — is the weak version. Six points, in
rough order of leverage:

### 6.1 Leverage is in tools, not the index

Once §2–§5 exist there are typed schemas, a lineage graph, run diffs, cheap slice execution and a
target OpenAPI spec. An agent with tool access to *those* beats semantic search over
documentation by a wide margin. Build the tool surface deliberately:

`get_schema` · `sample_column` (distinct values, null rate, cardinality, inferred format
patterns) · `lineage_of(field)` · `run_diff(a,b)` · `propose_change` · `run_slice`

**The AI is a client of the same API the UI uses.** Its power comes from the determinism and
introspectability built for humans.

### 6.2 Give the AI the verification loop, not just generation

This is the second, larger argument for investing in Slices early. An agent that proposes a rule,
runs it on a Slice in under a second, reads the diff and iterates is worth far more than one that
emits SQL blind. **Design the slice runner for an agent calling it hundreds of times** — cheap,
side-effect-free, structured errors.

### 6.3 The AI proposes; it never writes

Every AI change lands as a proposed changeset against the versioned DAG, carrying its rationale,
through the same review path as a human's. Not governance theatre: it is what makes the audit
trail survive the question a client's auditor asks in month 14. It also means the AI's reasoning
becomes knowledge-base content automatically.

### 6.4 Bright line: no LLM in the run path

An LLM call inside a transformation breaks `Output = Apply(…)` and destroys the property the
system rests on. **The AI writes rules; it is never itself a rule.**

One defensible exception, with guardrails: LLM-assisted bulk classification whose output is
**materialized and frozen as a versioned artifact** — classify 4,000 distinct free-text security
descriptions once, human-review the mapping table, thereafter it is a versioned lookup input like
any other Dataset. Live inference in the hot path is not the same thing and is not allowed.

### 6.5 Invert the knowledge base

The project description frames it as vendor documentation plus RAG. The tedious vendor docs are
the low-value bulk. The high-value content is the **decisions**:

> `ACCT-TYPE-CD = 'R'` means retirement account in this client's usage — confirmed with their ops
> lead 2026-03-04 — so tax-lot handling differs from the vendor default.

That knowledge is generated *by* the migration and currently dies in Slack. Capture it as typed
notes **attached to the objects they describe** — a schema field, a rule, an Outcome. Retrieval is
then structural first ("every note on this field and its downstream rules") and semantic second.
Hybrid beats pure vector search here because the graph is authoritative and already present.

### 6.6 Two-tier knowledge with a hard firewall

Knowledge should carry across projects — but project A is Bank A's confidential data, and leaking
it into Bank B's project is a business-ending event, not a bug.

- **Client-specific** knowledge stays in the project.
- **System-general** knowledge (about the legacy platform itself) is promotable to global.

Promotion is an **explicit, reviewed, audited action**. Never automatic, never inferred. This
constraint is absent from `project_description.md` and needs to be in the data model from day one
— retrofitting a tenancy boundary onto a shared index is miserable.

### 6.7 Evaluation

The assistant makes judgements that cost months when wrong. It needs its own regression suite:
past migration decisions with known-good answers, replayed on every prompt or model change.
Mining completed migrations for this corpus is high-value early work. `[OPEN]`

---

## 7. SQL lineage — the load-bearing spike

Column-level lineage underpins both impact analysis ("you changed this column; here are the 14
rules affected") and much of the AI's value. If it turns out to be hard, **Layer 2's design must
change to compensate** — hence spike it before committing.

Run it as a throwaway, timeboxed to ~2–3 days, against **5 deliberately nasty representative
queries** (`SELECT *`, CTEs, window functions, UNION, a self-join, a lateral).

1. **sqlglot (Python) as the oracle.** `optimize` + `lineage` does real column resolution — star
   expansion, CTE flattening, scope resolution. Establishes what *correct* looks like.
2. **DuckDB `json_serialize_sql` in Go.** Same dialect that actually executes, in-process, no new
   deployment unit. How close does it get, and how much AST-walking code does it cost?

The sqlglot run is not wasted even if the Go path wins — it becomes the **test oracle** for the
Go implementation.

**Fallback if both disappoint:** have models *declare* their inputs explicitly (dbt's `ref()`
model). Model-level lineage for free, column-level only where declared. Degraded but workable,
which is what bounds the risk.

---

## 8. Under-weighted in the project description: Run diffing

Regression tests catch what someone thought to test. What is also needed: **between Run 47 and
Run 48, what changed in the output, and which rule change caused it?**

That is how you catch the tweak that fixed 3 accounts and silently broke 40,000 — the
characteristic failure of month-9 migrations under deadline pressure. Given content-addressed
outputs it is nearly free. Should be in v1. `[LEANING]`

---

## 9. Stack

**Go backend `[DECIDED]`.** Heavy compute is in DuckDB; Go is the coordinator and is good at that.
Matches qniform6 — shared OpenAPI tooling, shared idioms, one language for the team. Anthropic
ships a Go SDK, so the AI layer is not a reason to move.

One genuine wrinkle: the best column-lineage tooling is Python (sqlglot), and lineage is
load-bearing. See §7 — resolve by spike, not by argument.

**React/TypeScript frontend `[DECIDED]`**, but budget honestly: this is an IDE, not a CRUD app.
SQL editor with intelligence, virtualized grids over millions of rows, a lineage graph, a
structured diff viewer. Most of where "fast, observable, feels good" is won or lost. qniform6's
Vite / Tailwind / TanStack Query stack carries over.

**Postgres `[DECIDED]`** for metadata; see §5 for why it is not the whole story.

---

## 10. Iteration 1 — walking skeleton

Goal: exercise the full end-to-end path (minus AI) on trivial volumes, to tease out how schemas,
rules and runs are actually represented.

### Scope

- Start a new project of Migration Type **FAS** (Fake Accounting System).
- The project ships with the **FAS Account Master** Source Schema (fields, types, descriptions,
  required flags). This is the **only** dataset — as in the real case, there is no party file.
- Upload a small Dataset (10–20 accounts), validated against the schema.
- Execute a Run, which **fans the one dataset out into two Outputs in dependency order**:
  1. derived parties — distinct customers (role `customer`) and reps (role `rep`), with minted
     codes → `PUT /parties`
  2. accounts, referencing those minted codes → `PUT /firms/{firm}/accounts`
- Observe both emissions in the app, alongside the source rows **and the derivation** — which
  source rows produced which party.
- **No** schema or xform editing yet.

20 source rows producing 7 parties and 20 accounts *is* the fan-out insight the product exists to
deliver, so it is worth showing well even at this scale.

### Deliberate design rules for iteration 1

These cost little now and are expensive to retrofit:

1. **FAS is an adversary, not a convenience.** Build it to emit realistically ugly data —
   packed dates (`0991231`), trailing-sign overpunch (`1234K`), fixed-width padding, a `FILLER`
   column, codes whose meaning depends on account type. A clean FAS makes iteration 1 succeed and
   teach nothing. **Include at least one derived-entity conflict** (§3a.3): the same `REP-ID`
   spelled two ways across rows, so the set-level failure class is exercised at 20 rows.
2. **The xform is an artifact, not a Go function.** A file (SQL + emitter binding) that is
   *loaded and executed*; the editor is simply not exposed yet. A hardcoded Go transform makes
   iteration 2 a rewrite and teaches nothing about how rules are managed — which is the point of
   the iteration.
3. **The xform declares a list of inputs, even though the list has one entry.** Guards against
   baking in a 1:1 Dataset→endpoint assumption, which the architecture would happily accept and
   then break on in iteration 2.
4. **Content-address and version from day one**, even with nothing editable. Schema, Dataset and
   xform hashed; the Run records the triple. Free now, a rewrite later.
5. **Rejects are a first-class output.** A Run produces *(payloads, rejects, stats)* — rejects
   carrying reason and source row. Same shape needed at 100M rows.
6. **Run the SQL through DuckDB even at 20 rows.** Absurd overkill, and exactly the point: find
   out in week 1 whether cgo hurts, not in month 4.
7. **Validate output against `openapi.yaml`.** "Qniform-shaped" output that is never checked
   against the real spec is a mock, and mocks in a walking skeleton hide precisely the integration
   problems the skeleton exists to find.

### Why parties are derived rather than seeded

Accounts reference parties by code (`counterparty` → role `customer`, `responsible_party` → role
`rep`, required for customer accounts) which must resolve *and* hold the right role.

Seeding those parties into Qniform out-of-band was considered and **rejected**: it would smuggle
in the 1:1 source→target assumption §3a exists to prevent, at the data level, while the artifact
design was busy guarding against it at the structural level. Deriving them is barely more work at
20 rows and exercises minting, dedupe, conflict and stage ordering — the four things that
actually generalise.

Helpfully, both `customer` and `rep` are **attribute-free roles** (`properties: {}` in
`PartyRoles` — the empty object is the whole assertion) and are asserted **inline on the party
upsert**, so no third Output is needed.

**Deferred deliberately:** the `branch` role and the `BranchOfFirm` / `BranchOfCorrespondent`
relationships. Reps belong to branches, and `correspondent` is normally derived through that
edge — but `correspondent` is optional on customer accounts, so iteration 1 can omit branches
entirely. Picking them up is a good iteration-2 or -3 increment, since party *relationships* are a
third structural shape beyond entities and roles.

### Questions iteration 1 must answer

- How is a Source Schema represented, and what is its type vocabulary? (It needs COBOL-ish types.)
- Where does the xform artifact live, and what is its unit of versioning?
- What exactly does a Run record contain?
- How does the emitter express the flat→nested binding, including conditional-by-`type` fields?
- How is a **minting function** expressed and where does it live — in the model SQL, or as a
  declared property of a derived entity?
- How are **Outputs and their ordering** declared in the xform artifact?
- How is a **set-level derived-entity conflict** (§3a.3) reported, as distinct from a row reject?
- Does DuckDB-in-Go hurt?

---

## 11. Decision queue

1. Will Qniform accept a **batched `/simulate`**? Changes the whole feedback loop's shape. `[OPEN]`
2. **SQL lineage extraction** spike — §7. `[OPEN]`, narrowed: DuckDB's `json_serialize_sql` works
   in-process and returns a full AST (with a `::VARCHAR` cast), so the Go-native path is worth
   spiking before committing to a Python sidecar. Producing the AST was never the hard half.
3. **Slice closure semantics** per Migration Type. `[OPEN]`
4. ~~DuckDB **in-process vs. worker service**~~ — `[DECIDED]`: in-process. Measured at ~25ms to
   open and query, ~10s first build, and Parquet needs no extra library. The constraint that will
   eventually force a change is concurrent runs colliding on materialised table names, not cgo.
   See `iteration1-artifacts.md` §5.1.
5. Transformation rule encoding — confirm §3. `[LEANING]`
6. ~~Where the **minting function** lives~~ — `[DECIDED]`: minting is plain SQL, but the identity
   is *declared* in the xform manifest so determinism can be checked statically and conflicts
   detected generically. `iteration1-artifacts.md` §2.2.
7. Whether the **target dependency graph** (§3a.4) is extracted from `openapi.yaml` or hand-
   maintained. `[OPEN]` — note it is now needed in both directions: forwards for emission
   ordering, backwards for the pre-emission quarantine cascade (§3a.3).
8. Conflict-resolution vocabulary beyond `error` — when `most_common` / `first_by` become
   available, and how the *reason* for choosing one is captured. `[OPEN]`

---

## 3b. No intermediate model, and an emitter that cannot become one

`[DECIDED]`

### The problem

Iteration 1 grew a model called `stg_accounts` that looked like staging and was not: it minted
identity codes, decided a party's role from a sibling column, combined joint holder names, and
mapped source enums onto Qniform's. Meanwhile the emitter could rename any column (`from`) and drop
any field on an arbitrary SQL predicate (`omit_when`).

Between them, source data reached the target through **three** vocabularies — the source's, an
accidental middle one, and the target's — with business decisions distributed across all three.

The sharper point is not that this happened, but that **the architecture permitted it**. Whatever a
user can do, a user will do, under deadline, at month nine.

### The decision

**There is no intermediate model. A rule produces the target's own fields, named as the target
names them.** The model layer is the target's shape, flattened; the only reason a flat form exists
at all is that SQL cannot produce nesting.

Enforced at run time (`checkColumnsAgainstTarget`): every column a rule produces must be

1. a field of the target endpoint, resolved from `openapi.yaml`; or
2. a column a nesting key is read from, which becomes a key rather than a field and so has no
   target name; or
3. `__`-prefixed infrastructure that never leaves the system.

Anything else fails the run and names the column. A typo (`allocaton_method`) fails the same way,
which is a useful side effect rather than the point.

### The emitter is reduced to mechanics

Both escape hatches are gone.

- **`from` is deleted.** One name from end to end. Renaming in the emitter would be a second place
  to make a semantic decision about what a column *means*.
- **`omit_when` is deleted.** It was shown to be redundant: the model already produced `NULL` for
  `counterparty` on inventory accounts, and the emitter already omits NULL columns rather than
  sending `null`. Removing it changed no output. The distinction between *absent* and *null* is
  real and load-bearing in Qniform's API, but SQL expresses it perfectly well as NULL, decided by a
  fixed rule nobody can configure.

What remains — the field list, `map`/`key_from` nesting, `references`, batch config — has **no
expression syntax**, so it cannot carry a business rule. That is the property being bought, and it
is structural rather than a matter of discipline.

### What this costs

A genuinely shared derivation used by several rules — an FX lookup, a normalised security
identifier — now has nowhere to live and must be repeated. That is already the case for the mint
expressions (`'FAS_CUST_' || "ACCT-NO"` appears in two rules), and it is survivable because drift
is caught: mismatched mints make every account reference unresolvable, which the cascade test
asserts against.

When repetition stops being survivable, the answer is an **explicitly declared shared artifact** —
versioned and visible like a rule — and not an unclaimed model that quietly becomes a vocabulary.
`[OPEN]`

The other real cost is coupling: an intermediate would insulate the rules from Qniform's API
changing. That coupling is accepted deliberately. The target *is* the product, and its spec is
versioned and available.

---

## 3c. A rule owns its whole chain

`[DECIDED]` Supersedes the split in §3b where the payload shape hung off the endpoint.

A **Rule** is now the complete path from source to target: the schemas it reads, the identity it
mints, the SQL that flattens, and the shape of the payload that leaves. Nothing downstream renames,
reinterprets or reshapes what a rule produces, so a rule can be read end to end.

Three concepts dissolved into that:

- **Output is gone.** A rule names its target endpoint directly (`to: PUT /parties`). There is no
  OnRamp-side object standing between a rule and Qniform's endpoint, so there is nothing to drift
  from the spec.
- **The entities section is gone.** With identity per-rule, the conflict policy moved onto the mint.
  What an entity *is* belongs to Qniform's spec, so `entity:` is now just a name linking a
  reference to the rules that mint it.
- **`after:` is gone.** Qniform's referential order (securities, parties, party-relations, accounts,
  events) is fixed for every migration, so it lives once in `qniform.go` rather than being restated
  per project. An unranked target is an error, which forces a decision about what it depends on
  instead of silently landing last.

### What this cost, and how it was bought back

Sharing one payload shape across the rules feeding an endpoint was doing real work: it was the only
thing making identity an entity-wide property. Collapse ran across the union, so two rules could not
mint the same code unnoticed. Per-rule pipelines lose that, and Qniform explicitly models a party
holding several roles, so a party that is both a customer and a rep is foreseeable rather than
exotic.

Bought back by **declaring the mint**:

```yaml
mints:
  entity: party
  prefix: FAS_REP_
  from: [REP-ID]
  conflict: { on: [name], resolve: error }
```

Two rules minting one entity must have **disjoint prefixes**, checked at load. Collision becomes
impossible by construction rather than detected after the fact — and the consequence, which is the
right one, is that a multi-role party must be authored by one rule explicitly rather than emerging
from a collapse that got it silently wrong before.

### Why the mint is declared rather than left in SQL

Identity does far more work than the expression suggests. It deduplicates, detects disagreement,
resolves what other rules reference, drives the quarantine cascade, aligns one Run against another
for diffing (§8), and is the closure a Slice has to follow (§2). **Every one of those needs to know
the mint without executing it.**

The slice case is the sharpest: closing over "these 12 accounts" means finding the parties they
reference, and parties do not exist in the source. Either you run the whole transform and walk the
result — which defeats the point — or you walk symbolically: *this account references
`FAS_REP_ || REP-ID`, so pull the source rows where `REP-ID` matches*. That is a join on the mint's
source columns, which is exactly `mints.from`.

Two further consequences now in place:

- **References never restate the mint.** `counterparty: { references: customers }` — the model holds
  the raw source key, and the referenced rule's prefix is applied when the payload is built. The
  expression is written once, so the two cannot drift. Previously drift was caught only by the
  accident that it made every reference unresolvable.
- **The mint is checked and constrained.** `checkMintedPrefix` verifies the SQL actually mints into
  the declared key space, and a mint reading `now()`, `random()`, a sequence or a UUID is rejected
  at load: an identity that is not a pure function of the source makes output depend on insertion
  order, which breaks reproducibility and makes every row of a run diff look changed.

### A win that fell out

The `role` column in `party_customers.sql` existed **only** because one payload shape served two
rules. With its own shape, the customers rule states its role as a literal
(`roles: { object: { customer: {} } }`) and drops the column — and §3b's guard flagged it as dead
the moment it stopped being read. Literals add no expressiveness, since a constant cannot compute.
