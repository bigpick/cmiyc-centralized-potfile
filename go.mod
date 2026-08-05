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
	github.com/jackc/pgx/v5 v5.7.2
)
