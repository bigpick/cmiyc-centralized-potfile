package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/smtp"
	"os"
	"strings"
)

// SMTPConfig, when non-nil on SubmitOptions, makes `submit` send the payload
// itself. When nil, `submit` runs in assisted-manual mode: it prints exactly
// what to send where, waits for your confirmation, then marks. Manual is the
// default because emailing an autoresponder from an arbitrary host is exactly
// the fragile step you want a human eye on.
type SMTPConfig struct {
	Host string
	Port int
	User string
	Pass string
	From string
}

// SubmitOptions configures a submit.
type SubmitOptions struct {
	Iter         string // iteration token produced by pull (required)
	Dir          string // directory holding the pull artifacts
	ContestEmail string // overrides the address recorded in the sidecar
	Subject      string // email subject line
	SMTP         *SMTPConfig
	Yes          bool      // skip the confirmation prompt (implied for SMTP auto-send)
	In           io.Reader // confirmation input (os.Stdin)
}

// RunSubmit sends a previously pulled bundle to KoreLogic and, only after a
// confirmed send, marks its eligible ids submitted in the pool.
func RunSubmit(ctx context.Context, pool *Pool, opt SubmitOptions) error {
	if opt.Iter == "" {
		return fmt.Errorf("--iter is required (the token printed by pull)")
	}

	metaBytes, err := os.ReadFile(MetaPath(opt.Dir, opt.Iter))
	if err != nil {
		return fmt.Errorf("read sidecar for iter %s: %w", opt.Iter, err)
	}
	var meta SubmissionMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return fmt.Errorf("parse sidecar: %w", err)
	}

	ascPath := AscPath(opt.Dir, opt.Iter)
	asc, err := os.ReadFile(ascPath)
	if err != nil {
		return fmt.Errorf("read encrypted payload: %w", err)
	}

	to := opt.ContestEmail
	if to == "" {
		to = meta.ContestEmail
	}
	subject := opt.Subject
	if subject == "" {
		subject = "CMIYC submission " + meta.Iter
	}

	sent := false
	switch {
	case opt.SMTP != nil:
		if to == "" {
			return fmt.Errorf("no contest email set (use --to or record it at pull time)")
		}
		if err := sendSMTP(opt.SMTP, to, subject, asc); err != nil {
			return fmt.Errorf("smtp send failed (nothing marked): %w", err)
		}
		fmt.Printf("sent %s to %s via %s:%d\n", ascPath, to, opt.SMTP.Host, opt.SMTP.Port)
		sent = true
	default:
		printManualInstructions(ascPath, to, subject, meta)
		if opt.Yes {
			sent = true
		} else {
			sent = confirm(opt.In, fmt.Sprintf(
				"Confirm you have sent %s to %s? [y/N]: ", ascPath, orPlaceholder(to)))
		}
	}

	if !sent {
		fmt.Println("aborted: nothing was marked. Re-run submit after sending, or the")
		fmt.Println("next pull will include these cracks again.")
		return nil
	}

	marked, err := pool.Mark(ctx, meta.IDs)
	if err != nil {
		return fmt.Errorf("send succeeded but marking failed: %w\n"+
			"the send is fine; KoreLogic dedupes, so just re-run submit --iter %s "+
			"or let the next pull re-include these", err, opt.Iter)
	}
	fmt.Printf("marked %d/%d cracks submitted\n", marked, len(meta.IDs))
	return nil
}

func sendSMTP(cfg *SMTPConfig, to, subject string, body []byte) error {
	var auth smtp.Auth
	if cfg.User != "" {
		auth = smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)
	}
	from := cfg.From
	if from == "" {
		from = cfg.User
	}
	msg := buildMessage(from, to, subject, body)
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	return smtp.SendMail(addr, auth, from, []string{to}, msg)
}

// buildMessage places the ASCII-armored block directly in the body, which is
// what CMIYC autoresponders have historically parsed.
func buildMessage(from, to, subject string, body []byte) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.Write(body)
	return []byte(b.String())
}

func printManualInstructions(ascPath, to, subject string, meta SubmissionMeta) {
	fmt.Println("assisted-manual send")
	fmt.Printf("  to      : %s\n", orPlaceholder(to))
	fmt.Printf("  subject : %s\n", subject)
	fmt.Printf("  body    : the full contents of %s\n", ascPath)
	fmt.Printf("  lines   : %d (%s)\n", meta.LineCount, meta.Kind)
	fmt.Println()
	fmt.Println("options:")
	fmt.Printf("  - paste the armored block into a new Gmail message, or\n")
	if to != "" {
		fmt.Printf("  - mailx -s %q %s < %s\n", subject, to, ascPath)
	} else {
		fmt.Printf("  - mailx -s %q <contest address> < %s\n", subject, ascPath)
	}
	fmt.Println()
}

func confirm(in io.Reader, prompt string) bool {
	if in == nil {
		return false
	}
	fmt.Print(prompt)
	sc := bufio.NewScanner(in)
	if !sc.Scan() {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "y" || ans == "yes"
}
