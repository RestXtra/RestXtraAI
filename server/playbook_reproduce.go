package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/RestXtra/RestXtraAI/db"
)

// CVE 复现 → 攻击模式库：给定 CVE + PoC + Docker 镜像（沙箱主机），
// 自动起容器 → 在容器内运行 PoC 复现 → 验证成功 → 存入 attack_patterns。
// 复现指令（execution_steps）可被 agent 的 search_playbook 检索后按图复用。

type playbookReproduceReq struct {
	HostID           int64  `json:"host_id"` // 沙箱主机 id；0 = 第一个可用主机
	Image            string `json:"image"`   // 漏洞环境镜像，如 vulhub/xxe-1
	CVE              string `json:"cve_id"`
	Title            string `json:"title"`
	PoC              string `json:"poc"` // 在容器内执行的命令/脚本；支持 {{ip}} {{host}} {{port}}
	Port             int    `json:"port"`
	Marker           string `json:"marker"`          // 成功标志子串；空 = 仅看退出码 0
	AttackTechniqueID string `json:"attack_technique_id"`
	Tags             string `json:"tags"`
	Confidence       int    `json:"confidence"`
	KeepRunning      bool   `json:"keep_running"`
}

func (s *Server) playbookReproduce(w http.ResponseWriter, r *http.Request) {
	var req playbookReproduceReq
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(req.Image) == "" || strings.TrimSpace(req.PoC) == "" {
		writeErr(w, 400, "镜像与 PoC 必填")
		return
	}
	// 解析沙箱主机
	var host *db.SandboxHost
	if req.HostID > 0 {
		h, err := s.m.pg.GetSandboxHost(req.HostID)
		if err != nil {
			writeErr(w, 404, "host not found")
			return
		}
		host = h
	} else {
		hosts, err := s.m.pg.ListSandboxHosts()
		if err != nil || len(hosts) == 0 {
			writeErr(w, 400, "请先在「沙箱主机」添加 Docker 主机")
			return
		}
		host = hosts[0]
	}
	api, err := newDockerAPI(host.Addr)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}

	ctx := r.Context()
	// 1) 创建容器
	name := "repro-" + uuid.NewString()[:8]
	spec := map[string]any{
		"Image":  req.Image,
		"Labels": map[string]string{"sandbox.managed": "true", "sandbox.role": "repro"},
		"HostConfig": map[string]any{
			"Memory":       int64(1024) * 1024 * 1024,
			"NanoCpus":     int64(2 * 1e9),
			"PidsLimit":    int64(256),
			"NetworkMode":  "bridge",
			"ReadonlyRootfs": false,
		},
	}
	var created struct {
		ID string `json:"Id"`
	}
	if _, err := api.do(ctx, http.MethodPost, "/containers/create?name="+name, spec, &created); err != nil {
		writeErr(w, 502, "创建容器失败: "+err.Error())
		return
	}
	cleanup := func() {
		_, _ = api.do(context.Background(), http.MethodDelete, "/containers/"+created.ID+"?force=1&v=1", nil, nil)
	}
	defer func() {
		if !req.KeepRunning {
			cleanup()
		}
	}()
	if _, err := api.do(ctx, http.MethodPost, "/containers/"+created.ID+"/start", nil, nil); err != nil {
		writeErr(w, 502, "启动容器失败: "+err.Error())
		return
	}
	if err := api.waitRunning(ctx, created.ID, 90*time.Second); err != nil {
		writeErr(w, 502, err.Error())
		return
	}

	// 2) 容器内执行 PoC（本地 127.0.0.1 指向同一容器内的漏洞应用）
	port := req.Port
	if port <= 0 {
		port = 80
	}
	poc := req.PoC
	poc = strings.ReplaceAll(poc, "{{ip}}", "127.0.0.1")
	poc = strings.ReplaceAll(poc, "{{host}}", "localhost")
	poc = strings.ReplaceAll(poc, "{{port}}", fmt.Sprint(port))

	start := time.Now()
	output, code, execErr := api.exec(ctx, created.ID, poc, 120*time.Second)
	elapsed := time.Since(start).Seconds()

	// 3) 判定成功
	success := execErr == nil && code == 0
	if success && req.Marker != "" && !strings.Contains(output, req.Marker) {
		success = false
	}
	verification := "draft"
	if success {
		verification = "validated"
	}

	// 4) 存入攻击模式库
	envSig, _ := json.Marshal(map[string]any{
		"image": req.Image, "container": name, "port": port,
	})
	steps := fmt.Sprintf("复现环境：镜像 %s（容器内执行）\n```bash\n%s\n```", req.Image, req.PoC)
	evidence, _ := json.Marshal(map[string]any{
		"repro_exit": code, "repro_success": success,
		"repro_output": firstLine(output, 500), "repro_seconds": elapsed,
	})
	title := req.Title
	if title == "" {
		title = req.CVE
	}
	pat, err := s.m.pg.CreateAttackPattern(&db.AttackPattern{
		ID:                   uuid.New().String(),
		Title:                title,
		Summary:              "CVE 自动复现：" + req.CVE + "（" + verification + "）",
		AttackTechniqueID:    req.AttackTechniqueID,
		CveID:                req.CVE,
		Tags:                 req.Tags,
		Verification:         verification,
		EnvironmentSignature: string(envSig),
		ExecutionSteps:       steps,
		ValidationNotes:      fmt.Sprintf("复现验证：退出码 %d，用时 %.1fs，%s", code, elapsed, map[bool]string{true: "成功", false: "失败"}[success]),
		Source:               "reproduced",
		EvidenceRefs:         string(evidence),
		Confidence:           req.Confidence,
	})
	if err != nil {
		writeErr(w, 500, "入库失败: "+err.Error())
		return
	}

	writeJSON(w, 200, map[string]any{
		"ok": true, "success": success, "exit_code": code, "verification": verification,
		"seconds": elapsed, "output": firstLine(output, 2000), "pattern_id": pat.ID,
		"container": name, "keep_running": req.KeepRunning,
	})
}
