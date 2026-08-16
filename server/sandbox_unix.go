//go:build !windows

package server

import (
	"context"
	"net"
	"strings"
)

// dialDocker is the non-Windows implementation: unix sockets only.
func dialDocker(ctx context.Context, dialSpec string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", strings.TrimPrefix(dialSpec, "unix:"))
}
