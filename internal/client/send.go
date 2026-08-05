package client

import (
	"context"
	"fmt"
	"os"
)

// RunSend streams a local potfile to the pool, attributing every crack in it to
// author. It is fully idempotent: run it whenever, on a whole or partial
// potfile, in any order. The server de-duplicates, so re-sending the same file
// repeatedly (for example on a cron every few minutes) is the intended usage
// and never double-counts.
func RunSend(ctx context.Context, pool *Pool, author, potfilePath string) error {
	if author == "" {
		return fmt.Errorf("--author is required (set CMIYC_AUTHOR to avoid repeating it)")
	}
	f, err := os.Open(potfilePath)
	if err != nil {
		return fmt.Errorf("open potfile: %w", err)
	}
	defer f.Close()

	res, err := pool.Upload(ctx, author, f)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	fmt.Printf("sent as %q\n", author)
	fmt.Printf("  new for team   : %d\n", res.NewCracks)
	fmt.Printf("  new for you    : %d\n", res.NewForAuthor)
	fmt.Printf("  in this upload : %d unique\n", res.BatchUnique)
	fmt.Printf("  team total     : %d unique\n", res.TotalUnique)
	fmt.Printf("  pending to CMIYC: %d\n", res.Pending)
	return nil
}
