package db

import "testing"

func BenchmarkTaskPerformanceCohort(b *testing.B) {
	value := func(n int64) *int64 { return &n }
	items := make([]*TaskPerformanceBaseline, 100)
	for i := range items {
		items[i] = &TaskPerformanceBaseline{
			TaskID: int64(i + 1), Repeatable: true,
			TimeToFirstConfirmedFactSeconds:    value(int64(10 + i)),
			TimeToFirstEvidenceFactSeconds:     value(int64(15 + i)),
			TimeToFirstConfirmedFindingSeconds: value(int64(30 + i)),
			TaskCompletionSeconds:              value(int64(100 + i)),
		}
	}
	b.ReportAllocs()
	for range b.N {
		result := SummarizeTaskPerformanceBaselines(items)
		if result.RepeatableTasks != len(items) || result.TaskCompletion.P95 == nil {
			b.Fatal("invalid cohort summary")
		}
	}
}

func BenchmarkAssetDSLParseAndBuild(b *testing.B) {
	query := `(domain==example.com OR root_domain==example.org) AND technology=nginx AND status_code>=200 AND status_code<500`
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		node, err := ParseDSL(query)
		if err != nil {
			b.Fatal(err)
		}
		where, args, err := buildDSLWhere(node)
		if err != nil || where == "" || len(args) != 5 {
			b.Fatalf("where=%q args=%d err=%v", where, len(args), err)
		}
	}
}

func BenchmarkAgentAssemblyQueries(b *testing.B) {
	d, err := Open(testDSN(b))
	if err != nil {
		b.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer d.Close()

	b.Run("combined_snapshot", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			snapshot, err := d.AgentAssemblyByKey("planner")
			if err != nil || snapshot == nil || snapshot.Agent == nil {
				b.Fatalf("snapshot=%+v err=%v", snapshot, err)
			}
		}
	})

	b.Run("legacy_three_queries", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			a, err := d.GetAgentByKey("planner")
			if err != nil || a == nil {
				b.Fatalf("agent=%+v err=%v", a, err)
			}
			if _, err := d.AgentSkillNames(a.ID); err != nil {
				b.Fatal(err)
			}
			if _, err := d.AgentVisible(a.ID, "mcp"); err != nil {
				b.Fatal(err)
			}
		}
	})
}
