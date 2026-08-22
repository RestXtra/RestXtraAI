package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RestXtra/RestXtraAI/db"
)

const (
	workingSetMaxItems   = 8
	workingSetMaxTextRun = 320
)

// renderWorkingSetRecovery renders only the bounded structured projection. The
// full model summary and old transcript stay external so a restart cannot cause
// an unbounded prompt or make stale prose override the current graph.
func renderWorkingSetRecovery(ws *db.WorkingSet) string {
	if ws == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n\n【历史 Working Set 恢复快照】version=%d schema=%d hash=%s source_event_id=%d\n", ws.Version, ws.SchemaVersion, bounded(ws.ContentHash), ws.SourceEventID)
	b.WriteString("该快照只用于恢复任务约束和未完成线索；若与本轮实时资产/探索图冲突，以实时图为准。不要据此重复已完成工作。\n")
	writeRecoveryLines(&b, "目标", []string{ws.Fixed.Objective})
	writeRecoveryLines(&b, "授权范围", ws.Fixed.AuthorizationScope)
	writeRecoveryLines(&b, "约束", ws.Fixed.Constraints)
	writeRecoveryLines(&b, "风险策略", ws.Fixed.RiskPolicy)
	writeRecoveryNodes(&b, "已确认事实", ws.Fixed.ConfirmedFacts)
	writeRecoveryNodes(&b, "最近意图", ws.Sliding.RecentIntents)
	writeRecoveryNodes(&b, "待处理依赖", ws.Sliding.PendingDependencies)
	writeRecoveryNodes(&b, "待补证据", ws.Sliding.PendingEvidence)
	if len(ws.Sliding.RecentToolErrors) > 0 {
		b.WriteString("最近工具错误：\n")
		for _, item := range ws.Sliding.RecentToolErrors[:min(len(ws.Sliding.RecentToolErrors), workingSetMaxItems)] {
			fmt.Fprintf(&b, "- event=%d tool=%s %s\n", item.ID, bounded(item.Tool), bounded(item.Summary))
		}
	}
	if len(ws.Sliding.ArtifactRefs) > 0 {
		b.WriteString("外置证据：\n")
		for _, item := range ws.Sliding.ArtifactRefs[:min(len(ws.Sliding.ArtifactRefs), workingSetMaxItems)] {
			fmt.Fprintf(&b, "- artifact=%d hash=%s mime=%s bytes=%d %s\n", item.ID, bounded(item.ContentHash), bounded(item.MIMEType), item.ByteSize, bounded(item.Summary))
		}
	}
	fmt.Fprintf(&b, "按需检索完整原文：events=%s artifacts=%s cursor=%d\n", bounded(ws.External.EventsAPI), bounded(ws.External.ArtifactsAPI), ws.External.EventCursor)
	return b.String()
}

func writeRecoveryLines(b *strings.Builder, label string, values []string) {
	values = values[:min(len(values), workingSetMaxItems)]
	for _, value := range values {
		if value = bounded(value); value != "" {
			fmt.Fprintf(b, "%s：%s\n", label, value)
		}
	}
}

func writeRecoveryNodes(b *strings.Builder, label string, nodes []db.WorkingSetNodeRef) {
	nodes = nodes[:min(len(nodes), workingSetMaxItems)]
	for _, node := range nodes {
		fmt.Fprintf(b, "%s：node=%d state=%s assets=%v %s", label, node.ID, bounded(node.State), node.AssetIDs, bounded(node.Summary))
		if node.Evidence != "" {
			fmt.Fprintf(b, " evidence=%s", bounded(node.Evidence))
		}
		b.WriteByte('\n')
	}
}

func bounded(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\x00", ""))
	if len([]rune(value)) <= workingSetMaxTextRun {
		return value
	}
	return string([]rune(value)[:workingSetMaxTextRun]) + "..."
}

func restoreWorkingSet(ts *db.ExplorationStore, agentName string, intentID *int64) string {
	ws, err := ts.WorkingSet()
	if err != nil || ws == nil {
		return ""
	}
	payload, _ := json.Marshal(map[string]any{
		"working_set_id":  ws.ID,
		"version":         ws.Version,
		"content_hash":    ws.ContentHash,
		"source_event_id": ws.SourceEventID,
		"agent":           agentName,
		"intent_id":       intentID,
	})
	_, _ = ts.AppendActivity(db.Activity{
		NodeID: intentID, Worker: agentName, EventType: db.EventWorkingSetRestored,
		EventOnly: true, Payload: payload,
	})
	return renderWorkingSetRecovery(ws)
}
