// Command cmiyc is a single self-contained binary with four operational modes
// plus a stats helper. One codebase, one image, one thing to reason about.
//
//	cmiyc serve    run the shared pool (this is what deploys to Railway)
//	cmiyc send     upload a local hashcat potfile to the pool (every teammate)
//	cmiyc pull     build the encrypted submission bundle locally (operator only)
//	cmiyc submit   send a pulled bundle to KoreLogic, then mark it (operator only)
//	cmiyc stats    print the internal team-distribution snapshot
//
// serve is the only mode that needs the database; the rest talk to serve over
// HTTP, so PGP keys and the contest email account never leave the operator's
// machine.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/zilla/cmiyc-pool/internal/client"
	"github.com/zilla/cmiyc-pool/internal/server"
	"github.com/zilla/cmiyc-pool/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]

	var err error
	switch cmd {
	case "serve":
		err = cmdServe(args)
	case "send":
		err = cmdSend(args)
	case "pull":
		err = cmdPull(args)
	case "submit":
		err = cmdSubmit(args)
	case "stats":
		err = cmdStats(args)
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `cmiyc - CMIYC submission pool

usage: cmiyc <command> [flags]

commands:
  serve    run the shared pool server (deploys to Railway)
  send     upload a local hashcat potfile to the pool
  pull     build the encrypted submission bundle locally (--full for a failsafe sweep)
  submit   send a pulled bundle to KoreLogic and mark it submitted
  stats    print internal team-distribution stats

run "cmiyc <command> -h" for command-specific flags.
`)
}

// --- serve ---

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.String("port", envOr("PORT", "8080"),
		"TCP port to bind (Railway injects PORT)")
	dsn := fs.String("database-url", os.Getenv("DATABASE_URL"),
		"PostgreSQL connection string (Railway injects DATABASE_URL)")
	token := fs.String("token", os.Getenv("POOL_TOKEN"),
		"shared bearer token required on every API call")
	fs.Parse(args)

	if *dsn == "" {
		return fmt.Errorf("database-url is required (set DATABASE_URL)")
	}
	if *token == "" {
		return fmt.Errorf("token is required (set POOL_TOKEN)")
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.New(ctx, *dsn)
	if err != nil {
		return err
	}
	defer st.Close()

	srv, err := server.New(st, *token, log)
	if err != nil {
		return err
	}
	// Bind all interfaces so Railway's proxy can reach the container.
	return srv.ListenAndServe("0.0.0.0:" + *port)
}

// --- send (upload) ---

func cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	author := fs.String("author", os.Getenv("CMIYC_AUTHOR"),
		"who is uploading (or set CMIYC_AUTHOR)")
	potPath := fs.String("potfile-path", defaultPotfile(),
		"path to the hashcat potfile to upload")
	poolURL, token := clientFlags(fs)
	fs.Parse(args)

	pool := client.NewPool(*poolURL, *token)
	return client.RunSend(context.Background(), pool, *author, *potPath)
}

// --- pull ---

func cmdPull(args []string) error {
	fs := flag.NewFlagSet("pull", flag.ExitOnError)
	full := fs.Bool("full", false,
		"ship EVERY cracked line (daily failsafe), not just pending")
	outDir := fs.String("out-dir", "./out",
		"directory to write founds/asc/sidecar artifacts")
	recipient := fs.String("recipient-key", os.Getenv("CMIYC_RECIPIENT_KEY"),
		"path to KoreLogic armored public key (or set CMIYC_RECIPIENT_KEY)")
	signKey := fs.String("sign-key", os.Getenv("CMIYC_SIGN_KEY"),
		"optional path to team private key to sign the payload")
	passphrase := fs.String("passphrase", os.Getenv("CMIYC_SIGN_PASSPHRASE"),
		"optional passphrase unlocking the signing key")
	contestEmail := fs.String("to", os.Getenv("CMIYC_CONTEST_EMAIL"),
		"contest submission address, recorded for the later submit step")
	poolURL, token := clientFlags(fs)
	fs.Parse(args)

	pool := client.NewPool(*poolURL, *token)
	return client.RunPull(context.Background(), pool, client.PullOptions{
		Full:         *full,
		OutDir:       *outDir,
		RecipientKey: *recipient,
		SignKey:      *signKey,
		Passphrase:   []byte(*passphrase),
		ContestEmail: *contestEmail,
	})
}

// --- submit ---

func cmdSubmit(args []string) error {
	fs := flag.NewFlagSet("submit", flag.ExitOnError)
	iter := fs.String("iter", "", "iteration token printed by pull (required)")
	dir := fs.String("dir", "./out", "directory holding the pulled artifacts")
	to := fs.String("to", os.Getenv("CMIYC_CONTEST_EMAIL"),
		"override the contest address recorded at pull time")
	subject := fs.String("subject", "", "email subject (defaults to CMIYC submission <iter>)")
	yes := fs.Bool("yes", false, "skip the confirmation prompt")

	smtpHost := fs.String("smtp-host", "", "SMTP host; if set, submit sends the email itself")
	smtpPort := fs.Int("smtp-port", 587, "SMTP port")
	smtpUser := fs.String("smtp-user", os.Getenv("CMIYC_SMTP_USER"), "SMTP username")
	smtpPass := fs.String("smtp-pass", os.Getenv("CMIYC_SMTP_PASS"), "SMTP password")
	smtpFrom := fs.String("smtp-from", os.Getenv("CMIYC_SMTP_FROM"), "envelope/from address")

	poolURL, token := clientFlags(fs)
	fs.Parse(args)

	opt := client.SubmitOptions{
		Iter:         *iter,
		Dir:          *dir,
		ContestEmail: *to,
		Subject:      *subject,
		Yes:          *yes,
		In:           os.Stdin,
	}
	if *smtpHost != "" {
		opt.SMTP = &client.SMTPConfig{
			Host: *smtpHost, Port: *smtpPort,
			User: *smtpUser, Pass: *smtpPass, From: *smtpFrom,
		}
	}

	pool := client.NewPool(*poolURL, *token)
	return client.RunSubmit(context.Background(), pool, opt)
}

// --- stats ---

func cmdStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	poolURL, token := clientFlags(fs)
	fs.Parse(args)

	pool := client.NewPool(*poolURL, *token)
	st, err := pool.Stats(context.Background())
	if err != nil {
		return err
	}
	fmt.Printf("total unique cracks : %d\n", st.TotalUnique)
	fmt.Printf("submitted to CMIYC  : %d\n", st.Submitted)
	fmt.Printf("pending             : %d\n", st.Pending)
	fmt.Printf("independent overlap : %d cracks held by >1 teammate\n", st.OverlapCracks)
	fmt.Println("first-crack (race winner) by author:")
	for a, n := range st.FirstByAuthor {
		fmt.Printf("  %-16s %d\n", a, n)
	}
	fmt.Println("total cracks ever held by author:")
	for a, n := range st.ContribByAuthor {
		fmt.Printf("  %-16s %d\n", a, n)
	}
	return nil
}

// --- shared flag/env helpers ---

func clientFlags(fs *flag.FlagSet) (poolURL, token *string) {
	poolURL = fs.String("pool-url", envOr("POOL_URL", "http://localhost:8080"),
		"base URL of the pool server (or set POOL_URL)")
	token = fs.String("token", os.Getenv("POOL_TOKEN"),
		"shared bearer token (or set POOL_TOKEN)")
	return poolURL, token
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// defaultPotfile returns hashcat's default potfile location under $HOME.
func defaultPotfile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "hashcat.potfile"
	}
	return filepath.Join(home, ".local", "share", "hashcat", "hashcat.potfile")
}
