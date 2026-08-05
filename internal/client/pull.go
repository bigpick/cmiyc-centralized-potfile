package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/zilla/cmiyc-pool/internal/pgpcrypto"
	"github.com/zilla/cmiyc-pool/internal/store"
)

// PullOptions configures a pull.
type PullOptions struct {
	Full         bool   // false: only pending. true: everything (daily failsafe)
	OutDir       string // where artifacts are written
	RecipientKey string // path to KoreLogic armored public key (required)
	SignKey      string // optional path to team private key for signing
	Passphrase   []byte // optional passphrase for the signing key
	ContestEmail string // recorded in the sidecar and printed for manual send
}

// RunPull builds the local submission bundle. It never transmits anything and
// never mutates pool state. It writes three artifacts:
//
//	founds_<iter>.txt          cleartext hash:plain, for your own backup/inspection
//	submission_<iter>.asc      the encrypted (optionally signed) payload to send
//	submission_<iter>.ids.json the ids `submit` may mark once the send is confirmed
func RunPull(ctx context.Context, pool *Pool, opt PullOptions) error {
	if opt.RecipientKey == "" {
		return fmt.Errorf("--recipient-key (KoreLogic public key) is required")
	}

	var (
		lines []store.CrackLine
		err   error
		kind  = "delta"
	)
	if opt.Full {
		kind = "full"
		lines, err = pool.All(ctx)
	} else {
		lines, err = pool.Pending(ctx) // every returned line has WasPending == true
	}
	if err != nil {
		return fmt.Errorf("fetch %s: %w", kind, err)
	}

	if len(lines) == 0 {
		if opt.Full {
			fmt.Println("nothing to pull: the pool is empty.")
		} else {
			fmt.Println("nothing to pull: no cracks are pending submission.")
		}
		return nil
	}

	// Cleartext payload is every shipped line. Eligible-to-mark ids are the
	// was_pending subset only, which for a plain pull is all of them.
	var payload strings.Builder
	var markIDs []int64
	for _, l := range lines {
		payload.WriteString(l.Line)
		payload.WriteByte('\n')
		if l.WasPending {
			markIDs = append(markIDs, l.ID)
		}
	}

	recipientKey, err := pgpcrypto.LoadKeyFile(opt.RecipientKey)
	if err != nil {
		return err
	}
	signKey, err := pgpcrypto.LoadKeyFile(opt.SignKey)
	if err != nil {
		return err
	}
	enc, err := pgpcrypto.EncryptArmored([]byte(payload.String()), recipientKey, signKey, opt.Passphrase)
	if err != nil {
		return fmt.Errorf("encrypt payload: %w", err)
	}

	if err := os.MkdirAll(opt.OutDir, 0o750); err != nil {
		return fmt.Errorf("create out dir: %w", err)
	}
	iter := NewIter()
	foundsPath := FoundsPath(opt.OutDir, iter)
	ascPath := AscPath(opt.OutDir, iter)
	metaPath := MetaPath(opt.OutDir, iter)

	if err := os.WriteFile(foundsPath, []byte(payload.String()), 0o640); err != nil {
		return fmt.Errorf("write founds: %w", err)
	}
	if err := os.WriteFile(ascPath, enc, 0o640); err != nil {
		return fmt.Errorf("write asc: %w", err)
	}
	meta := SubmissionMeta{
		Iter:         iter,
		Kind:         kind,
		ContestEmail: opt.ContestEmail,
		IDs:          markIDs,
		LineCount:    len(lines),
		CreatedAt:    iter,
	}
	metaBytes, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(metaPath, metaBytes, 0o640); err != nil {
		return fmt.Errorf("write meta: %w", err)
	}

	fmt.Printf("pull ok (%s)\n", kind)
	fmt.Printf("  iter          : %s\n", iter)
	fmt.Printf("  lines shipped : %d\n", len(lines))
	fmt.Printf("  eligible mark : %d (previously pending)\n", len(markIDs))
	fmt.Printf("  cleartext     : %s\n", foundsPath)
	fmt.Printf("  encrypted     : %s\n", ascPath)
	fmt.Printf("  sidecar       : %s\n", metaPath)
	fmt.Println()
	fmt.Printf("next: send %s to %s, then run:\n", ascPath, orPlaceholder(opt.ContestEmail))
	fmt.Printf("  cmiyc submit --iter %s\n", iter)
	return nil
}

func orPlaceholder(email string) string {
	if email == "" {
		return "<contest submission address>"
	}
	return email
}
