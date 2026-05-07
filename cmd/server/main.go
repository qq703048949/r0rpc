package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"r0rpc/internal/app"
	"r0rpc/internal/config"
	"r0rpc/internal/store"
	"r0rpc/internal/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := cfg.ApplyTimeZone(); err != nil {
		log.Fatalf("apply time zone: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := store.BootstrapSchema(ctx, cfg); err != nil {
		log.Fatalf("bootstrap schema: %v", err)
	}

	st, err := store.New(cfg)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}

	application := app.New(cfg, st)
	if err := application.Store.EnsureBootstrapAdmin(context.Background(), cfg.BootstrapAdminUser, cfg.BootstrapAdminPass); err != nil {
		log.Fatalf("bootstrap admin: %v", err)
	}
	rebuildCtx, rebuildCancel := context.WithTimeout(context.Background(), 20*time.Second)
	if err := application.Store.RebuildRecentDeviceMetricsFromRequests(rebuildCtx, cfg.RawRetentionDays); err != nil {
		log.Printf("rebuild recent device metrics failed: %v", err)
	}
	rebuildCancel()
	application.StartBackgroundJobs(context.Background())

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           web.New(application).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("server listening on %s", cfg.HTTPAddr)
	log.Printf("time zone: %s", cfg.TimeZone)
	log.Printf("bootstrap admin: %s / %s", cfg.BootstrapAdminUser, cfg.BootstrapAdminPass)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen and serve: %v", err)
	}
}
