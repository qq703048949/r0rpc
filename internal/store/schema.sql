CREATE DATABASE IF NOT EXISTS `r0rpc` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE `r0rpc`;

CREATE TABLE IF NOT EXISTS users (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    username VARCHAR(64) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    role ENUM('admin', 'client') NOT NULL DEFAULT 'client',
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    can_rpc TINYINT(1) NOT NULL DEFAULT 1,
    notes VARCHAR(255) NOT NULL DEFAULT '',
    last_login_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_users_role_enabled (role, enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS devices (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    client_id VARCHAR(128) NOT NULL UNIQUE,
    user_id BIGINT NOT NULL,
    group_name VARCHAR(128) NOT NULL,
    platform VARCHAR(64) NOT NULL DEFAULT 'xposed',
    status ENUM('online', 'offline') NOT NULL DEFAULT 'offline',
    last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_ip VARCHAR(64) NOT NULL DEFAULT '',
    extra_json LONGTEXT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_devices_user FOREIGN KEY (user_id) REFERENCES users(id),
    INDEX idx_devices_group_status (group_name, status),
    INDEX idx_devices_group_last_seen (group_name, last_seen_at),
    INDEX idx_devices_group_status_last_seen (group_name, status, last_seen_at),
    INDEX idx_devices_status_last_seen (status, last_seen_at),
    INDEX idx_devices_user (user_id),
    INDEX idx_devices_last_seen (last_seen_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS rpc_requests (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    request_id VARCHAR(64) NOT NULL UNIQUE,
    group_name VARCHAR(128) NOT NULL,
    action_name VARCHAR(128) NOT NULL,
    client_id VARCHAR(128) NOT NULL,
    requester_user_id BIGINT NULL,
    request_payload_json LONGTEXT NULL,
    response_payload_json LONGTEXT NULL,
    status ENUM('pending', 'success', 'error', 'timeout', 'no_client', 'rejected') NOT NULL DEFAULT 'pending',
    http_code INT NOT NULL DEFAULT 200,
    latency_ms BIGINT NOT NULL DEFAULT 0,
    error_message VARCHAR(255) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at DATETIME NULL,
    INDEX idx_rpc_requests_lookup (group_name, action_name, client_id, created_at),
    INDEX idx_rpc_requests_group_client_created (group_name, client_id, created_at),
    INDEX idx_rpc_requests_client_created (client_id, created_at),
    INDEX idx_rpc_requests_action_created (action_name, created_at),
    INDEX idx_rpc_requests_created_group_action (created_at, group_name, action_name),
    INDEX idx_rpc_requests_created_at (created_at),
    INDEX idx_rpc_requests_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS device_daily_metrics (
    stat_date DATE NOT NULL,
    client_id VARCHAR(128) NOT NULL,
    group_name VARCHAR(128) NOT NULL,
    total_requests BIGINT NOT NULL DEFAULT 0,
    success_requests BIGINT NOT NULL DEFAULT 0,
    failed_requests BIGINT NOT NULL DEFAULT 0,
    timeout_requests BIGINT NOT NULL DEFAULT 0,
    total_latency_ms BIGINT NOT NULL DEFAULT 0,
    max_latency_ms BIGINT NOT NULL DEFAULT 0,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (stat_date, client_id, group_name),
    INDEX idx_device_daily_metrics_client_date (client_id, stat_date),
    INDEX idx_device_daily_metrics_group_date (group_name, stat_date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS rpc_daily_metrics (
    stat_date DATE NOT NULL,
    group_name VARCHAR(128) NOT NULL,
    action_name VARCHAR(128) NOT NULL,
    client_id VARCHAR(128) NOT NULL DEFAULT '',
    total_requests BIGINT NOT NULL DEFAULT 0,
    success_requests BIGINT NOT NULL DEFAULT 0,
    failed_requests BIGINT NOT NULL DEFAULT 0,
    timeout_requests BIGINT NOT NULL DEFAULT 0,
    total_latency_ms BIGINT NOT NULL DEFAULT 0,
    max_latency_ms BIGINT NOT NULL DEFAULT 0,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (stat_date, group_name, action_name, client_id),
    INDEX idx_rpc_daily_metrics_group_date (group_name, stat_date),
    INDEX idx_rpc_daily_metrics_action_date (action_name, stat_date),
    INDEX idx_rpc_daily_metrics_client_date (client_id, stat_date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;



