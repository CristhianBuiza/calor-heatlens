// Command calor-api runs the Calor heatmap analytics API.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	"calor/api/internal/server"
	"calor/api/internal/store"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func main() {
	logger := log.New(os.Stderr, "calor ", log.LstdFlags)

	// Without GOMEMLIMIT in the environment, default to a soft limit that
	// keeps the API well inside a small VPS; the runtime honours an explicit
	// GOMEMLIMIT on its own.
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(int64(envInt("CALOR_MEMORY_LIMIT_MB", 192)) << 20)
	}

	st, err := store.Open(env("CALOR_DB_PATH", "calor.db"), store.Options{
		CacheKiB:      envInt("CALOR_SQLITE_CACHE_KB", 4096),
		MmapBytes:     int64(envInt("CALOR_SQLITE_MMAP_MB", 0)) << 20,
		MaxReadConns:  envInt("CALOR_DB_READERS", 2),
		SoftHeapBytes: int64(envInt("CALOR_SQLITE_HEAP_MB", 64)) << 20,
	})
	if err != nil {
		logger.Fatalf("open store: %v", err)
	}

	adminKey := os.Getenv("CALOR_ADMIN_KEY")
	if adminKey == "" {
		logger.Printf("WARNING: CALOR_ADMIN_KEY is empty; read/admin endpoints are unauthenticated")
	}
	srv := server.New(st, server.Config{
		AdminKey:     adminKey,
		TrackerDir:   os.Getenv("CALOR_TRACKER_DIR"),
		MaxBodyBytes: int64(envInt("CALOR_MAX_BODY_KB", 256)) << 10,
		MaxBatch:     envInt("CALOR_MAX_BATCH", 200),
		StrictOrigin: env("CALOR_STRICT_ORIGIN", "true") != "false",
		CacheTTL:     time.Duration(envInt("CALOR_CACHE_TTL_SEC", 15)) * time.Second,
		QueueBatches: envInt("CALOR_QUEUE_SIZE", 2048),
		Logger:       logger,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if days := envInt("CALOR_RETENTION_DAYS", 0); days > 0 {
		go func() {
			t := time.NewTicker(time.Hour)
			defer t.Stop()
			for {
				cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
				if n, err := st.PruneEvents(ctx, cutoff); err != nil {
					logger.Printf("retention: %v", err)
				} else if n > 0 {
					logger.Printf("retention: pruned %d events older than %d days", n, days)
				}
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
			}
		}()
	}

	addr := env("CALOR_ADDR", ":8080")
	hs := &http.Server{
		Addr:              addr,
		Handler:           server.Logging(logger, srv),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	go func() {
		logger.Printf("listening on %s (db %s)", addr, env("CALOR_DB_PATH", "calor.db"))
		if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("listen: %v", err)
		}
	}()
	<-ctx.Done()
	logger.Printf("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = hs.Shutdown(shutdownCtx)
	srv.Close()
	_ = st.Close()
}
