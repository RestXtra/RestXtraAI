package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/Autumn-27/norma/harness"
	"github.com/Autumn-27/norma/llm"
)

// Reflexion：失败自动分类 + L0-L4 payload 升级。
// worker 内某工具反复失败（被 WAF/403 拦截或执行出错）时，累计连败 → 达到阈值把
// 升级提示排进队列，下一次工具调用前由 reflexionHooks 拦截并交给模型，让它在
// 编码/关键字/语法上换招重试，而不是原地空转。

// 每档的绕过技术（level 0-4，逐级加码）。
var escalationLadder = [][]string{
	{"原始 payload（换一种攻击面）"},
	{"URL 编码", "关键字大小写变形", "空白字符（%20/%09/%0a）"},
	{"双重 URL 编码", "内联注释（/*!50000*/）", "HTML 实体（&#x27;）"},
	{"Unicode / hex 编码", "关键字拼接（sel'ect）", "替代函数（concat/||）"},
	{"多层嵌套编码", "盲注 / OOB（dnslog）", "彻底切换攻击面/参数/方法"},
}

const (
	reflexionFailThreshold   = 2 // 连败≥2 触发一次升级提示
	reflexionNoProgressAt    = 5 // 连败≥5 记一次反思并清零（强制进入更高档）
	reflexionMaxLevel        = 4
)

// Reflexion 是单次 worker 运行的失败升级跟踪器。
type Reflexion struct {
	mu                  sync.Mutex
	consecutiveFailures int
	reflections         int
	queued              []string
}

func NewReflexion() *Reflexion { return &Reflexion{} }

// hasBlockSignal 识别"被挡"的信号（WAF/403/限流等）。
var reBlock = regexp.MustCompile(`(?i)(403|forbidden|waf|blocked|rate\s*limit|429|captcha|denied|拒绝访问|拦截)`)

// Observe 在每次工具结果后调用：判断失败/被挡并累积，达标则入队升级提示。
func (r *Reflexion) Observe(tool string, input, result []byte, isErr bool) {
	text := strings.ToLower(string(result))
	blocked := isErr || reBlock.MatchString(text)
	r.mu.Lock()
	defer r.mu.Unlock()
	if !blocked {
		r.consecutiveFailures = 0
		return
	}
	r.consecutiveFailures++
	if r.consecutiveFailures >= reflexionFailThreshold && len(r.queued) == 0 {
		r.queued = []string{renderEscalation(r.level(), tool, string(input))}
	}
	if r.consecutiveFailures >= reflexionNoProgressAt {
		r.reflections++
		r.consecutiveFailures = 0
		if len(r.queued) == 0 {
			r.queued = []string{renderEscalation(r.level(), tool, string(input))}
		}
	}
}

func (r *Reflexion) level() int {
	lvl := r.consecutiveFailures/2 + r.reflections
	if lvl < 0 {
		return 0
	}
	if lvl > reflexionMaxLevel {
		return reflexionMaxLevel
	}
	return lvl
}

// Drain 取出一条待注入的升级提示（FIFO）；无则 ("", false)。
func (r *Reflexion) Drain() (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.queued) == 0 {
		return "", false
	}
	msg := r.queued[0]
	if len(r.queued) == 1 {
		r.queued = nil
	} else {
		r.queued = r.queued[1:]
	}
	return msg, true
}

// renderEscalation 生成一段给模型的升级提示。
func renderEscalation(level int, tool, input string) string {
	tips := escalationLadder[level]
	return fmt.Sprintf("【失败升级 L%d】工具 %s 的尝试被拦截/失败，当前输入：%s。"+
		"不要放弃该方向；请按以下技巧换一种编码/写法重试：\n- %s\n"+
		"（若已尝试超过 5 次仍无进展，考虑切换到别的参数、方法或攻击面。）",
		level, tool, firstLine(input, 120), strings.Join(tips, "\n- "))
}

// reflexionHooks 包裹外层 hooks：工具调用前先注入排队的升级提示；工具结果后更新升级状态。
type reflexionHooks struct {
	inner harness.HookRunner
	rx    *Reflexion
}

func (h reflexionHooks) PreToolUse(ctx context.Context, name string, input []byte) (bool, string, []byte) {
	if msg, ok := h.rx.Drain(); ok {
		return true, msg, nil // 拦截本次调用，把升级提示交给模型重新规划
	}
	if h.inner != nil {
		return h.inner.PreToolUse(ctx, name, input)
	}
	return false, "", nil
}

func (h reflexionHooks) PostToolUse(ctx context.Context, name string, input, result []byte, isErr bool) {
	if h.rx != nil {
		h.rx.Observe(name, input, result, isErr)
	}
	if h.inner != nil {
		h.inner.PostToolUse(ctx, name, input, result, isErr)
	}
}

func (h reflexionHooks) Stop(ctx context.Context, messages []llm.Message) (bool, []string, string) {
	if h.inner != nil {
		return h.inner.Stop(ctx, messages)
	}
	return false, nil, ""
}
