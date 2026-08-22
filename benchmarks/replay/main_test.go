package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestReplayCreatesTasksAndExportsCohort(t *testing.T) {
	var mu sync.Mutex
	nextID := 40
	created := []map[string]any{}
	mux := http.NewServeMux()
	for _, endpoint := range snapshotEndpoints {
		mux.HandleFunc(endpoint, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Error("missing bearer token")
			}
			_, _ = w.Write([]byte(`{"snapshot":true}`))
		})
	}
	mux.HandleFunc("POST /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		created = append(created, body)
		nextID++
		id := nextID
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
	})
	mux.HandleFunc("GET /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": r.PathValue("id"), "status": "done"})
	})
	mux.HandleFunc("GET /api/tasks/{id}/performance-baseline", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"task_id": r.PathValue("id"), "repeatable": true})
	})
	mux.HandleFunc("GET /api/tasks/performance-baselines", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"task_ids": r.URL.Query().Get("task_ids")})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	dir := t.TempDir()
	scenarioPath := filepath.Join(dir, "scenario.json")
	outPath := filepath.Join(dir, "result.json")
	spec := `{
  "schema_version":"restxtra.benchmark-scenario.v1",
  "dataset":{"name":"fixture","version":"1","authorization_reference":"test","reset_reference":"snapshot-1"},
  "environment":{"model":"fixture-model"},
  "repetitions":2,
  "poll_interval_seconds":1,
  "max_wait_seconds":2,
  "tasks":[{"id":"web","description":"fixture","goal":"test http://127.0.0.1:3000","timeout_seconds":1}]
}`
	if err := os.WriteFile(scenarioPath, []byte(spec), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), scenarioPath, outPath, server.URL, "test-token", false, server.Client()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var result replayResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Complete || len(result.Runs) != 2 || len(result.EnvironmentHashes) != len(snapshotEndpoints) {
		t.Fatalf("unexpected replay result: %+v", result)
	}
	if len(created) != 2 || created[0]["goal"] != created[1]["goal"] {
		t.Fatalf("task input drifted: %+v", created)
	}
	var cohort map[string]string
	if err := json.Unmarshal(result.Cohort, &cohort); err != nil || cohort["task_ids"] != "41,42" {
		t.Fatalf("unexpected cohort: %s (err=%v)", result.Cohort, err)
	}
}

func TestReplayRejectsBearerTokenOverRemoteHTTP(t *testing.T) {
	err := run(context.Background(), "missing.json", filepath.Join(t.TempDir(), "out.json"), "http://example.com", "secret", false, nil)
	if err == nil || !strings.Contains(err.Error(), "remote HTTP") {
		t.Fatalf("expected insecure HTTP rejection, got %v", err)
	}
}

func TestLoadScenarioRejectsUnsafeOrDriftingInputs(t *testing.T) {
	base := `{"schema_version":"restxtra.benchmark-scenario.v1","dataset":{"name":"d","version":"v","authorization_reference":"a","reset_reference":"r"},"environment":{"model":"m"},"repetitions":1,"tasks":[{"id":"x","description":"d","goal":"g","timeout_seconds":10}]}`
	for name, mutation := range map[string]string{
		"missing authorization": strings.Replace(base, `"authorization_reference":"a"`, `"authorization_reference":""`, 1),
		"no timeout":            strings.Replace(base, `"timeout_seconds":10`, `"timeout_seconds":0`, 1),
		"unknown field":         strings.TrimSuffix(base, "}") + `,"unexpected":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "scenario.json")
			if err := os.WriteFile(path, []byte(mutation), 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := loadScenario(path); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
