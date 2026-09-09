package server

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/RestXtra/RestXtraAI/db"
)

// seedOpenSourceMCPServers registers the studied open-source IR MCP servers
// (AI-SOC-Agent/SamiGPT, Cisco IR AI) as stdio MCP configs so they can be
// enabled one-click from the MCP management UI (docs §9.8 / §16 缝①).
//
// Repo paths are machine-specific, so they come from env vars:
//
//	RESTXTRA_MCP_AI_SOC_AGENT_DIR  → python -m src.mcp.mcp_server (PYTHONPATH=repo)
//	RESTXTRA_MCP_CISCO_IR_DIR      → java -jar <repo>/target/incident-response-ai-1.0.0.jar
//
// Rows are seeded disabled (user enables when the runtimes + backends are ready)
// and skipped entirely when the env var / directory is absent. responder + auto
// get MCP visibility so the server's tools surface in their sessions once enabled.
func (s *Server) seedOpenSourceMCPServers() {
	type def struct {
		name string
		cmd  string
		env  string
	}
	defs := []def{
		{name: "ai-soc-agent (SamiGPT IR)", cmd: "python", env: "RESTXTRA_MCP_AI_SOC_AGENT_DIR"},
		{name: "cisco-ir-ai", cmd: "java", env: "RESTXTRA_MCP_CISCO_IR_DIR"},
	}
	existing, err := s.m.pg.ListMCP()
	if err != nil {
		return
	}
	for _, d := range defs {
		repo := os.Getenv(d.env)
		if repo == "" {
			continue
		}
		abs, err := filepath.Abs(repo)
		if err != nil {
			continue
		}
		if st, err := os.Stat(abs); err != nil || !st.IsDir() {
			continue
		}
		nameTaken := false
		for _, m := range existing {
			if m.Name == d.name {
				nameTaken = true
				break
			}
		}
		if nameTaken {
			continue
		}
		var args []string
		envMap := map[string]string{}
		switch d.cmd {
		case "python":
			args = []string{"-m", "src.mcp.mcp_server"}
			envMap["PYTHONPATH"] = abs // 让 python -m 在仓库根解析模块
		case "java":
			args = []string{"-jar", filepath.Join(abs, "target", "incident-response-ai-1.0.0.jar"), "--spring.ai.mcp.server.stdio=true"}
		}
		argsJSON, _ := json.Marshal(args)
		envJSON, _ := json.Marshal(envMap)
		id, err := s.m.pg.SaveMCP(&db.MCPServer{
			Name: d.name, Transport: "stdio", Command: d.cmd,
			Args: json.RawMessage(argsJSON), Env: json.RawMessage(envJSON), Enabled: false,
		})
		if err != nil {
			continue
		}
		// 授 MCP 可见性给 responder + auto(启用后工具即出现在它们会话)。
		for _, ak := range []string{"responder", "auto"} {
			asm, _ := s.m.pg.AgentAssemblyByKey(ak)
			if asm == nil || asm.Agent == nil {
				continue
			}
			ids := asm.MCPIDs
			has := false
			for _, v := range ids {
				if v == id {
					has = true
					break
				}
			}
			if !has {
				ids = append(ids, id)
				_ = s.m.pg.SetAgentVisibilityKind(asm.Agent.ID, "mcp", ids)
			}
		}
	}
}
