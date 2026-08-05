// Package potfile parses hashcat potfile / --show output into normalized,
// opaque cracked lines. It deliberately knows nothing about hash formats: a
// "line" is whatever text sits between newlines (typically "hash:plaintext"),
// and the only transformation applied is stripping the trailing CR/LF so that
// the same crack hashes identically regardless of the uploader's line endings.
//
// Plaintexts may legally contain ':' and arbitrary UTF-8, so lines are never
// split on ':' anywhere in this codebase. The dedupe key is a SHA-256 over the
// exact normalized bytes.
package potfile

import (
	"bufio"
	"crypto/sha256"
	"io"
)

// maxLineBytes bounds a single scanned line. Network hash formats such as WPA
// (mode 22000) and some Kerberos lines are long, so the default bufio.Scanner
// limit (64 KiB) is raised generously.
const maxLineBytes = 8 * 1024 * 1024 // 8 MiB

// Line is one normalized cracked record.
type Line struct {
	Text string   // exact normalized line, e.g. "$2b$05$...:hunter2"
	Hash [32]byte // sha256(Text), the global dedupe key
}

// Parse reads a potfile stream and returns normalized, de-duplicated lines.
// Blank lines are skipped. Duplicate lines within the same stream are collapsed
// so the caller ships each unique crack once. Order of first appearance is
// preserved to keep output stable and human-diffable.
func Parse(r io.Reader) ([]Line, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	var out []Line
	seen := make(map[[32]byte]struct{})

	for sc.Scan() {
		text := normalize(sc.Text())
		if text == "" {
			continue
		}
		h := sha256.Sum256([]byte(text))
		if _, dup := seen[h]; dup {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, Line{Text: text, Hash: h})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// normalize strips only the trailing carriage return that bufio.Scanner leaves
// on CRLF input. Everything else in the line is preserved byte-for-byte.
func normalize(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\r' || s[len(s)-1] == '\n') {
		s = s[:len(s)-1]
	}
	return s
}
