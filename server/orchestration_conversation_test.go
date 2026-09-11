package server

import (
	"context"
	"testing"
)

// TestConversationContext verifies the conversation-scoped ctx helper used to
// group chat-driven orchestration.
func TestConversationContext(t *testing.T) {
	base := context.Background()
	if got := currentConversationID(base); got != 0 {
		t.Fatalf("base ctx should carry no conversation, got %d", got)
	}
	ctx := withCurrentConversation(base, 42)
	if got := currentConversationID(ctx); got != 42 {
		t.Fatalf("withCurrentConversation round-trip failed, got %d", got)
	}
	if got := currentConversationID(withCurrentConversation(base, 0)); got != 0 {
		t.Fatalf("zero conversation id should not be attached, got %d", got)
	}
}
