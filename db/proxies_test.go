package db

import "testing"

func TestParseProxyLines(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		expect []*Proxy
	}{
		{
			name:  "bare ip:port",
			input: "1.2.3.4:8080",
			expect: []*Proxy{
				{Protocol: "http", Host: "1.2.3.4", Port: 8080, Source: "import"},
			},
		},
		{
			name:  "bare ip:port:user:pass",
			input: "1.2.3.4:8080:alice:secret",
			expect: []*Proxy{
				{Protocol: "http", Host: "1.2.3.4", Port: 8080, Username: "alice", Password: "secret", Source: "import"},
			},
		},
		{
			name:  "scheme http with auth",
			input: "http://alice:secret@1.2.3.4:3128",
			expect: []*Proxy{
				{Protocol: "http", Host: "1.2.3.4", Port: 3128, Username: "alice", Password: "secret", Source: "import"},
			},
		},
		{
			name:  "scheme socks5 no auth",
			input: "socks5://1.2.3.4:1080",
			expect: []*Proxy{
				{Protocol: "socks5", Host: "1.2.3.4", Port: 1080, Source: "import"},
			},
		},
		{
			name:  "mixed lines with comment",
			input: "# header\n1.2.3.4:8080\nsocks5://user:pass@1.2.3.5:1080\n\nbogus",
			expect: []*Proxy{
				{Protocol: "http", Host: "1.2.3.4", Port: 8080, Source: "import"},
				{Protocol: "socks5", Host: "1.2.3.5", Port: 1080, Username: "user", Password: "pass", Source: "import"},
			},
		},
	}
	for _, c := range cases {
		got := ParseProxyLines(c.input)
		if len(got) != len(c.expect) {
			t.Errorf("%s: got %d proxies, want %d (%v)", c.name, len(got), len(c.expect), got)
			continue
		}
		for i, want := range c.expect {
			g := got[i]
			if g.Protocol != want.Protocol || g.Host != want.Host || g.Port != want.Port ||
				g.Username != want.Username || g.Password != want.Password {
				t.Errorf("%s[%d]: got %+v, want %+v", c.name, i, g, want)
			}
		}
	}
}

func TestParseClashText(t *testing.T) {
	input := `mixed-port: 7890
mode: rule

proxy-groups:
  - name: Proxy
    type: select
    proxies:
      - DIRECT
      - socks-out-39.108.232.134

proxies:
  - name: socks-out-39.108.232.134
    type: socks5
    server: 39.108.232.134
    port: 51010
    udp: true
    username: dwniWd
    password: FW2d2wF3a

  - name: http-out
    type: http
    server: 1.2.3.4
    port: 8080

  - name: vmess-node
    type: vmess
    server: 5.6.7.8
    port: 443

rules:
  - PROCESS-NAME,MAgent.exe,DIRECT
  - MATCH,Proxy
`
	proxies, groups, rules, skipped, err := ParseClashText(input)
	if err != nil {
		t.Fatalf("ParseClashText: %v", err)
	}
	if len(proxies) != 2 {
		t.Fatalf("expected 2 supported proxies, got %d (%+v)", len(proxies), proxies)
	}
	if skipped != 1 {
		t.Errorf("expected 1 skipped (vmess), got %d", skipped)
	}
	p := proxies[0]
	if p.Name != "socks-out-39.108.232.134" || p.Protocol != "socks5" ||
		p.Host != "39.108.232.134" || p.Port != 51010 ||
		p.Username != "dwniWd" || p.Password != "FW2d2wF3a" {
		t.Errorf("socks5 node mismatch: %+v", p)
	}
	if proxies[1].Protocol != "http" || proxies[1].Host != "1.2.3.4" || proxies[1].Port != 8080 {
		t.Errorf("http node mismatch: %+v", proxies[1])
	}
	if len(groups) != 1 || groups[0] != "Proxy" {
		t.Errorf("groups = %v, want [Proxy]", groups)
	}
	if len(rules) != 2 {
		t.Errorf("rules = %v, want 2", rules)
	}
}
