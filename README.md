# OnRamp

Migration tooling for moving large financial firms from a legacy books-and-records system onto
[Qniform](../qniform6).

The whole system is one pure function:

```
Output = Apply(SourceSchemas, SourceDatasets, Xforms)
```

All three inputs are content-addressed, so a Run's identity is the hash of the triple and any Run
is exactly reproducible — which is both the audit property and the mechanism that makes
incremental recomputation possible.

## Running it

```
createdb onramp
echo "DATABASE_URL=postgres://$(whoami)@localhost:5432/onramp?sslmode=disable" > .env
bash run.sh          # http://localhost:8090
```

`bash build.sh` is the full pass: format, vet, test, build both halves.

The test suite validates emitted payloads against Qniform's real OpenAPI spec, found via
`QNIFORM_SPEC` or `~/Code/qniform6/openapi.yaml`. It fails rather than skips when absent.

## What iteration 1 does

Start a project of Migration Type **FAS** (Fake Accounting System), which supplies the Account
Master schema and the transformation rules. Upload a CSV. Run it. Read the output.

FAS has **no party file** — as is typical of the mainframe systems being migrated from — so every
party Qniform requires is *derived* from the account records. One flat 20-row file becomes two
emissions in dependency order:

```
FAS Account Master ──┬─→ distinct customers ─→ PUT /parties  (roles: {customer:{}})
  (one flat file)    ├─→ distinct reps      ─→ PUT /parties  (roles: {rep:{}} | {trader:{}})
                     └─→ accounts           ─→ PUT /firms/{firm}/accounts
```

Schemas and rules are not editable yet. That is iteration 2.

## Layout

| Path                       | What                                                              |
|----------------------------|-------------------------------------------------------------------|
| `run.go`                 | the pure function; read this first                                |
| `schema.go`, `encoding.go` | Source Schemas — logical type and physical encoding kept separate |
| `dataset.go`               | ingest: coerce, land as Parquet, withhold what cannot be read     |
| `transform.go`, `binding.go`   | the rules: a manifest, SQL models, declarative emitters           |
| `openapi.go`               | validation against Qniform's real spec                            |
| `engine.go`                | DuckDB, in-process                                                |
| `migration_types/FAS/`     | the shipped schema and rules                                      |
| `devnotes/`                | design discussion and decisions                                   |
| `web/`                     | React + Vite frontend                                             |

## Design notes

- `devnotes/design.md` — architecture, constraints read out of Qniform's API, open decisions.
- `devnotes/iteration1-artifacts.md` — the concrete formats, and §5 on what building changed.
