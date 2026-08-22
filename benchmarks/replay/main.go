package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const scenarioSchemaV1 = "restxtra.benchmark-scenario.v1"

var snapshotEndpoints = []string{
	"/api/settings", "/api/llm/profiles", "/api/agents", "/api/tools", "/api/skills",
}

type datasetSpec struct {
	Name                   string `json:"name"`
	Version                string `json:"version"`
	AuthorizationReference string `json:"authorization_reference"`
	ResetReference         string `json:"reset_reference"`
}

type taskSpec struct {
	ID                   string          `json:"id"`
	Description          string          `json:"description"`
	Goal                 string          `json:"goal"`
	LLMProfileID         *int64          `json:"llm_profile_id,omitempty"`
	TimeoutSeconds       int             `json:"timeout_seconds"`
	PlanHeartbeatSeconds int             `json:"plan_heartbeat_seconds,omitempty"`
	Workflow             json.RawMessage `json:"workflow,omitempty"`
}

type scenario struct {
	SchemaVersion       string                     `json:"schema_version"`
	Dataset             datasetSpec                `json:"dataset"`
	Environment         map[string]json.RawMessage `json:"environment"`
	Repetitions         int                        `json:"repetitions"`
	PollIntervalSeconds int                        `json:"poll_interval_seconds"`
	MaxWaitSeconds      int                        `json:"max_wait_seconds"`
	Tasks               []taskSpec                 `json:"tasks"`
}

type taskRun struct {
	CaseID     string          `json:"case_id"`
	Repetition int             `json:"repetition"`
	TaskID     string          `json:"task_id"`
	Status     string          `json:"status"`
	Baseline   json.RawMessage `json:"baseline,omitempty"`
}

type replayResult struct {
	SchemaVersion       string            `json:"schema_version"`
	Complete            bool              `json:"complete"`
	StartedAt           time.Time         `json:"started_at"`
	FinishedAt          *time.Time        `json:"finished_at,omitempty"`
	BaseURL             string            `json:"base_url"`
	ScenarioSHA256      string            `json:"scenario_sha256"`
	SourceRevision      string            `json:"source_revision,omitempty"`
	SourceDirty         bool              `json:"source_dirty"`
	SourceDiffSHA256    string            `json:"source_diff_sha256,omitempty"`
	Dataset             datasetSpec       `json:"dataset"`
	Environment         map[string]any    `json:"environment"`
	EnvironmentHashes   map[string]string `json:"environment_hashes"`
	Runs                []taskRun         `json:"runs"`
	Cohort              json.RawMessage   `json:"cohort,omitempty"`
	LastCheckpointError string            `json:"last_checkpoint_error,omitempty"`
}

type apiClient struct {
	base  string
	token string
	http  *http.Client
}

func main() {
	var scenarioPath, outPath, baseURL string
	var allowInsecureHTTP bool
	flag.StringVar(&scenarioPath, "scenario", "", "benchmark scenario JSON")
	flag.StringVar(&outPath, "out", "", "checkpoint/result JSON path")
	flag.StringVar(&baseURL, "base-url", "http://localhost:8787", "RestXtraAI base URL")
	flag.BoolVar(&allowInsecureHTTP, "allow-insecure-http", false, "allow Bearer auth over non-loopback HTTP")
	flag.Parse()
	if err := run(context.Background(), scenarioPath, outPath, baseURL, os.Getenv("RESTXTRA_BENCH_TOKEN"), allowInsecureHTTP, nil); err != nil {
		fmt.Fprintln(os.Stderr, "benchmark replay:", err)
		os.Exit(1)
	}
}

func loadScenario(path string) (scenario, []byte, error) {
	var spec scenario
	if strings.TrimSpace(path) == "" {
		return spec, nil, errors.New("-scenario is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return spec, nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&spec); err != nil {
		return spec, nil, fmt.Errorf("decode scenario: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return spec, nil, errors.New("scenario must contain exactly one JSON object")
	}
	if spec.SchemaVersion != scenarioSchemaV1 {
		return spec, nil, fmt.Errorf("schema_version must be %q", scenarioSchemaV1)
	}
	if strings.TrimSpace(spec.Dataset.Name) == "" || strings.TrimSpace(spec.Dataset.Version) == "" {
		return spec, nil, errors.New("dataset name and version are required")
	}
	if strings.TrimSpace(spec.Dataset.AuthorizationReference) == "" {
		return spec, nil, errors.New("dataset authorization_reference is required")
	}
	if strings.TrimSpace(spec.Dataset.ResetReference) == "" {
		return spec, nil, errors.New("dataset reset_reference is required")
	}
	if len(spec.Environment) == 0 {
		return spec, nil, errors.New("environment metadata is required")
	}
	if spec.Repetitions <= 0 || spec.Repetitions > 20 {
		return spec, nil, errors.New("repetitions must be between 1 and 20")
	}
	if len(spec.Tasks) == 0 || len(spec.Tasks)*spec.Repetitions > 100 {
		return spec, nil, errors.New("tasks must produce between 1 and 100 total runs")
	}
	seen := map[string]bool{}
	for i, task := range spec.Tasks {
		task.ID = strings.TrimSpace(task.ID)
		if task.ID == "" || seen[task.ID] {
			return spec, nil, fmt.Errorf("task %d has an empty or duplicate id", i)
		}
		seen[task.ID] = true
		if strings.TrimSpace(task.Description) == "" || strings.TrimSpace(task.Goal) == "" {
			return spec, nil, fmt.Errorf("task %q requires description and goal", task.ID)
		}
		if task.TimeoutSeconds <= 0 {
			return spec, nil, fmt.Errorf("task %q requires a positive timeout_seconds", task.ID)
		}
		spec.Tasks[i] = task
	}
	if spec.PollIntervalSeconds <= 0 {
		spec.PollIntervalSeconds = 5
	}
	if spec.PollIntervalSeconds > 60 {
		return spec, nil, errors.New("poll_interval_seconds cannot exceed 60")
	}
	maxTimeout := 0
	for _, task := range spec.Tasks {
		if task.TimeoutSeconds > maxTimeout {
			maxTimeout = task.TimeoutSeconds
		}
	}
	if spec.MaxWaitSeconds <= 0 {
		spec.MaxWaitSeconds = maxTimeout + 300
	}
	if spec.MaxWaitSeconds < maxTimeout {
		return spec, nil, errors.New("max_wait_seconds cannot be shorter than a task timeout")
	}
	return spec, raw, nil
}

func run(ctx context.Context, scenarioPath, outPath, baseURL, token string, allowInsecureHTTP bool, client *http.Client) error {
	if strings.TrimSpace(outPath) == "" {
		return errors.New("-out is required")
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("RESTXTRA_BENCH_TOKEN is required")
	}
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("-base-url must be an absolute HTTP(S) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("-base-url must use http or https")
	}
	if parsed.Scheme == "http" && !allowInsecureHTTP && !loopbackHost(parsed.Hostname()) {
		return errors.New("refusing to send Bearer token over remote HTTP; use HTTPS or explicitly pass -allow-insecure-http")
	}
	spec, raw, err := loadScenario(scenarioPath)
	if err != nil {
		return err
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	api := &apiClient{base: strings.TrimRight(parsed.String(), "/"), token: token, http: client}
	hash := sha256.Sum256(raw)
	revision, dirty, diffHash := sourceState(filepath.Dir(filepath.Dir(scenarioPath)))
	result := replayResult{
		SchemaVersion: "restxtra.benchmark-replay.v1", StartedAt: time.Now().UTC(), BaseURL: api.base,
		ScenarioSHA256: hex.EncodeToString(hash[:]), SourceRevision: revision, SourceDirty: dirty, SourceDiffSHA256: diffHash,
		Dataset: spec.Dataset, Environment: rawEnvironment(spec.Environment), EnvironmentHashes: map[string]string{}, Runs: []taskRun{},
	}
	if err := checkpoint(outPath, &result); err != nil {
		return err
	}
	for _, endpoint := range snapshotEndpoints {
		body, err := api.get(ctx, endpoint)
		if err != nil {
			return fmt.Errorf("environment snapshot %s: %w", endpoint, err)
		}
		digest := sha256.Sum256(body)
		result.EnvironmentHashes[endpoint] = hex.EncodeToString(digest[:])
	}
	if err := checkpoint(outPath, &result); err != nil {
		return err
	}

	for repetition := 1; repetition <= spec.Repetitions; repetition++ {
		for _, task := range spec.Tasks {
			id, err := api.createTask(ctx, task)
			if err != nil {
				return fmt.Errorf("create %s repetition %d: %w", task.ID, repetition, err)
			}
			result.Runs = append(result.Runs, taskRun{CaseID: task.ID, Repetition: repetition, TaskID: id, Status: "created"})
			if err := checkpoint(outPath, &result); err != nil {
				return err
			}
			status, err := api.waitTerminal(ctx, id, time.Duration(spec.PollIntervalSeconds)*time.Second, time.Duration(spec.MaxWaitSeconds)*time.Second)
			if err != nil {
				return fmt.Errorf("wait task %s: %w", id, err)
			}
			baseline, err := api.get(ctx, "/api/tasks/"+url.PathEscape(id)+"/performance-baseline")
			if err != nil {
				return fmt.Errorf("baseline task %s: %w", id, err)
			}
			last := &result.Runs[len(result.Runs)-1]
			last.Status, last.Baseline = status, json.RawMessage(baseline)
			if err := checkpoint(outPath, &result); err != nil {
				return err
			}
		}
	}
	ids := make([]string, 0, len(result.Runs))
	for _, item := range result.Runs {
		ids = append(ids, item.TaskID)
	}
	cohort, err := api.get(ctx, "/api/tasks/performance-baselines?task_ids="+url.QueryEscape(strings.Join(ids, ",")))
	if err != nil {
		return fmt.Errorf("cohort: %w", err)
	}
	finished := time.Now().UTC()
	result.Cohort, result.Complete, result.FinishedAt = json.RawMessage(cohort), true, &finished
	return checkpoint(outPath, &result)
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
}

func rawEnvironment(in map[string]json.RawMessage) map[string]any {
	out := make(map[string]any, len(in))
	for key, raw := range in {
		var value any
		if json.Unmarshal(raw, &value) == nil {
			out[key] = value
		}
	}
	return out
}

func checkpoint(path string, result *replayResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0600); err != nil {
		result.LastCheckpointError = err.Error()
		return fmt.Errorf("write checkpoint: %w", err)
	}
	return nil
}

func sourceState(repo string) (string, bool, string) {
	cmd := exec.Command("git", "-C", repo, "rev-parse", "HEAD")
	raw, err := cmd.Output()
	if err != nil {
		return "", false, ""
	}
	diff := exec.Command("git", "-C", repo, "diff", "--binary", "HEAD", "--")
	diffRaw, _ := diff.Output()
	if len(diffRaw) == 0 {
		return strings.TrimSpace(string(raw)), false, ""
	}
	hash := sha256.Sum256(diffRaw)
	return strings.TrimSpace(string(raw)), true, hex.EncodeToString(hash[:])
}

func (c *apiClient) request(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if len(raw) > 4096 {
			raw = raw[:4096]
		}
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	if !json.Valid(raw) {
		return nil, errors.New("server returned invalid JSON")
	}
	return raw, nil
}

func (c *apiClient) get(ctx context.Context, path string) ([]byte, error) {
	return c.request(ctx, http.MethodGet, path, nil)
}

func (c *apiClient) createTask(ctx context.Context, task taskSpec) (string, error) {
	body := map[string]any{
		"description": task.Description, "goal": task.Goal, "timeout_seconds": task.TimeoutSeconds,
		"plan_heartbeat_seconds": task.PlanHeartbeatSeconds,
	}
	if task.LLMProfileID != nil {
		body["llm_profile_id"] = *task.LLMProfileID
	}
	if len(task.Workflow) > 0 && string(task.Workflow) != "null" {
		var workflow any
		if err := json.Unmarshal(task.Workflow, &workflow); err != nil {
			return "", err
		}
		body["workflow"] = workflow
	}
	raw, err := c.request(ctx, http.MethodPost, "/api/tasks", body)
	if err != nil {
		return "", err
	}
	var response struct {
		ID any `json:"id"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&response); err != nil {
		return "", err
	}
	switch id := response.ID.(type) {
	case string:
		if strings.TrimSpace(id) != "" {
			return id, nil
		}
	case json.Number:
		if _, err := strconv.ParseInt(id.String(), 10, 64); err == nil {
			return id.String(), nil
		}
	}
	return "", errors.New("create task response has no valid id")
}

func (c *apiClient) waitTerminal(ctx context.Context, id string, interval, maxWait time.Duration) (string, error) {
	deadline := time.NewTimer(maxWait)
	defer deadline.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		raw, err := c.get(ctx, "/api/tasks/"+url.PathEscape(id))
		if err != nil {
			return "", err
		}
		var task struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(raw, &task); err != nil {
			return "", err
		}
		if task.Status == "done" || task.Status == "failed" || task.Status == "timeout" {
			return task.Status, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", fmt.Errorf("did not reach terminal state within %s", maxWait)
		case <-ticker.C:
		}
	}
}
