package client

import (
	"path/filepath"
	"time"
)

// SubmissionMeta is the sidecar written by `pull` and read by `submit`. It is
// how the two commands agree on exactly which ids may be marked, without a
// second server round trip. Only was_pending ids appear in IDs, so a `--full`
// pull re-ships everything but can only ever mark the previously-pending subset.
type SubmissionMeta struct {
	Iter         string  `json:"iter"`
	Kind         string  `json:"kind"` // "delta" or "full"
	ContestEmail string  `json:"contest_email"`
	IDs          []int64 `json:"ids"`        // eligible-to-mark ids (was_pending)
	LineCount    int     `json:"line_count"` // total lines shipped in the payload
	CreatedAt    string  `json:"created_at"`
}

// NewIter returns a compact UTC timestamp token that names one submission
// iteration, e.g. 20260807T140355Z.
func NewIter() string {
	return time.Now().UTC().Format("20060102T150405Z")
}

func FoundsPath(dir, iter string) string {
	return filepath.Join(dir, "founds_"+iter+".txt")
}

func AscPath(dir, iter string) string {
	return filepath.Join(dir, "submission_"+iter+".asc")
}

func MetaPath(dir, iter string) string {
	return filepath.Join(dir, "submission_"+iter+".ids.json")
}
