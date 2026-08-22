package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"reflect"
	"sort"
	"strings"
)

type replayFile struct {
	SchemaVersion     string            `json:"schema_version"`
	Complete          bool              `json:"complete"`
	ScenarioSHA256    string            `json:"scenario_sha256"`
	SourceRevision    string            `json:"source_revision"`
	SourceDiffSHA256  string            `json:"source_diff_sha256"`
	Dataset           json.RawMessage   `json:"dataset"`
	Environment       json.RawMessage   `json:"environment"`
	EnvironmentHashes map[string]string `json:"environment_hashes"`
	Runs              []replayRun       `json:"runs"`
}

type replayRun struct {
	CaseID     string          `json:"case_id"`
	Repetition int             `json:"repetition"`
	Status     string          `json:"status"`
	Baseline   json.RawMessage `json:"baseline"`
}

type metricStats struct {
	Samples int     `json:"samples"`
	Mean    float64 `json:"mean"`
	P50     float64 `json:"p50"`
	P95     float64 `json:"p95"`
}

type metricDelta struct {
	Direction          string      `json:"direction"`
	ComparableSamples  bool        `json:"comparable_samples"`
	Before             metricStats `json:"before"`
	After              metricStats `json:"after"`
	MeanDelta          float64     `json:"mean_delta"`
	MeanDeltaPercent   *float64    `json:"mean_delta_percent,omitempty"`
	ImprovementPercent *float64    `json:"improvement_percent,omitempty"`
}

type comparison struct {
	SchemaVersion    string                    `json:"schema_version"`
	Comparable       bool                      `json:"comparable"`
	ScenarioHash     string                    `json:"scenario_sha256"`
	BeforeSource     map[string]string         `json:"before_source"`
	AfterSource      map[string]string         `json:"after_source"`
	RunCount         int                       `json:"run_count"`
	Statuses         map[string]map[string]int `json:"statuses"`
	EnvironmentDrift map[string][2]string      `json:"environment_drift,omitempty"`
	Metrics          map[string]metricDelta    `json:"metrics"`
}

func main() {
	var beforePath, afterPath, outPath, allowedDrift string
	flag.StringVar(&beforePath, "before", "", "completed before replay JSON")
	flag.StringVar(&afterPath, "after", "", "completed after replay JSON")
	flag.StringVar(&outPath, "out", "", "comparison JSON path; stdout when empty")
	flag.StringVar(&allowedDrift, "allow-environment-drift", "", "comma-separated API fingerprint paths intentionally changed")
	flag.Parse()
	result, err := compareFiles(beforePath, afterPath, splitAllowedDrift(allowedDrift)...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "benchmark compare:", err)
		os.Exit(1)
	}
	raw, _ := json.MarshalIndent(result, "", "  ")
	raw = append(raw, '\n')
	if outPath == "" {
		_, _ = os.Stdout.Write(raw)
		return
	}
	if err := os.WriteFile(outPath, raw, 0600); err != nil {
		fmt.Fprintln(os.Stderr, "benchmark compare:", err)
		os.Exit(1)
	}
}

func readReplay(path string) (replayFile, error) {
	var value replayFile
	if strings.TrimSpace(path) == "" {
		return value, errors.New("both -before and -after are required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, err
	}
	if !value.Complete || value.SchemaVersion != "restxtra.benchmark-replay.v1" {
		return value, errors.New("replay must be complete and use restxtra.benchmark-replay.v1")
	}
	if len(value.Runs) == 0 {
		return value, errors.New("replay has no runs")
	}
	return value, nil
}

func compareFiles(beforePath, afterPath string, allowedDrift ...string) (*comparison, error) {
	before, err := readReplay(beforePath)
	if err != nil {
		return nil, fmt.Errorf("before: %w", err)
	}
	after, err := readReplay(afterPath)
	if err != nil {
		return nil, fmt.Errorf("after: %w", err)
	}
	if before.ScenarioSHA256 == "" || before.ScenarioSHA256 != after.ScenarioSHA256 {
		return nil, errors.New("scenario hashes differ")
	}
	if !jsonEqual(before.Dataset, after.Dataset) || !jsonEqual(before.Environment, after.Environment) {
		return nil, errors.New("dataset or declared environment differs")
	}
	drift, err := environmentDrift(before.EnvironmentHashes, after.EnvironmentHashes, allowedDrift)
	if err != nil {
		return nil, err
	}
	if err := sameRuns(before.Runs, after.Runs); err != nil {
		return nil, err
	}
	beforeMetrics, err := collectMetrics(before.Runs)
	if err != nil {
		return nil, fmt.Errorf("before metrics: %w", err)
	}
	afterMetrics, err := collectMetrics(after.Runs)
	if err != nil {
		return nil, fmt.Errorf("after metrics: %w", err)
	}
	out := &comparison{
		SchemaVersion: "restxtra.benchmark-comparison.v1", Comparable: true,
		ScenarioHash: before.ScenarioSHA256, RunCount: len(before.Runs),
		BeforeSource:     map[string]string{"revision": before.SourceRevision, "diff_sha256": before.SourceDiffSHA256},
		AfterSource:      map[string]string{"revision": after.SourceRevision, "diff_sha256": after.SourceDiffSHA256},
		Statuses:         map[string]map[string]int{"before": statusCounts(before.Runs), "after": statusCounts(after.Runs)},
		EnvironmentDrift: drift,
		Metrics:          map[string]metricDelta{},
	}
	keys := make([]string, 0, len(beforeMetrics))
	for key := range beforeMetrics {
		if _, ok := afterMetrics[key]; ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		beforeStats, afterStats := summarize(beforeMetrics[key]), summarize(afterMetrics[key])
		delta := metricDelta{
			Direction: metricDirection(key), ComparableSamples: beforeStats.Samples == afterStats.Samples,
			Before: beforeStats, After: afterStats, MeanDelta: afterStats.Mean - beforeStats.Mean,
		}
		if delta.ComparableSamples && beforeStats.Mean != 0 {
			rawPercent := 100 * delta.MeanDelta / math.Abs(beforeStats.Mean)
			delta.MeanDeltaPercent = &rawPercent
			improvement := rawPercent
			if delta.Direction == "lower_is_better" {
				improvement = -rawPercent
			}
			if delta.Direction != "informational" {
				delta.ImprovementPercent = &improvement
			}
		}
		out.Metrics[key] = delta
	}
	return out, nil
}

func splitAllowedDrift(raw string) []string {
	var out []string
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func environmentDrift(before, after map[string]string, allowed []string) (map[string][2]string, error) {
	allow := map[string]bool{}
	for _, key := range allowed {
		if !strings.HasPrefix(key, "/api/") {
			return nil, fmt.Errorf("invalid allowed environment path %q", key)
		}
		if _, beforeOK := before[key]; !beforeOK {
			return nil, fmt.Errorf("allowed environment path %q is not present in before", key)
		}
		if _, afterOK := after[key]; !afterOK {
			return nil, fmt.Errorf("allowed environment path %q is not present in after", key)
		}
		allow[key] = true
	}
	drift := map[string][2]string{}
	keys := map[string]bool{}
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	for key := range keys {
		if before[key] == after[key] {
			continue
		}
		if !allow[key] {
			return nil, fmt.Errorf("live environment hash differs at %s", key)
		}
		drift[key] = [2]string{before[key], after[key]}
	}
	return drift, nil
}

func jsonEqual(left, right json.RawMessage) bool {
	var a, b any
	return json.Unmarshal(left, &a) == nil && json.Unmarshal(right, &b) == nil && reflect.DeepEqual(a, b)
}

func runKey(run replayRun) string { return run.CaseID + "#" + fmt.Sprint(run.Repetition) }

func sameRuns(before, after []replayRun) error {
	if len(before) != len(after) {
		return errors.New("run counts differ")
	}
	seen := map[string]bool{}
	for _, run := range before {
		key := runKey(run)
		if seen[key] {
			return fmt.Errorf("before has duplicate run %s", key)
		}
		seen[key] = true
	}
	matched := map[string]bool{}
	for _, run := range after {
		key := runKey(run)
		if !seen[key] || matched[key] {
			return fmt.Errorf("run set differs at %s", key)
		}
		matched[key] = true
	}
	return nil
}

func collectMetrics(runs []replayRun) (map[string][]float64, error) {
	out := map[string][]float64{}
	for _, run := range runs {
		var baseline map[string]any
		dec := json.NewDecoder(bytes.NewReader(run.Baseline))
		dec.UseNumber()
		if err := dec.Decode(&baseline); err != nil {
			return nil, fmt.Errorf("%s: %w", runKey(run), err)
		}
		flattenMetrics("", baseline, out)
	}
	return out, nil
}

func flattenMetrics(prefix string, value any, out map[string][]float64) {
	if object, ok := value.(map[string]any); ok {
		for key, child := range object {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			flattenMetrics(path, child, out)
		}
		return
	}
	if !metricPath(prefix) {
		return
	}
	number, ok := value.(json.Number)
	if !ok {
		return
	}
	if parsed, err := number.Float64(); err == nil {
		out[prefix] = append(out[prefix], parsed)
	}
}

func metricPath(path string) bool {
	for _, prefix := range []string{"results.", "intents.", "coverage.", "usage.", "efficiency."} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return strings.HasPrefix(path, "time_to_") || path == "task_completion_seconds"
}

func summarize(values []float64) metricStats {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	stats := metricStats{Samples: len(sorted)}
	for _, value := range sorted {
		stats.Mean += value
	}
	if len(sorted) == 0 {
		return stats
	}
	stats.Mean /= float64(len(sorted))
	stats.P50, stats.P95 = percentile(sorted, 0.50), percentile(sorted, 0.95)
	return stats
}

func percentile(sorted []float64, p float64) float64 {
	index := int(math.Ceil(p*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	return sorted[index]
}

func metricDirection(path string) string {
	lower := strings.HasPrefix(path, "time_to_") || path == "task_completion_seconds" ||
		path == "usage.input_tokens" || path == "usage.output_tokens" || path == "usage.tool_calls" ||
		path == "usage.tool_errors" || path == "usage.rounds" ||
		path == "intents.total" || path == "intents.attempts" || path == "intents.repeated_attempts" ||
		path == "intents.duplicate_intents" || path == "intents.zero_yield_intents" ||
		strings.Contains(path, "error_rate") || strings.Contains(path, "duplicate_intent_rate") ||
		strings.Contains(path, "zero_yield_intent_rate") || strings.Contains(path, "tokens_per_finding") ||
		strings.Contains(path, "tool_calls_per_finding")
	if lower {
		return "lower_is_better"
	}
	higher := strings.HasPrefix(path, "results.") || path == "coverage.verified" ||
		path == "coverage.verified_rate" || path == "efficiency.cache_read_rate" ||
		path == "efficiency.evidence_coverage_rate" || strings.Contains(path, "results_per_")
	if higher {
		return "higher_is_better"
	}
	return "informational"
}

func statusCounts(runs []replayRun) map[string]int {
	out := map[string]int{}
	for _, run := range runs {
		out[run.Status]++
	}
	return out
}
