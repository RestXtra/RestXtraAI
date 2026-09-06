// Command restxtra runs the RestXtra AI backend: the autonomous exploration engine
// exploration engine (norma agent runtime + PostgreSQL) fused with RestXtra
// platform capabilities (RBAC / audit / knowledge / playbook / batch, ...),
// exposed through the JSON HTTP API consumed by the Next.js frontend.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/RestXtra/RestXtraAI/config"
	"github.com/RestXtra/RestXtraAI/server"
)

// version is the build version, injected at release time via
// -ldflags "-X main.version=<tag>". Defaults to "dev" for local builds.
var version = "dev"

const banner = `
 ____           _  __  ___             
|  _ \ ___  ___| |_\ \/ / |_ _ __ __ _ 
| |_) / _ \/ __| __|\  /| __| '__/ _` + "`" + ` |
|  _ <  __/\__ \ |_ /  \| |_| | | (_| |
|_| \_\___||___/\__/_/\_\\__|_|  \__,_|
                                       `

// printBanner writes the startup banner + version/runtime info to stdout.
func printBanner(addr string) {
	fmt.Print(banner)
	fmt.Println("  RestXtra AI · 基础设施分析与自动化协作平台")
	fmt.Printf("  版本 %s  ·  %s/%s  ·  %s  ·  监听 %s\n\n",
		version, runtime.GOOS, runtime.GOARCH, runtime.Version(), addr)
}

func main() {
	var (
		addr    = flag.String("addr", ":8787", "HTTP listen address")
		dataDir = flag.String("data", filepath.Join(config.BaseDir(), "data"), "data directory for SQLite stores (default: data/ next to the executable)")
		proxy   = flag.String("proxy", ":8788", "traffic recording proxy address (empty to disable)")
	)
	flag.Parse()

	printBanner(*addr)

	// capture backend logs into the in-memory sink (still to stderr) so the /logs
	// page can show a live log stream. Do this first, to catch startup logs too.
	server.StartLogCapture()

	// surface which config file the binary reads (absolute, so `go run`'s relative
	// "config.json" — resolved against the CWD — is unambiguous).
	cfgPath := config.Path()
	if abs, e := filepath.Abs(cfgPath); e == nil {
		cfgPath = abs
	}
	if _, e := os.Stat(cfgPath); e == nil {
		log.Printf("[config] 配置文件: %s", cfgPath)
	} else {
		log.Printf("[config] 配置文件: %s (不存在 — 将仅尝试环境变量 RESTXTRA_PG_DSN)", cfgPath)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mgr, err := server.NewManager(*dataDir, *proxy)
	if err != nil {
		log.Fatalf("open stores: %v", err)
	}
	defer mgr.Close()

	skillDir := config.SkillDir()
	if abs, err := filepath.Abs(skillDir); err == nil {
		skillDir = abs
	}
	log.Printf("[config] skill 目录: %s", skillDir)
	srv := server.New(ctx, mgr, skillDir, *dataDir)
	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		// Protect slow-client response-hold from pinning handler goroutines.
		// SSE endpoints explicitly clear this deadline via ResponseController,
		// so long-lived streams are unaffected.
		WriteTimeout:   120 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	go func() {
		log.Printf("RestXtra AI %s backend listening on %s (data=%s, workers=%d)", version, *addr, *dataDir, mgr.Workers())
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}
