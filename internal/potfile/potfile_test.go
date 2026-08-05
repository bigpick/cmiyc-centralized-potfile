package potfile

import (
	"strings"
	"testing"
)

func TestParseDedupesAndNormalizes(t *testing.T) {
	in := strings.Join([]string{
		"$2b$05$abc:hunter2",
		"$2b$05$abc:hunter2", // exact dup, dropped
		"",                   // blank, skipped
		"deadbeef:pass:with:colons\r", // CRLF trimmed, colons preserved
		"deadbeef:pass:with:colons",   // dup of the above after normalization
		"aaaa:naïve",                  // unicode preserved
	}, "\n")

	lines, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got, want := len(lines), 3; got != want {
		t.Fatalf("unique lines = %d, want %d", got, want)
	}

	want := []string{
		"$2b$05$abc:hunter2",
		"deadbeef:pass:with:colons",
		"aaaa:naïve",
	}
	for i, w := range want {
		if lines[i].Text != w {
			t.Errorf("line[%d] = %q, want %q", i, lines[i].Text, w)
		}
	}
}

func TestParseHashIsStable(t *testing.T) {
	a, _ := Parse(strings.NewReader("x:y\n"))
	b, _ := Parse(strings.NewReader("x:y\r\n"))
	if a[0].Hash != b[0].Hash {
		t.Fatal("CRLF vs LF produced different hashes; normalization is broken")
	}
}
