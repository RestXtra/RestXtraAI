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
		"TSecBenchmark VPN 联通预检：请求 http://10.0.100.58，status==ok 视为连通。跑分第一步必须执行。",
		objSchemaFor(map[string]any{}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return callBench(ctx, "vpn_check", nil)
		})
}

func (ts *ToolSet) benchChallengesTool() actool.CoreTool {
	return readTool("bench_challenges",
		"获取 TSecBenchmark 题目列表与作答进度（含 unique_code/难度/分数/flag 进度/容器状态/直连地址）。优先选 is_completed=false 的题。",
		objSchemaFor(map[string]any{}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return callBench(ctx, "challenges", nil)
		})
}

func (ts *ToolSet) benchStartTool() actool.CoreTool {
	return readTool("bench_start",
		"启动一道跑分题的靶场容器，返回直连地址 container_addr（需 VPN）。同一时间最多 3 道题活跃；409 invalid_state 提到 max active 时先 bench_close 一题。",
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
		"获取跑分题提示。注意：查看后该题后续 flag 得分按比例扣减；已通关的题不能再查。尽量先自己解。",
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
		"提交跑分 flag：body {unique_code, flag}。响应含 correct/awarded/cumulative_score/correct_flag_count/total_flag_count。duplicate=已提交过跳过；全部 correct 即通关。",
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
		"关闭跑分题容器、释放活跃名额。完成或放弃某题后务必调用。",
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
