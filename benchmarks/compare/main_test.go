package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeReplay(t *testing.T, path, scenario, envHash string, tokens, findings float64) {
	t.Helper()
	baseline, _ := json.Marshal(map[string]any{
		"time_to_first_confirmed_fact_seconds": tokens / 10,
		"results":                              map[string]any{"confirmed_findings": findings},
		"usage":                                map[string]any{"input_tokens": tokens},
		"efficiency": map[string]any{
			"confirmed_results_per_1k_input_tokens": findings * 1000 / tokens,
		},
	})
	value := map[string]any{
		"schema_version": "restxtra.benchmark-replay.v1", "complete": true,
		"scenario_sha256": scenario, "source_revision": path,
		"dataset":            map[string]any{"name": "fixture", "version": "1"},
		"environment":        map[string]any{"model": "fixture"},
		"environment_hashes": map[string]string{"/api/tools": envHash},
		"runs": []any{
			map[string]any{"case_id": "web", "repetition": 1, "status": "done", "baseline": json.RawMessage(baseline)},
			map[string]any{"case_id": "web", "repetition": 2, "status": "done", "baseline": json.RawMessage(baseline)},
		},
	}
	raw, _ := json.Marshal(value)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCompareFilesAggregatesAndClassifiesMetrics(t *testing.T) {
	dir := t.TempDir()
	before, after := filepath.Join(dir, "before.json"), filepath.Join(dir, "after.json")
	writeReplay(t, before, "same", "same-env", 100, 1)
	writeReplay(t, after, "same", "same-env", 50, 2)
	result, err := compareFiles(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Comparable || result.RunCount != 2 {
		t.Fatalf("unexpected comparison: %+v", result)
	}
	tokens := result.Metrics["usage.input_tokens"]
	if tokens.Direction != "lower_is_better" || tokens.MeanDelta != -50 || tokens.ImprovementPercent == nil || *tokens.ImprovementPercent != 50 {
		t.Fatalf("unexpected token delta: %+v", tokens)
	}
	findings := result.Metrics["results.confirmed_findings"]
	if findings.Direction != "higher_is_better" || findings.ImprovementPercent == nil || *findings.ImprovementPercent != 100 {
		t.Fatalf("unexpected finding delta: %+v", findings)
	}
}

func TestCompareFilesRejectsEnvironmentDrift(t *testing.T) {
	dir := t.TempDir()
	before, after := filepath.Join(dir, "before.json"), filepath.Join(dir, "after.json")
	writeReplay(t, before, "same", "env-a", 100, 1)
	writeReplay(t, after, "same", "env-b", 100, 1)
	if _, err := compareFiles(before, after); err == nil {
		t.Fatal("expected environment drift rejection")
	}
	result, err := compareFiles(before, after, "/api/tools")
	if err != nil || len(result.EnvironmentDrift) != 1 {
		t.Fatalf("explicit drift should be recorded: result=%+v err=%v", result, err)
	}
}
