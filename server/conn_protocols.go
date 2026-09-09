package server

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/reiver/go-telnet"
	"github.com/x90skysn3k/grdp/client"
)

// ---- P2 真协议：RDP / Telnet（研究仓库本地 replace 固定，见 go.mod）----

// rdpCredentialCheck 通过 grdp 的 NLA/CredSSP auth-only 模式验证 RDP 凭据
// （快速，不建立完整桌面会话）。要求服务端支持 NLA（多数 Windows 默认开）。
func rdpCredentialCheck(host string, port int, user, passwd string) error {
	if port == 0 {
		port = 3389
	}
	setting := client.NewSetting()
	setting.TLSVerify = false // 自签名证书常见；仅做连通/凭据验证
	c := client.NewClient(fmt.Sprintf("%s:%d", host, port), user, passwd, client.TC_RDP, setting)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	return c.LoginAuthOnly(ctx)
}

// telnetRun 通过 go-telnet 连上并发送一条命令，收集回复。telnet 无可靠命令完成
// 信号，故在「新数据空闲超过 idle」或总超时后返回已收到内容（尽力而为）。
func telnetRun(host string, port int, cmd string, timeout time.Duration) (string, error) {
	if port == 0 {
		port = 23
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	conn, err := telnet.DialTo(fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, cmd+"\r\n"); err != nil {
		return "", err
	}
	idle := timeout / 3
	if idle > time.Second {
		idle = time.Second
	}
	var mu sync.Mutex
	var got strings.Builder
	lastLen := 0
	lastData := time.Now()
	go func() {
		// go-telnet 的 dataReader 会填满整个 buffer 才返回；用小 buffer 让它按
		// 块返回，配合 idle 判定即可及时收到部分输出。
		buf := make([]byte, 32)
		for {
			n, rerr := conn.Read(buf)
			mu.Lock()
			if n > 0 {
				got.Write(buf[:n])
			}
			mu.Unlock()
			if rerr != nil {
				return
			}
		}
	}()
	start := time.Now()
	for {
		time.Sleep(100 * time.Millisecond)
		mu.Lock()
		if got.Len() > lastLen {
			lastData = time.Now()
		}
		lastLen = got.Len()
		mu.Unlock()
		if time.Since(lastData) > idle || time.Since(start) > timeout {
			break
		}
	}
	mu.Lock()
	out := got.String()
	mu.Unlock()
	if out == "" {
		return "", fmt.Errorf("telnet 无输出(连接可能被拒或未返回数据)")
	}
	return out, nil
}
