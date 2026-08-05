# Deploy and operations

## Deploy to Railway

1. Push this repo to GitHub. A **private** repo is fine and recommended; Railway
   deploys from private repos through its GitHub app. Grant the Railway app
   access to just this repository.
2. Create a Railway project from the repo. Railway detects the `Dockerfile` and
   builds it on every push to the default branch.
3. Add a **PostgreSQL** database to the project (`+ New`, then `Database`).
4. In the app service **Variables**, set:
   - `DATABASE_URL` = the reference `${{Postgres.DATABASE_URL}}`
   - `POOL_TOKEN` = a long random secret (`openssl rand -hex 32`)
   - `PORT` is injected by Railway automatically; the server reads it.
5. Under **Networking**, generate a public domain. Railway terminates TLS at its
   edge, so teammates get HTTPS for free. Use that URL as `POOL_URL` on the
   client side.
6. Health checks hit `/healthz`; this is already wired in `railway.json`.

The schema is applied idempotently on startup, so there is no separate migration
step. Keep the service always-on for the contest window.

## Public vs private repository

Nothing here requires a public repo. Keep it private for the least fuss.

If you do make it public, the repo as written does not leak your deployment
target: `POOL_URL` only ever comes from env or flags, `.env` is gitignored, and
the README and `.env.example` use a `your-service.up.railway.app` placeholder.
Two things to keep in mind:

- Do not paste the live URL into the README, issues, commits, or the CI badge.
- The cracked material (`founds_*.txt`, `submission_*`) and any keys must never
  be committed. They are gitignored already. This matters for contest rules too,
  which forbid sharing plaintexts between teams.

## Threat model and the security boundary

**The Railway URL is not a secret, and the design does not rely on it being
one.** Subdomains under `*.up.railway.app` are discoverable through TLS
certificate transparency logs regardless of repo visibility, so treat the URL as
low-sensitivity, not confidential.

The actual security boundary is the bearer token:

- Every endpoint except `/healthz` requires `Authorization: Bearer <POOL_TOKEN>`.
- The comparison is constant-time (`crypto/subtle`), so it does not leak length
  or content through timing.
- Unknown paths return 404; missing or wrong tokens return 401. `/healthz` only
  reveals that something is listening.

Operational hygiene:

- Use a long, random `POOL_TOKEN`. Rotate it if it leaks. Rotation is one Railway
  variable change plus telling teammates the new value.
- Prefer keeping the repo private so the whole question is moot.
- IP allowlisting is impractical during a con (teammates are on shifting
  networks), so the token is the pragmatic gate.

### Hardening options (optional)

If you want more than a single shared token:

- Map per-user tokens to authors server-side (env like
  `POOL_TOKENS=alice:tok1,bob:tok2`) so attribution is authenticated rather than
  self-declared via `--author`. This is a small change in `internal/server`.
- Put the pool behind a private network and expose it only through a VPN or
  Tailscale, dropping the public Railway domain entirely. Teammates then reach it
  over the tailnet. This trades convenience for a much smaller exposed surface.

## Contest weekend runbook

Before the contest opens:

- Register the team PGP key with KoreLogic. This is the real critical path.
- Confirm the live submission address and whether signing is required from the
  current registration and submission HOWTO pages.
- Deploy the pool, set `POOL_TOKEN`, share `POOL_URL` and the token with the team
  over a private channel.
- Each teammate exports `POOL_URL`, `POOL_TOKEN`, and `CMIYC_AUTHOR`.

Steady state (per teammate):

```sh
watch -n 300 'cmiyc send'   # re-upload the potfile every 5 minutes, idempotent
```

Steady state (operator, tight loop):

```sh
cmiyc pull                  # bundles pending, prints an iter token
# ...send ./out/submission_<iter>.asc to the contest address...
cmiyc submit --iter <iter>  # confirm, then mark
```

Once a day, or after any suspected dropped send, run the failsafe sweep:

```sh
cmiyc pull --full
cmiyc submit --iter <iter>
```

Run `cmiyc stats` any time to see totals, pending count, and the internal
per-teammate breakdown.

## Local development

```sh
export DATABASE_URL='postgres://user:pass@localhost:5432/cmiyc?sslmode=disable'
export POOL_TOKEN="$(openssl rand -hex 32)"
make run
```

Then point a second shell at it with `POOL_URL=http://localhost:8080` and the
same token.
