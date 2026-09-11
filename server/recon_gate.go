package server

import "github.com/RestXtra/RestXtraAI/agent"

// reconMandatory lists the asset dimensions an asset_intel (信息收集) sub-task
// must cover before it may be judged complete. Each entry passes if ANY of its
// asset types has a non-zero count for the task.
var reconMandatory = []struct {
	Types []string
	Label string
}{
	{[]string{"root_domain", "domain"}, "根域名"},
	{[]string{"subdomain"}, "子域名"},
	{[]string{"ip"}, "IP"},
}

// reconMissing returns the labels of mandatory recon dimensions that counts does
// not cover (empty = gate satisfied). Pure so it is unit-testable without a DB.
func reconMissing(counts map[string]int) []string {
	var missing []string
	for _, dim := range reconMandatory {
		ok := false
		for _, typ := range dim.Types {
			if counts[typ] > 0 {
				ok = true
				break
			}
		}
		if !ok {
			missing = append(missing, dim.Label)
		}
	}
	return missing
}

// wireReconGate 让 asset_intel（信息收集）子任务在目标已达成、但必查资产维度尚未齐全时
// 不能被判定完成，避免“信息收集不完整就收尾”。其它 agent（含根任务/对话）不受影响。
// 逃生通道：goal_met 传 force=true 可强制收官（用于维度确实不适用时）。
func (s *Server) wireReconGate() {
	agent.ReconGate = func(taskID int64) []string {
		if s.m == nil || s.m.pg == nil || s.m.assets == nil {
			return nil
		}
		t, err := s.m.pg.GetTask(taskID)
		if err != nil || t == nil || t.AgentKey != "asset_intel" {
			return nil
		}
		counts, err := s.m.assets.CountsByTask(taskID)
		if err != nil {
			return nil // 查询失败不阻塞收官（视为闸门放行）
		}
		return reconMissing(counts)
	}
}
