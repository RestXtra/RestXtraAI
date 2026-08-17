package agent

import "testing"

func TestEvidenceGate(t *testing.T) {
	ev := NewEvidenceStore()
	ev.Add("fetch", `{"url":"http://t/flag"}`,
		`HTTP 200 <body>flag{abc123def}</body>`, false)
	ev.Add("Bash", `{"command":"curl /admin"}`,
		"200 OK admin panel", false)

	cases := []struct {
		name, final, goal string
		wantOK            bool
	}{
		{"flag verbatim ok", "拿到 flag：flag{abc123def}，引用证据 e1", "拿到 flag", true},
		{"flag not in evidence", "拿到 flag：flag{FAKEFAKE}", "拿到 flag", false},
		{"missing flag", "未发现漏洞", "目标：拿下 flag", false},
		{"unknown evidence id", "结论见 e999", "", false},
		{"unknown low evidence id", "结论见 e5", "", false},
		{"no flag goal, ok", "完成：admin 面板无鉴权，见 e2", "测试后台", true},
	}
	for _, c := range cases {
		ok, why := ev.CheckCompletion(c.final, c.goal)
		if ok != c.wantOK {
			t.Errorf("%s: got ok=%v (why=%q), want %v", c.name, ok, why, c.wantOK)
		}
	}
}

func TestEvidenceDedup(t *testing.T) {
	ev := NewEvidenceStore()
	a := ev.Add("Bash", "", "same output", false)
	b := ev.Add("Bash", "", "same output", false)
	if b.DuplicateOf != a.ID {
		t.Errorf("expected duplicate_of=%d, got %d", a.ID, b.DuplicateOf)
	}
	if n := len(ev.Records()); n != 1 {
		t.Errorf("expected 1 dedup record, got %d", n)
	}
}

func TestReflexionEscalation(t *testing.T) {
	rx := NewReflexion()
	// 成功一次 → 连败清零
	rx.Observe("Bash", []byte(`{"command":"curl /a"}`), []byte("200 OK"), false)
	if rx.level() != 0 {
		t.Fatalf("expected level 0 after success, got %d", rx.level())
	}
	// 连败两次 → 入队升级提示
	rx.Observe("Bash", []byte(`{"command":"curl /a"}' OR 1=1`), []byte("403 Forbidden"), false)
	if rx.level() != 0 {
		t.Fatalf("expected level 0 after 1 fail, got %d", rx.level())
	}
	rx.Observe("Bash", []byte(`{"command":"curl /a"}' OR 1=1`), []byte("403 Forbidden"), false)
	if rx.level() != 1 {
		t.Fatalf("expected level 1 after 2 fails, got %d", rx.level())
	}
	if _, ok := rx.Drain(); !ok {
		t.Fatal("expected a queued escalation hint after 2 failures")
	}
	// 连败 5 次 → 反思 + 清零，level 回升
	for i := 0; i < 5; i++ {
		rx.Observe("Bash", []byte(""), []byte("blocked"), false)
	}
	if rx.level() < 2 {
		t.Fatalf("expected level>=2 after no-progress reflection, got %d", rx.level())
	}
}
