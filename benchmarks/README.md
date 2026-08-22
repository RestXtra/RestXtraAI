# Performance baselines

Run the same benchmark suite before and after an optimization:

```powershell
./benchmarks/run.ps1 -OutFile benchmarks/results/before.txt
./benchmarks/run.ps1 -OutFile benchmarks/results/after.txt
benchstat benchmarks/results/before.txt benchmarks/results/after.txt
```

The suite covers API permission routing and JWT verification, asset DSL parsing
and SQL generation, per-turn agent prompt/default preparation, and the
TSecBenchmark HTTP adapter against an in-process test server. It does not contact
the real TSecBenchmark service and therefore does not represent an online score.

Use the same machine, power mode, Go version, `Count`, and `Benchtime` for both
runs. Commit benchmark code and methodology, but not local result files.

## Result-efficiency baseline

The microbenchmarks above measure code paths, not pentest outcomes. After a task
reaches `done`, `failed`, or `timeout`, export:

```text
GET /api/tasks/{task_id}/performance-baseline
```

The versioned `restxtra.task-performance.v1` payload records time to first fact,
first evidence-backed fact, first confirmed finding and completion; token/tool
costs; duplicate/retried intents; evidence coverage; cache-read rate; and asset
verification coverage. It also reports terminal intents that produced no fact or
finding (`zero_yield_intents`), making no-result exploration directly comparable.
Rejected exact duplicates and repeated zero-yield scopes are reported separately
as `duplicate_intent_rejections` and `zero_yield_scope_rejections`, so prevented
planner work is measurable rather than inferred from the surviving graph.
Keep the task inputs, target dataset version, model/profile,
skills, tool catalog, concurrency and budget identical when comparing two runs.

Only snapshots with `repeatable: true` are stable baselines. Running tasks are
observable but must not be used for before/after claims. A missing finding keeps
per-finding costs as `null`, rather than reporting a misleading zero.

For repeated runs of the same fixed dataset, request an explicit cohort:

```text
GET /api/tasks/performance-baselines?task_ids=101,102,103,104,105
```

The response includes each deterministic snapshot and nearest-rank p50/p95 timing.
Each timing metric reports its own sample count; runs with no fact or finding are
not silently converted to zero. Cohorts are explicit rather than “latest N” so
later tasks cannot change an already saved comparison. Save the JSON response as
the baseline record; later manual changes to a task's persisted graph will be
reflected by a new export.

## Automated task replay

The replay runner creates repeated tasks from one immutable scenario, waits for
terminal state, and exports every baseline plus the cohort in one checkpointed
JSON file. It requires an authenticated token but never writes that token to the
result:

```powershell
Copy-Item benchmarks/scenarios/local-example.json benchmarks/scenarios/baseline.local.json
# Edit baseline.local.json with the authorized target and immutable dataset metadata.
$env:RESTXTRA_BENCH_TOKEN = "<bearer token>"
go run ./benchmarks/replay `
  -scenario benchmarks/scenarios/baseline.local.json `
  -out benchmarks/results/before.json `
  -base-url http://localhost:8787
Remove-Item Env:RESTXTRA_BENCH_TOKEN
```

Copy the example scenario to a `*.local.json` file (ignored by Git) and replace every descriptive
value before running it. `authorization_reference`, immutable dataset `version`,
and `reset_reference` are mandatory. Each task must have a positive timeout, and
one invocation is capped at 100 task runs. The runner does not reset or delete a
target; reset it through the procedure named by `reset_reference` before every
repeat when the dataset is stateful.

The result records the exact scenario SHA-256, Git revision/dirty flag and tracked
diff SHA-256, user
environment metadata, and hashes of the live settings, LLM profiles, agents,
tools, and skills API responses. It writes a checkpoint after environment capture,
task creation, and each completed baseline, so interrupted runs retain created
task IDs. A valid comparison requires `complete: true`, identical scenario and
environment hashes, and a freshly reset authorized dataset.
