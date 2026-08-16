//go:build windows

package server

import (
	"context"
	"net"
	"strings"

	"github.com/Microsoft/go-winio"
)

// dialDocker is the Windows implementation: it supports named pipes
// (npipe:////./pipe/docker_engine) in addition to unix sockets.
func dialDocker(ctx context.Context, dialSpec string) (net.Conn, error) {
	if strings.HasPrefix(dialSpec, "npipe:") {
		return winio.DialPipeContext(ctx, strings.TrimPrefix(dialSpec, "npipe:"))
	}
	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", strings.TrimPrefix(dialSpec, "unix:"))
}
