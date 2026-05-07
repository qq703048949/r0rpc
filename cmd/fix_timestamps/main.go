package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	"r0rpc/internal/config"
	"r0rpc/internal/store"
)

type fixStep struct {
	name  string
	query string
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := cfg.ApplyTimeZone(); err != nil {
		log.Fatalf("apply time zone: %v", err)
	}

	st, err := store.New(cfg)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.DB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cutoff := time.Now()
	log.Printf("fix cutoff: %s", cutoff.Format(time.RFC3339))

	if err := applyFix(ctx, st.DB, cutoff); err != nil {
		log.Fatalf("fix timestamps: %v", err)
	}
	log.Println("historical timezone fix completed")
}

func applyFix(ctx context.Context, db *sql.DB, cutoff time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	steps := []fixStep{
		{name: "users last_login_at", query: "UPDATE users SET last_login_at = DATE_ADD(last_login_at, INTERVAL 8 HOUR) WHERE last_login_at IS NOT NULL AND last_login_at < ?"},
		{name: "users created_at updated_at", query: "UPDATE users SET created_at = DATE_ADD(created_at, INTERVAL 8 HOUR), updated_at = DATE_ADD(updated_at, INTERVAL 8 HOUR) WHERE created_at < ?"},
		{name: "devices last_seen_at", query: "UPDATE devices SET last_seen_at = DATE_ADD(last_seen_at, INTERVAL 8 HOUR) WHERE last_seen_at < ?"},
		{name: "devices created_at updated_at", query: "UPDATE devices SET created_at = DATE_ADD(created_at, INTERVAL 8 HOUR), updated_at = DATE_ADD(updated_at, INTERVAL 8 HOUR) WHERE created_at < ?"},
		{name: "rpc_requests created_at", query: "UPDATE rpc_requests SET created_at = DATE_ADD(created_at, INTERVAL 8 HOUR) WHERE created_at < ?"},
		{name: "rpc_requests finished_at", query: "UPDATE rpc_requests SET finished_at = DATE_ADD(finished_at, INTERVAL 8 HOUR) WHERE finished_at IS NOT NULL AND finished_at < ?"},
		{name: "device_daily_metrics updated_at", query: "UPDATE device_daily_metrics SET updated_at = DATE_ADD(updated_at, INTERVAL 8 HOUR) WHERE updated_at < ?"},
		{name: "rpc_daily_metrics updated_at", query: "UPDATE rpc_daily_metrics SET updated_at = DATE_ADD(updated_at, INTERVAL 8 HOUR) WHERE updated_at < ?"},
	}

	for _, step := range steps {
		result, execErr := tx.ExecContext(ctx, step.query, cutoff)
		if execErr != nil {
			err = execErr
			return err
		}
		affected, _ := result.RowsAffected()
		log.Printf("%s: %d rows", step.name, affected)
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return commitErr
	}
	return nil
}
