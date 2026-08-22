package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

const defaultMaxOutput = 30000

func maxOut(tc *ToolContext) int {
	if tc != nil && tc.MaxOutputChars > 0 {
		return tc.MaxOutputChars
	}
	return defaultMaxOutput
}

var spillSeq atomic.Int64

// PersistedOutput is the machine-readable pointer emitted when Capture spills a
// large result. Hosts can register the file as an artifact without parsing prose.
type PersistedOutput struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Lines int64  `json:"lines"`
	MIME  string `json:"mime"`
}

// Capture bounds a tool's textual output for the model. When the output exceeds
// the limit (ToolContext.MaxOutputChars, default 30000) and the host configured
// an OutputDir, the FULL output is written to a file there and a tight head + a
// pointer to that file is returned — so the model can read slices on demand
// (grep/sed) instead of losing the overflow. Without an OutputDir it falls back
// to a head+tail truncation (the overflow is discarded). Tools that produce
// potentially large output should return Capture(tc, output) rather than the raw
// string.
func Capture(tc *ToolContext, s string) string {
	max := maxOut(tc)
	if len(s) <= max {
		return s
	}
	if tc != nil && tc.OutputDir != "" {
		if path, err := spillOutput(tc.OutputDir, s); err == nil {
			ref := path
			if tc.WorkingDir != "" {
				if rel, e := filepath.Rel(tc.WorkingDir, path); e == nil && !strings.HasPrefix(rel, "..") {
					ref = rel
				}
			}
			lines := strings.Count(s, "\n") + 1
			meta, _ := json.Marshal(PersistedOutput{Path: ref, Bytes: int64(len(s)), Lines: int64(lines), MIME: "text/plain; charset=utf-8"})
			return s[:max] + "\n\n... Full output saved as an artifact: <persisted-output>" + string(meta) + "</persisted-output>"
		}
	}
	return truncate(s, max)
}

// ParsePersistedOutputs extracts Capture's stable metadata envelopes. Invalid
// or incomplete envelopes are ignored so untrusted tool text cannot fabricate
// an artifact without also resolving to a real file at the host boundary.
func ParsePersistedOutputs(s string) []PersistedOutput {
	const open, close = "<persisted-output>", "</persisted-output>"
	var out []PersistedOutput
	for {
		start := strings.Index(s, open)
		if start < 0 {
			return out
		}
		s = s[start+len(open):]
		end := strings.Index(s, close)
		if end < 0 {
			return out
		}
		var ref PersistedOutput
		if json.Unmarshal([]byte(s[:end]), &ref) == nil && ref.Path != "" {
			out = append(out, ref)
		}
		s = s[end+len(close):]
	}
}

func spillOutput(dir, s string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("output-%d-%d.txt", time.Now().Unix(), spillSeq.Add(1))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// resolvePath joins a relative path against the working directory.
func resolvePath(tc *ToolContext, p string) string {
	if filepath.IsAbs(p) || tc == nil || tc.WorkingDir == "" {
		return p
	}
	return filepath.Join(tc.WorkingDir, p)
}

// truncate trims s to max characters, keeping head and tail.
func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	half := max / 2
	return s[:half] + fmt.Sprintf("\n\n... [%d characters truncated] ...\n\n", len(s)-max) + s[len(s)-half:]
}
