package officialbot

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	DefaultTemplate                  = "## QQ 掉线提醒\n\n- 系统：{{sys_name}} v{{sys_version}}\n- QQ：{{qq}}\n- 时间：{{time}}\n- 原因：{{reason}}"
	DefaultActivityStartedTemplate   = "## 计划任务开始\n\n- 系统：{{sys_name}} v{{sys_version}}\n- QQ：{{qq}}\n- 活动：{{activity}}\n- {{plan_expectation}}\n- 时间：{{time}}"
	DefaultActivityCompletedTemplate = "## 计划任务完成\n\n- 系统：{{sys_name}} v{{sys_version}}\n- QQ：{{qq}}\n- 活动：{{activity}}\n- {{plan_expectation}}\n- 今日执行：{{today_summary}}\n- 时间：{{time}}"
)

type Config struct {
	AppID                    string `json:"app_id"`
	ClientSecret             string `json:"client_secret,omitempty"`
	ClientSecretSet          bool   `json:"client_secret_set"`
	GroupOpenID              string `json:"group_openid"`
	OfflineEnabled           bool   `json:"offline_enabled"`
	OfflineTemplate          string `json:"offline_template"`
	ActivityStartedEnabled   bool   `json:"activity_started_enabled"`
	ActivityStartedTemplate  string `json:"activity_started_template"`
	ActivityCompleteEnabled  bool   `json:"activity_completed_enabled"`
	ActivityCompleteTemplate string `json:"activity_completed_template"`
}

type Notifier struct {
	db             *sql.DB
	log            *slog.Logger
	httpClient     *http.Client
	mu             sync.Mutex
	accessToken    string
	tokenExpiresAt time.Time
	version        string
}

func New(db *sql.DB, logger *slog.Logger, version ...string) *Notifier {
	n := &Notifier{db: db, log: logger, httpClient: &http.Client{Timeout: 15 * time.Second}}
	if len(version) > 0 {
		n.version = version[0]
	}
	return n
}

func (n *Notifier) Load(ctx context.Context) (Config, error) {
	values := map[string]string{}
	rows, err := n.db.QueryContext(ctx, "SELECT setting_key, setting_value FROM system_settings WHERE setting_key IN ('official_bot_app_id','official_bot_client_secret','official_bot_group_openid','official_bot_offline_enabled','official_bot_offline_template','official_bot_activity_started_enabled','official_bot_activity_started_template','official_bot_activity_completed_enabled','official_bot_activity_completed_template')")
	if err != nil {
		return Config{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return Config{}, err
		}
		values[key] = value
	}
	offlineTemplate := values["official_bot_offline_template"]
	if offlineTemplate == "" {
		offlineTemplate = DefaultTemplate
	}
	startedTemplate := values["official_bot_activity_started_template"]
	if startedTemplate == "" {
		startedTemplate = DefaultActivityStartedTemplate
	}
	completedTemplate := values["official_bot_activity_completed_template"]
	if completedTemplate == "" {
		completedTemplate = DefaultActivityCompletedTemplate
	}
	secret := values["official_bot_client_secret"]
	return Config{
		AppID: values["official_bot_app_id"], ClientSecret: secret, ClientSecretSet: secret != "", GroupOpenID: values["official_bot_group_openid"],
		OfflineEnabled: values["official_bot_offline_enabled"] == "true", OfflineTemplate: offlineTemplate,
		ActivityStartedEnabled: values["official_bot_activity_started_enabled"] == "true", ActivityStartedTemplate: startedTemplate,
		ActivityCompleteEnabled: values["official_bot_activity_completed_enabled"] == "true", ActivityCompleteTemplate: completedTemplate,
	}, rows.Err()
}

func (n *Notifier) Save(ctx context.Context, cfg Config, updatedBy int64) (Config, error) {
	current, err := n.Load(ctx)
	if err != nil {
		return Config{}, err
	}
	cfg.AppID, cfg.GroupOpenID = strings.TrimSpace(cfg.AppID), strings.TrimSpace(cfg.GroupOpenID)
	cfg.OfflineTemplate = strings.TrimSpace(cfg.OfflineTemplate)
	cfg.ActivityStartedTemplate = strings.TrimSpace(cfg.ActivityStartedTemplate)
	cfg.ActivityCompleteTemplate = strings.TrimSpace(cfg.ActivityCompleteTemplate)
	if cfg.ClientSecret == "" {
		cfg.ClientSecret = current.ClientSecret
	}
	if cfg.OfflineTemplate == "" || cfg.ActivityStartedTemplate == "" || cfg.ActivityCompleteTemplate == "" {
		return Config{}, errors.New("消息模板不能为空")
	}
	tx, err := n.db.BeginTx(ctx, nil)
	if err != nil {
		return Config{}, err
	}
	defer tx.Rollback()
	values := map[string]string{
		"official_bot_app_id": cfg.AppID, "official_bot_client_secret": cfg.ClientSecret, "official_bot_group_openid": cfg.GroupOpenID,
		"official_bot_offline_enabled": fmt.Sprint(cfg.OfflineEnabled), "official_bot_offline_template": cfg.OfflineTemplate,
		"official_bot_activity_started_enabled": fmt.Sprint(cfg.ActivityStartedEnabled), "official_bot_activity_started_template": cfg.ActivityStartedTemplate,
		"official_bot_activity_completed_enabled": fmt.Sprint(cfg.ActivityCompleteEnabled), "official_bot_activity_completed_template": cfg.ActivityCompleteTemplate,
	}
	for key, value := range values {
		if _, err = tx.ExecContext(ctx, "INSERT INTO system_settings(setting_key,setting_value,updated_by,updated_at) VALUES(?,?,?,CURRENT_TIMESTAMP) ON CONFLICT(setting_key) DO UPDATE SET setting_value=excluded.setting_value,updated_by=excluded.updated_by,updated_at=excluded.updated_at", key, value, updatedBy); err != nil {
			return Config{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Config{}, err
	}
	n.mu.Lock()
	n.accessToken = ""
	n.tokenExpiresAt = time.Time{}
	n.mu.Unlock()
	cfg.ClientSecretSet = cfg.ClientSecret != ""
	cfg.ClientSecret = ""
	return cfg, nil
}

func (n *Notifier) SendTest(ctx context.Context) error {
	cfg, err := n.Load(ctx)
	if err != nil {
		return err
	}
	return n.send(ctx, cfg, "## 官方机器人测试消息\n\n配置验证成功，推送可以正常发送。")
}

func (n *Notifier) NotifyActivityStarted(ctx context.Context, qq, activity, option, expectation string) error {
	cfg, err := n.Load(ctx)
	if err != nil || !cfg.ActivityStartedEnabled {
		return err
	}
	return n.send(ctx, cfg, n.render(ctx, cfg.ActivityStartedTemplate, map[string]string{
		"qq": qq, "activity": activity, "option": option, "duration": expectation, "plan_expectation": expectation,
	}))
}

func (n *Notifier) NotifyActivityCompleted(ctx context.Context, qq, activity, option, expectation, todaySummary string) error {
	cfg, err := n.Load(ctx)
	if err != nil || !cfg.ActivityCompleteEnabled {
		return err
	}
	return n.send(ctx, cfg, n.render(ctx, cfg.ActivityCompleteTemplate, map[string]string{
		"qq": qq, "activity": activity, "option": option, "duration": expectation, "plan_expectation": expectation, "today_summary": todaySummary,
	}))
}

func (n *Notifier) NotifyOffline(ctx context.Context, qq, reason string) error {
	cfg, err := n.Load(ctx)
	if err != nil || !cfg.OfflineEnabled {
		return err
	}
	return n.send(ctx, cfg, n.render(ctx, cfg.OfflineTemplate, map[string]string{"qq": qq, "reason": reason}))
}

func (n *Notifier) render(ctx context.Context, template string, values map[string]string) string {
	var systemName string
	_ = n.db.QueryRowContext(ctx, "SELECT setting_value FROM system_settings WHERE setting_key = 'system_name'").Scan(&systemName)
	if qq := values["qq"]; qq != "" {
		values["qq"] = maskQQ(qq)
	}
	values["sys_name"] = systemName
	values["sys_version"] = n.version
	if values["time"] == "" {
		values["time"] = time.Now().Format("2006-01-02 15:04:05")
	}
	replacements := make([]string, 0, len(values)*2)
	for key, value := range values {
		replacements = append(replacements, "{{"+key+"}}", value)
	}
	return strings.NewReplacer(replacements...).Replace(template)
}

func maskQQ(qq string) string {
	characters := []rune(qq)
	if len(characters) <= 5 {
		return "***"
	}
	return string(characters[:3]) + "***" + string(characters[len(characters)-2:])
}

func formatDuration(seconds int64) string {
	if seconds <= 0 {
		return "未知"
	}
	duration := time.Duration(seconds) * time.Second
	if duration%time.Hour == 0 {
		return fmt.Sprintf("%d小时", int64(duration/time.Hour))
	}
	if duration%time.Minute == 0 {
		return fmt.Sprintf("%d分钟", int64(duration/time.Minute))
	}
	return duration.String()
}

func (n *Notifier) HandleEvent(raw []byte) {
	var event struct {
		EventType  string `json:"event_type"`
		SelfID     int64  `json:"self_id"`
		OccurredAt int64  `json:"occurred_at"`
		Reason     string `json:"reason"`
		Summary    string `json:"summary"`
		Message    string `json:"message"`
	}
	if json.Unmarshal(raw, &event) != nil || event.EventType != "account_offline" {
		return
	}

	go func() {
		cfg, err := n.Load(context.Background())
		if err != nil {
			n.log.Error("官方机器人配置加载失败", "error", err)
			return
		}
		if !cfg.OfflineEnabled {
			return
		}

		reason := strings.TrimSpace(event.Reason)
		if reason == "" {
			reason = strings.TrimSpace(event.Summary)
		}
		if reason == "" {
			reason = strings.TrimSpace(event.Message)
		}
		if reason == "" {
			reason = "听雨框架检测到账号离线"
		}
		t := time.Now()
		if event.OccurredAt > 0 {
			t = time.UnixMilli(event.OccurredAt)
		}
		message := n.render(context.Background(), cfg.OfflineTemplate, map[string]string{
			"qq": fmt.Sprint(event.SelfID), "time": t.Format("2006-01-02 15:04:05"), "reason": reason,
		})
		if err := n.send(context.Background(), cfg, message); err != nil {
			n.log.Error("官方机器人掉线提醒发送失败", "qq", event.SelfID, "error", err)
		}
	}()
}

func (n *Notifier) send(ctx context.Context, cfg Config, content string) error {
	if cfg.AppID == "" || cfg.ClientSecret == "" || cfg.GroupOpenID == "" {
		return errors.New("请先完整配置 AppID、ClientSecret 和群 OpenID")
	}
	token, err := n.token(ctx, cfg)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"msg_type": 2, "markdown": map[string]string{"content": content}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.bot.qq.com/v2/groups/"+url.PathEscape(cfg.GroupOpenID)+"/messages", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "QQBot "+token)
	resp, err := n.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("发送失败（HTTP %d）：%s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

func (n *Notifier) token(ctx context.Context, cfg Config) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.accessToken != "" && time.Now().Before(n.tokenExpiresAt) {
		return n.accessToken, nil
	}
	body, _ := json.Marshal(map[string]string{"appId": cfg.AppID, "clientSecret": cfg.ClientSecret})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.bot.qq.com/app/getAppAccessToken", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("获取访问凭证失败（HTTP %d）：%s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var result struct {
		AccessToken string      `json:"access_token"`
		ExpiresIn   json.Number `json:"expires_in"`
	}
	if json.Unmarshal(data, &result) != nil || result.AccessToken == "" {
		return "", errors.New("访问凭证响应无效")
	}
	seconds, _ := result.ExpiresIn.Int64()
	if seconds < 120 {
		seconds = 120
	}
	n.accessToken, n.tokenExpiresAt = result.AccessToken, time.Now().Add(time.Duration(seconds-60)*time.Second)
	return n.accessToken, nil
}
