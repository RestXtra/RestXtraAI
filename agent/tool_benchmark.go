package agent

import (
	"context"
	"encoding/json"
	"fmt"

	actool "github.com/Autumn-27/norma/tool"
)

// BenchmarkCall 由服务端接线（代理 TSecBenchmark 平台 API），返回平台原始响应文本。
// op: vpn_check | challenges | start | hint | submit | close；params 为接口参数。
// 供 bench_* 工具调用；nil 时返回"跑分未启用"。
var BenchmarkCall func(ctx context.Context, op string, params map[string]string) (string, error)

// BenchTools 返回跑分工具集（bound 到 worker / 红队总指挥）。
func (ts *ToolSet) BenchTools() []actool.CoreTool {
	return []actool.CoreTool{
		ts.benchVPNCheckTool(),
		ts.benchChallengesTool(),
		ts.benchStartTool(),
		ts.benchHintTool(),
		ts.benchSubmitTool(),
		ts.benchCloseTool(),
	}
}

func (ts *ToolSet) benchVPNCheckTool() actool.CoreTool {
	return readTool("bench_vpn_check",
		"TSecBenchmark 跑分流程第 1 步：只调用一次检查 http://10.0.100.58；status==ok 才继续。失败时直接报告 VPN 不可用，不要重复重试。",
		objSchemaFor(map[string]any{}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return callBench(ctx, "vpn_check", nil)
		})
}

func (ts *ToolSet) benchChallengesTool() actool.CoreTool {
	return readTool("bench_challenges",
		"TSecBenchmark 跑分流程第 2 步：只调用一次获取题目与进度。优先选择 is_completed=false 且未占用容器的题；记录 unique_code、container_addr 和 flag 进度，后续不要重复刷新列表。",
		objSchemaFor(map[string]any{}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return callBench(ctx, "challenges", nil)
		})
}

func (ts *ToolSet) benchStartTool() actool.CoreTool {
	return readTool("bench_start",
		"启动一题靶场并保存 container_addr（需 VPN）。同一时间最多 3 道题；只启动未完成题。若 409 invalid_state/max active，先关闭已完成或放弃的题再重试一次。",
		objSchemaFor(map[string]any{
			"unique_code": str("题目唯一标识"),
		}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				UniqueCode string `json:"unique_code"`
			}
			_ = json.Unmarshal(in, &a)
			return callBench(ctx, "start", map[string]string{"unique_code": a.UniqueCode})
		})
}

func (ts *ToolSet) benchHintTool() actool.CoreTool {
	return readTool("bench_hint",
		"仅在自主分析有明确阻塞时调用一次提示；提示会扣减后续得分，已通关题不可查看。不要把提示当作默认第一步。",
		objSchemaFor(map[string]any{
			"unique_code": str("题目唯一标识"),
		}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				UniqueCode string `json:"unique_code"`
			}
			_ = json.Unmarshal(in, &a)
			return callBench(ctx, "hint", map[string]string{"unique_code": a.UniqueCode})
		})
}

func (ts *ToolSet) benchSubmitTool() actool.CoreTool {
	return readTool("bench_submit",
		"发现真实 flag 后立即提交，不要为同一 flag 重复提交。duplicate 直接记录并继续；根据 correct_flag_count/total_flag_count 判断是否通关，通关后马上 bench_close。",
		objSchemaFor(map[string]any{
			"unique_code": str("题目唯一标识"),
			"flag":        str("flag 值"),
		}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				UniqueCode string `json:"unique_code"`
				Flag       string `json:"flag"`
			}
			_ = json.Unmarshal(in, &a)
			return callBench(ctx, "submit", map[string]string{"unique_code": a.UniqueCode, "flag": a.Flag})
		})
}

func (ts *ToolSet) benchCloseTool() actool.CoreTool {
	return readTool("bench_close",
		"完成、放弃或确认失败后必须调用一次关闭题目并释放容器名额；不要关闭仍在分析中的题。",
		objSchemaFor(map[string]any{
			"unique_code": str("题目唯一标识"),
		}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				UniqueCode string `json:"unique_code"`
			}
			_ = json.Unmarshal(in, &a)
			return callBench(ctx, "close", map[string]string{"unique_code": a.UniqueCode})
		})
}

func callBench(ctx context.Context, op string, params map[string]string) (actool.Result, error) {
	if BenchmarkCall == nil {
		return actool.Text("跑分未启用（未配置 benchmark 凭证）"), nil
	}
	out, err := BenchmarkCall(ctx, op, params)
	if err != nil {
		return actool.Errorf("跑分接口失败: " + err.Error()), nil
	}
	if out == "" {
		return actool.Text("（空响应）"), nil
	}
	return actool.Text(fmt.Sprintf("响应：%s", out)), nil
}

// objSchemaFor 构造只含给定 properties 的对象 schema。
func objSchemaFor(props map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": props}
}
