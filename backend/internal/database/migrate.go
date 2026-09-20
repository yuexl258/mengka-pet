package database

import (
	"database/sql"
	"strings"
)

const schema = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'user',
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS wallets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL UNIQUE,
    coins INTEGER NOT NULL DEFAULT 0 CHECK (coins >= 0),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS wallet_transactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    wallet_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    amount INTEGER NOT NULL,
    balance_after INTEGER NOT NULL CHECK (balance_after >= 0),
    transaction_type TEXT NOT NULL,
    reference_id TEXT,
    description TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (wallet_id) REFERENCES wallets(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS system_settings (
    setting_key TEXT PRIMARY KEY,
    setting_value TEXT NOT NULL,
    updated_by INTEGER,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS admin_audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    admin_user_id INTEGER NOT NULL,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT,
    details TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (admin_user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_settings_updated_by ON system_settings(updated_by);
CREATE INDEX IF NOT EXISTS idx_audit_admin_user_id ON admin_audit_logs(admin_user_id);
CREATE INDEX IF NOT EXISTS idx_audit_resource ON admin_audit_logs(resource_type, resource_id);

CREATE TABLE IF NOT EXISTS redeem_codes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code_hash TEXT NOT NULL UNIQUE,
    code_ciphertext TEXT,
    amount INTEGER NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL DEFAULT 'unused' CHECK (status IN ('unused', 'redeemed')),
    created_by INTEGER NOT NULL,
    redeemed_by INTEGER,
    redeemed_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (redeemed_by) REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_redeem_codes_status ON redeem_codes(status);
CREATE INDEX IF NOT EXISTS idx_redeem_codes_created_by ON redeem_codes(created_by);

CREATE TABLE IF NOT EXISTS announcements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'archived')),
    created_by INTEGER NOT NULL,
    updated_by INTEGER,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_announcements_status ON announcements(status, id);

CREATE TABLE IF NOT EXISTS mokant_nodes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    host TEXT NOT NULL,
    port INTEGER NOT NULL CHECK (port >= 1 AND port <= 65535),
    token TEXT NOT NULL DEFAULT '',
    is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_mokant_nodes_default ON mokant_nodes(is_default) WHERE is_default = 1;

CREATE TABLE IF NOT EXISTS qq_bindings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    qq_number TEXT NOT NULL UNIQUE,
    user_id INTEGER,
    node_id INTEGER NOT NULL DEFAULT 1,
	login_node_id INTEGER NOT NULL DEFAULT 0,
	login_node_name TEXT NOT NULL DEFAULT '',
    service_months INTEGER NOT NULL DEFAULT 1,
    service_expires_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL,
    FOREIGN KEY (node_id) REFERENCES mokant_nodes(id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS qq_plans (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    price INTEGER NOT NULL CHECK (price >= 0),
    service_days INTEGER NOT NULL CHECK (service_days > 0),
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_qq_plans_status_sort ON qq_plans(status, sort_order, id);

CREATE INDEX IF NOT EXISTS idx_qq_bindings_user_id ON qq_bindings(user_id);

CREATE TABLE IF NOT EXISTS qq_friends (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    qq_binding_id INTEGER NOT NULL,
    friend_qq TEXT NOT NULL,
    nickname TEXT NOT NULL DEFAULT '',
    has_pet INTEGER NOT NULL DEFAULT 0 CHECK (has_pet IN (0, 1)),
    friend_data TEXT NOT NULL DEFAULT '{}',
    pet_id TEXT NOT NULL DEFAULT '',
    pet_data TEXT,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (qq_binding_id, friend_qq),
    FOREIGN KEY (qq_binding_id) REFERENCES qq_bindings(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_qq_friends_binding ON qq_friends(qq_binding_id, id);
CREATE INDEX IF NOT EXISTS idx_qq_friends_auto_poke ON qq_friends(qq_binding_id, has_pet, updated_at, friend_qq);

CREATE TABLE IF NOT EXISTS qq_friend_refresh_states (
    qq_binding_id INTEGER PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'idle',
    message TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    started_at TEXT,
    finished_at TEXT,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    total_count INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (qq_binding_id) REFERENCES qq_bindings(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS pet_profiles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    qq_binding_id INTEGER NOT NULL UNIQUE,
    pet_id TEXT NOT NULL,
    pet_name TEXT NOT NULL DEFAULT '',
    story_id TEXT NOT NULL DEFAULT '',
    baseline_attributes TEXT,
    baseline_gold INTEGER,
    pk_power INTEGER,
    baseline_pk_power INTEGER,
    dominant_type INTEGER,
    pk_updated_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (qq_binding_id) REFERENCES qq_bindings(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_pet_profiles_pet_id ON pet_profiles(pet_id);

CREATE TABLE IF NOT EXISTS pet_pk_strangers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL UNIQUE,
    pet_id TEXT NOT NULL DEFAULT '',
    pet_name TEXT NOT NULL DEFAULT '',
    nickname TEXT NOT NULL DEFAULT '',
    power INTEGER,
    dominant_type INTEGER,
    raw_data TEXT NOT NULL DEFAULT '{}',
    imported_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    power_updated_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_pet_pk_strangers_updated ON pet_pk_strangers(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_pet_pk_strangers_pet ON pet_pk_strangers(pet_id);

CREATE TABLE IF NOT EXISTS pet_daily_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    qq_binding_id INTEGER NOT NULL,
    snapshot_date TEXT NOT NULL,
    attributes TEXT NOT NULL DEFAULT '{}',
    gold INTEGER,
    pk_power INTEGER,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (qq_binding_id, snapshot_date),
    FOREIGN KEY (qq_binding_id) REFERENCES qq_bindings(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_pet_daily_snapshots_binding_date ON pet_daily_snapshots(qq_binding_id, snapshot_date DESC);

CREATE TABLE IF NOT EXISTS pet_auto_pk_configs (
    qq_binding_id INTEGER PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
    target_starts INTEGER NOT NULL DEFAULT 10 CHECK (target_starts >= 0),
    start_time TEXT NOT NULL DEFAULT '01:00',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (qq_binding_id) REFERENCES qq_bindings(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS pet_auto_pk_internal_pets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL,
    pet_id TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_pet_auto_pk_internal_pets_id ON pet_auto_pk_internal_pets(id);

CREATE TABLE IF NOT EXISTS pet_auto_pk_states (
    qq_binding_id INTEGER PRIMARY KEY,
    running INTEGER NOT NULL DEFAULT 0 CHECK (running IN (0, 1)),
    successful_starts INTEGER NOT NULL DEFAULT 0 CHECK (successful_starts >= 0),
    last_opponent_user_id TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '未启动自动 PK',
    last_error TEXT NOT NULL DEFAULT '',
    last_run_at TEXT,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (qq_binding_id) REFERENCES qq_bindings(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS pet_auto_pk_daily_opponents (
    qq_binding_id INTEGER NOT NULL,
    opponent_user_id TEXT NOT NULL,
    run_date TEXT NOT NULL,
    start_count INTEGER NOT NULL DEFAULT 0 CHECK (start_count >= 0),
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (qq_binding_id, opponent_user_id, run_date),
    FOREIGN KEY (qq_binding_id) REFERENCES qq_bindings(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_pet_auto_pk_daily_binding_date ON pet_auto_pk_daily_opponents(qq_binding_id, run_date);

CREATE TABLE IF NOT EXISTS pet_auto_pk_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    qq_binding_id INTEGER NOT NULL,
    action TEXT NOT NULL,
    opponent_user_id TEXT NOT NULL DEFAULT '0',
    opponent_pet_id TEXT NOT NULL DEFAULT '',
    opponent_power INTEGER NOT NULL DEFAULT 0,
    success INTEGER NOT NULL DEFAULT 0 CHECK (success IN (0, 1)),
    message TEXT NOT NULL DEFAULT '',
    details TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (qq_binding_id) REFERENCES qq_bindings(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_pet_auto_pk_logs_binding_created ON pet_auto_pk_logs(qq_binding_id, id DESC);

CREATE TABLE IF NOT EXISTS pet_auto_control_configs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    qq_binding_id INTEGER NOT NULL UNIQUE,
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
    learning_target TEXT NOT NULL DEFAULT '力量',
    activity_order TEXT NOT NULL DEFAULT '["school","work","adventure"]',
    plan_json TEXT NOT NULL DEFAULT '[]',
    school_count INTEGER NOT NULL DEFAULT 0 CHECK (school_count >= 0),
    work_count INTEGER NOT NULL DEFAULT 0 CHECK (work_count >= 0),
    adventure_count INTEGER NOT NULL DEFAULT 0 CHECK (adventure_count >= 0),
    school_remaining INTEGER NOT NULL DEFAULT 0 CHECK (school_remaining >= 0),
    work_remaining INTEGER NOT NULL DEFAULT 0 CHECK (work_remaining >= 0),
    adventure_remaining INTEGER NOT NULL DEFAULT 0 CHECK (adventure_remaining >= 0),
    school_executed INTEGER NOT NULL DEFAULT 0 CHECK (school_executed >= 0),
    work_executed INTEGER NOT NULL DEFAULT 0 CHECK (work_executed >= 0),
    adventure_executed INTEGER NOT NULL DEFAULT 0 CHECK (adventure_executed >= 0),
    school_control_mode TEXT NOT NULL DEFAULT 'count' CHECK (school_control_mode IN ('count', 'duration')),
    work_control_mode TEXT NOT NULL DEFAULT 'count' CHECK (work_control_mode IN ('count', 'duration')),
    adventure_control_mode TEXT NOT NULL DEFAULT 'count' CHECK (adventure_control_mode IN ('count', 'duration')),
    school_duration_seconds INTEGER NOT NULL DEFAULT 0 CHECK (school_duration_seconds >= 0),
    work_duration_seconds INTEGER NOT NULL DEFAULT 0 CHECK (work_duration_seconds >= 0),
    adventure_duration_seconds INTEGER NOT NULL DEFAULT 0 CHECK (adventure_duration_seconds >= 0),
    school_duration_executed INTEGER NOT NULL DEFAULT 0 CHECK (school_duration_executed >= 0),
    work_duration_executed INTEGER NOT NULL DEFAULT 0 CHECK (work_duration_executed >= 0),
    adventure_duration_executed INTEGER NOT NULL DEFAULT 0 CHECK (adventure_duration_executed >= 0),
    work_option TEXT NOT NULL DEFAULT '',
    auto_feed_enabled INTEGER NOT NULL DEFAULT 0 CHECK (auto_feed_enabled IN (0, 1)),
    auto_feed_threshold INTEGER NOT NULL DEFAULT 30 CHECK (auto_feed_threshold >= 0),
    auto_feed_food TEXT NOT NULL DEFAULT '饼干',
    auto_bathe_enabled INTEGER NOT NULL DEFAULT 0 CHECK (auto_bathe_enabled IN (0, 1)),
    auto_bathe_threshold INTEGER NOT NULL DEFAULT 30 CHECK (auto_bathe_threshold >= 0),
    auto_bathe_item TEXT NOT NULL DEFAULT '香皂片',
    encourage_probability INTEGER NOT NULL DEFAULT 100 CHECK (encourage_probability BETWEEN 0 AND 100),
    auto_poke_enabled INTEGER NOT NULL DEFAULT 0 CHECK (auto_poke_enabled IN (0, 1)),
    auto_poke_daily_limit INTEGER NOT NULL DEFAULT 0 CHECK (auto_poke_daily_limit >= 0),
    auto_poke_daily_count INTEGER NOT NULL DEFAULT 0 CHECK (auto_poke_daily_count >= 0),
    auto_poke_interval_seconds INTEGER NOT NULL DEFAULT 1 CHECK (auto_poke_interval_seconds >= 1),
    auto_poke_last_run_at TEXT,
    auto_poke_last_friend_qq TEXT NOT NULL DEFAULT '',
    last_reset_date TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (qq_binding_id) REFERENCES qq_bindings(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS pet_auto_control_states (
    qq_binding_id INTEGER PRIMARY KEY,
    running INTEGER NOT NULL DEFAULT 0 CHECK (running IN (0, 1)),
    current_activity TEXT NOT NULL DEFAULT '',
    current_option TEXT NOT NULL DEFAULT '',
    current_story_id TEXT NOT NULL DEFAULT '',
    current_plan_item_id TEXT NOT NULL DEFAULT '',
    current_duration_seconds INTEGER NOT NULL DEFAULT 0,
    message TEXT NOT NULL DEFAULT '未启动自动控制',
    last_action TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    last_run_at TEXT,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (qq_binding_id) REFERENCES qq_bindings(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS pet_auto_control_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    qq_binding_id INTEGER NOT NULL,
    action TEXT NOT NULL,
    activity TEXT NOT NULL DEFAULT '',
    option_name TEXT NOT NULL DEFAULT '',
    success INTEGER NOT NULL DEFAULT 0 CHECK (success IN (0, 1)),
    message TEXT NOT NULL DEFAULT '',
    details TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (qq_binding_id) REFERENCES qq_bindings(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_pet_auto_control_logs_binding_created ON pet_auto_control_logs(qq_binding_id, id DESC);

INSERT OR IGNORE INTO system_settings (setting_key, setting_value) VALUES ('registration_enabled', 'true');
INSERT OR IGNORE INTO system_settings (setting_key, setting_value) VALUES ('register_gift_coins', '0');
INSERT OR IGNORE INTO system_settings (setting_key, setting_value) VALUES ('system_name', 'QQ 宠物');
INSERT OR IGNORE INTO system_settings (setting_key, setting_value) VALUES ('official_bot_app_id', '');
INSERT OR IGNORE INTO system_settings (setting_key, setting_value) VALUES ('official_bot_client_secret', '');
INSERT OR IGNORE INTO system_settings (setting_key, setting_value) VALUES ('official_bot_group_openid', '');
INSERT OR IGNORE INTO system_settings (setting_key, setting_value) VALUES ('official_bot_offline_enabled', 'false');
INSERT OR IGNORE INTO system_settings (setting_key, setting_value) VALUES ('official_bot_offline_template', '## QQ 掉线提醒\n\n- 系统：{{sys_name}} v{{sys_version}}\n- QQ：{{qq}}\n- 时间：{{time}}\n- 原因：{{reason}}');
INSERT OR IGNORE INTO system_settings (setting_key, setting_value) VALUES ('official_bot_activity_started_enabled', 'false');
INSERT OR IGNORE INTO system_settings (setting_key, setting_value) VALUES ('official_bot_activity_started_template', '## 计划任务开始\n\n- 系统：{{sys_name}} v{{sys_version}}\n- QQ：{{qq}}\n- 活动：{{activity}}\n- {{plan_expectation}}\n- 时间：{{time}}');
UPDATE system_settings SET setting_value = '## 计划任务开始\n\n- 系统：{{sys_name}} v{{sys_version}}\n- QQ：{{qq}}\n- 活动：{{activity}}\n- {{plan_expectation}}\n- 时间：{{time}}' WHERE setting_key = 'official_bot_activity_started_template' AND setting_value = '## 活动开始\n\n- 系统：{{sys_name}} v{{sys_version}}\n- QQ：{{qq}}\n- 活动：{{activity}}\n- 项目：{{option}}\n- 计划时长：{{duration}}\n- 时间：{{time}}';
INSERT OR IGNORE INTO system_settings (setting_key, setting_value) VALUES ('official_bot_activity_completed_enabled', 'false');
INSERT OR IGNORE INTO system_settings (setting_key, setting_value) VALUES ('official_bot_activity_completed_template', '## 计划任务完成\n\n- 系统：{{sys_name}} v{{sys_version}}\n- QQ：{{qq}}\n- 活动：{{activity}}\n- {{plan_expectation}}\n- 今日执行：{{today_summary}}\n- 时间：{{time}}');
UPDATE system_settings SET setting_value = '## 计划任务完成\n\n- 系统：{{sys_name}} v{{sys_version}}\n- QQ：{{qq}}\n- 活动：{{activity}}\n- {{plan_expectation}}\n- 今日执行：{{today_summary}}\n- 时间：{{time}}' WHERE setting_key = 'official_bot_activity_completed_template' AND setting_value = '## 活动完成\n\n- 系统：{{sys_name}} v{{sys_version}}\n- QQ：{{qq}}\n- 活动：{{activity}}\n- 项目：{{option}}\n- 本次时长：{{duration}}\n- 今日执行：{{today_summary}}\n- 时间：{{time}}';
INSERT OR IGNORE INTO mokant_nodes (id, name, host, port, token, is_default)
VALUES (1, '默认节点',
    COALESCE((SELECT setting_value FROM system_settings WHERE setting_key = 'mokant_host'), '127.0.0.1'),
    CAST(COALESCE((SELECT setting_value FROM system_settings WHERE setting_key = 'mokant_port'), '3001') AS INTEGER),
    COALESCE((SELECT setting_value FROM system_settings WHERE setting_key = 'mokant_token'), ''), 1);
INSERT OR IGNORE INTO qq_plans (id, name, price, service_days, description, status, sort_order) VALUES (1, '按月套餐', 0, 30, '按 30 天购买 QQ 服务', 'active', 0);
INSERT OR IGNORE INTO wallets (user_id, coins) SELECT id, 0 FROM users;
`

func Migrate(db *sql.DB) error {
	_, err := db.Exec(schema)
	if err != nil {
		return err
	}
	var legacyOwnerColumn int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('pet_pk_strangers') WHERE name = 'owner_user_id'`).Scan(&legacyOwnerColumn); err != nil {
		return err
	}
	if legacyOwnerColumn > 0 {
		if _, err := db.Exec(`PRAGMA foreign_keys = OFF;
			BEGIN;
			CREATE TABLE pet_pk_strangers_new (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				user_id TEXT NOT NULL UNIQUE,
				pet_id TEXT NOT NULL DEFAULT '', pet_name TEXT NOT NULL DEFAULT '', nickname TEXT NOT NULL DEFAULT '',
				power INTEGER, dominant_type INTEGER, raw_data TEXT NOT NULL DEFAULT '{}',
				imported_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, power_updated_at TEXT
			);
			INSERT OR REPLACE INTO pet_pk_strangers_new (id, user_id, pet_id, pet_name, nickname, power, dominant_type, raw_data, imported_at, updated_at, power_updated_at)
				SELECT id, user_id, pet_id, pet_name, nickname, power, dominant_type, raw_data, imported_at, updated_at, power_updated_at
				FROM pet_pk_strangers ORDER BY id;
			DROP TABLE pet_pk_strangers;
			ALTER TABLE pet_pk_strangers_new RENAME TO pet_pk_strangers;
			CREATE INDEX idx_pet_pk_strangers_updated ON pet_pk_strangers(updated_at DESC);
			CREATE INDEX idx_pet_pk_strangers_pet ON pet_pk_strangers(pet_id);
			COMMIT;
			PRAGMA foreign_keys = ON;`); err != nil {
			_, _ = db.Exec("ROLLBACK; PRAGMA foreign_keys = ON;")
			return err
		}
	}
	if _, err := db.Exec("DROP TABLE IF EXISTS pet_pk_stranger_sources"); err != nil {
		return err
	}
	for _, statement := range []string{
		"ALTER TABLE qq_friends ADD COLUMN pet_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE qq_friend_refresh_states ADD COLUMN total_count INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE qq_bindings ADD COLUMN service_months INTEGER NOT NULL DEFAULT 1",
		"ALTER TABLE qq_bindings ADD COLUMN service_expires_at TEXT",
		"ALTER TABLE qq_bindings ADD COLUMN node_id INTEGER",
		"UPDATE qq_bindings SET node_id = 1 WHERE node_id IS NULL",
		"CREATE INDEX IF NOT EXISTS idx_qq_bindings_node_id ON qq_bindings(node_id)",
		"ALTER TABLE qq_bindings ADD COLUMN login_node_id INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE qq_bindings ADD COLUMN login_node_name TEXT NOT NULL DEFAULT ''",
		"CREATE INDEX IF NOT EXISTS idx_qq_bindings_login_node_id ON qq_bindings(login_node_id)",
		"ALTER TABLE pet_profiles ADD COLUMN story_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE pet_profiles ADD COLUMN baseline_attributes TEXT",
		"ALTER TABLE pet_profiles ADD COLUMN baseline_gold INTEGER",
		"ALTER TABLE pet_profiles ADD COLUMN pk_power INTEGER",
		"ALTER TABLE pet_profiles ADD COLUMN baseline_pk_power INTEGER",
		"ALTER TABLE pet_profiles ADD COLUMN dominant_type INTEGER",
		"ALTER TABLE pet_profiles ADD COLUMN pk_updated_at TEXT",
		"ALTER TABLE pet_auto_pk_logs ADD COLUMN opponent_power INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE pet_auto_pk_configs ADD COLUMN start_time TEXT NOT NULL DEFAULT '00:00'",
		"UPDATE pet_auto_pk_configs SET target_starts = 10, start_time = '01:00', updated_at = CURRENT_TIMESTAMP",
		"ALTER TABLE redeem_codes ADD COLUMN code_ciphertext TEXT",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN auto_feed_enabled INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN auto_feed_threshold INTEGER NOT NULL DEFAULT 30",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN auto_feed_food TEXT NOT NULL DEFAULT '饼干'",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN auto_bathe_enabled INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN auto_bathe_threshold INTEGER NOT NULL DEFAULT 30",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN auto_bathe_item TEXT NOT NULL DEFAULT '香皂片'",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN encourage_probability INTEGER NOT NULL DEFAULT 100",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN auto_poke_enabled INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN auto_poke_start_time TEXT NOT NULL DEFAULT '00:00'",
		"UPDATE pet_auto_control_configs SET auto_poke_start_time = '00:00', updated_at = CURRENT_TIMESTAMP",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN auto_poke_daily_limit INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN auto_poke_daily_count INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN auto_poke_interval_seconds INTEGER NOT NULL DEFAULT 1",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN auto_poke_last_run_at TEXT",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN auto_poke_last_friend_qq TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN last_reset_date TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN school_executed INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN work_executed INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN adventure_executed INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN school_control_mode TEXT NOT NULL DEFAULT 'count'",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN work_control_mode TEXT NOT NULL DEFAULT 'count'",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN adventure_control_mode TEXT NOT NULL DEFAULT 'count'",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN school_duration_seconds INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN work_duration_seconds INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN adventure_duration_seconds INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN school_duration_executed INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN work_duration_executed INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN adventure_duration_executed INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN work_option TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE pet_auto_control_configs ADD COLUMN plan_json TEXT NOT NULL DEFAULT '[]'",
		"ALTER TABLE pet_auto_control_states ADD COLUMN current_story_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE pet_auto_control_states ADD COLUMN current_plan_item_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE pet_auto_control_states ADD COLUMN current_duration_seconds INTEGER NOT NULL DEFAULT 0",
		"UPDATE pet_auto_control_configs SET school_executed = MAX(school_count - school_remaining, 0), work_executed = MAX(work_count - work_remaining, 0), adventure_executed = MAX(adventure_count - adventure_remaining, 0) WHERE school_executed = 0 AND work_executed = 0 AND adventure_executed = 0",
		"DELETE FROM system_settings WHERE setting_key = 'qq_monthly_price'",
	} {
		if _, err = db.Exec(statement); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return nil
}
