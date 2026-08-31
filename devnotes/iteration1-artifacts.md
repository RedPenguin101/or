# Iteration 1 — concrete artifacts

Status: **sketch for discussion.** Nothing built. Companion to `design.md` §10.

Three artifacts, in the order they are exercised: the **Source Schema**, the **xform**, and the
**Run record**. Every open question `design.md` §10 asks is answered here as a proposal, so the
argument can be about shapes rather than principles.

---

## 1. Source Schema — FAS Account Master

### 1.1 Design decision: type ≠ encoding

The single most important call in the schema format. A legacy field has **a logical type** and
**a physical encoding**, and conflating them makes the type vocabulary explode.

`PIC S9(7)V99` with trailing-sign overpunch is not a distinct type — it is a `decimal` with an
`overpunch_trailing` encoding. `0991231` is not a distinct type — it is a `date` with a `cyymmdd`
encoding.

Consequences:

- The **type vocabulary stays small and target-facing**: `string`, `integer`, `decimal`, `date`,
  `boolean`, `enum`.
- The **encoding vocabulary grows per Migration Type** and is where the mainframe ugliness is
  quarantined. It maps directly onto the UDF registry in `design.md` §3.
- Layer 1 (schema-on-read) becomes: read text → apply encoding → get typed value → coercion
  failures are rejects, not crashes.

### 1.2 Design decision: null sentinels are a schema property

Legacy systems do not have NULL. They have `0000000`, `99999999`, spaces, `N/A`, and `UNKNOWN` —
and which sentinel means "absent" is field-specific knowledge that is discovered, argued about,
and then must not be lost. It belongs in the schema, versioned and auditable, not buried in a rule.

### 1.3 Layout

`[LEANING]` Iteration 1 uses **CSV layout with COBOL-encoded values**. This is realistic — a
mainframe extract to CSV preserves the ugly value formats while losing the fixed-width framing —
and it defers the fixed-width reader without losing any of the adversarial quality.

The format reserves `layout: fixed` with per-field `offset`/`width` for later.

### 1.4 Sketch: `migration_types/FAS/schemas/account-master.yaml`

```yaml
source_system: FAS
schema: ACCOUNT-MASTER
title: Account Master
description: >
  One record per account. FAS has no party file — customers and registered
  representatives exist only as denormalised columns on this record and must
  be derived. See devnotes/design.md §3a.
layout: csv
header: true

fields:
  - name: ACCT-NO
    type: string
    required: true
    description: Account number. Unique. Space-padded to 10 in the source file.
    encoding: { kind: trim }

  - name: ACCT-TYPE-CD
    type: enum
    required: true
    description: >
      Account class. Drives almost everything downstream: H maps to a Qniform
      `inventory` account (no counterparty, responsible party is a *trader*),
      everything else to a `customer` account (counterparty required, responsible
      party is a *rep*).
    values:
      I: Individual
      J: Joint
      C: Corporate
      H: House / firm inventory

  - name: ACCT-SHORT-NAME
    type: string
    required: true
    description: Display name, 30 chars, space-padded.
    encoding: { kind: trim }

  - name: CUST-NAME-1
    type: string
    required: false
    description: >
      Primary holder name. Absent on H accounts. NOTE (open): on J accounts this
      is one of two holders — Qniform models a joint holding as a single party,
      so the two lines must be combined rather than producing two parties.
    encoding: { kind: trim }
    null_values: ["", "N/A"]

  - name: CUST-NAME-2
    type: string
    required: false
    description: Second holder on J accounts; otherwise blank. Frequently misused as an address line.
    encoding: { kind: trim }
    null_values: ["", "N/A"]

  - name: REP-ID
    type: string
    required: true
    description: >
      Registered rep number, zero-padded to 4. On H accounts this is the trading
      desk id, not a rep — the same column carries a different meaning by account
      type. `0000` means unassigned.
    encoding: { kind: trim }
    null_values: ["0000"]

  - name: REP-NAME
    type: string
    required: false
    description: >
      Rep display name. Denormalised onto every account and NOT kept consistent —
      the same REP-ID appears with several spellings. Deriving one party per rep
      means resolving that conflict. See design.md §3a.3.
    encoding: { kind: trim }

  - name: BRANCH-CD
    type: string
    required: false
    description: Branch/office code. Not consumed in iteration 1 (see design.md §10, deferred).
    encoding: { kind: trim }

  - name: OPEN-DT
    type: date
    required: true
    description: Account opening date.
    encoding: { kind: cyymmdd }

  - name: CLOSE-DT
    type: date
    required: false
    description: Account closing date; sentinel-null when open.
    encoding: { kind: cyymmdd }
    null_values: ["0000000", "0991231"]

  - name: CASH-BAL
    type: decimal
    scale: 2
    required: true
    description: Ledger cash balance, signed. Not migrated directly — opening balances are events.
    encoding: { kind: overpunch_trailing }

  - name: COST-METHOD-CD
    type: enum
    required: false
    description: Lot relief method. Blank on accounts that hold no positions.
    values:
      F: FIFO
      L: LIFO
      A: Average cost
      " ": None
    null_values: [""]

  - name: MARGIN-IND
    type: boolean
    required: false
    description: Margin agreement on file.
    encoding: { kind: yn }
    null_values: [" ", ""]

  - name: STATUS-CD
    type: enum
    required: true
    description: Record status.
    values: { A: Active, C: Closed, P: Pending }

  - name: FILLER-1
    type: string
    required: false
    description: Unused in the copybook. Retained because extracts still carry it.
```

### 1.5 Encoding vocabulary for FAS

| kind | meaning |
|---|---|
| `trim` | strip pad spaces |
| `cyymmdd` | 7-char date, leading century indicator (`0`=19xx, `1`=20xx) |
| `overpunch_trailing` | signed decimal, last character carries the final digit *and* the sign (`{`..`I` = +0..+9, `}`..`R` = -0..-9), so `1234K` is -12342, or -123.42 at scale 2 |
| `yn` | `Y`/`N`/space → boolean |
| `packed_decimal` | COMP-3. Reserved — needs `layout: fixed`, not used in iteration 1 |

---

## 2. The xform

### 2.1 Design decision: a directory, not a file

Models are SQL and want to be SQL files — editable, diffable, syntax-highlighted, and readable by
the client's own data people (`design.md` §3). The manifest is small YAML. Splitting them keeps
each in its natural form.

```
transforms/fas-accounts/
  transform.yaml                    manifest: inputs, entities, stages, ordering
  models/
    stg_account_master.sql      typed staging view over the raw dataset
    party_customers.sql         derived customer parties
    party_reps.sql              derived rep/trader parties
    parties.sql                 UNION of the two — one model per emission target
    accounts.sql                the accounts themselves
  emitters/
    parties.yaml
    accounts.yaml
```

`parties.sql` unions the two derivations so there is **one model per emission target**, which
keeps stages aligned with the target dependency graph (`design.md` §3a.4). The union carries a
`derived_from` tag column so observability does not lose which derivation produced a row.

### 2.2 Design decision: minting is SQL, but identity is *declared*

`design.md` §11 item 6, answered.

The minting expression is plain SQL — `'FAS_REP_' || "REP-ID"` — because the whole thesis is
SQL-as-substrate and inventing a minting DSL would be a second language for no gain.

But the **fact that a column is an entity identity** is declared in the manifest. That declaration
is what makes it possible to:

- check determinism statically (reject a mint that references a sequence, `random()`, `now()`,
  or a window function without a total order);
- drive impact analysis ("`REP-ID` changed type — 1 identity and 3 rules depend on it");
- detect the set-level conflicts in §2.4 generically rather than per-rule.

### 2.3 Sketch: `transform.yaml`

```yaml
xform: fas-accounts
version: 1
description: FAS Account Master to Qniform parties and accounts.

inputs:
  - schema: FAS/ACCOUNT-MASTER      # list of one today, list by construction
    as: account_master              # (design.md §10, rule 3)

entities:
  - name: party
    model: parties
    identity:
      column: code
      derived_from: [REP-ID, ACCT-NO]   # for impact analysis + determinism check
      conflict:
        # what to do when one identity is derived from rows that disagree
        on: [name]
        resolve: error                  # error | first_by | most_common
  - name: account
    model: accounts
    identity:
      column: code
      derived_from: [ACCT-NO]

stages:
  - name: parties
    entity: party
    emitter: emitters/parties.yaml
    target: PUT /parties

  - name: accounts
    entity: account
    emitter: emitters/accounts.yaml
    target: PUT /firms/{firm}/accounts
    after: [parties]                    # explicit for now; extractable later
```

`resolve: error` is the right iteration-1 default: surface the REP-NAME conflict loudly rather
than silently picking a spelling. `most_common` and `first_by` exist so the eventual UI has
somewhere to record the decision — and the *reason* for it, per the audit requirement.

**`error` quarantines, it does not abort.** `[DECIDED]` The conflicted entity is withheld from the
emission; every other entity in the stage emits normally. This matches the per-item failure mode
of the reference endpoints, and it stops one bad rep from hiding every other problem in the run —
which at 100M rows is the difference between one useful run and twenty wasted ones.

### 2.3.1 Quarantine is transitive

The consequence that does the real work. A quarantined identity cannot be referenced, so **every
downstream row naming it must be quarantined too**, carrying `cause` back to the upstream reject.

Crucially, OnRamp should apply that cascade **itself, before emitting** — not emit the rows and
let Qniform reject them. Three reasons:

1. **The error is causally accurate.** Locally: *"account 0000998877 withheld — its rep
   FAS_REP_0042 is quarantined by a name conflict across 4 source rows."* From Qniform: *"party
   code does not resolve"*, twenty times over, with the actual cause nowhere in sight.
2. **It saves the round trip**, which matters once a stage is 200,000 requests.
3. **It keeps `cause` truthful.** If the target discovers the failure, the causal link has to be
   reconstructed by inference; if OnRamp withholds the row, it already knows why.

So the loader needs a **pre-emission quarantine pass** over the reference graph — the same target
dependency graph as `design.md` §3a.4, walked in the opposite direction. That is a real piece of
iteration-1 machinery and it is easy to miss when there are only 20 rows and everything resolves.

### 2.3.2 Quarantine preserves determinism

Worth stating explicitly, because it is the property everything else rests on: quarantine is a
**pure function of the inputs**. The same triple quarantines exactly the same entities. A run with
quarantines is still reproducible, still diffable against another run, and still a legitimate
`Output = Apply(…)`. It is a *partial* run, not a *failed* one, and the Run record's status
vocabulary must reflect that rather than being binary.

### 2.4 Sketch: `models/party_reps.sql`

```sql
-- One party per distinct rep. The same REP-ID appears on many accounts with
-- inconsistent REP-NAME spellings; the identity is the ID, and the name conflict
-- is surfaced by the `conflict` rule in transform.yaml rather than silently resolved.
--
-- ACCT-TYPE-CD = 'H' makes this a trading desk, not a rep, so the role differs.

SELECT DISTINCT
    'FAS_REP_' || "REP-ID"                    AS code,
    "REP-NAME"                                AS name,
    ''                                        AS pre_name,
    CASE WHEN "ACCT-TYPE-CD" = 'H'
         THEN 'trader' ELSE 'rep' END         AS role,
    'rep'                                     AS derived_from
FROM stg_account_master
WHERE "REP-ID" IS NOT NULL
```

Note what this one small model already demonstrates: derivation from a source with no such
entity, deterministic minting, set-based dedupe, and **one source column yielding a different
target role depending on a sibling column**.

### 2.5 Sketch: `emitters/accounts.yaml`

The emitter is deliberately not a programming language (`design.md` §3, Layer 3).

```yaml
target: PUT /firms/{firm}/accounts
batch:
  max_items: 500
  failure_mode: per_item          # vs all_or_nothing for the event endpoints
row_key: code

fields:
  code:              { from: code }
  type:              { from: account_type }
  name:              { from: name }
  allocation_method: { from: alloc_method }

  responsible_party:
    from: rep_code

  counterparty:
    from: customer_code
    omit_when: "account_type = 'inventory'"

  # Demonstrated, unused in iteration 1: array-from-many-rows.
  # links:
  #   group_by: code
  #   items:
  #     party: { from: link_party_code }
  #     role:  { from: link_role }
```

### 2.6 Design decision: `omit_when`, and absent ≠ null

`design.md` §1 found two places where omit-vs-empty is load-bearing (`settlement_mode`, and
`roles` on `PUT /parties`). So the emitter must distinguish **field absent** from **field null**,
which most naive mapping layers lose by round-tripping through a map type.

`omit_when` is that primitive, and it also carries the conditional-requiredness logic the accounts
endpoint imposes: `counterparty` is required for `custody`/`bank`/`pab`, optional for `customer`,
and **rejected** for `inventory`/`control` — sending it on an inventory account fails the whole
request with a 400, not just the item.

**Validation opportunity:** because `openapi.yaml` documents that conditionality, an `omit_when`
that fails to cover a forbidden combination is statically detectable — the emitter can be checked
against the spec at edit time, before a single row is transformed. This is the concrete payoff of
keeping Layer 3 non-Turing-complete.

---

## 3. The Run record

### 3.1 What it is for

Three jobs, and the format has to serve all three:

1. **Reproduction** — the input triple, hashed, so the Run can be re-executed identically.
2. **Incremental recompute** — per-node hashes, so the next Run only recomputes the changed cone.
3. **Explanation** — what came out, what failed, and why, at both row and set level.

### 3.2 Sketch

```jsonc
{
  "run": 3,
  "project": "fas-demo",
  "tier": "full",                      // slice | sample | full
  "started_at": "2026-08-26T14:02:11Z",
  "finished_at": "2026-08-26T14:02:13Z",
  "actor": { "kind": "user", "id": "joe" },
  "reason": "Check rep name conflict handling",   // the audited *why*

  // ---- the input triple: this is what makes the Run a pure function ----
  "inputs": {
    "schemas":  { "FAS/ACCOUNT-MASTER": "sha256:9f2c…" },
    "datasets": { "account_master":     "sha256:41ab…" },
    "xform":    "sha256:c70d…",
    "triple":   "sha256:e18b…"          // hash of the three above; the Run's identity
  },

  // ---- per-node hashes drive incremental recompute (design.md §4) ----
  "nodes": {
    "stg_account_master": { "hash": "sha256:1a…", "cached": false, "rows_out": 20, "ms": 12 },
    "party_customers":    { "hash": "sha256:2b…", "cached": false, "rows_out": 14, "ms": 4  },
    "party_reps":         { "hash": "sha256:3c…", "cached": true,  "rows_out": 5,  "ms": 0  },
    "parties":            { "hash": "sha256:4d…", "cached": false, "rows_out": 19, "ms": 2  },
    "accounts":           { "hash": "sha256:5e…", "cached": false, "rows_out": 20, "ms": 3  }
  },

  "target": {
    "kind": "ephemeral",
    "qniform_schema": "onramp_run_3_a41f",       // dropped on teardown
    "firm": "PERSUS"
  },

  // ---- one entry per Output, in execution order ----
  "stages": [
    {
      "name": "parties",
      "target": "PUT /parties",
      "emitted": 19, "accepted": 18, "rejected": 1,
      "requests": 1,
      "rejects": [
        {
          "class": "conflict",                    // set-level: no single row is invalid
          "entity": "party",
          "identity": "FAS_REP_0042",
          "field": "name",
          "values": ["J SMITH", "JOHN SMITH", "SMITH, J"],
          "source_rows": [3, 7, 11, 15],
          "message": "4 source rows derive party FAS_REP_0042 with 3 distinct names"
        }
      ]
    },
    {
      "name": "accounts",
      "target": "PUT /firms/{firm}/accounts",
      "emitted": 20, "accepted": 18, "rejected": 2,
      "requests": 1,
      "rejects": [
        {
          "class": "row",                          // row-level: this row is bad
          "entity": "account",
          "identity": "0000123456",
          "source_row": 12,
          "stage": "coercion",                     // coercion | validation | target
          "field": "OPEN-DT",
          "value": "0000000",
          "message": "not a valid cyymmdd date and not a declared null sentinel"
        },
        {
          "class": "row",
          "entity": "account",
          "identity": "0000998877",
          "source_row": 3,
          "stage": "target",                       // Qniform said no
          "message": "counterparty FAS_CUST_0000998877 does not resolve",
          "cause": { "stage": "parties", "identity": "FAS_REP_0042" }   // upstream link
        }
      ]
    }
  ],

  "totals": { "emitted": 39, "accepted": 36, "rejected": 3 }
}
```

### 3.3 Design decisions embedded above

- **`reason` is a required field on the Run**, not optional metadata. The audit requirement in
  `project_description.md` is about *why*, and a field that can be skipped will be.
- **Rejects carry `class`** — `row` vs `conflict` — because §3a.3's set-level failures do not fit
  the row model and will otherwise be crammed into it badly.
- **Rejects carry `stage`** — `coercion` / `validation` / `target` — because "the date was
  unparseable", "our rule produced something invalid" and "Qniform refused it" are three different
  problems with three different owners, and the UI must not blur them.
- **Rejects can carry `cause`**, linking a downstream failure to the upstream reject that produced
  it. With staged Outputs this is the difference between "18 accounts failed" and "18 accounts
  failed *because* one rep conflict was unresolved". At 100M rows it is the difference between a
  usable error report and an unusable one.
- **`cached` per node** proves the incremental machinery works from iteration 1, when it is
  trivially verifiable, rather than being asserted later at a scale where it cannot be checked
  by eye.

---

## 3.4 Quarantine count is the migration's burn-down

Not a design decision so much as an observation that should shape the UI early: over a migration,
**rows quarantined trending toward zero is the shape of the project**. It is the most honest
progress metric the system has — more honest than rules written or datasets loaded — and it ties
the data side to the project side (`project_description.md`, Milestones and Outcomes) without
anyone having to maintain a separate status report.

Worth surfacing per stage, per entity and over time from the first iteration, even when the
numbers are trivially small.

---

## 4. What this leaves open

- **Joint accounts.** `CUST-NAME-1` + `CUST-NAME-2` on a `J` account are one Qniform party, not
  two. The combination rule is a genuine judgement call and a good first knowledge-base entry.
- **`STATUS-CD = 'C'`** — do closed accounts migrate at all? A business question, and exactly the
  kind that should attach to an Outcome.
- **`CASH-BAL`** is deliberately not mapped. Balances arrive as events, not as account attributes;
  wiring it would drag the event endpoints into iteration 1.
- **Fixed-width layout** and `packed_decimal`, reserved in the format, unimplemented.
- **Branches and party relationships**, deferred per `design.md` §10.

---

## 5. What building it changed

Iteration 1 is built. The sketches above survived contact largely intact; this section records
where they did not, and what the build settled.

### 5.1 DuckDB in Go is fine `[DECIDED]`

`design.md` §5 left this open on the grounds that cgo might hurt. Measured:

- First build ~10s (a prebuilt static library ships with the driver); incremental builds are
  unaffected.
- Open + first query: **~25ms**.
- **Parquet needs no separate library** — DuckDB's own `COPY … (FORMAT PARQUET)` writes the
  staged rows and `read_parquet()` reads them back, so the format never enters `go.mod`.

In-process it is, until runs need to be concurrent. The constraint that will force the change is
not cgo but table naming: models materialise into fixed names, so runs are currently serialised
behind a mutex. Per-run connections or per-run schemas are the fix, and neither is urgent at these
volumes.

### 5.2 The Go-native lineage path exists `[OPEN → narrowed]`

`json_serialize_sql` is available in-process and returns a full parse tree — ~1.3KB of AST for a
two-table join. It scans as a JSON value rather than a string, so it needs a `::VARCHAR` cast,
which is the entire difficulty.

This does not settle §7, because producing an AST is the easy half and *resolving columns through
it* is the hard half. But it does mean the Go path is worth spiking before committing to a Python
sidecar, which was the open question.

### 5.3 The emitter compiles to SQL rather than interpreting it `[DECIDED]`

`omit_when` needs an expression evaluator. Rather than write one, the emitter **compiles to a
SELECT** over its entity's model: every bound column, plus one boolean column per `omit_when`.

So there is exactly one evaluation engine, `omit_when` is ordinary SQL with the semantics the
author already knows from writing the models, and the emitter stays declarative — it describes a
query rather than executing logic.

### 5.4 `references` is the target dependency graph, declared locally `[DECIDED]`

The quarantine cascade needs to know which fields carry which entity's identity. That is declared
on the binding:

```yaml
responsible_party:
  from: rep_code
  references: party
```

Read forwards it orders the stages; read backwards it drives the pre-emission cascade. Confirms
§3a.4's note that the graph is needed in both directions, and that it is a property of Qniform
rather than of the project — which is the argument for eventually extracting it from
`openapi.yaml` instead of restating it per xform.

### 5.5 The Run must record its source counts, not infer them `[DECIDED]`

The first UI drew "rows delivered" from the first stage's emitted count. That was wrong by three,
and wrong in a way that is specific to this system: **derivation means no stage's row count stands
for the source's.** 20 account rows became 22 parties, so any inference from a stage back to the
source is guesswork.

The Run record now carries `source: {delivered, staged}` explicitly. `delivered` and `staged`
differ by exactly the rows withheld at coercion, which makes the first two nodes of the flow
honest.

Deliberately excluded from the input triple: the counts are an *observation* of the inputs, not an
input, and folding them in would change the Run's identity without changing what produced it.

### 5.6 Two ordering bugs that would have broken determinism silently

Both found by the determinism test rather than by reading the code, which is an argument for
writing that test early:

1. A model emitting several rows per identity had no total order, so which row spoke for the
   entity depended on scan order. Fixed by ordering on `(identity, __row)` in the compiled
   emitter query, and by sorting each group on `__row` in the collapse.
2. The payload map was built from the first row seen; the emitter and the collapse could disagree
   about which that was. Both now take the lowest `__row`.

Neither would have failed a functional test. Both would have made run diffs unusable months later,
which is the failure mode §8 is about.

### 5.7 The fixture earns its keep

The 20-row FAS extract produces, without contrivance: 1 coercion reject (month 13), 2 rep-name
conflicts (3 and 2 spellings), a 10-account cascade from those 2 conflicts, 3 house accounts
exercising conditional-by-type field omission, 3 joint accounts exercising the one-party rule, an
unassigned rep, and a closed account.

Every mechanism in `design.md` §3a is exercised at a scale where the whole output fits on one
screen and can be checked by eye.

### 5.8 Run records are never migrated, so readers must tolerate every past shape

Adding `source` (§5.5) broke the UI for the run that came before it: destructuring a field that did
not exist when that record was written. The null check is trivial; the reason it will recur is not.

**A Run record is an audit artifact.** Its whole purpose is to be read years after the code that
wrote it has moved on, and rewriting it to match today's shape would destroy the property it
exists to provide. So the record is immutable, older shapes stay on disk forever, and **the
obligation is on the reader**.

Two consequences, both now in place:

- New Runs carry `record_version` (currently `1`; absent means `0`, the shape before source counts).
  A reader branches on it explicitly rather than probing for fields and guessing what their absence
  means. Bump it whenever the record gains or changes a field.
- **A reader that cannot render an old field omits it — it does not reconstruct it.** The UI shows
  the flow for a v0 run starting at the first Output, with a line saying the source counts
  were not recorded. Inferring them from the stages is precisely the bug §5.5 fixed: derivation
  means no stage's row count stands for the source's, so a plausible-looking number would be wrong
  in exactly the way that is hardest to notice.

The same obligation will apply to the emitted payloads themselves once xforms become editable: a
payload emitted six months ago was shaped by rules that have since changed, and reading it back
must not silently reinterpret it under today's rules.

### 5.9 The summary needed a concept the manifest did not have

The project page listed "model order", "derived entities" and "emissions". All three are
implementation vocabulary, and none of them answers the question the page exists to answer: **how
does this source become that target?**

The missing concept was a **Rule**: a named group of transformation logic binding source schemas to
one Output. It is declared in `transform.yaml` with a title and a summary, because the valuable part is
the prose — *why the role depends on a sibling column*, *why a joint holding is one party* — and no
analysis of the SQL produces that.

With Rules in place the summary is three sets and their bindings: sources, rules, outputs. Two
things fall out of it:

- **Models a Rule does not claim are shared preparation.** That gives a principled definition of
  "implementation detail" rather than a judgement call about what to hide. `stg_accounts` (staging
  before the rules) and `parties` (combining what two rules produce) are both correctly excluded
  from the summary and both reachable behind it.
- **Identity is per-entity, minting is per-rule.** Two rules produce parties; they mint different
  codes from different columns but share one identity declaration and one conflict policy. The rule
  page has to say so rather than presenting the entity's full `derived_from` as its own.

Vocabulary settled at the same time: **Output** replaces "stage"/"emission" throughout, and a
Reject's `stage` became `phase`. The two were the same word for different things in one record —
`run.stages[].rejects[].stage` — which is the kind of collision that costs an afternoon a year from
now.

### 5.10 The Output page is a coverage view, not a spec viewer

Making outputs clickable raised the question of what an Output page is *for*. Rendering Qniform's
OpenAPI entry would be a documentation viewer, and Qniform already serves one at `/docs`.

The useful thing is the **join**: every field the target accepts, marked with whether any rule
binds it. For `PUT /firms/{firm}/accounts` that reads **6 of 14 bound**, and names the eight that
are not — addresses, `cash_sweep`, `correspondent`, `settlement_mode`. That list is the migration's
real backlog, and it is otherwise carried in somebody's head.

The unbound set is not a defect list: some fields are derived by Qniform, some belong to source
files the project has not received. The page says so. The point is that the set is *visible* rather
than remembered, and that it shrinks as the migration progresses — the same burn-down argument as
§3.4, from the other end.

The page also flags the inverse — a bound field the spec does not have — which `ValidateBatch`
already catches at run time. Catching it while someone is reading the rule is cheaper.

**Known limit:** properties that are a `$ref` to a component (`roles` → `PartyRoles`) show the
component's name but not its description or inner shape, because `components:` is not parsed.
The same gap makes `ValidateBatch` refuse a `$ref`'d item schema outright rather than validate
nothing. Resolving one level of `$ref` would close both.

### 5.11 There is no staging layer: the coerced table IS the Dataset

The first cut had a `stg_accounts` model, presented in the UI as shared preparation and excluded
from the mapping summary as implementation. Two problems, and the second is the real one.

**The framing was wrong.** A "staging table for accounts derived from the FAS Account Master CSV"
is not a thing the user has. The CSV is *transport*; what matters is the FAS Account Master, and
the typed, decoded table on the far side of ingest **is** that Dataset. There is nothing to stage.

**And the model was not doing staging.** It minted `FAS_CUST_` and `FAS_REP_` codes, decided
rep-versus-trader from `ACCT-TYPE-CD`, combined joint holder names into one party, and mapped
`COST-METHOD-CD` onto Qniform's lot methods. All of that is rule work. The consequence was that
`party_reps.sql` — the thing the reps rule page displayed as "how it is computed" — was a nine-line
projection, while every decision it was actually making lived in a model the summary called
implementation and never showed.

**A label that hides rule logic is worse than no label.** Three invariants now, the last two
enforced:

1. **Ingest does decoding and typing only** — trimming, encodings, null sentinels, type coercion.
   No joins, no enrichment, no derivation, no minting. If a step needs to know anything about the
   *target*, it is a rule.
2. **Every model belongs to exactly one Rule.** `Transform.Validate` rejects an unclaimed model, which
   removes the place rule logic was able to hide.
3. **An entity is the union of what its Rules produce**, generated rather than authored.
   `parties.sql` was a hand-written `UNION ALL` and is gone.

   `Engine.UnionInto` built that union as a DuckDB table, and checked that rules feeding one entity
   agreed on columns. It was never wired into a Run — rules emit separately and share one key space
   by their disjoint prefixes, so no table is ever unioned — and it was deleted as dead code on
   2026-08-31. The column check went with it, and is not missed: `checkColumnsAgainstTarget` holds
   every rule to the target endpoint's own fields, which is the constraint that mattered.

The shared-mint duplication this removes indirection from is real — `'FAS_CUST_' || "ACCT-NO"`
appears in both the customers rule and the accounts rule — but it cannot rot silently: if the two
expressions drift, every account referencing a party is withheld for an unresolvable reference, and
`TestApplyFanOutAndCascade` asserts the surviving count. Indirection to avoid a repeated string,
paid for by hiding the derivation, was the wrong trade.

Record version 3 followed: `source.staged` became `source.rows`, because with no staging step there
is no staged thing to name — only the file as delivered, and the Dataset.

### 5.12 JSONB was silently discarding payload field order

`runs.body` was `JSONB`. Postgres normalises JSONB object keys — by key length, then bytewise — so
a payload declared `code, name, pre_name, roles` came back as `code, name, roles, pre_name`.

The `OrderedMap` built to preserve authored order was working correctly and being undone one layer
down, which is the kind of bug that survives a long time because nothing fails.

Field order carries nothing to Qniform, but a great deal to a human reading a run diff, and making
that reading easy is most of the product. The column is now `JSON`, which stores the text verbatim.
The `->` and `->>` operators used by `ListRuns` work on both, so nothing else changed.

**The general lesson:** an ordering guarantee is only as good as every layer it passes through. Worth
remembering for the Parquet round trip and for anything that reaches the browser.

### 5.13 Convention beats declaration where there is nothing to decide

A rule used to name its SQL: `sql: party_reps`. Since a rule has exactly one, and the name was the
only thing linking them, the declaration existed solely to be got wrong. It is now
`sql/<rule name>.sql` by convention, and the field is gone.

What that bought:

- **A file claimed by two rules is no longer possible.** The check that caught it is deleted — not
  because it stopped mattering, but because the name is the link and a name is unique.
- **An orphan file still fails to load**, and that is the check worth keeping. An unclaimed SQL file
  is exactly where rule logic hid last time, behind the label "implementation", while the rule page
  showed a thin projection (§5.11).

The general shape: a declaration that can only ever restate something already determined is not
configuration, it is an opportunity for two sources of truth to disagree. Worth applying the same
test to what remains — `row_key` is a candidate, since it is `code` on every rule and is really a
property of the target's upsert key rather than a choice.

### 5.14 Record versioning was premature, and is gone

§5.8 argued that Run records are audit artifacts, never migrated, so every reader must tolerate
every past shape — and added `record_version` plus a normalizer to do it.

The reasoning holds for a system with real migrations in it. It did not hold for this one. In two
days the version went to **6**, the normalizer grew four fallbacks and a branch, and every record it
protected was throwaway data from the session that wrote it. Six versions of ceremony guarding
nothing.

Reading how it was actually used made the imbalance clear: of the six steps, exactly **one** was
handled by branching on the version. The rest were handled by probing — `digest ?? triple`,
`transform ?? xform`, `rows ?? staged` — which is precisely what the field's own comment said it
existed to avoid.

That split was not an accident, and it is the part worth keeping:

- **Probing suffices for a rename.** `digest` absent unambiguously means "look for `triple`";
  nothing else could cause the absence.
- **A version number is needed for a removal or a change of meaning.** v1→v2 moved the ingest
  rejects out of `stages`, where they had ridden as a pseudo-entry. No field's presence distinguishes
  those shapes, so probing cannot detect it.

So the discipline is right, and the trigger for adopting it is **the first record anyone will
actually read back** — not the first change to the struct. Until then, keeping the field while not
really branching on it was the worst of the three options: it looked like a guarantee that was not
being enforced.

Deleting it invalidated every run then in the dev database. That was the correct trade at this
stage, and is worth noticing as the signal for when it stops being: once clearing the runs table is
not an acceptable answer, the versioning comes back.
