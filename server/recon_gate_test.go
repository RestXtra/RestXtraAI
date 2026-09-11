package server

import "testing"

func TestReconMissing(t *testing.T) {
	cases := []struct {
		name   string
		counts map[string]int
		want   []string
	}{
		{"all covered", map[string]int{"root_domain": 1, "subdomain": 2, "ip": 3}, nil},
		{"domain alias covers root", map[string]int{"domain": 1, "subdomain": 1, "ip": 1}, nil},
		{"only root", map[string]int{"root_domain": 1}, []string{"子域名", "IP"}},
		{"empty", map[string]int{}, []string{"根域名", "子域名", "IP"}},
	}
	for _, tc := range cases {
		got := reconMissing(tc.counts)
		if len(got) != len(tc.want) {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
			}
		}
	}
}
