// Package metrics 提供进程级的轻量关键路径计数器（P5.4），经 /api/metrics 暴露。
// 原子计数，不依赖外部依赖；用于观察引擎健康与 token 相关行为（闸门拒绝/修复/卡死等）。
package metrics

import "sync/atomic"

// Metrics 是进程级计数器集合。字段名即 /api/metrics 输出的 key。
type Metrics struct {
	PlannerRounds   int64 // planner 规划轮次
	WorkerRuns      int64 // worker 执行次数
	WritesFacts     int64 // 写回：事实
	WritesAssets    int64 // 写回：资产
	WritesFindings  int64 // 写回：漏洞
	GateRejections  int64 // 证据闸门拒绝次数（P2.3 重试）
	ReflectorHints  int64 // P4.1 reflector 回注次数
	ToolFixes       int64 // P5.1 工具参数 JSON 修复次数
	ModelErrors     int64 // model_error 终态（含重试前）
	StuckRequeues   int64 // P3.4 卡死 requeue 次数
	StuckBlocked    int64 // P3.4 卡死超限转 blocked 次数
	IntentReadySkip int64 // P3.1 依赖未满足被跳过认领的意图次数
}

// M 是全局进程级计数器（服务端与 agent 包共享）。
var M = &Metrics{}

func (m *Metrics) Inc(p *int64) { atomic.AddInt64(p, 1) }
func (m *Metrics) Add(p *int64, n int64) { atomic.AddInt64(p, n) }

// Snapshot 返回当前计数快照（map[string]int64），供 /api/metrics 序列化。
func (m *Metrics) Snapshot() map[string]int64 {
	return map[string]int64{
		"planner_rounds":   atomic.LoadInt64(&m.PlannerRounds),
		"worker_runs":      atomic.LoadInt64(&m.WorkerRuns),
		"writes_facts":     atomic.LoadInt64(&m.WritesFacts),
		"writes_assets":    atomic.LoadInt64(&m.WritesAssets),
		"writes_findings":  atomic.LoadInt64(&m.WritesFindings),
		"gate_rejections":  atomic.LoadInt64(&m.GateRejections),
		"reflector_hints":  atomic.LoadInt64(&m.ReflectorHints),
		"tool_fixes":       atomic.LoadInt64(&m.ToolFixes),
		"model_errors":     atomic.LoadInt64(&m.ModelErrors),
		"stuck_requeues":   atomic.LoadInt64(&m.StuckRequeues),
		"stuck_blocked":    atomic.LoadInt64(&m.StuckBlocked),
		"intent_ready_skip": atomic.LoadInt64(&m.IntentReadySkip),
	}
}
