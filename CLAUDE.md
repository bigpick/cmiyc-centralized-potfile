# CLAUDE.md

Orientation for anyone (human or agent) working in this repository. Read this
before making changes.

## What this is

A single Go binary that runs a shared pool for the KoreLogic Crack Me If You Can
contest. Teammates upload hashcat potfiles, the pool de-duplicates and attributes
them, and an operator builds and sends PGP submissions to the contest. One
codebase, one container image, five subcommands (`serve`, `send`, `pull`,
`submit`, `stats`).

Deep design lives in `docs/ARCHITECTURE.md`. Deployment and the security model
live in `docs/DEPLOY.md`. Hash-format handling lives in `docs/HASH_FORMATS.md`.

## Conventions (follow these exactly)

- **No em dashes or en dashes anywhere.** Not in code, comments, docs, commit
  messages, or generated output. Use a plain hyphen or restructure the sentence.
  This is a hard project rule.
- **Standard library first.** Only two external dependencies are allowed without
  discussion: `pgx/v5` (Postgres) and `go-crypto/openpgp` (PGP). Reach for the
  stdlib before adding anything else. No web framework, no CLI framework, no ORM.
- **The server owns all correctness.** Clients are thin. Dedupe, attribution,
  and mark logic live in `internal/store`. Do not push that logic into the CLI.
- **gofmt is enforced.** Run `make fmt` before committing; CI fails on unformatted
  code.
- **Never commit secrets or cracked material.** `.env`, `keys/`, `*.asc`,
  `founds_*.txt`, and `submission_*` are gitignored. Keep it that way.

## Common commands

```sh
make tidy     # go mod tidy; needed once on a networked machine to write go.sum
make check    # fmt + vet + test (run before every commit)
make build    # -> ./bin/cmiyc
make run      # cmiyc serve locally (needs DATABASE_URL and POOL_TOKEN)
make docker   # build the distroless image
make deps-upgrade  # go get -u ./... then go mod tidy (bump all Go deps)
```

Routine dependency bumps (Go modules, Actions, the Docker base image) arrive as
weekly Dependabot PRs; within-major bumps are normally safe to merge. Use
`make deps-upgrade` when you want to pull everything forward at once locally.

Run any mode directly during development:

```sh
go run ./cmd/cmiyc serve
go run ./cmd/cmiyc send --author me --potfile-path ./sample.potfile
go run ./cmd/cmiyc pull --recipient-key ./keys/korelogic.asc
go run ./cmd/cmiyc submit --iter <token>
```

## Architecture in brief

```
cmd/cmiyc            subcommand dispatch, flag parsing, env fallbacks
internal/server      HTTP API, bearer auth, routing (net/http only)
internal/store       pgx pool, schema, dedupe/attribution/mark SQL
internal/client      CLI logic for send/pull/submit/stats + local artifacts
internal/pgpcrypto   encrypt-to-recipient with optional signing (local only)
internal/potfile     opaque line parsing + sha256 dedupe
```

Trust boundary: `serve` is the only component that touches Postgres. Everything
else is a client that talks to it over HTTPS. PGP keys and the contest email
account never leave the operator machine, so no secret material lives on the
host running `serve`.

## Data model and invariants

Two tables (`internal/store/schema.sql`):

- `cracks`: the unique set of cracked lines. `submitted_at IS NULL` means
  pending. `first_by` records the race winner for attribution. This is the only
  table that feeds a contest submission.
- `crack_contributors`: bounded `(crack_id, author)` set, one row per teammate
  who ever uploaded a line. Idempotent re-uploads do not grow it. This is the
  post-contest overlap-analysis surface.

Invariants that must not regress (see `docs/ARCHITECTURE.md` for the reasoning):

1. **Idempotent upload.** `send` posts the whole potfile; the server dedupes on
   `sha256(line)`. Re-sending the same file must never double-count.
2. **Reads never mutate.** `pull` (pending or `--full`) only reads. Only
   `submit`, via the `/v1/mark` endpoint, mutates `submitted_at`.
3. **Mark only after a confirmed send.** `submit` marks nothing unless SMTP send
   succeeded or the operator confirmed a manual send.
4. **Set-exact marking.** `pull` writes the exact bundled ids to a sidecar;
   `submit` marks only those, guarded by `submitted_at IS NULL`. A crack that
   races in after the pull snapshot can never be falsely marked.
5. **`--full` is a safe superset.** `pull --full` re-ships every crack but marks
   only the previously-pending subset (`was_pending`), so a full sweep is also a
   correct baseline advance with no follow-up pull.

A "line" is opaque: it is never split on `:` because plaintexts legally contain
colons and arbitrary UTF-8. The only normalization is trimming trailing CR/LF.

## Gotchas

- **go.sum is not committed** (this scaffold was authored offline). Run
  `make tidy` once with network access before the first build.
- **pgx array binding.** `store.Submit` passes `[][]byte` and `[]string` as
  `bytea[]` / `text[]`. pgx v5 maps these by default; if a resolved version
  rejects the `[][]byte` binding, wrap the hashes in a `pgtype` array. This is
  the single most likely place to need a fix after `make check`.
- **Railway PORT.** `serve` must bind `0.0.0.0:$PORT`. Do not hardcode a port.
- **Distroless has no shell.** The runtime image cannot do shell expansion; all
  config is read from the environment inside Go. Keep it that way.
- **DB may lag the app on boot.** Railway has no `depends_on`, so `store.New`
  retries for up to 60 seconds. Do not remove that loop.

## Testing

`internal/potfile` has pure unit tests (no database) covering dedupe and CRLF
normalization. Store-level SQL is not yet covered by an integration test; adding
a `docker-compose` Postgres plus a test that exercises submit/pull/mark against
real SQL is the highest-value next test to write.
