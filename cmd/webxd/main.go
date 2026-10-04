// webxd — the durable webx server edition. Same routes as `webx serve`,
// backed by Postgres (multi-worker job claims via SKIP LOCKED) or SQLite
// for single-node durability.
//
//	webxd --addr :8080 --dsn postgres://user:pass@host/webx
//	webxd --addr :8080 --store sqlite --dsn /path/jobs.db
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kasyap1234/webx/internal/serve"
	"github.com/kasyap1234/webx/internal/store"
)

func main() {
	addr := flag.String("addr", envOr("WEBXD_ADDR", ":8080"), "listen address")
	backend := flag.String("store", envOr("WEBXD_STORE", "pg"), "job store: pg|sqlite")
	dsn := flag.String("dsn", envOr("WEBXD_DSN", ""), "postgres://… or sqlite file path")
	workers := flag.Int("workers", envInt("WEBXD_WORKERS", 4), "job workers")
	flag.Parse()

	var st store.Store
	var err error
	switch *backend {
	case "pg", "postgres":
		if *dsn == "" {
			fmt.Fprintln(os.Stderr, "webxd: --dsn postgres://… required for --store pg")
			os.Exit(1)
		}
		st, err = store.OpenPostgres(*dsn)
	case "sqlite":
		st, err = store.OpenSQLite(*dsn)
	default:
		fmt.Fprintln(os.Stderr, "webxd: unknown --store "+*backend)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "webxd: store:", err)
		os.Exit(1)
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := serve.New(st)
	srv.RunWorkers(ctx, *workers)
	go srv.RunScheduler(ctx)

	httpSrv := &http.Server{Addr: *addr, Handler: srv.Handler(), ReadHeaderTimeout: 15 * time.Second}
	go func() {
		<-ctx.Done()
		shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpSrv.Shutdown(shCtx)
	}()

	fmt.Fprintf(os.Stderr, "webxd listening on %s (store=%s workers=%d)\n", *addr, *backend, *workers)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "webxd:", err)
		os.Exit(1)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	var n int
	if v := os.Getenv(k); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return def
}
