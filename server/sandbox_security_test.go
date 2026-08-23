package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateSandboxCreateRejectsInfrastructureAccess(t *testing.T) {
	for _, network := range []string{"host", "restxtra_default", "container:postgres"} {
		req := sandboxCreateReq{NetworkMode: network}
		if err := validateSandboxCreate(&req); err == nil {
			t.Fatalf("network mode %q was allowed", network)
		}
	}
	for _, env := range []string{
		"RESTXTRA_PG_DSN=postgres://secret",
		"POSTGRES_PASSWORD=secret",
		"PGPASSFILE=/run/secrets/pgpass",
		"DATABASE_URL=postgres://secret",
		"DOCKER_HOST=unix:///var/run/docker.sock",
	} {
		req := sandboxCreateReq{NetworkMode: "bridge", Env: []string{env}}
		if err := validateSandboxCreate(&req); err == nil {
			t.Fatalf("environment %q was allowed", env)
		}
	}
	req := sandboxCreateReq{Env: []string{"TARGET_URL=https://example.test"}}
	if err := validateSandboxCreate(&req); err != nil {
		t.Fatalf("safe request rejected: %v", err)
	}
	if req.NetworkMode != "bridge" {
		t.Fatalf("default network = %q, want bridge", req.NetworkMode)
	}
}

func TestSandboxContainerSpecForcesSecurityBoundary(t *testing.T) {
	req := sandboxCreateReq{
		Image: "scanner:latest", NetworkMode: "bridge",
		ReadOnly: false, CapDropAll: false, Managed: false,
	}
	spec := sandboxContainerSpec(req, 1024, 1, 32)
	labels := spec["Labels"].(map[string]string)
	if labels[sandboxManagedLabel] != "true" {
		t.Fatal("managed label was not forced")
	}
	host := spec["HostConfig"].(map[string]any)
	if host["ReadonlyRootfs"] != true {
		t.Fatal("read-only root was not forced")
	}
	if got := host["NetworkMode"]; got != "bridge" {
		t.Fatalf("network mode = %v", got)
	}
	encoded, _ := json.Marshal(host)
	for _, required := range []string{`"CapDrop":["ALL"]`, `"SecurityOpt":["no-new-privileges:true"]`} {
		if !strings.Contains(string(encoded), required) {
			t.Fatalf("host security setting missing: %s in %s", required, encoded)
		}
	}
}

func TestRequireManagedContainerRejectsPostgres(t *testing.T) {
	var requested []string
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/containers/sandbox-1/json":
			_, _ = w.Write([]byte(`{"Config":{"Labels":{"sandbox.managed":"true"}}}`))
		case "/containers/postgres/json":
			_, _ = w.Write([]byte(`{"Config":{"Labels":{"com.docker.compose.service":"postgres"}}}`))
		case "/containers/protected/json":
			_, _ = w.Write([]byte(`{"Config":{"Labels":{"sandbox.managed":"true","restxtra.protected":"true"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer docker.Close()
	api := &dockerAPI{client: docker.Client(), base: docker.URL}

	if path, err := requireManagedContainer(context.Background(), api, "sandbox-1"); err != nil || path != "/containers/sandbox-1" {
		t.Fatalf("managed sandbox rejected: path=%q err=%v", path, err)
	}
	if _, err := requireManagedContainer(context.Background(), api, "postgres"); err == nil || !strings.Contains(err.Error(), "非沙箱") {
		t.Fatalf("postgres container was not rejected: %v", err)
	}
	if _, err := requireManagedContainer(context.Background(), api, "protected"); err == nil || !strings.Contains(err.Error(), "受保护") {
		t.Fatalf("protected infrastructure was not rejected: %v", err)
	}
	if len(requested) != 3 {
		t.Fatalf("inspect requests = %v", requested)
	}
}

func TestManagedContainerPathRejectsPathInjection(t *testing.T) {
	for _, id := range []string{"", "../postgres", "postgres/json", "x?force=1"} {
		if _, err := managedContainerPath(id); err == nil {
			t.Fatalf("container id %q was allowed", id)
		}
	}
}
