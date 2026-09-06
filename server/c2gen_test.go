package server

import (
	"os"
	"testing"
)

func TestBuildBeaconArtifacts(t *testing.T) {
	if os.Getenv("RESTXTRA_BEACON_SRC") == "" {
		os.Setenv("RESTXTRA_BEACON_SRC", "..") // repo root relative to server/ package
	}
	s := &Server{dataDir: t.TempDir()}
	if _, _, ok := s.c2BuildEnv(); !ok {
		t.Skip("go toolchain / beacon source not available")
	}
	cfg := []byte(`{"server_url":"http://127.0.0.1:9999","session_id":"gen-test","interval":5,"jitter":20,"register_uri":"/register","poll_uri":"/poll","result_uri":"/result"}`)
	for _, tt := range []struct{ os, arch, format string }{
		{"linux", "amd64", "stageless"},
		{"windows", "amd64", "stageless"},
		{"darwin", "arm64", "stageless"},
	} {
		path, size, err := s.buildBeaconArtifact(1, tt.os, tt.arch, tt.format, cfg)
		if err != nil {
			t.Fatalf("%s/%s: %v", tt.os, tt.arch, err)
		}
		if size == 0 {
			t.Fatalf("%s/%s produced empty artifact", tt.os, tt.arch)
		}
		if fi, _ := os.Stat(path); fi == nil || fi.Size() == 0 {
			t.Fatalf("no artifact file at %s", path)
		}
		t.Logf("%s/%s (%s) built: %d bytes", tt.os, tt.arch, tt.format, size)
	}
}
