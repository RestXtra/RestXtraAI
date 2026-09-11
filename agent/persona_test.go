package agent

import (
	"strings"
	"testing"
)

func TestWorkerSystemIncludesPersona(t *testing.T) {
	sys := workerSystem("", "/tmp/w", "asset_intel", "你是信息收集专家：只做被动测绘。")
	if !strings.Contains(sys, "专家身份｜asset_intel") || !strings.Contains(sys, "你是信息收集专家：只做被动测绘。") {
		t.Fatalf("persona not injected into worker system:\n%s", sys)
	}
}

func TestPlannerSystemIncludesPersona(t *testing.T) {
	sys := plannerSystem("goal", "/tmp/w", "web_vuln", "你是漏洞猎人。")
	if !strings.Contains(sys, "专家身份｜web_vuln") || !strings.Contains(sys, "你是漏洞猎人。") {
		t.Fatalf("persona not injected into planner system:\n%s", sys)
	}
}

func TestPersonaEmptyIsNoop(t *testing.T) {
	if strings.Contains(plannerSystem("goal", "/tmp/w", "", ""), "专家身份") {
		t.Fatal("empty persona must not add an identity block")
	}
	if strings.Contains(workerSystem("", "/tmp/w", "asset_intel", "  "), "专家身份") {
		t.Fatal("blank persona must not add an identity block")
	}
}
