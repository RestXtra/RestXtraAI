package agent

import (
	"strings"
	"testing"
)

func TestComposeFindingReport(t *testing.T) {
	got := composeFindingReport("标题A", "摘要", "https://t.example", "high",
		"可读全站用户订单", []string{"GET https://t.example/api/order/1", "POST https://t.example/api/order"},
		"1. 登录\n2. 换 id", "加鉴权", "curl ...")
	for _, want := range []string{
		"漏洞标题：标题A", "目标网站URL：https://t.example", "漏洞等级：high",
		"漏洞描述：", "漏洞危害：", "可读全站用户订单", "涉及接口清单：",
		"1. GET https://t.example/api/order/1", "2. POST https://t.example/api/order",
		"复现步骤：", "修复建议：", "加鉴权", "证据/PoC：",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("report missing %q:\n%s", want, got)
		}
	}
}

func TestComposeFindingReportTitleFallsBack(t *testing.T) {
	got := composeFindingReport("", "只有摘要", "", "medium", "", nil, "", "", "")
	if !strings.Contains(got, "漏洞标题：只有摘要") {
		t.Fatalf("title fallback failed:\n%s", got)
	}
	if strings.Contains(got, "涉及接口清单") {
		t.Fatalf("empty endpoints should be omitted:\n%s", got)
	}
}
