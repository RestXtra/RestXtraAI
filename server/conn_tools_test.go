package server

import (
	"testing"
)

func TestConnDangerousRe(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{"whoami", false},
		{"ipconfig", false},
		{"cat /etc/passwd", false},
		{"ls -la /tmp", false},
		{"netstat -an", false},
		{"ps aux", false},
		{"echo hi", false},
		{"rm -rf /var/log/app", true},
		{"rm -r /opt/x", true},
		{"shutdown -h now", true},
		{"systemctl stop nginx", true},
		{"service mysql stop", true},
		{"kill -9 1234", true},
		{"pkill chrome", true},
		{"iptables -A INPUT -j DROP", true},
		{"userdel bob", true},
		{"passwd alice", true},
		{"dd if=/dev/zero of=/dev/sda", true},
		{"mkfs.ext4 /dev/sdb1", true},
		{"taskkill /F /IM evil.exe", true},
		{"del /f /q C:\\tmp\\x.dll", true},
	}
	for _, c := range cases {
		if got := connDangerousRe.MatchString(c.cmd); got != c.want {
			t.Errorf("connDangerousRe(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

func TestConnToolSchemas(t *testing.T) {
	cl := (&Server{}).toolConnList()
	if cl.Name() != "conn_list" {
		t.Fatalf("conn_list name = %q", cl.Name())
	}
	if cl.InputSchema() == nil {
		t.Fatal("conn_list schema nil")
	}
	ex := (&Server{}).toolConnExec()
	if ex.Name() != "conn_exec" {
		t.Fatalf("conn_exec name = %q", ex.Name())
	}
	if ex.InputSchema() == nil {
		t.Fatal("conn_exec schema nil")
	}
	co := (&Server{}).toolConnContain()
	if co.Name() != "conn_contain" {
		t.Fatalf("conn_contain name = %q", co.Name())
	}
	if co.InputSchema() == nil {
		t.Fatal("conn_contain schema nil")
	}
}

func TestVerifyContainment(t *testing.T) {
	// block_ip: 回查输出应包含目标 IP(iptables 规则存在)。
	if got := verifyContainment("block_ip", "1.2.3.4", "Chain INPUT\nDROP       all  --  1.2.3.4             anywhere"); !got {
		t.Error("block_ip: rule present should verify")
	}
	if got := verifyContainment("block_ip", "1.2.3.4", "Chain INPUT\n(policy ACCEPT)"); got {
		t.Error("block_ip: rule absent should NOT verify")
	}
	if got := verifyContainment("isolate", "1.2.3.4", "DROP 1.2.3.4"); !got {
		t.Error("isolate: rule present should verify")
	}
	// kill_process: 回查输出不应包含目标 pid(进程已终止)。
	if got := verifyContainment("kill_process", "7788", ""); !got {
		t.Error("kill_process: empty ps output (gone) should verify")
	}
	if got := verifyContainment("kill_process", "7788", "7788 pts/0 00:00:01 evil"); got {
		t.Error("kill_process: pid still present should NOT verify")
	}
	// 未知动作 → 不验证。
	if got := verifyContainment("unknown", "x", "anything"); got {
		t.Error("unknown action should never verify")
	}
}

func TestConnContainVerifyCmd(t *testing.T) {
	if got := connContainVerifyCmd("block_ip", "1.2.3.4"); got != "iptables -L INPUT -n | grep 1.2.3.4" {
		t.Errorf("block_ip verify cmd = %q", got)
	}
	if got := connContainVerifyCmd("kill_process", "7788"); got != "ps -p 7788" {
		t.Errorf("kill_process verify cmd = %q", got)
	}
	if got := connContainVerifyCmd("unknown", "x"); got != "" {
		t.Errorf("unknown verify cmd = %q", got)
	}
}
