package main

import (
	"context"
	"log"
	"time"

	"r0rpc/internal/config"
	"r0rpc/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := cfg.ApplyTimeZone(); err != nil {
		log.Fatalf("apply time zone: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := store.BootstrapSchema(ctx, cfg); err != nil {
		log.Fatalf("bootstrap schema failed: %v", err)
	}
	st, err := store.New(cfg)
	if err != nil {
		log.Fatalf("open store failed: %v", err)
	}
	if err := st.EnsureBootstrapAdmin(ctx, cfg.BootstrapAdminUser, cfg.BootstrapAdminPass); err != nil {
		log.Fatalf("ensure bootstrap admin failed: %v", err)
	}
	log.Println("database and tables are ready")
}
