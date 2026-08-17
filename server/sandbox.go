package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/RestXtra/RestXtraAI/db"
)

// 沙箱管理（Docker 主机 + 受管沙箱容器 + 出口范围）：
//   - 沙箱主机：注册的 Docker daemon（tcp/http(s)/unix socket），容器跑在其上。
//   - 沙箱容器：受管沙箱容器生命周期（创建/启停/删除），默认只读根 + cap-drop ALL +
//     资源限额（memory/cpu/pids），带 sandbox.managed 标签防误删。
//   - 出口范围：授权 scope 规则（CIDR/域名 allow/deny），创建容器时按此登记。
//
// 通过 Docker Engine REST API 直连（零第三方依赖），API 版本兼容 1.41+。

const sandboxManagedLabel = "sandbox.managed"

// ---------- Docker HTTP 客户端 ----------

type dockerAPI struct {
	client *http.Client
	base   string
	sock   bool // unix socket
}

// dockerBase normalizes a docker daemon address into an HTTP base URL + dial spec.
// dialSpec: "" = TCP， "unix:<path>" = unix socket，"npipe:<path>" = Windows 命名管道。
func dockerBase(addr string) (base, dialSpec string, err error) {
	addr = strings.TrimSpace(addr)
	switch {
	case strings.HasPrefix(addr, "unix://"):
		return "http://docker", "unix:" + strings.TrimPrefix(addr, "unix://"), nil
	case strings.HasPrefix(addr, "npipe://"):
		p := strings.TrimPrefix(addr, "npipe://")
		if !strings.HasPrefix(p, `\\`) && !strings.HasPrefix(p, "//") {
			p = `\\` + p
		}
		return "http://docker", "npipe:" + p, nil
	case strings.HasPrefix(addr, "tcp://"):
		host := strings.TrimPrefix(addr, "tcp://")
		return "http://" + host, "", nil
	case strings.HasPrefix(addr, "http://"), strings.HasPrefix(addr, "https://"):
		return strings.TrimRight(addr, "/"), "", nil
	case strings.Contains(addr, ":"):
		return "http://" + addr, "", nil
	default:
		return "", "", fmt.Errorf("无法识别的 Docker 地址 %q（支持 tcp://host:2375 / http(s):// / unix:///var/run/docker.sock / npipe:////./pipe/docker_engine）", addr)
	}
}

func newDockerAPI(addr string) (*dockerAPI, error) {
	base, dialSpec, err := dockerBase(addr)
	if err != nil {
		return nil, err
	}
	d := &dockerAPI{base: base, sock: dialSpec != ""}
	if dialSpec != "" {
		d.client = &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return dialDocker(ctx, dialSpec)
				},
			},
			Timeout: 15 * time.Second,
		}
	} else {
		d.client = &http.Client{Timeout: 15 * time.Second}
	}
	return d, nil
}

func (d *dockerAPI) do(ctx context.Context, method, path string, body any, out any) (int, error) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, d.base+path, rdr)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if d.sock {
		req.Header.Set("Host", "docker")
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return resp.StatusCode, fmt.Errorf("docker API %s %s -> %s: %s", method, path, resp.Status, strings.TrimSpace(string(b)))
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("解析 docker 响应失败: %w", err)
		}
	}
	return resp.StatusCode, nil
}

// doRaw 与 do 相同，但返回原始响应体与响应头（用于 exec start 等二进制流）。
func (d *dockerAPI) doRaw(ctx context.Context, method, path string, body any, extra http.Header) (int, []byte, http.Header, error) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, d.base+path, rdr)
	if err != nil {
		return 0, nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if d.sock {
		req.Header.Set("Host", "docker")
	}
	for k, vs := range extra {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return resp.StatusCode, b, resp.Header, fmt.Errorf("docker API %s %s -> %s: %s", method, path, resp.Status, strings.TrimSpace(string(b)))
	}
	return resp.StatusCode, b, resp.Header, nil
}

// dockerInspectInfo 是容器 inspect 的精简结构。
type dockerInspectInfo struct {
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

func (d *dockerAPI) inspect(ctx context.Context, id string) (*dockerInspectInfo, error) {
	var out dockerInspectInfo
	if _, err := d.do(ctx, http.MethodGet, "/containers/"+id+"/json", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// waitRunning 轮询等待容器进入 running 态。
func (d *dockerAPI) waitRunning(ctx context.Context, id string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		info, err := d.inspect(ctx, id)
		if err != nil {
			return err
		}
		if info.State.Running {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("容器 %s 未在 %s 内进入 running", id, timeout)
}

// exec 在容器内执行命令（/bin/sh -c），返回合并输出与退出码。
func (d *dockerAPI) exec(ctx context.Context, id, cmd string, timeout time.Duration) (string, int, error) {
	var created struct {
		Id string `json:"Id"`
	}
	if _, err := d.do(ctx, http.MethodPost, "/containers/"+id+"/exec", map[string]any{
		"AttachStdout": true, "AttachStderr": true, "Tty": true,
		"Cmd": []string{"/bin/sh", "-c", cmd},
	}, &created); err != nil {
		return "", 0, err
	}
	if created.Id == "" {
		return "", 0, fmt.Errorf("exec 创建失败")
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, body, hdr, err := d.doRaw(cctx, http.MethodPost, "/exec/"+created.Id+"/start", map[string]any{"Detach": false, "Tty": true}, nil)
	if err != nil {
		return "", 0, err
	}
	code := 0
	if v := hdr.Get("X-Docker-Exit-Code"); v != "" {
		code, _ = strconv.Atoi(v)
	}
	return string(body), code, nil
}

// ---------- Docker 数据模型 ----------

type dockerVersion struct {
	Version    string `json:"Version"`
	APIVersion string `json:"ApiVersion"`
	Os         string `json:"Os"`
	Arch       string `json:"Arch"`
}

type dockerContainer struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	Command string            `json:"Command"`
	Created int64             `json:"Created"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Labels  map[string]string `json:"Labels"`
	Ports   []struct {
		IP          string `json:"IP"`
		PrivatePort uint16 `json:"PrivatePort"`
		PublicPort  uint16 `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
}

type dockerImage struct {
	ID       string            `json:"Id"`
	RepoTags []string          `json:"RepoTags"`
	Size     int64             `json:"Size"`
	Labels   map[string]string `json:"Labels"`
}

// ---------- 主机 ----------

func (s *Server) sandboxListHosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.m.pg.ListSandboxHosts()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"hosts": hosts})
}

func (s *Server) sandboxUpsertHost(w http.ResponseWriter, r *http.Request) {
	var h db.SandboxHost
	if err := decode(r, &h); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(h.Name) == "" || strings.TrimSpace(h.Addr) == "" {
		writeErr(w, 400, "名称与 Docker 地址必填")
		return
	}
	if _, _, err := dockerBase(h.Addr); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	id, err := s.m.pg.UpsertSandboxHost(&h)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) sandboxDeleteHost(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	if err := s.m.pg.DeleteSandboxHost(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

// sandboxPingHost probes the docker daemon and returns version info.
func (s *Server) sandboxPingHost(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	h, err := s.m.pg.GetSandboxHost(id)
	if err != nil {
		writeErr(w, 404, "host not found")
		return
	}
	api, err := newDockerAPI(h.Addr)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	var v dockerVersion
	if _, err := api.do(ctx, http.MethodGet, "/version", nil, &v); err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "version": v.Version, "api_version": v.APIVersion, "os": v.Os, "arch": v.Arch,
	})
}

// ---------- 容器 ----------

func (s *Server) sandboxHostOr404(w http.ResponseWriter, r *http.Request) (*db.SandboxHost, *dockerAPI) {
	id, _ := pathInt(r, "id")
	h, err := s.m.pg.GetSandboxHost(id)
	if err != nil {
		writeErr(w, 404, "host not found")
		return nil, nil
	}
	api, err := newDockerAPI(h.Addr)
	if err != nil {
		writeErr(w, 400, err.Error())
		return nil, nil
	}
	return h, api
}

func (s *Server) sandboxListContainers(w http.ResponseWriter, r *http.Request) {
	_, api := s.sandboxHostOr404(w, r)
	if api == nil {
		return
	}
	all := r.URL.Query().Get("all") == "1"
	managed := r.URL.Query().Get("managed") == "1"
	params := url.Values{}
	if all {
		params.Set("all", "1")
	}
	if managed {
		f, _ := json.Marshal(map[string][]string{"label": {sandboxManagedLabel + "=true"}})
		params.Set("filters", url.QueryEscape(string(f)))
	}
	path := "/containers/json"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	var out []dockerContainer
	if _, err := api.do(r.Context(), http.MethodGet, path, nil, &out); err != nil {
		writeErr(w, 502, "Docker: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"containers": out})
}

func (s *Server) sandboxListImages(w http.ResponseWriter, r *http.Request) {
	_, api := s.sandboxHostOr404(w, r)
	if api == nil {
		return
	}
	var out []dockerImage
	if _, err := api.do(r.Context(), http.MethodGet, "/images/json", nil, &out); err != nil {
		writeErr(w, 502, "Docker: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"images": out})
}

type sandboxCreateReq struct {
	Name        string   `json:"name"`
	Image       string   `json:"image"`
	MemoryMB    int      `json:"memory_mb"`    // 0 = 默认 4096
	CPUs        float64  `json:"cpus"`         // 0 = 默认 2
	PidsLimit   int64    `json:"pids_limit"`   // 0 = 默认 512
	ReadOnly    bool     `json:"read_only"`    // 默认 true（只读根）
	CapDropAll  bool     `json:"cap_drop_all"` // 默认 true
	NetworkMode string   `json:"network_mode"` // bridge|host|none|internal(专用)
	AutoStart   bool     `json:"auto_start"`
	Env         []string `json:"env"`
	Managed     bool     `json:"managed"` // 打 sandbox.managed 标签（防误删）
}

func (s *Server) sandboxCreateContainer(w http.ResponseWriter, r *http.Request) {
	_, api := s.sandboxHostOr404(w, r)
	if api == nil {
		return
	}
	var req sandboxCreateReq
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(req.Image) == "" {
		writeErr(w, 400, "镜像必填")
		return
	}
	memory := int64(4096) * 1024 * 1024
	if req.MemoryMB > 0 {
		memory = int64(req.MemoryMB) * 1024 * 1024
	}
	cpus := 2.0
	if req.CPUs > 0 {
		cpus = req.CPUs
	}
	pids := int64(512)
	if req.PidsLimit > 0 {
		pids = req.PidsLimit
	}
	network := req.NetworkMode
	if network == "" {
		network = "bridge"
	}
	labels := map[string]string{}
	if req.Managed {
		labels[sandboxManagedLabel] = "true"
	}
	capDrop := []string(nil)
	if req.CapDropAll {
		capDrop = []string{"ALL"}
	}
	spec := map[string]any{
		"Image":  req.Image,
		"Labels": labels,
		"Env":    req.Env,
		"HostConfig": map[string]any{
			"Memory":         memory,
			"NanoCpus":       int64(cpus * 1e9),
			"PidsLimit":      pids,
			"ReadonlyRootfs": req.ReadOnly,
			"CapDrop":        capDrop,
			"NetworkMode":    network,
		},
	}
	path := "/containers/create"
	if strings.TrimSpace(req.Name) != "" {
		path += "?name=" + url.QueryEscape(req.Name)
	}
	var created struct {
		ID string `json:"Id"`
	}
	if _, err := api.do(r.Context(), http.MethodPost, path, spec, &created); err != nil {
		writeErr(w, 502, "Docker: "+err.Error())
		return
	}
	if req.AutoStart && created.ID != "" {
		if _, err := api.do(r.Context(), http.MethodPost, "/containers/"+created.ID+"/start", nil, nil); err != nil {
			log.Printf("[sandbox] start %s failed: %v", created.ID, err)
		}
	}
	writeJSON(w, 200, map[string]any{"id": created.ID})
}

// sandboxContainerAction performs start|stop|restart|kill|remove on a container.
func (s *Server) sandboxContainerAction(w http.ResponseWriter, r *http.Request) {
	_, api := s.sandboxHostOr404(w, r)
	if api == nil {
		return
	}
	cid := r.PathValue("cid")
	action := r.PathValue("action")
	var path string
	switch action {
	case "start", "stop", "restart", "kill":
		path = "/containers/" + cid + "/" + action
	case "remove":
		path = "/containers/" + cid + "?force=1&v=1"
	default:
		writeErr(w, 400, "action 必须为 start|stop|restart|kill|remove")
		return
	}
	method := http.MethodPost
	if action == "remove" {
		method = http.MethodDelete
	}
	if _, err := api.do(r.Context(), method, path, nil, nil); err != nil {
		writeErr(w, 502, "Docker: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "action": action, "container": cid})
}

// sandboxRemoveContainersBatch removes multiple (or all) containers on a host.
func (s *Server) sandboxRemoveContainersBatch(w http.ResponseWriter, r *http.Request) {
	_, api := s.sandboxHostOr404(w, r)
	if api == nil {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
		All bool     `json:"all"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	ids := req.IDs
	if req.All {
		var all []dockerContainer
		if _, err := api.do(r.Context(), http.MethodGet, "/containers/json?all=1", nil, &all); err != nil {
			writeErr(w, 502, "Docker: "+err.Error())
			return
		}
		ids = make([]string, 0, len(all))
		for _, c := range all {
			ids = append(ids, c.ID)
		}
	}
	var deleted []string
	var failed []string
	for _, cid := range ids {
		if _, err := api.do(r.Context(), http.MethodDelete, "/containers/"+cid+"?force=1&v=1", nil, nil); err != nil {
			failed = append(failed, cid)
			continue
		}
		deleted = append(deleted, cid)
	}
	writeJSON(w, 200, map[string]any{"deleted": deleted, "failed": failed})
}

// ---------- 出口范围 ----------

func (s *Server) sandboxListEgress(w http.ResponseWriter, r *http.Request) {
	rules, err := s.m.pg.ListSandboxEgress()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"rules": rules})
}

func (s *Server) sandboxUpsertEgress(w http.ResponseWriter, r *http.Request) {
	var e db.SandboxEgress
	if err := decode(r, &e); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if e.Kind != "cidr" && e.Kind != "domain" {
		writeErr(w, 400, "kind 必须为 cidr|domain")
		return
	}
	if strings.TrimSpace(e.Value) == "" {
		writeErr(w, 400, "value 必填")
		return
	}
	if e.Action != "allow" && e.Action != "deny" {
		e.Action = "allow"
	}
	id, err := s.m.pg.UpsertSandboxEgress(&e)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) sandboxDeleteEgress(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	if err := s.m.pg.DeleteSandboxEgress(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

// sandboxDeleteEgressBatch removes multiple (or all) egress rules.
func (s *Server) sandboxDeleteEgressBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	var n int64
	var err error
	if req.All {
		n, err = s.m.pg.ClearSandboxEgresses()
	} else {
		n, err = s.m.pg.DeleteSandboxEgresses(req.IDs)
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": n})
}
