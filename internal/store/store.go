package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"r0rpc/internal/config"
	"r0rpc/internal/model"

	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

//go:embed schema.sql
var schemaSQL string

type Store struct {
	DB    *sql.DB
	Redis *redis.Client
}

func New(cfg config.Config) (*Store, error) {
	db, err := sql.Open("mysql", cfg.MySQLDSN(false))
	if err != nil {
		return nil, err
	}

	maxOpenConns := cfg.MySQL.MaxOpenConns
	if maxOpenConns <= 0 {
		maxOpenConns = 100
	}
	maxIdleConns := cfg.MySQL.MaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = 25
	}
	connMaxLifetimeMinutes := cfg.MySQL.ConnMaxLifetimeMinutes
	if connMaxLifetimeMinutes <= 0 {
		connMaxLifetimeMinutes = 10
	}
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(time.Duration(connMaxLifetimeMinutes) * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	if err := configureDBSession(ctx, db, cfg); err != nil {
		_ = db.Close()
		return nil, err
	}

	var redisClient *redis.Client
	if cfg.Redis.Addr != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:            cfg.Redis.Addr,
			Password:        cfg.Redis.Password,
			DB:              cfg.Redis.DB,
			ReadTimeout:     5 * time.Second,
			WriteTimeout:    5 * time.Second,
			ConnMaxIdleTime: 2 * time.Minute,
		})
	}

	return &Store{DB: db, Redis: redisClient}, nil
}

func BootstrapSchema(ctx context.Context, cfg config.Config) error {
	db, err := sql.Open("mysql", cfg.MySQLDSN(true))
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS `"+cfg.MySQL.DB+"` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		_ = db.Close()
		return err
	}
	_ = db.Close()

	db, err = sql.Open("mysql", cfg.MySQLDSN(false))
	if err != nil {
		return err
	}
	defer db.Close()

	cleanSchema := strings.TrimPrefix(schemaSQL, "\uFEFF")
	for _, stmt := range strings.Split(cleanSchema, ";") {
		stmt = strings.TrimSpace(strings.TrimPrefix(stmt, "\uFEFF"))
		if stmt == "" || strings.HasPrefix(strings.ToUpper(stmt), "CREATE DATABASE") || strings.EqualFold(stmt, "USE `"+cfg.MySQL.DB+"`") {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := ensureIndexes(ctx, db, cfg.MySQL.DB); err != nil {
		return err
	}
	return nil
}

func ensureIndexes(ctx context.Context, db *sql.DB, schema string) error {
	indexes := []struct {
		Table string
		Name  string
		Cols  string
	}{
		{Table: "devices", Name: "idx_devices_group_last_seen", Cols: "group_name, last_seen_at"},
		{Table: "devices", Name: "idx_devices_group_status_last_seen", Cols: "group_name, status, last_seen_at"},
		{Table: "devices", Name: "idx_devices_status_last_seen", Cols: "status, last_seen_at"},
		{Table: "rpc_requests", Name: "idx_rpc_requests_group_client_created", Cols: "group_name, client_id, created_at"},
		{Table: "rpc_requests", Name: "idx_rpc_requests_client_created", Cols: "client_id, created_at"},
		{Table: "rpc_requests", Name: "idx_rpc_requests_action_created", Cols: "action_name, created_at"},
		{Table: "rpc_requests", Name: "idx_rpc_requests_created_group_action", Cols: "created_at, group_name, action_name"},
	}
	for _, item := range indexes {
		exists, err := indexExists(ctx, db, schema, item.Table, item.Name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE %s ADD INDEX %s (%s)", item.Table, item.Name, item.Cols)
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if me, ok := err.(*mysql.MySQLError); ok && (me.Number == 1061 || me.Number == 1060) {
				continue
			}
			return err
		}
	}
	return nil
}

func indexExists(ctx context.Context, db *sql.DB, schema, tableName, indexName string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM information_schema.statistics
		WHERE table_schema = ? AND table_name = ? AND index_name = ?
	`, schema, tableName, indexName).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Store) EnsureBootstrapAdmin(ctx context.Context, username, password string) error {
	var existingID int64
	err := s.DB.QueryRowContext(ctx, "SELECT id FROM users WHERE username = ?", username).Scan(&existingID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, role, enabled, can_rpc, notes)
		VALUES (?, ?, 'admin', 1, 1, 'bootstrap admin')
	`, username, string(hash))
	return err
}

func (s *Store) AuthenticateUser(ctx context.Context, username, password string, requireRPC bool) (*model.User, error) {
	user := &model.User{}
	query := `
		SELECT id, username, password_hash, role, enabled, can_rpc, notes, last_login_at, created_at, updated_at
		FROM users WHERE username = ?
	`
	err := s.DB.QueryRowContext(ctx, query, username).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.Enabled, &user.CanRPC,
		&user.Notes, &user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if !user.Enabled {
		return nil, fmt.Errorf("account disabled")
	}
	if requireRPC && !user.CanRPC {
		return nil, fmt.Errorf("rpc access denied")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	_, _ = s.DB.ExecContext(ctx, "UPDATE users SET last_login_at = NOW() WHERE id = ?", user.ID)
	return user, nil
}

func (s *Store) CreateUser(ctx context.Context, username, password, role string, enabled, canRPC bool, notes string) (*model.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, role, enabled, can_rpc, notes)
		VALUES (?, ?, ?, ?, ?, ?)
	`, username, string(hash), role, enabled, canRPC, notes)
	if err != nil {
		if me, ok := err.(*mysql.MySQLError); ok && me.Number == 1062 {
			return nil, fmt.Errorf("username already exists")
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetUserByID(ctx, id)
}

func (s *Store) GetUserByID(ctx context.Context, userID int64) (*model.User, error) {
	user := &model.User{}
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, username, password_hash, role, enabled, can_rpc, notes, last_login_at, created_at, updated_at
		FROM users WHERE id = ?
	`, userID).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.Enabled, &user.CanRPC,
		&user.Notes, &user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]model.User, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, username, password_hash, role, enabled, can_rpc, notes, last_login_at, created_at, updated_at
		FROM users ORDER BY id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []model.User
	for rows.Next() {
		var item model.User
		if err := rows.Scan(&item.ID, &item.Username, &item.PasswordHash, &item.Role, &item.Enabled, &item.CanRPC,
			&item.Notes, &item.LastLoginAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) UpdateUserStatus(ctx context.Context, userID int64, enabled, canRPC bool) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE users SET enabled = ?, can_rpc = ? WHERE id = ?", enabled, canRPC, userID)
	return err
}

func (s *Store) UpdateUserPassword(ctx context.Context, userID int64, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE id = ?", string(hash), userID)
	return err
}

func (s *Store) UpsertDevice(ctx context.Context, clientID string, userID int64, groupName, platform, ip string, extra map[string]any) error {
	extraJSON, _ := json.Marshal(extra)
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO devices (client_id, user_id, group_name, platform, status, last_seen_at, last_ip, extra_json)
		VALUES (?, ?, ?, ?, 'online', NOW(), ?, ?)
		ON DUPLICATE KEY UPDATE
			user_id = VALUES(user_id),
			group_name = VALUES(group_name),
			platform = VALUES(platform),
			status = 'online',
			last_seen_at = NOW(),
			last_ip = VALUES(last_ip),
			extra_json = VALUES(extra_json)
	`, clientID, userID, groupName, platform, ip, string(extraJSON))
	if err == nil && s.Redis != nil {
		_ = s.Redis.Set(ctx, "presence:"+clientID, groupName, 2*time.Minute).Err()
	}
	return err
}

func (s *Store) TouchDevice(ctx context.Context, clientID, ip string) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE devices SET status = 'online', last_seen_at = NOW(), last_ip = COALESCE(NULLIF(?, ''), last_ip) WHERE client_id = ?", ip, clientID)
	if err == nil && s.Redis != nil {
		_ = s.Redis.Expire(ctx, "presence:"+clientID, 2*time.Minute).Err()
	}
	return err
}

func (s *Store) MarkDeviceOffline(ctx context.Context, clientID string) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE devices SET status = 'offline' WHERE client_id = ?", clientID)
	if err == nil && s.Redis != nil {
		_ = s.Redis.Del(ctx, "presence:"+clientID).Err()
	}
	return err
}

func (s *Store) MarkStaleDevicesOffline(ctx context.Context, offlineBefore time.Time) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE devices SET status = 'offline' WHERE status <> 'offline' AND last_seen_at < ?", offlineBefore)
	return err
}

func (s *Store) CreateRPCRequest(ctx context.Context, item *model.RPCRequest) error {
	_, err := s.DB.ExecContext(ctx, `
        INSERT INTO rpc_requests (
            request_id, group_name, action_name, client_id, requester_user_id,
            request_payload_json, response_payload_json, status, http_code, latency_ms, error_message
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON DUPLICATE KEY UPDATE
            group_name = CASE WHEN VALUES(group_name) <> '' THEN VALUES(group_name) ELSE group_name END,
            action_name = CASE WHEN VALUES(action_name) <> '' THEN VALUES(action_name) ELSE action_name END,
            client_id = CASE WHEN VALUES(client_id) <> '' THEN VALUES(client_id) ELSE client_id END,
            requester_user_id = COALESCE(VALUES(requester_user_id), requester_user_id),
            request_payload_json = COALESCE(VALUES(request_payload_json), request_payload_json)
    `, item.RequestID, item.GroupName, item.ActionName, item.ClientID, item.RequesterUserID,
		nullableJSON(item.RequestPayloadJSON), nullableJSON(item.ResponsePayloadJSON), item.Status, item.HTTPCode, item.LatencyMS, item.ErrorMessage)
	return err
}

func (s *Store) CompleteRPCRequest(ctx context.Context, item *model.RPCRequest) error {
	_, err := s.DB.ExecContext(ctx, `
        INSERT INTO rpc_requests (
            request_id, group_name, action_name, client_id, requester_user_id,
            request_payload_json, response_payload_json, status, http_code, latency_ms, error_message, finished_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW())
        ON DUPLICATE KEY UPDATE
            group_name = CASE WHEN VALUES(group_name) <> '' THEN VALUES(group_name) ELSE group_name END,
            action_name = CASE WHEN VALUES(action_name) <> '' THEN VALUES(action_name) ELSE action_name END,
            client_id = CASE WHEN VALUES(client_id) <> '' THEN VALUES(client_id) ELSE client_id END,
            requester_user_id = COALESCE(VALUES(requester_user_id), requester_user_id),
            request_payload_json = COALESCE(VALUES(request_payload_json), request_payload_json),
            response_payload_json = VALUES(response_payload_json),
            status = VALUES(status),
            http_code = VALUES(http_code),
            latency_ms = VALUES(latency_ms),
            error_message = VALUES(error_message),
            finished_at = VALUES(finished_at)
    `, item.RequestID, item.GroupName, item.ActionName, item.ClientID, item.RequesterUserID,
		nullableJSON(item.RequestPayloadJSON), nullableJSON(item.ResponsePayloadJSON), item.Status, item.HTTPCode, item.LatencyMS, item.ErrorMessage)
	return err
}

func (s *Store) IncrementDailyMetric(ctx context.Context, statDate time.Time, clientID, groupName, status string, latencyMS int64) error {
	success := 0
	failed := 0
	timeoutCount := 0
	switch status {
	case "success":
		success = 1
	case "timeout":
		timeoutCount = 1
	default:
		failed = 1
	}

	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO device_daily_metrics (
			stat_date, client_id, group_name, total_requests, success_requests, failed_requests,
			timeout_requests, total_latency_ms, max_latency_ms
		) VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			total_requests = total_requests + 1,
			success_requests = success_requests + VALUES(success_requests),
			failed_requests = failed_requests + VALUES(failed_requests),
			timeout_requests = timeout_requests + VALUES(timeout_requests),
			total_latency_ms = total_latency_ms + VALUES(total_latency_ms),
			max_latency_ms = GREATEST(max_latency_ms, VALUES(max_latency_ms))
	`, statDate.Format("2006-01-02"), clientID, groupName, success, failed, timeoutCount, latencyMS, latencyMS)
	return err
}

func (s *Store) IncrementRPCDailyMetric(ctx context.Context, statDate time.Time, clientID, groupName, actionName, status string, latencyMS int64) error {
	success := 0
	failed := 0
	timeoutCount := 0
	switch status {
	case "success":
		success = 1
	case "timeout":
		timeoutCount = 1
	default:
		failed = 1
	}

	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO rpc_daily_metrics (
			stat_date, group_name, action_name, client_id, total_requests, success_requests, failed_requests,
			timeout_requests, total_latency_ms, max_latency_ms
		) VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			total_requests = total_requests + 1,
			success_requests = success_requests + VALUES(success_requests),
			failed_requests = failed_requests + VALUES(failed_requests),
			timeout_requests = timeout_requests + VALUES(timeout_requests),
			total_latency_ms = total_latency_ms + VALUES(total_latency_ms),
			max_latency_ms = GREATEST(max_latency_ms, VALUES(max_latency_ms))
	`, statDate.Format("2006-01-02"), groupName, actionName, clientID, success, failed, timeoutCount, latencyMS, latencyMS)
	return err
}

type DeviceDailyMetricDelta struct {
	StatDate        string
	ClientID        string
	GroupName       string
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	TimeoutRequests int64
	TotalLatencyMS  int64
	MaxLatencyMS    int64
}

func (d *DeviceDailyMetricDelta) AddTotals(totalRequests, successRequests, failedRequests, timeoutRequests, totalLatencyMS, maxLatencyMS int64) {
	d.TotalRequests += totalRequests
	d.SuccessRequests += successRequests
	d.FailedRequests += failedRequests
	d.TimeoutRequests += timeoutRequests
	d.TotalLatencyMS += totalLatencyMS
	if maxLatencyMS > d.MaxLatencyMS {
		d.MaxLatencyMS = maxLatencyMS
	}
}

type RPCDailyMetricDelta struct {
	StatDate        string
	ClientID        string
	GroupName       string
	ActionName      string
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	TimeoutRequests int64
	TotalLatencyMS  int64
	MaxLatencyMS    int64
}

func (d *RPCDailyMetricDelta) AddTotals(totalRequests, successRequests, failedRequests, timeoutRequests, totalLatencyMS, maxLatencyMS int64) {
	d.TotalRequests += totalRequests
	d.SuccessRequests += successRequests
	d.FailedRequests += failedRequests
	d.TimeoutRequests += timeoutRequests
	d.TotalLatencyMS += totalLatencyMS
	if maxLatencyMS > d.MaxLatencyMS {
		d.MaxLatencyMS = maxLatencyMS
	}
}

func (s *Store) CompleteRPCRequests(ctx context.Context, items []*model.RPCRequest) error {
	if len(items) == 0 {
		return nil
	}

	const maxRowsPerStatement = 128

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for start := 0; start < len(items); start += maxRowsPerStatement {
		end := start + maxRowsPerStatement
		if end > len(items) {
			end = len(items)
		}

		batch := items[start:end]
		args := make([]any, 0, len(batch)*11)
		var sqlBuilder strings.Builder
		sqlBuilder.Grow(len(batch) * 64)
		sqlBuilder.WriteString(`
        INSERT INTO rpc_requests (
            request_id, group_name, action_name, client_id, requester_user_id,
            request_payload_json, response_payload_json, status, http_code, latency_ms, error_message, finished_at
        ) VALUES `)
		for index, item := range batch {
			if index > 0 {
				sqlBuilder.WriteString(",")
			}
			sqlBuilder.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW())")
			args = append(args,
				item.RequestID, item.GroupName, item.ActionName, item.ClientID, item.RequesterUserID,
				nullableJSON(item.RequestPayloadJSON), nullableJSON(item.ResponsePayloadJSON), item.Status, item.HTTPCode, item.LatencyMS, item.ErrorMessage,
			)
		}
		sqlBuilder.WriteString(`
        ON DUPLICATE KEY UPDATE
            group_name = CASE WHEN VALUES(group_name) <> '' THEN VALUES(group_name) ELSE group_name END,
            action_name = CASE WHEN VALUES(action_name) <> '' THEN VALUES(action_name) ELSE action_name END,
            client_id = CASE WHEN VALUES(client_id) <> '' THEN VALUES(client_id) ELSE client_id END,
            requester_user_id = COALESCE(VALUES(requester_user_id), requester_user_id),
            request_payload_json = COALESCE(VALUES(request_payload_json), request_payload_json),
            response_payload_json = VALUES(response_payload_json),
            status = VALUES(status),
            http_code = VALUES(http_code),
            latency_ms = VALUES(latency_ms),
            error_message = VALUES(error_message),
            finished_at = VALUES(finished_at)
    `)
		if _, err := tx.ExecContext(ctx, sqlBuilder.String(), args...); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *Store) IncrementDeviceDailyMetricsBatch(ctx context.Context, items []DeviceDailyMetricDelta) error {
	if len(items) == 0 {
		return nil
	}

	const maxRowsPerStatement = 256

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for start := 0; start < len(items); start += maxRowsPerStatement {
		end := start + maxRowsPerStatement
		if end > len(items) {
			end = len(items)
		}

		batch := items[start:end]
		args := make([]any, 0, len(batch)*9)
		var sqlBuilder strings.Builder
		sqlBuilder.Grow(len(batch) * 64)
		sqlBuilder.WriteString(`
		INSERT INTO device_daily_metrics (
			stat_date, client_id, group_name, total_requests, success_requests, failed_requests,
			timeout_requests, total_latency_ms, max_latency_ms
		) VALUES `)
		for index, item := range batch {
			if index > 0 {
				sqlBuilder.WriteString(",")
			}
			sqlBuilder.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?)")
			args = append(args,
				item.StatDate, item.ClientID, item.GroupName, item.TotalRequests, item.SuccessRequests,
				item.FailedRequests, item.TimeoutRequests, item.TotalLatencyMS, item.MaxLatencyMS,
			)
		}
		sqlBuilder.WriteString(`
		ON DUPLICATE KEY UPDATE
			total_requests = total_requests + VALUES(total_requests),
			success_requests = success_requests + VALUES(success_requests),
			failed_requests = failed_requests + VALUES(failed_requests),
			timeout_requests = timeout_requests + VALUES(timeout_requests),
			total_latency_ms = total_latency_ms + VALUES(total_latency_ms),
			max_latency_ms = GREATEST(max_latency_ms, VALUES(max_latency_ms))
	`)
		if _, err := tx.ExecContext(ctx, sqlBuilder.String(), args...); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *Store) IncrementRPCDailyMetricsBatch(ctx context.Context, items []RPCDailyMetricDelta) error {
	if len(items) == 0 {
		return nil
	}

	const maxRowsPerStatement = 256

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for start := 0; start < len(items); start += maxRowsPerStatement {
		end := start + maxRowsPerStatement
		if end > len(items) {
			end = len(items)
		}

		batch := items[start:end]
		args := make([]any, 0, len(batch)*10)
		var sqlBuilder strings.Builder
		sqlBuilder.Grow(len(batch) * 72)
		sqlBuilder.WriteString(`
		INSERT INTO rpc_daily_metrics (
			stat_date, group_name, action_name, client_id, total_requests, success_requests, failed_requests,
			timeout_requests, total_latency_ms, max_latency_ms
		) VALUES `)
		for index, item := range batch {
			if index > 0 {
				sqlBuilder.WriteString(",")
			}
			sqlBuilder.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
			args = append(args,
				item.StatDate, item.GroupName, item.ActionName, item.ClientID, item.TotalRequests,
				item.SuccessRequests, item.FailedRequests, item.TimeoutRequests, item.TotalLatencyMS, item.MaxLatencyMS,
			)
		}
		sqlBuilder.WriteString(`
		ON DUPLICATE KEY UPDATE
			total_requests = total_requests + VALUES(total_requests),
			success_requests = success_requests + VALUES(success_requests),
			failed_requests = failed_requests + VALUES(failed_requests),
			timeout_requests = timeout_requests + VALUES(timeout_requests),
			total_latency_ms = total_latency_ms + VALUES(total_latency_ms),
			max_latency_ms = GREATEST(max_latency_ms, VALUES(max_latency_ms))
	`)
		if _, err := tx.ExecContext(ctx, sqlBuilder.String(), args...); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
func (s *Store) CleanupOldRequests(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		retentionDays = 3
	}
	_, err := s.DB.ExecContext(ctx, "DELETE FROM rpc_requests WHERE created_at < DATE_SUB(NOW(), INTERVAL ? DAY)", retentionDays)
	return err
}

func (s *Store) TrimRPCRequestsForScope(ctx context.Context, groupName, actionName, clientID string, keep int) error {
	if keep <= 0 {
		keep = 100
	}
	_, err := s.DB.ExecContext(ctx, `
		DELETE FROM rpc_requests
		WHERE group_name = ?
		  AND action_name = ?
		  AND client_id = ?
		  AND id NOT IN (
			SELECT id FROM (
				SELECT id
				FROM rpc_requests
				WHERE group_name = ?
				  AND action_name = ?
				  AND client_id = ?
				ORDER BY created_at DESC, id DESC
				LIMIT ?
			) AS keep_rows
		  )
	`, groupName, actionName, clientID, groupName, actionName, clientID, keep)
	return err
}

func (s *Store) TrimAllRPCRequestScopes(ctx context.Context, keep int) error {
	if keep <= 0 {
		keep = 100
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT group_name, action_name, client_id
		FROM rpc_requests
		GROUP BY group_name, action_name, client_id
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var groupName, actionName, clientID string
		if err := rows.Scan(&groupName, &actionName, &clientID); err != nil {
			return err
		}
		if err := s.TrimRPCRequestsForScope(ctx, groupName, actionName, clientID, keep); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Store) CleanupOldMetrics(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		retentionDays = 30
	}
	cutoffDate := time.Now().AddDate(0, 0, -(retentionDays - 1)).Format("2006-01-02")
	if _, err := s.DB.ExecContext(ctx, "DELETE FROM device_daily_metrics WHERE stat_date < ?", cutoffDate); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, "DELETE FROM rpc_daily_metrics WHERE stat_date < ?", cutoffDate)
	return err
}

func (s *Store) RebuildRecentDeviceMetricsFromRequests(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		retentionDays = 3
	}
	cutoffDate := time.Now().AddDate(0, 0, -(retentionDays - 1)).Format("2006-01-02")
	cutoffStart := cutoffDate + " 00:00:00"

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, "DELETE FROM device_daily_metrics WHERE stat_date >= ?", cutoffDate); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO device_daily_metrics (
			stat_date, client_id, group_name, total_requests, success_requests, failed_requests,
			timeout_requests, total_latency_ms, max_latency_ms
		)
		SELECT
			DATE(created_at) AS stat_date,
			client_id,
			group_name,
			COUNT(*) AS total_requests,
			SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) AS success_requests,
			SUM(CASE WHEN status NOT IN ('success', 'timeout', 'pending') THEN 1 ELSE 0 END) AS failed_requests,
			SUM(CASE WHEN status = 'timeout' THEN 1 ELSE 0 END) AS timeout_requests,
			SUM(latency_ms) AS total_latency_ms,
			MAX(latency_ms) AS max_latency_ms
		FROM rpc_requests
		WHERE client_id <> ''
		  AND created_at >= ?
		  AND status <> 'pending'
		GROUP BY DATE(created_at), client_id, group_name
	`, cutoffStart); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *Store) ListRPCRequests(ctx context.Context, groupName, actionName, clientID, status string, page, pageSize int) ([]model.RPCRequest, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	args := []any{}
	var parts []string
	if groupName != "" {
		parts = append(parts, "group_name = ?")
		args = append(args, groupName)
	}
	if actionName != "" {
		parts = append(parts, "action_name = ?")
		args = append(args, actionName)
	}
	if clientID != "" {
		parts = append(parts, "client_id = ?")
		args = append(args, clientID)
	}
	if status != "" {
		parts = append(parts, "status = ?")
		args = append(args, status)
	}

	whereClause := ""
	if len(parts) > 0 {
		whereClause = " WHERE " + strings.Join(parts, " AND ")
	}

	countQuery := "SELECT COUNT(1) FROM rpc_requests" + whereClause
	var total int64
	if err := s.DB.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []model.RPCRequest{}, 0, nil
	}

	query := `
		SELECT id, request_id, group_name, action_name, client_id, requester_user_id,
		       COALESCE(CAST(request_payload_json AS CHAR), ''),
		       COALESCE(CAST(response_payload_json AS CHAR), ''),
		       status, http_code, latency_ms, error_message, created_at, finished_at
		FROM rpc_requests
	` + whereClause + ` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`
	queryArgs := append(append([]any{}, args...), pageSize, offset)

	rows, err := s.DB.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var result []model.RPCRequest
	for rows.Next() {
		var item model.RPCRequest
		if err := rows.Scan(
			&item.ID, &item.RequestID, &item.GroupName, &item.ActionName, &item.ClientID, &item.RequesterUserID,
			&item.RequestPayloadJSON, &item.ResponsePayloadJSON, &item.Status, &item.HTTPCode, &item.LatencyMS,
			&item.ErrorMessage, &item.CreatedAt, &item.FinishedAt,
		); err != nil {
			return nil, 0, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return result, total, nil
}

func (s *Store) ListRPCRequestFilterOptions(ctx context.Context, groupName, actionName, clientID string) (*model.RequestFilterOptions, error) {
	groups, err := s.listDistinctRPCRequestValues(ctx, "group_name", "", actionName, clientID)
	if err != nil {
		return nil, err
	}
	actions, err := s.listDistinctRPCRequestValues(ctx, "action_name", groupName, "", clientID)
	if err != nil {
		return nil, err
	}
	clientIDs, err := s.listDistinctRPCRequestValues(ctx, "client_id", groupName, actionName, "")
	if err != nil {
		return nil, err
	}
	return &model.RequestFilterOptions{
		Groups:    groups,
		Actions:   actions,
		ClientIDs: clientIDs,
	}, nil
}

func (s *Store) listDistinctRPCRequestValues(ctx context.Context, column, groupName, actionName, clientID string) ([]string, error) {
	switch column {
	case "group_name", "action_name", "client_id":
	default:
		return nil, fmt.Errorf("unsupported column: %s", column)
	}

	query := fmt.Sprintf(
		"SELECT DISTINCT r.%s FROM rpc_requests r WHERE COALESCE(TRIM(r.%s), '') <> '' AND EXISTS (SELECT 1 FROM devices d WHERE d.group_name = r.group_name)",
		column, column,
	)
	args := make([]any, 0, 3)

	if column != "group_name" && groupName != "" {
		query += " AND r.group_name = ?"
		args = append(args, groupName)
	}
	if column != "action_name" && actionName != "" {
		query += " AND r.action_name = ?"
		args = append(args, actionName)
	}
	if column != "client_id" && clientID != "" {
		query += " AND r.client_id = ?"
		args = append(args, clientID)
	}
	if column == "client_id" {
		query += " AND EXISTS (SELECT 1 FROM devices dc WHERE dc.group_name = r.group_name AND dc.client_id = r.client_id)"
	}

	query += fmt.Sprintf(" ORDER BY r.%s ASC LIMIT 200", column)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, rows.Err()
}
func (s *Store) GroupSummary(ctx context.Context, hours int) ([]map[string]any, error) {
	if hours <= 0 {
		hours = 24
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT r.group_name, r.action_name, COUNT(*) AS total, "+
		"SUM(CASE WHEN r.status = 'success' THEN 1 ELSE 0 END) AS success_count, "+
		"SUM(CASE WHEN r.status = 'timeout' THEN 1 ELSE 0 END) AS timeout_count, "+
		"AVG(r.latency_ms) AS avg_latency_ms "+
		"FROM rpc_requests r "+
		"WHERE r.created_at >= DATE_SUB(NOW(), INTERVAL ? HOUR) "+
		"AND EXISTS (SELECT 1 FROM devices d WHERE d.group_name = r.group_name) "+
		"GROUP BY r.group_name, r.action_name ORDER BY total DESC",
		hours,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]any
	for rows.Next() {
		var groupName, actionName string
		var total, successCount, timeoutCount int64
		var avgLatency sql.NullFloat64
		if err := rows.Scan(&groupName, &actionName, &total, &successCount, &timeoutCount, &avgLatency); err != nil {
			return nil, err
		}
		result = append(result, map[string]any{
			"group":        groupName,
			"action":       actionName,
			"total":        total,
			"success":      successCount,
			"timeout":      timeoutCount,
			"avgLatencyMs": int64(avgLatency.Float64),
		})
	}
	return result, rows.Err()
}

func (s *Store) WeeklyMetrics(ctx context.Context, groupName, clientID string) ([]model.WeeklyMetric, error) {
	args := []any{}
	query := `
		SELECT client_id, group_name, SUM(total_requests), SUM(success_requests), SUM(failed_requests),
		       SUM(timeout_requests),
		       CASE WHEN SUM(total_requests) = 0 THEN 0 ELSE SUM(total_latency_ms) / SUM(total_requests) END AS avg_latency,
		       MAX(max_latency_ms)
		FROM device_daily_metrics
		WHERE stat_date >= DATE_SUB(CURDATE(), INTERVAL 6 DAY)
	`
	if groupName != "" {
		query += " AND group_name = ?"
		args = append(args, groupName)
	}
	if clientID != "" {
		query += " AND client_id = ?"
		args = append(args, clientID)
	}
	query += " GROUP BY client_id, group_name ORDER BY SUM(total_requests) DESC"

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []model.WeeklyMetric
	for rows.Next() {
		var item model.WeeklyMetric
		var avgLatency sql.NullString
		if err := rows.Scan(
			&item.ClientID, &item.GroupName, &item.TotalRequests, &item.SuccessRequests, &item.FailedRequests,
			&item.TimeoutRequests, &avgLatency, &item.MaxLatencyMS,
		); err != nil {
			return nil, err
		}
		if avgLatency.Valid && avgLatency.String != "" {
			if value, parseErr := strconv.ParseFloat(avgLatency.String, 64); parseErr == nil {
				item.AvgLatencyMS = int64(value)
			}
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ClientDailyMetrics(ctx context.Context, clientID string, days int) ([]model.DailyMetric, error) {
	if days <= 0 || days > 30 {
		days = 7
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT DATE_FORMAT(stat_date, '%Y-%m-%d'), client_id, group_name, total_requests, success_requests,
		       failed_requests, timeout_requests, total_latency_ms, max_latency_ms
		FROM device_daily_metrics
		WHERE client_id = ? AND stat_date >= DATE_SUB(CURDATE(), INTERVAL ? DAY)
		ORDER BY stat_date DESC
	`, clientID, days-1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []model.DailyMetric
	for rows.Next() {
		var item model.DailyMetric
		if err := rows.Scan(
			&item.StatDate, &item.ClientID, &item.GroupName, &item.TotalRequests, &item.SuccessRequests,
			&item.FailedRequests, &item.TimeoutRequests, &item.TotalLatencyMS, &item.MaxLatencyMS,
		); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ListDevices(ctx context.Context, groupName, clientID, status string, limit int) ([]model.Device, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	args := []any{}
	var parts []string
	query := `
		SELECT id, client_id, user_id, group_name, platform, status, last_seen_at, last_ip,
		       COALESCE(extra_json, ''), created_at, updated_at
		FROM devices
	`
	if groupName != "" {
		parts = append(parts, "group_name = ?")
		args = append(args, groupName)
	}
	if clientID != "" {
		parts = append(parts, "client_id = ?")
		args = append(args, clientID)
	}
	if status != "" {
		parts = append(parts, "status = ?")
		args = append(args, status)
	}
	if len(parts) > 0 {
		query += " WHERE " + strings.Join(parts, " AND ")
	}
	query += " ORDER BY last_seen_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []model.Device
	for rows.Next() {
		var item model.Device
		if err := rows.Scan(
			&item.ID, &item.ClientID, &item.UserID, &item.GroupName, &item.Platform,
			&item.Status, &item.LastSeenAt, &item.LastIP, &item.ExtraJSON, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ListGroups(ctx context.Context) ([]model.GroupInfo, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT g.group_name, "+
		"COALESCE(d.total_devices, 0) AS total_devices, "+
		"COALESCE(d.online_devices, 0) AS online_devices, "+
		"d.last_seen_at, "+
		"COALESCE(m.requests_7d, 0) AS requests_7d, "+
		"COALESCE(m.success_7d, 0) AS success_7d, "+
		"m.last_request_at "+
		"FROM (SELECT DISTINCT group_name FROM devices WHERE COALESCE(TRIM(group_name), '') <> '') g "+
		"LEFT JOIN ("+
		"  SELECT group_name, COUNT(*) AS total_devices, "+
		"         SUM(CASE WHEN status = 'online' THEN 1 ELSE 0 END) AS online_devices, "+
		"         MAX(last_seen_at) AS last_seen_at "+
		"  FROM devices GROUP BY group_name"+
		") d ON d.group_name = g.group_name "+
		"LEFT JOIN ("+
		"  SELECT group_name, "+
		"         SUM(CASE WHEN stat_date >= DATE_SUB(CURDATE(), INTERVAL 6 DAY) THEN total_requests ELSE 0 END) AS requests_7d, "+
		"         SUM(CASE WHEN stat_date >= DATE_SUB(CURDATE(), INTERVAL 6 DAY) THEN success_requests ELSE 0 END) AS success_7d, "+
		"         MAX(updated_at) AS last_request_at "+
		"  FROM rpc_daily_metrics GROUP BY group_name"+
		") m ON m.group_name = g.group_name "+
		"ORDER BY g.group_name ASC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []model.GroupInfo
	for rows.Next() {
		var item model.GroupInfo
		var lastSeenAt sql.NullTime
		var lastRequestAt sql.NullTime
		if err := rows.Scan(&item.GroupName, &item.TotalDevices, &item.OnlineDevices, &lastSeenAt, &item.Requests7d, &item.Success7d, &lastRequestAt); err != nil {
			return nil, err
		}
		if lastSeenAt.Valid {
			item.LastSeenAt = &lastSeenAt.Time
		}
		if lastRequestAt.Valid {
			item.LastRequestAt = &lastRequestAt.Time
		}
		if item.Requests7d > 0 {
			item.SuccessRate = float64(item.Success7d) * 100 / float64(item.Requests7d)
		}
		now := time.Now()
		switch {
		case item.TotalDevices == 0:
			item.Status = "no_device"
			item.StatusLabel = "No Device"
		case item.OnlineDevices > 0:
			item.Status = "online"
			item.StatusLabel = "Online"
		case item.LastSeenAt != nil && item.LastSeenAt.Before(now.AddDate(0, 0, -7)):
			item.Status = "stale"
			item.StatusLabel = "Stale"
		default:
			item.Status = "offline"
			item.StatusLabel = "Offline"
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) TrendMetrics(ctx context.Context, groupName, actionName, clientID string, days int) ([]model.TrendPoint, error) {
	if days <= 0 || days > 30 {
		days = 7
	}
	args := []any{days - 1}
	query := "SELECT DATE_FORMAT(stat_date, '%Y-%m-%d') AS stat_date, " +
		"SUM(total_requests) AS total_requests, " +
		"SUM(success_requests) AS success_requests, " +
		"SUM(failed_requests) AS failed_requests, " +
		"SUM(timeout_requests) AS timeout_requests, " +
		"SUM(total_latency_ms) AS total_latency_ms, " +
		"MAX(max_latency_ms) AS max_latency_ms " +
		"FROM rpc_daily_metrics m " +
		"WHERE stat_date >= DATE_SUB(CURDATE(), INTERVAL ? DAY) " +
		"AND EXISTS (SELECT 1 FROM devices d WHERE d.group_name = m.group_name)"
	if groupName != "" {
		query += " AND m.group_name = ?"
		args = append(args, groupName)
	}
	if actionName != "" {
		query += " AND m.action_name = ?"
		args = append(args, actionName)
	}
	if clientID != "" {
		query += " AND m.client_id = ?"
		args = append(args, clientID)
	}
	query += " GROUP BY stat_date ORDER BY stat_date ASC"

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pointsByDate := map[string]model.TrendPoint{}
	for rows.Next() {
		var item model.TrendPoint
		var totalLatencyMS int64
		if err := rows.Scan(&item.StatDate, &item.TotalRequests, &item.SuccessRequests, &item.FailedRequests, &item.TimeoutRequests, &totalLatencyMS, &item.MaxLatencyMS); err != nil {
			return nil, err
		}
		if item.TotalRequests > 0 {
			item.AvgLatencyMS = totalLatencyMS / item.TotalRequests
			item.SuccessRate = float64(item.SuccessRequests) * 100 / float64(item.TotalRequests)
		}
		pointsByDate[item.StatDate] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make([]model.TrendPoint, 0, days)
	for i := days - 1; i >= 0; i-- {
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		point, ok := pointsByDate[date]
		if !ok {
			point = model.TrendPoint{StatDate: date}
		}
		result = append(result, point)
	}
	return result, nil
}

func nullableJSON(raw string) any {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return raw
}

func configureDBSession(ctx context.Context, db *sql.DB, cfg config.Config) error {
	_, err := db.ExecContext(ctx, "SET time_zone = ?", cfg.TimeZoneOffsetString())
	return err
}
