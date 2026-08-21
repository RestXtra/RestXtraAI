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
