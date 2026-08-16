package agent

import (
	"context"
	"iter"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/norma/llm"
)

// keyPoolProvider 多 Key 池 + 故障切换（P5.2，借鉴 VulnClaw multi-key failover）：
// 持有多个底层 provider（每个 API key 一个）。当当前 key 遇到鉴权/限流错误
// （401/403/429/rate limit 等）时，切到下一个 key 重试整个流；其余错误原样传播。
// 一个 key 稳定失败多次后可被临时熔断（30s），避免在坏 key 上反复空转。
type keyPoolProvider struct {
	provs []llm.Provider
	mu    sync.Mutex
	cur   int
	// 熔断：key 连续失败计数与熔断截止时间。
	fails    map[int]int
	cooldown map[int]time.Time
}

// newKeyPool 用多个 provider 构造池。len(provs)<=1 时应直接返回单 provider，不走池。
func newKeyPool(provs []llm.Provider) *keyPoolProvider {
	return &keyPoolProvider{provs: provs, fails: map[int]int{}, cooldown: map[int]time.Time{}}
}

func (k *keyPoolProvider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		n := len(k.provs)
		if n == 0 {
			return
		}
		for attempt := 0; attempt < n; attempt++ {
			idx := k.nextActive()
			if idx < 0 {
				return // 全部 key 都在熔断
			}
			p := k.provs[idx]
			switchKey := false
			for ev, err := range p.Stream(ctx, req) {
				if err != nil {
					if n > 1 && keyPoolSwitchable(err) {
						k.recordFail(idx)
						switchKey = true
						break
					}
					k.recordSuccess(idx)
					if !yield(ev, nil) {
						return
					}
					return
				}
				if !yield(ev, nil) {
					return
				}
			}
			if !switchKey {
				k.recordSuccess(idx)
				return // 正常结束
			}
			// 切下一个 key 前稍等，给网关恢复时间。
			if sleepCtxLocal(ctx, 300*time.Millisecond) {
				return
			}
		}
	}
}

// nextActive 返回下一个未熔断的 key 下标；没有可用 key 返回 -1。
func (k *keyPoolProvider) nextActive() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := time.Now()
	for i := 0; i < len(k.provs); i++ {
		idx := (k.cur + i) % len(k.provs)
		if cd, ok := k.cooldown[idx]; ok && now.Before(cd) {
			continue
		}
		k.cur = idx
		return idx
	}
	return -1
}

func (k *keyPoolProvider) recordFail(idx int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.fails[idx]++
	if k.fails[idx] >= 3 {
		k.cooldown[idx] = time.Now().Add(30 * time.Second) // 熔断 30s
		k.fails[idx] = 0
	}
}

func (k *keyPoolProvider) recordSuccess(idx int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.fails, idx)
	delete(k.cooldown, idx)
}

// keyPoolSwitchable 判断错误是否需要切换 key：鉴权失败或限流。
func keyPoolSwitchable(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, marker := range []string{
		"status 401", "status 403", "status 429",
		"rate limit", "authentication", "unauthorized", "api key",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

func sleepCtxLocal(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return true
	case <-time.After(d):
		return false
	}
}
