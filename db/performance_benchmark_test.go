package db

import "testing"

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
