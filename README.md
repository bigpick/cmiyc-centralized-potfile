<p align="center">
  <img src="docs/assets/banner.svg" alt="CMIYC Pool" width="920">
</p>

<p align="center">
  <a href="https://github.com/zilla/cmiyc-pool/actions/workflows/ci.yml"><img src="https://github.com/zilla/cmiyc-pool/actions/workflows/ci.yml/badge.svg" alt="ci"></a>
  <img src="https://img.shields.io/badge/go-1.26.5-00ADD8" alt="go 1.26.5">
  <img src="https://img.shields.io/badge/deploy-Railway-6b3bf5" alt="railway">
  <img src="https://img.shields.io/badge/license-MIT-black" alt="mit">
</p>

<p align="center">
  <b>A shared submission pool for the KoreLogic Crack Me If You Can contest.</b><br>
  Teammates upload potfiles. The pool dedupes and attributes them.<br>
  One operator builds and sends the PGP submissions. Nothing double-counts.
</p>

---

## Yeah, if you could just crack these hashes, that would be great

It is a 48 hour contest run by tired people at 3 AM. So this tool is built so
**there is no wrong way to use it**: uploads are idempotent, reads never change
anything, and cracks are marked submitted only after a send you confirmed. Skip
a step and the next run picks up whatever was missed.

<p align="center">
  <img src="docs/assets/flow.svg" alt="data flow" width="900">
</p>

One Go binary. Two tables. Two dependencies. The standard library for the rest.
Your PGP keys and contest email never touch the hosted pool.

---

## Getting started

<img src="docs/assets/stapler.svg" alt="" width="72" align="left">

Needs Go 1.26.5 (and Docker for the container path). Because this ships without
a `go.sum`, resolve dependencies once on a networked machine, then build.

<br clear="left">

```sh
make tidy      # go mod tidy: writes go.sum (needs network, do this first)
make check     # fmt + vet + test
make build     # -> ./bin/cmiyc
```

### Run the pool

```sh
export DATABASE_URL='postgres://user:pass@localhost:5432/cmiyc?sslmode=disable'
export POOL_TOKEN="$(openssl rand -hex 32)"
make run        # cmiyc serve on :8080, applies the schema on boot
```

### Point the team at it

```sh
export POOL_URL=https://your-service.up.railway.app
export POOL_TOKEN=...          # the same shared token
export CMIYC_AUTHOR=you

cmiyc send                     # uploads your hashcat potfile; safe to loop
```

### Build and send a submission (operator)

```sh
export CMIYC_RECIPIENT_KEY=./keys/korelogic-2026-pub.asc
export CMIYC_CONTEST_EMAIL=sub-2026@contest.korelogic.com   # confirm the real address

cmiyc pull                     # bundles pending -> ./out/, prints an iter token
# ...send ./out/submission_<iter>.asc to the contest address...
cmiyc submit --iter <iter>     # confirm, then mark those cracks submitted
```

Deploying to Railway takes about five minutes. See **[docs/DEPLOY.md](docs/DEPLOY.md)**.

---

## Binary entrypoints

One binary, five modes. Run `cmiyc <command> -h` for the full flag list.

| command  | who runs it      | what it does |
|----------|------------------|--------------|
| `serve`  | Railway (hosted) | the shared pool: dedupe store, attribution, HTTP API |
| `send`   | every teammate   | upload a local hashcat potfile to the pool |
| `pull`   | operator         | build the encrypted submission bundle locally |
| `submit` | operator         | send a pulled bundle to KoreLogic, then mark it |
| `stats`  | anyone           | internal team-distribution snapshot |

### `send`

```sh
cmiyc send --author you --potfile-path ~/.local/share/hashcat/hashcat.potfile
```

Idempotent. Re-run it whenever, on a whole or partial potfile. The server
dedupes, so a cron every few minutes never double-counts:

```sh
watch -n 300 'cmiyc send'
```

### `pull`

```sh
cmiyc pull                 # only pending cracks
cmiyc pull --full          # every crack: daily failsafe sweep
```

Writes three artifacts to `./out/`: a cleartext `founds_<iter>.txt` backup, the
encrypted `submission_<iter>.asc`, and a `submission_<iter>.ids.json` sidecar
that records exactly which ids `submit` may mark. Transmits nothing.

### `submit`

```sh
cmiyc submit --iter <iter>                 # assisted-manual: prints, confirms, marks
cmiyc submit --iter <iter> --smtp-host ... # optional: sends the email itself
```

Marks nothing until the send is confirmed. If it aborts, the next `pull` simply
re-includes the cracks.

### `stats`

```sh
cmiyc stats
```

Totals, pending count, independent-overlap count, and per-teammate first-crack
and total-held breakdowns.

---

## Why you cannot break it

- **Idempotent upload.** Re-send the same potfile forever; it converges to one
  unique set.
- **Reads never mutate.** `pull` cannot hurt anything. Only a confirmed `submit`
  marks.
- **Set-exact marking.** `submit` marks precisely the ids `pull` bundled, guarded
  by `submitted_at IS NULL`, so a crack that raced in after the snapshot can never
  be falsely marked.
- **`--full` is a safe superset.** It re-ships everything (KoreLogic dedupes) but
  marks only the previously-pending subset, so a full sweep is also a correct
  baseline advance.

The reasoning behind each of these is in **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)**.

---

## Docs

- **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)** design philosophy, data model, correctness invariants
- **[docs/DEPLOY.md](docs/DEPLOY.md)** Railway deploy, private-repo notes, the security model, a contest runbook
- **[docs/HASH_FORMATS.md](docs/HASH_FORMATS.md)** which formats upload raw and which need `hashcat --show`
- **[CLAUDE.md](CLAUDE.md)** repo conventions and orientation for agents and contributors

---

## Before you rely on it

- **Run `make tidy` first.** No `go.sum` ships in the archive (authored offline);
  `go mod tidy` pins the two dependencies and lets it build.
- **The one likely fix-up spot** is the pgx array binding in `store.Submit`
  (`[][]byte` to `bytea[]`). pgx v5 maps it by default; wrap in a `pgtype` array
  if your resolved version balks. Details in `CLAUDE.md`.
- **Confirm the 2026 submission details** (address, and whether signing is
  required) from the live KoreLogic HOWTO pages.
- **Register the team PGP key now.** The tooling is the easy part; a verified team
  key is the critical path.

The module path is `github.com/zilla/cmiyc-pool`. If you host it elsewhere, update
the `module` line in `go.mod`, the internal import paths, and the badge URLs.

## License

MIT. See [LICENSE](LICENSE).
