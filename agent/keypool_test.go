package agent

import (
	"context"
	"errors"
	"iter"
	"testing"

	"github.com/Autumn-27/norma/llm"
)

// errProvider 是一个每次都返回固定错误的假 provider，用于验证错误传播。
type errProvider struct{ err error }

func (p errProvider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		yield(llm.StreamEvent{}, p.err)
	}
}

// okProvider 是一个正常返回单个文本事件的假 provider。
type okProvider struct{}

func (p okProvider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		yield(llm.StreamEvent{Type: llm.SETextDelta, Text: "ok"}, nil)
	}
}

// TestKeyPoolSingleKeyPropagatesError 验证单 key 场景下非切换错误原样传播
// （回归：此前错误被吞成 nil，LLM 失败被伪装成正常结束）。
func TestKeyPoolSingleKeyPropagatesError(t *testing.T) {
	pool := newKeyPool([]llm.Provider{errProvider{err: errors.New("boom")}})
	sawErr := false
	for _, err := range pool.Stream(context.Background(), llm.CompletionRequest{}) {
		if err != nil {
			sawErr = true
			if err.Error() != "boom" {
				t.Fatalf("expected original error, got %v", err)
			}
		}
	}
	if !sawErr {
		t.Fatal("expected the error to propagate to the caller, got nil")
	}
}

// TestKeyPoolSwitchableErrorFallsThrough 验证可切换错误（多 key）会尝试下一个 key。
func TestKeyPoolSwitchableErrorFallsThrough(t *testing.T) {
	pool := newKeyPool([]llm.Provider{
		errProvider{err: errors.New("status 429 rate limit")},
		okProvider{},
	})
	var texts []string
	for ev, err := range pool.Stream(context.Background(), llm.CompletionRequest{}) {
		if err != nil {
			t.Fatalf("unexpected error after failover: %v", err)
		}
		if ev.Text != "" {
			texts = append(texts, ev.Text)
		}
	}
	if len(texts) != 1 || texts[0] != "ok" {
		t.Fatalf("expected text from second key, got %v", texts)
	}
}

// TestKeyPoolNonSwitchableErrorNotRetried 验证多 key 下非切换错误不换 key、原样传播。
func TestKeyPoolNonSwitchableErrorNotRetried(t *testing.T) {
	pool := newKeyPool([]llm.Provider{
		errProvider{err: errors.New("unexpected EOF")},
		okProvider{},
	})
	sawErr := false
	for _, err := range pool.Stream(context.Background(), llm.CompletionRequest{}) {
		if err != nil {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatal("expected non-switchable error to propagate without failing over")
	}
}