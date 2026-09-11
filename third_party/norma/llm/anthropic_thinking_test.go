package llm

import "testing"

func TestFilterThinkingBlocksByModel(t *testing.T) {
	thinkingMsg := func(model, thinking, sig string) Message {
		return Message{Role: RoleAssistant, Content: []ContentBlock{
			{Type: BlockThinking, Thinking: thinking, Signature: sig, Model: model},
			{Type: BlockText, Text: "answer"},
		}}
	}

	// Thinking disabled: the thinking block is dropped, the text is kept.
	got := filterThinkingBlocks([]Message{thinkingMsg("m1", "reason", "sig1")}, false, "m1")
	if len(got) != 1 || len(got[0].Content) != 1 || got[0].Content[0].Type != BlockText {
		t.Fatalf("disabled: %+v", got)
	}

	// Thinking enabled, same model: replayed verbatim with its signature.
	got = filterThinkingBlocks([]Message{thinkingMsg("m1", "reason", "sig1")}, true, "m1")
	if len(got[0].Content) != 2 || got[0].Content[0].Type != BlockThinking || got[0].Content[0].Signature != "sig1" {
		t.Fatalf("same model: %+v", got[0].Content)
	}

	// Thinking enabled, different model: the opaque signature is dropped and the
	// visible reasoning is lowered to ordinary text.
	got = filterThinkingBlocks([]Message{thinkingMsg("m1", "reason", "sig1")}, true, "m2")
	if len(got[0].Content) != 2 || got[0].Content[0].Type != BlockText || got[0].Content[0].Text != "reason" {
		t.Fatalf("different model: %+v", got[0].Content)
	}

	// Different model with no visible reasoning: nothing to keep, message dropped.
	empty := Message{Role: RoleAssistant, Content: []ContentBlock{{Type: BlockThinking, Signature: "s", Model: "m1"}}}
	if got = filterThinkingBlocks([]Message{empty}, true, "m2"); len(got) != 0 {
		t.Fatalf("foreign thinking-only should drop: %+v", got)
	}

	// Unknown provenance (Model == ""): treated as current-model for safety.
	got = filterThinkingBlocks([]Message{thinkingMsg("", "reason", "sig1")}, true, "m2")
	if got[0].Content[0].Type != BlockThinking || got[0].Content[0].Signature != "sig1" {
		t.Fatalf("unknown provenance: %+v", got[0].Content)
	}
}
