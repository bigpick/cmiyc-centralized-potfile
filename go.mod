module github.com/zilla/cmiyc-pool

go 1.26.5

// External dependencies are intentionally minimal:
//   - pgx v5 : pure-Go PostgreSQL driver + pool (no cgo, works in distroless/static)
//   - go-crypto/openpgp : maintained OpenPGP fork (encrypt-to-recipient + optional signing)
//
// This scaffold was authored without network access, so go.sum is not
// committed. Run `make tidy` (which runs `go mod tidy`) once on a networked
// machine before the first build; it will pin exact patch versions and write
// go.sum. The version constraints below are floors, not exact pins.
require (
	github.com/ProtonMail/go-crypto v1.1.6
	github.com/jackc/pgx/v5 v5.10.0
)

require (
	github.com/cloudflare/circl v1.3.7 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/crypto v0.17.0 // indirect
	golang.org/x/sync v0.17.0 // indirect
	golang.org/x/sys v0.16.0 // indirect
	golang.org/x/text v0.29.0 // indirect
)
