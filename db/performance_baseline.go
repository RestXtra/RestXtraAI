package db

import (
	"database/sql"
	"math"
	"sort"
	"time"
)

const TaskPerformanceSchemaV1 = "restxtra.task-performance.v1"

type TaskResultYield struct {
	ConfirmedFacts         int `json:"confirmed_facts"`
	NegativeResults        int `json:"negative_results"`
	EvidenceBackedFacts    int `json:"evidence_backed_facts"`
	ConfirmedFindings      int `json:"confirmed_findings"`
	EvidenceBackedFindings int `json:"evidence_backed_findings"`
	Artifacts              int `json:"artifacts"`
}

type TaskIntentEfficiency struct {
	Total            int `json:"total"`
	Attempts         int `json:"attempts"`
	RepeatedAttempts int `json:"repeated_attempts"`
	DuplicateIntents int `json:"duplicate_intents"`
}

type TaskCoverageBaseline struct {
	Total          int     `json:"total"`
	Verified       int     `json:"verified"`
	Vulnerable     int     `json:"vulnerable"`
	VerifiedRate   float64 `json:"verified_rate"`
	VulnerableRate float64 `json:"vulnerable_rate"`
}

type TaskEfficiencyRatios struct {
	ToolErrorRate                    float64  `json:"tool_error_rate"`
	DuplicateIntentRate              float64  `json:"duplicate_intent_rate"`
	EvidenceCoverageRate             float64  `json:"evidence_coverage_rate"`
	CacheReadRate                    float64  `json:"cache_read_rate"`
	ConfirmedResultsPer1KInputTokens float64  `json:"confirmed_results_per_1k_input_tokens"`
	InputTokensPerFinding            *float64 `json:"input_tokens_per_finding"`
	ToolCallsPerFinding              *float64 `json:"tool_calls_per_finding"`
}

// TaskPerformanceBaseline is a deterministic result-efficiency snapshot. A
// terminal task has no now-relative fields, so exporting the same task twice
// produces the same measurements and can be used as a repeatable baseline.
type TaskPerformanceBaseline struct {
	SchemaVersion                      string               `json:"schema_version"`
	TaskID                             int64                `json:"task_id"`
	Status                             string               `json:"status"`
	Repeatable                         bool                 `json:"repeatable"`
	StartedAt                          time.Time            `json:"started_at"`
	CompletedAt                        *time.Time           `json:"completed_at,omitempty"`
	TimeToFirstConfirmedFactSeconds    *int64               `json:"time_to_first_confirmed_fact_seconds"`
	TimeToFirstEvidenceFactSeconds     *int64               `json:"time_to_first_evidence_fact_seconds"`
	TimeToFirstConfirmedFindingSeconds *int64               `json:"time_to_first_confirmed_finding_seconds"`
	TaskCompletionSeconds              *int64               `json:"task_completion_seconds"`
	Results                            TaskResultYield      `json:"results"`
	Intents                            TaskIntentEfficiency `json:"intents"`
	Coverage                           TaskCoverageBaseline `json:"coverage"`
	Usage                              AgentRoundCost       `json:"usage"`
	Efficiency                         TaskEfficiencyRatios `json:"efficiency"`
}

type DurationPercentiles struct {
	Samples int    `json:"samples"`
	P50     *int64 `json:"p50_seconds"`
	P95     *int64 `json:"p95_seconds"`
}

type TaskPerformanceCohort struct {
	SchemaVersion               string              `json:"schema_version"`
	TaskIDs                     []int64             `json:"task_ids"`
	RepeatableTasks             int                 `json:"repeatable_tasks"`
	ExcludedTaskIDs             []int64             `json:"excluded_task_ids"`
	TimeToFirstConfirmedFact    DurationPercentiles `json:"time_to_first_confirmed_fact"`
	TimeToFirstEvidenceFact     DurationPercentiles `json:"time_to_first_evidence_fact"`
	TimeToFirstConfirmedFinding DurationPercentiles `json:"time_to_first_confirmed_finding"`
	TaskCompletion              DurationPercentiles `json:"task_completion"`
}

type taskPerformanceNodeStats struct {
	confirmedFacts, negativeResults, evidenceFacts  int
	confirmedFindings, evidenceFindings, artifacts  int
	intents, attempts, repeatedAttempts, duplicates int
	firstFact, firstEvidenceFact, firstFinding      sql.NullTime
}

func secondsFrom(start time.Time, end sql.NullTime) *int64 {
	if !end.Valid {
		return nil
	}
	seconds := int64(end.Time.Sub(start).Seconds())
	if seconds < 0 {
		seconds = 0
	}
	return &seconds
}

func ratio(numerator, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func nullableRatio(numerator, denominator int) *float64 {
	if denominator <= 0 {
		return nil
	}
	value := float64(numerator) / float64(denominator)
	return &value
}

func durationPercentiles(values []int64) DurationPercentiles {
	out := DurationPercentiles{Samples: len(values)}
	if len(values) == 0 {
		return out
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	nearestRank := func(p float64) *int64 {
		index := int(math.Ceil(p*float64(len(sorted)))) - 1
		if index < 0 {
			index = 0
		}
		value := sorted[index]
		return &value
	}
	out.P50 = nearestRank(0.50)
	out.P95 = nearestRank(0.95)
	return out
}

// SummarizeTaskPerformanceBaselines computes nearest-rank p50/p95 values over
// terminal snapshots only. Missing facts/findings are excluded from that metric
// and represented by its Samples count instead of being treated as zero.
func SummarizeTaskPerformanceBaselines(items []*TaskPerformanceBaseline) TaskPerformanceCohort {
	out := TaskPerformanceCohort{SchemaVersion: "restxtra.task-performance-cohort.v1", TaskIDs: []int64{}, ExcludedTaskIDs: []int64{}}
	var firstFacts, evidenceFacts, firstFindings, completions []int64
	for _, item := range items {
		if item == nil {
			continue
		}
		out.TaskIDs = append(out.TaskIDs, item.TaskID)
		if !item.Repeatable {
			out.ExcludedTaskIDs = append(out.ExcludedTaskIDs, item.TaskID)
			continue
		}
		out.RepeatableTasks++
		if item.TimeToFirstConfirmedFactSeconds != nil {
			firstFacts = append(firstFacts, *item.TimeToFirstConfirmedFactSeconds)
		}
		if item.TimeToFirstEvidenceFactSeconds != nil {
			evidenceFacts = append(evidenceFacts, *item.TimeToFirstEvidenceFactSeconds)
		}
		if item.TimeToFirstConfirmedFindingSeconds != nil {
			firstFindings = append(firstFindings, *item.TimeToFirstConfirmedFindingSeconds)
		}
		if item.TaskCompletionSeconds != nil {
			completions = append(completions, *item.TaskCompletionSeconds)
		}
	}
	out.TimeToFirstConfirmedFact = durationPercentiles(firstFacts)
	out.TimeToFirstEvidenceFact = durationPercentiles(evidenceFacts)
	out.TimeToFirstConfirmedFinding = durationPercentiles(firstFindings)
	out.TaskCompletion = durationPercentiles(completions)
	return out
}

func (d *DB) taskPerformanceNodeStats(explorationID int64) (taskPerformanceNodeStats, error) {
	var stats taskPerformanceNodeStats
	err := d.QueryRow(`WITH nodes AS (
    SELECT kind, state, payload, attempt_count, created_at
    FROM exploration_nodes WHERE exploration_id=$1
), duplicate_intents AS (
    SELECT COALESCE(SUM(n-1),0)::int AS duplicates
    FROM (
        SELECT COUNT(*)::int AS n
        FROM nodes
        WHERE kind='intent' AND NULLIF(BTRIM(payload->>'summary'),'') IS NOT NULL
        GROUP BY LOWER(REGEXP_REPLACE(BTRIM(payload->>'summary'), '\s+', ' ', 'g'))
        HAVING COUNT(*) > 1
    ) grouped
)
SELECT
    COUNT(*) FILTER (WHERE kind='fact' AND state='confirmed'),
    COUNT(*) FILTER (WHERE kind='fact' AND state='confirmed' AND LOWER(COALESCE(payload->>'negative',''))='true'),
    COUNT(*) FILTER (WHERE kind='fact' AND state='confirmed' AND NULLIF(BTRIM(payload->>'evidence'),'') IS NOT NULL),
    COUNT(*) FILTER (WHERE kind='finding' AND state='confirmed'),
    COUNT(*) FILTER (WHERE kind='finding' AND state='confirmed' AND NULLIF(BTRIM(payload#>>'{evidence,poc}'),'') IS NOT NULL),
    (SELECT COUNT(*) FROM artifacts WHERE exploration_id=$1),
    COUNT(*) FILTER (WHERE kind='intent'),
    COALESCE(SUM(attempt_count) FILTER (WHERE kind='intent'),0),
    COALESCE(SUM(GREATEST(attempt_count-1,0)) FILTER (WHERE kind='intent'),0),
    (SELECT duplicates FROM duplicate_intents),
    MIN(created_at) FILTER (WHERE kind='fact' AND state='confirmed'),
    MIN(created_at) FILTER (WHERE kind='fact' AND state='confirmed' AND NULLIF(BTRIM(payload->>'evidence'),'') IS NOT NULL),
    MIN(created_at) FILTER (WHERE kind='finding' AND state='confirmed')
FROM nodes`, explorationID).Scan(
		&stats.confirmedFacts, &stats.negativeResults, &stats.evidenceFacts,
		&stats.confirmedFindings, &stats.evidenceFindings, &stats.artifacts,
		&stats.intents, &stats.attempts, &stats.repeatedAttempts, &stats.duplicates,
		&stats.firstFact, &stats.firstEvidenceFact, &stats.firstFinding,
	)
	return stats, err
}

// TaskPerformanceBaseline calculates a versioned task snapshot from persisted
// facts only. It returns nil for a missing/deleted task.
func (d *DB) TaskPerformanceBaseline(taskID int64) (*TaskPerformanceBaseline, error) {
	task, err := d.GetTask(taskID)
	if err != nil || task == nil {
		return nil, err
	}
	stats, err := d.taskPerformanceNodeStats(task.ExplorationID)
	if err != nil {
		return nil, err
	}
	costRows, err := d.Exploration(task.ExplorationID).RoundCostsByWorker()
	if err != nil {
		return nil, err
	}
	usage := AgentRoundCost{}
	for _, row := range costRows {
		usage.Rounds += row.Rounds
		usage.ToolCalls += row.ToolCalls
		usage.ToolErrors += row.ToolErrors
		usage.InputTokens += row.InputTokens
		usage.OutputTokens += row.OutputTokens
		usage.CacheReadTokens += row.CacheReadTokens
		usage.CacheWriteTokens += row.CacheWriteTokens
	}
	coverage, err := d.Assets().CoverageGraph(taskID)
	if err != nil {
		return nil, err
	}

	startedAt := task.CreatedAt
	if task.FirstRunAt != nil {
		startedAt = *task.FirstRunAt
	}
	var completionSeconds *int64
	if task.CompletedAt != nil {
		value := int64(task.CompletedAt.Sub(startedAt).Seconds())
		if value < 0 {
			value = 0
		}
		completionSeconds = &value
	}
	confirmedResults := stats.confirmedFacts + stats.confirmedFindings
	evidenceResults := stats.evidenceFacts + stats.evidenceFindings
	baseline := &TaskPerformanceBaseline{
		SchemaVersion: TaskPerformanceSchemaV1, TaskID: task.ID, Status: task.Status,
		Repeatable: IsTerminal(task.Status) && task.CompletedAt != nil,
		StartedAt:  startedAt, CompletedAt: task.CompletedAt,
		TimeToFirstConfirmedFactSeconds:    secondsFrom(startedAt, stats.firstFact),
		TimeToFirstEvidenceFactSeconds:     secondsFrom(startedAt, stats.firstEvidenceFact),
		TimeToFirstConfirmedFindingSeconds: secondsFrom(startedAt, stats.firstFinding),
		TaskCompletionSeconds:              completionSeconds,
		Results: TaskResultYield{ConfirmedFacts: stats.confirmedFacts, NegativeResults: stats.negativeResults,
			EvidenceBackedFacts: stats.evidenceFacts, ConfirmedFindings: stats.confirmedFindings,
			EvidenceBackedFindings: stats.evidenceFindings, Artifacts: stats.artifacts},
		Intents: TaskIntentEfficiency{Total: stats.intents, Attempts: stats.attempts,
			RepeatedAttempts: stats.repeatedAttempts, DuplicateIntents: stats.duplicates},
		Coverage: TaskCoverageBaseline{Total: coverage.Total, Verified: coverage.Tested, Vulnerable: coverage.Vulnerable,
			VerifiedRate: ratio(coverage.Tested, coverage.Total), VulnerableRate: ratio(coverage.Vulnerable, coverage.Total)},
		Usage: usage,
		Efficiency: TaskEfficiencyRatios{
			ToolErrorRate: ratio(usage.ToolErrors, usage.ToolCalls), DuplicateIntentRate: ratio(stats.duplicates, stats.intents),
			EvidenceCoverageRate: ratio(evidenceResults, confirmedResults), CacheReadRate: ratio(usage.CacheReadTokens, usage.InputTokens+usage.CacheReadTokens),
			ConfirmedResultsPer1KInputTokens: 1000 * ratio(confirmedResults, usage.InputTokens),
			InputTokensPerFinding:            nullableRatio(usage.InputTokens, stats.confirmedFindings),
			ToolCallsPerFinding:              nullableRatio(usage.ToolCalls, stats.confirmedFindings),
		},
	}
	return baseline, nil
}
