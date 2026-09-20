package server

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"qq-pet/backend/internal/admin"
	"qq-pet/backend/internal/announcement"
	"qq-pet/backend/internal/auth"
	"qq-pet/backend/internal/autocontrol"
	"qq-pet/backend/internal/autopk"
	"qq-pet/backend/internal/config"
	"qq-pet/backend/internal/logstream"
	"qq-pet/backend/internal/mokant"
	"qq-pet/backend/internal/officialbot"
	"qq-pet/backend/internal/response"
	"qq-pet/backend/internal/timeutil"
	"qq-pet/backend/internal/wallet"

	"github.com/go-chi/chi/v5"
)

//go:embed frontend-dist
var frontendDist embed.FS

var petSnapshotMu sync.Mutex

func NewRouter(db *sql.DB, cfg config.Config, logger *slog.Logger, clients ...any) http.Handler {
	mokantClient := mokant.NewClient(cfg, logger)
	var mokantManager *mokant.Manager
	var autoControlScheduler *autocontrol.Scheduler
	var autoPKScheduler *autopk.Scheduler
	var botNotifier *officialbot.Notifier
	var logHub *logstream.Hub
	for _, item := range clients {
		switch value := item.(type) {
		case *mokant.Client:
			mokantClient = value
		case *mokant.Manager:
			mokantManager = value
		case *autocontrol.Scheduler:
			autoControlScheduler = value
		case *autopk.Scheduler:
			autoPKScheduler = value
		case *officialbot.Notifier:
			botNotifier = value
		case *logstream.Hub:
			logHub = value
		}
	}
	if mokantManager != nil {
		mokantClient = mokant.NewManagerClient(cfg, logger, mokantManager)
	}
	router := chi.NewRouter()
	router.Use(requestIDMiddleware)
	router.Get("/healthz", healthHandler(db))
	router.Get("/api/version", versionHandler(cfg))
	authService := auth.NewService(db, cfg)
	router.Mount("/", authService.Routes())
	if logHub != nil {
		router.With(authService.RequireAdmin).Handle("/ws/admin/mokant/logs", logHub)
	}
	adminService := admin.NewService(db)
	announcementService := announcement.NewService(db)
	router.Get("/api/system/settings", adminService.PublicSettings)
	if mokantManager != nil {
		router.With(authService.RequireAdmin).Get("/api/admin/mokant/status", mokantManagerStatusHandler(mokantManager))
		router.With(authService.RequireAdmin).Post("/api/admin/mokant/connect", mokantManagerConnectionHandler(mokantManager, true))
		router.With(authService.RequireAdmin).Post("/api/admin/mokant/disconnect", mokantManagerConnectionHandler(mokantManager, false))
	} else {
		router.With(authService.RequireAdmin).Get("/api/admin/mokant/status", func(w http.ResponseWriter, r *http.Request) {
			response.JSON(w, http.StatusOK, 0, "ok", mokantClient.Status())
		})
		router.With(authService.RequireAdmin).Post("/api/admin/mokant/connect", mokantConnectHandler(mokantClient))
		router.With(authService.RequireAdmin).Post("/api/admin/mokant/disconnect", mokantDisconnectHandler(mokantClient))
	}
	if mokantManager != nil {
		router.With(authService.RequireAdmin).Get("/api/admin/mokant/nodes", mokantNodesHandler(db, mokantManager))
		router.With(authService.RequireAdmin).Post("/api/admin/mokant/nodes", mokantNodeCreateHandler(db, mokantManager))
		router.With(authService.RequireAdmin).Put("/api/admin/mokant/nodes/{id}", mokantNodeUpdateHandler(db, mokantManager))
		router.With(authService.RequireAdmin).Delete("/api/admin/mokant/nodes/{id}", mokantNodeDeleteHandler(db, mokantManager))
		router.With(authService.RequireAdmin).Post("/api/admin/mokant/nodes/{id}/connect", mokantNodeConnectionHandler(mokantManager, true))
		router.With(authService.RequireAdmin).Post("/api/admin/mokant/nodes/{id}/disconnect", mokantNodeConnectionHandler(mokantManager, false))
		router.With(authService.RequireAdmin).Get("/api/admin/mokant/nodes/{id}/status", mokantNodeStatusHandler(mokantManager))
	}
	router.With(authService.RequireAdmin).Get("/api/admin/mokant/config", mokantConfigGet(db))
	if mokantManager != nil {
		router.With(authService.RequireAdmin).Put("/api/admin/mokant/config", mokantConfigUpdateManager(db, mokantManager))
	} else {
		router.With(authService.RequireAdmin).Put("/api/admin/mokant/config", mokantConfigUpdate(db, mokantClient))
	}
	router.With(authService.RequireAdmin).Get("/api/admin/mokant/bots", mokantBotsHandler(db, mokantClient))
	router.With(authService.RequireAdmin).Post("/api/admin/mokant/debug", mokantDebugHandler(mokantClient))
	router.With(authService.RequireAdmin).Patch("/api/admin/qq-accounts/{qq}/binding", qqBindingUpdateHandler(db, autoControlScheduler))
	router.With(authService.RequireAdmin).Delete("/api/admin/qq-accounts/{qq}/binding", qqBindingDeleteHandler(db, mokantClient, autoControlScheduler, autoPKScheduler))
	router.Get("/api/announcements", announcementService.Public)
	walletService := wallet.NewService(db)
	router.With(authService.RequireUser).Get("/api/qq/account", qqAccountHandler(db, mokantClient))
	router.With(authService.RequireUser).Get("/api/qq/account/{qq}/info", qqAccountInfoHandler(db, mokantClient))
	router.With(authService.RequireUser).Get("/api/pets", petProfilesHandler(db, mokantClient, logger))
	router.With(authService.RequireUser).Get("/api/pets/accounts", petAccountsHandler(db, mokantClient))
	router.With(authService.RequireUser).Get("/api/pets/pk-cache", petPKCacheHandler(db))
	friendsHandler, refreshFriendsHandler, filterPetFriendsHandler, friendRefreshStateHandler, pokeFriendPetHandler, bindFriendPetHandler, friendPetProfileHandler := friendRefreshRoutes(db, mokantClient)
	router.With(authService.RequireUser).Get("/api/pets/{qq}/friends", friendsHandler)
	router.With(authService.RequireUser).Post("/api/pets/{qq}/friends/refresh", refreshFriendsHandler)
	router.With(authService.RequireUser).Post("/api/pets/{qq}/friends/filter", filterPetFriendsHandler)
	router.With(authService.RequireUser).Get("/api/pets/{qq}/friends/refresh-state", friendRefreshStateHandler)
	router.With(authService.RequireUser).Put("/api/pets/{qq}/friends/{friend_id}/pet-id", bindFriendPetHandler)
	router.With(authService.RequireUser).Get("/api/pets/{qq}/friends/{friend_id}/pet-profile", friendPetProfileHandler)
	router.With(authService.RequireUser).Post("/api/pets/{qq}/friends/{friend_id}/poke", pokeFriendPetHandler)
	router.With(authService.RequireUser).Get("/api/pets/{qq}", petProfileHandler(db, mokantClient, logger))
	router.With(authService.RequireUser).Get("/api/pets/{qq}/pk-strangers", petPKStrangersHandler(db, mokantClient))
	router.With(authService.RequireUser).Get("/api/pets/{qq}/pk-power", petPKPowerHandler(db, mokantClient))
	router.With(authService.RequireUser).Post("/api/pets/{qq}/pk-start", startPetPKHandler(db, mokantClient))
	router.With(authService.RequireUser).Get("/api/pets/{qq}/pk-status", petPKStatusHandler(db, mokantClient))
	router.With(authService.RequireUser).Post("/api/pets/{qq}/pk-settle", settlePetPKHandler(db, mokantClient))
	router.With(authService.RequireUser).Get("/api/pets/{qq}/daily-stats", petDailyStatsHandler(db))
	router.With(authService.RequireUser).Post("/api/pets/{qq}/feed", feedPetHandler(db, mokantClient, logger))
	router.With(authService.RequireUser).Post("/api/pets/{qq}/food-purchase", buyPetFoodHandler(db, mokantClient, logger))
	router.With(authService.RequireUser).Post("/api/pets/{qq}/bathe", bathePetHandler(db, mokantClient, logger))
	router.With(authService.RequireUser).Post("/api/pets/{qq}/bath-purchase", buyPetBathItemHandler(db, mokantClient, logger))
	router.With(authService.RequireUser).Get("/api/rankings", rankingHandler(db))
	router.With(authService.RequireUser).Get("/api/pets/{qq}/medals", petMedalGalleryHandler(db, mokantClient))
	router.With(authService.RequireUser).Get("/api/pets/{qq}/interaction-messages", petInteractionMessagesHandler(db, mokantClient))
	router.With(authService.RequireUser).Get("/api/pets/{qq}/activity-overview", petActivityOverviewHandler(db, mokantClient))
	router.With(authService.RequireUser).Get("/api/pets/{qq}/activity-options", petActivityOptionsHandler(db, mokantClient))
	router.With(authService.RequireUser).Post("/api/pets/{qq}/activity-start", startPetActivityHandler(db, mokantClient))
	router.With(authService.RequireUser).Get("/api/pets/{qq}/activity-status", petActivityStatusHandler(db, mokantClient, logger))
	router.With(authService.RequireUser).Post("/api/pets/{qq}/activity-encourage", encouragePetActivityHandler(db, mokantClient, logger))
	router.With(authService.RequireUser).Get("/api/pets/{qq}/auto-control/inventory", autoControlInventoryHandler(db, mokantClient))
	router.With(authService.RequireUser).Get("/api/pets/{qq}/auto-control/config", autoControlConfigHandler(db, false, autoControlScheduler))
	router.With(authService.RequireUser).Put("/api/pets/{qq}/auto-control/config", autoControlConfigHandler(db, true, autoControlScheduler))
	router.With(authService.RequireUser).Get("/api/pets/{qq}/auto-control/state", autoControlStateHandler(db))
	router.With(authService.RequireUser).Get("/api/pets/{qq}/auto-control/logs", autoControlLogsHandler(db))
	router.With(authService.RequireUser).Get("/api/pets/{qq}/auto-pk/config", autoPKConfigHandler(db, false))
	router.With(authService.RequireUser).Put("/api/pets/{qq}/auto-pk/config", autoPKConfigHandler(db, true))
	router.With(authService.RequireUser).Post("/api/pets/{qq}/auto-pk/start", autoPKStartHandler(db, autoPKScheduler))
	router.With(authService.RequireUser).Post("/api/pets/{qq}/auto-pk/stop", autoPKStopHandler(db, autoPKScheduler))
	router.With(authService.RequireUser).Get("/api/pets/{qq}/auto-pk/state", autoPKStateHandler(db))
	router.With(authService.RequireUser).Get("/api/pets/{qq}/auto-pk/logs", autoPKLogsHandler(db))
	router.With(authService.RequireUser).Get("/api/qq/account/nodes", qqAccountNodesHandler(mokantClient))
	router.With(authService.RequireUser).Get("/api/qq/account/options", qqAccountOptionsHandler(mokantClient, mokantManager))
	router.With(authService.RequireUser).Get("/api/qq/account/price", qqAccountPriceHandler(db))
	router.With(authService.RequireUser).Get("/api/qq/plans", qqPlansHandler(db))
	router.With(authService.RequireUser).Post("/api/qq/account/add", qqAccountAddHandler(db, mokantClient))
	router.With(authService.RequireUser).Patch("/api/qq/account/{qq}", qqAccountUpdateHandler(db, mokantClient))
	router.With(authService.RequireUser).Post("/api/qq/account/{qq}/login", qqAccountLoginHandler(db, mokantClient, autoControlScheduler))
	router.With(authService.RequireUser).Get("/api/qq/account/{qq}/security-methods", qqSecurityMethodsHandler(db, mokantClient))
	router.With(authService.RequireUser).Post("/api/qq/account/{qq}/security-qr", qqSecurityQRHandler(db, mokantClient))
	router.With(authService.RequireUser).Post("/api/qq/account/{qq}/security-qr/status", qqSecurityQRStatusHandler(db, mokantClient, autoControlScheduler))
	router.With(authService.RequireUser).Post("/api/qq/account/{qq}/security-confirm", qqSecurityConfirmHandler(db, mokantClient, autoControlScheduler))
	router.With(authService.RequireUser).Post("/api/qq/account/{qq}/security-sms", qqSecuritySMSHandler(db, mokantClient))
	router.With(authService.RequireUser).Post("/api/qq/account/{qq}/security-sms/check", qqSecuritySMSCheckHandler(db, mokantClient, autoControlScheduler))
	router.With(authService.RequireUser).Post("/api/qq/account/{qq}/security-slider", qqSecuritySliderHandler(db, mokantClient, autoControlScheduler))
	router.With(authService.RequireUser).Post("/api/qq/account/{qq}/identity-captcha", qqIdentityCaptchaHandler(db, mokantClient))
	router.With(authService.RequireUser).Post("/api/qq/account/{qq}/identity-phone", qqIdentityPhoneHandler(db, mokantClient))
	router.With(authService.RequireUser).Post("/api/qq/account/{qq}/identity-sms/confirm", qqIdentitySMSConfirmHandler(db, mokantClient, autoControlScheduler))
	router.With(authService.RequireUser).Post("/api/qq/account/{qq}/security-slider/script", qqSecuritySliderScriptHandler(db, mokantClient))
	router.With(authService.RequireUser).Get("/api/qq/account/{qq}/security-slider/script.js", qqSecuritySliderJavaScriptHandler(db, mokantClient))
	router.With(authService.RequireUser).Post("/api/qq/account/{qq}/captcha-proxy", qqCaptchaProxyHandler(db, mokantClient))
	router.With(authService.RequireUser).Handle("/api/qq/captcha-proxy/{qq}/*", qqCaptchaBrowserProxyHandler(db, mokantClient))
	router.With(authService.RequireUser).Post("/api/qq/account/{qq}/offline", qqAccountOfflineHandler(db, mokantClient))
	router.With(authService.RequireUser).Post("/api/qq/account/{qq}/renew", qqAccountRenewHandler(db))
	router.With(authService.RequireUser).Get("/api/wallet", walletHandler(walletService))
	router.With(authService.RequireUser).Get("/api/wallet/transactions", transactionHandler(walletService))
	router.With(authService.RequireUser).Post("/api/wallet/transfer", transferHandler(walletService, db))
	router.With(authService.RequireAdmin).Get("/api/admin/stranger-pets", adminService.StrangerPets)
	router.With(authService.RequireAdmin).Get("/api/admin/internal-pets", internalPetListHandler(db))
	router.With(authService.RequireAdmin).Post("/api/admin/internal-pets", internalPetCreateHandler(db))
	router.With(authService.RequireAdmin).Delete("/api/admin/internal-pets/{id}", internalPetDeleteHandler(db))
	router.With(authService.RequireAdmin).Get("/api/admin/stranger-pets/export", adminService.ExportStrangerTXT)
	router.With(authService.RequireAdmin).Get("/api/admin/stranger-pets/{id}", adminService.StrangerPetDetail)
	router.With(authService.RequireAdmin).Delete("/api/admin/stranger-pets/{id}", adminService.DeleteStrangerPet)
	router.With(authService.RequireAdmin).Post("/api/admin/stranger-pets/import", adminService.ImportStrangerPet)
	router.With(authService.RequireAdmin).Post("/api/admin/stranger-pets/import-txt", adminService.ImportStrangerTXT)
	router.With(authService.RequireAdmin).Get("/api/admin/users", adminService.Users)
	router.With(authService.RequireAdmin).Patch("/api/admin/users/{id}/status", adminService.UpdateStatus)
	router.With(authService.RequireAdmin).Patch("/api/admin/users/{id}/role", adminService.UpdateRole)
	router.With(authService.RequireAdmin).Post("/api/admin/users/{id}/password/reset", adminService.ResetPassword)
	router.With(authService.RequireAdmin).Get("/api/admin/settings", adminService.GetSettings)
	router.With(authService.RequireAdmin).Patch("/api/admin/settings", adminService.UpdateSettings)
	router.With(authService.RequireAdmin).Patch("/api/admin/settings/registration", adminService.UpdateSettings)
	if botNotifier != nil {
		router.With(authService.RequireAdmin).Get("/api/admin/official-bot", officialBotConfigHandler(botNotifier, false))
		router.With(authService.RequireAdmin).Put("/api/admin/official-bot", officialBotConfigHandler(botNotifier, true))
		router.With(authService.RequireAdmin).Post("/api/admin/official-bot/test", officialBotTestHandler(botNotifier))
	}
	router.With(authService.RequireAdmin).Get("/api/admin/audit-logs", adminService.AuditLogs)
	router.With(authService.RequireAdmin).Post("/api/admin/users/{id}/wallet/adjust", adminService.AdjustWallet)
	router.With(authService.RequireAdmin).Post("/api/admin/redeem-codes", adminService.GenerateRedeemCodes)
	router.With(authService.RequireAdmin).Get("/api/admin/redeem-codes", adminService.RedeemCodes)
	router.With(authService.RequireAdmin).Get("/api/admin/redeem-codes/stats", adminService.RedeemCodeStats)
	router.With(authService.RequireAdmin).Get("/api/admin/statistics/users", adminService.UserStatistics)
	router.With(authService.RequireAdmin).Get("/api/admin/qq-plans", adminService.QQPlans)
	router.With(authService.RequireAdmin).Post("/api/admin/qq-plans", adminService.CreateQQPlan)
	router.With(authService.RequireAdmin).Patch("/api/admin/qq-plans/{id}", adminService.UpdateQQPlan)
	router.With(authService.RequireAdmin).Delete("/api/admin/qq-plans/{id}", adminService.DeleteQQPlan)
	router.With(authService.RequireAdmin).Get("/api/admin/system/status", adminSystemStatusHandler(db, cfg))
	router.With(authService.RequireAdmin).Get("/api/admin/announcements", announcementService.AdminList)
	router.With(authService.RequireAdmin).Post("/api/admin/announcements", announcementService.Create)
	router.With(authService.RequireAdmin).Patch("/api/admin/announcements/{id}", announcementService.Update)
	router.With(authService.RequireUser).Post("/api/wallet/redeem", redeemHandler(walletService))
	return router
}

func officialBotConfigHandler(notifier *officialbot.Notifier, update bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !update {
			cfg, err := notifier.Load(r.Context())
			if err != nil {
				response.JSON(w, 500, 9002, "机器人配置加载失败", nil)
				return
			}
			cfg.ClientSecret = ""
			response.JSON(w, 200, 0, "ok", cfg)
			return
		}
		var input officialbot.Config
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			response.JSON(w, 400, 4004, "机器人配置不合法", nil)
			return
		}
		user, _ := auth.UserFromContext(r.Context())
		cfg, err := notifier.Save(r.Context(), input, user.ID)
		if err != nil {
			response.JSON(w, 400, 4004, err.Error(), nil)
			return
		}
		response.JSON(w, 200, 0, "机器人配置已保存", cfg)
	}
}

func officialBotTestHandler(notifier *officialbot.Notifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := notifier.SendTest(r.Context()); err != nil {
			response.JSON(w, 502, 5032, err.Error(), nil)
			return
		}
		response.JSON(w, 200, 0, "测试消息发送成功", nil)
	}
}

type petProfileItem struct {
	QQNumber      string           `json:"qq_number"`
	QQNickname    string           `json:"qq_nickname"`
	Profile       any              `json:"profile"`
	Vitals        any              `json:"vitals"`
	Attributes    any              `json:"attributes"`
	Power         *int64           `json:"power"`
	BaselinePower *int64           `json:"baseline"`
	DominantType  *int64           `json:"dominant_type"`
	PowerChange   int64            `json:"power_change"`
	Fatigue       petFatigueStatus `json:"fatigue"`
}

type petFatigueStatus struct {
	TodayHours  float64 `json:"todayHours"`
	SchoolHours float64 `json:"schoolHours"`
	WorkHours   float64 `json:"workHours"`
}

func loadPetFatigueStatus(ctx context.Context, db *sql.DB, bindingID int64) (petFatigueStatus, error) {
	config, err := autocontrol.LoadConfig(ctx, db, bindingID)
	if err != nil {
		return petFatigueStatus{}, err
	}
	if config.LastResetDate != timeutil.Current().Format("2006-01-02") {
		return petFatigueStatus{}, nil
	}
	schoolHours := float64(config.SchoolDurationUsed) / 3600
	workHours := float64(config.WorkDurationUsed) / 3600
	return petFatigueStatus{TodayHours: schoolHours + workHours, SchoolHours: schoolHours, WorkHours: workHours}, nil
}

type petIdentity struct {
	ID   string
	Name string
}

type petMetricChanges struct {
	Gold       int64            `json:"gold"`
	Attributes map[string]int64 `json:"attributes"`
}

func petNumericMetrics(attributes, vitals any) (map[string]int64, int64, bool) {
	values := make(map[string]int64)
	var gold int64
	var hasGold bool
	var visit func(any)
	visit = func(value any) {
		switch item := value.(type) {
		case map[string]any:
			if name, ok := item["name"].(string); ok {
				if number, ok := item["value"].(float64); ok {
					values[name] = int64(number)
				}
			}
			for key, child := range item {
				if key == "gold" || key == "coins" {
					if number, ok := child.(float64); ok {
						gold = int64(number)
						hasGold = true
					}
				}
				visit(child)
			}
		case []any:
			for _, child := range item {
				visit(child)
			}
		}
	}
	visit(attributes)
	visit(vitals)
	return values, gold, hasGold
}

func savePetMetricChanges(ctx context.Context, db *sql.DB, bindingID int64, attributes, vitals any) (petMetricChanges, error) {
	current, gold, hasGold := petNumericMetrics(attributes, vitals)
	var baselineJSON sql.NullString
	var baselineGold sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT baseline_attributes, baseline_gold FROM pet_profiles WHERE qq_binding_id = ?", bindingID).Scan(&baselineJSON, &baselineGold); err != nil {
		return petMetricChanges{}, err
	}
	changes := petMetricChanges{Attributes: make(map[string]int64)}
	var baseline map[string]int64
	if baselineJSON.Valid {
		_ = json.Unmarshal([]byte(baselineJSON.String), &baseline)
	} else if len(current) > 0 {
		encoded, err := json.Marshal(current)
		if err != nil {
			return changes, err
		}
		if _, err := db.ExecContext(ctx, "UPDATE pet_profiles SET baseline_attributes = ?, updated_at = ? WHERE qq_binding_id = ?", string(encoded), time.Now(), bindingID); err != nil {
			return changes, err
		}
		baseline = current
	}
	if !baselineGold.Valid && hasGold {
		if _, err := db.ExecContext(ctx, "UPDATE pet_profiles SET baseline_gold = ?, updated_at = ? WHERE qq_binding_id = ?", gold, time.Now(), bindingID); err != nil {
			return changes, err
		}
		baselineGold = sql.NullInt64{Int64: gold, Valid: true}
	}
	for name, value := range current {
		if previous, ok := baseline[name]; ok && value != previous {
			changes.Attributes[name] = value - previous
		}
	}
	if hasGold && baselineGold.Valid && gold != baselineGold.Int64 {
		changes.Gold = gold - baselineGold.Int64
	}
	return changes, nil
}

func savePetDailySnapshot(ctx context.Context, db *sql.DB, bindingID int64, attributes, vitals any, power int64) error {
	petSnapshotMu.Lock()
	defer petSnapshotMu.Unlock()
	metrics, gold, hasGold := petNumericMetrics(attributes, vitals)
	encoded, err := json.Marshal(metrics)
	if err != nil {
		return err
	}
	date := timeutil.Current().Format("2006-01-02")
	var goldValue any
	if hasGold {
		goldValue = gold
	}
	_, err = db.ExecContext(ctx, `INSERT INTO pet_daily_snapshots (qq_binding_id, snapshot_date, attributes, gold, pk_power, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(qq_binding_id, snapshot_date) DO UPDATE SET attributes = excluded.attributes, gold = excluded.gold, pk_power = excluded.pk_power, updated_at = CURRENT_TIMESTAMP`, bindingID, date, string(encoded), goldValue, power)
	return err
}

func petDailyStatsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		qq := strings.TrimSpace(r.PathValue("qq"))
		rangeValue := r.URL.Query().Get("range")
		if rangeValue != "30d" {
			rangeValue = "7d"
		}
		days := 7
		if rangeValue == "30d" {
			days = 30
		}
		var bindingID int64
		if err := db.QueryRowContext(r.Context(), "SELECT id FROM qq_bindings WHERE qq_number = ? AND user_id = ?", qq, user.ID).Scan(&bindingID); err != nil {
			response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
			return
		}
		startDate := timeutil.Current().AddDate(0, 0, -(days - 1)).Format("2006-01-02")
		rows, err := db.QueryContext(r.Context(), `SELECT snapshot_date, attributes, gold, pk_power FROM pet_daily_snapshots WHERE qq_binding_id = ? AND snapshot_date >= ? ORDER BY snapshot_date`, bindingID, startDate)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "每日数据加载失败", nil)
			return
		}
		defer rows.Close()
		items := make([]map[string]any, 0)
		for rows.Next() {
			var date, encoded string
			var gold, power sql.NullInt64
			if rows.Scan(&date, &encoded, &gold, &power) != nil {
				continue
			}
			var attributes map[string]int64
			if json.Unmarshal([]byte(encoded), &attributes) != nil {
				attributes = map[string]int64{}
			}
			item := map[string]any{"date": date, "attributes": attributes, "gold": nil, "power": nil}
			if gold.Valid {
				item["gold"] = gold.Int64
			}
			if power.Valid {
				item["power"] = power.Int64
			}
			items = append(items, item)
		}
		response.JSON(w, http.StatusOK, 0, "ok", items)
	}
}

type petAccountItem struct {
	QQNumber         string        `json:"qq_number"`
	Nickname         string        `json:"nickname"`
	ServiceMonths    int64         `json:"service_months"`
	ServiceExpiresAt string        `json:"service_expires_at"`
	Expired          bool          `json:"expired"`
	Online           bool          `json:"online"`
	NodeID           int64         `json:"node_id"`
	NodeName         string        `json:"node_name"`
	NodeStatus       mokant.Status `json:"node_status"`
}

func petAccountsHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		rows, err := db.QueryContext(r.Context(), "SELECT q.qq_number, q.node_id, q.service_months, COALESCE(q.service_expires_at, ''), COALESCE(p.pet_name, '') FROM qq_bindings q LEFT JOIN pet_profiles p ON p.qq_binding_id = q.id WHERE q.user_id = ? ORDER BY q.id DESC", user.ID)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "QQ 列表加载失败", nil)
			return
		}
		defer rows.Close()
		items := make([]petAccountItem, 0)
		for rows.Next() {
			var item petAccountItem
			var cachedNickname string
			if err := rows.Scan(&item.QQNumber, &item.NodeID, &item.ServiceMonths, &item.ServiceExpiresAt, &cachedNickname); err != nil {
				continue
			}
			item.Nickname = cachedNickname
			if item.Nickname == "" {
				item.Nickname = item.QQNumber
			}
			item.Expired = accountExpired(item.ServiceExpiresAt)
			items = append(items, item)
		}
		botsByNode := make(map[int64][]mokantBot)
		loadedNodes := make(map[int64]bool)
		for index := range items {
			nodeID := items[index].NodeID
			if !loadedNodes[nodeID] {
				loadedNodes[nodeID] = true
				nodeClient, nodeErr := client.Node(nodeID)
				if nodeErr != nil {
					continue
				}
				botsData, listErr := nodeClient.ListBots(r.Context())
				if listErr != nil {
					continue
				}
				bots, valid := parseMokantBots(botsData)
				if valid {
					botsByNode[nodeID] = bots
				}
			}
			for _, bot := range botsByNode[nodeID] {
				if strconv.FormatInt(bot.SelfID, 10) == items[index].QQNumber {
					items[index].Nickname = bot.Nickname
					items[index].Online = bot.Status == 1
					break
				}
			}
		}
		response.JSON(w, http.StatusOK, 0, "ok", items)
	}
}

func petProfileHandler(db *sql.DB, client *mokant.Client, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		qq := strings.TrimSpace(r.PathValue("qq"))
		selfID, err := strconv.ParseInt(qq, 10, 64)
		if err != nil || !validQQ(qq) {
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号不合法", nil)
			return
		}
		var bindingID int64
		if err := db.QueryRowContext(r.Context(), "SELECT id FROM qq_bindings WHERE qq_number = ? AND user_id = ?", qq, user.ID).Scan(&bindingID); err != nil {
			response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
			return
		}
		fatigue, fatigueErr := loadPetFatigueStatus(r.Context(), db, bindingID)
		if fatigueErr != nil {
			logger.Error("宠物今日执行统计读取失败", "binding_id", bindingID, "error", fatigueErr)
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		online := false
		if botsData, listErr := client.ListBots(ctx); listErr == nil {
			if bots, valid := parseMokantBots(botsData); valid {
				for _, bot := range bots {
					if strconv.FormatInt(bot.SelfID, 10) == qq {
						online = bot.Status == 1
						break
					}
				}
			}
		}
		if !online {
			var petID, petName string
			var power, baseline, dominantType sql.NullInt64
			err := db.QueryRowContext(r.Context(), "SELECT pet_id, pet_name, pk_power, baseline_pk_power, dominant_type FROM pet_profiles WHERE qq_binding_id = ?", bindingID).Scan(&petID, &petName, &power, &baseline, &dominantType)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				response.JSON(w, http.StatusInternalServerError, 9002, "宠物资料加载失败", nil)
				return
			}
			profile := map[string]any{"pet_id": petID, "pet_name": petName}
			var powerValue, baselineValue, dominantTypeValue *int64
			if power.Valid {
				powerValue = &power.Int64
			}
			if baseline.Valid {
				baselineValue = &baseline.Int64
			}
			if dominantType.Valid {
				dominantTypeValue = &dominantType.Int64
			}
			response.JSON(w, http.StatusOK, 0, "ok", map[string]any{"qq_number": qq, "qq_nickname": "", "profile": profile, "vitals": nil, "attributes": nil, "metric_changes": petMetricChanges{Attributes: map[string]int64{}}, "power": powerValue, "baseline": baselineValue, "dominant_type": dominantTypeValue, "power_change": int64(0), "fatigue": fatigue})
			return
		}
		profileData, err := client.GetPetProfile(ctx, selfID)
		if err != nil {
			logger.Error("听雨框架宠物资料请求失败", "action", "get_pet_profile", "self_id", selfID, "error", err)
			response.JSON(w, http.StatusBadGateway, 5032, "宠物资料加载失败", nil)
			return
		}
		var profile any
		if err := json.Unmarshal(profileData, &profile); err != nil {
			response.JSON(w, http.StatusBadGateway, 5033, "宠物资料格式无效", nil)
			return
		}
		if _, err := syncPetIdentity(r.Context(), db, user.ID, qq, profileData); err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "宠物资料保存失败", nil)
			return
		}
		var vitals, attributes any
		identity, _ := parsePetIdentity(profileData)
		if data, e := client.GetPetVitals(ctx, selfID, identity.ID); e == nil {
			_ = json.Unmarshal(data, &vitals)
		}
		if data, e := client.GetPetAttributes(ctx, selfID, identity.ID); e == nil {
			_ = json.Unmarshal(data, &attributes)
		}
		metricChanges, _ := savePetMetricChanges(r.Context(), db, bindingID, attributes, vitals)
		power, baseline, dominantType, powerChange, _ := refreshPetPower(ctx, db, client, bindingID, selfID, identity.ID)
		if power != nil {
			_ = savePetDailySnapshot(r.Context(), db, bindingID, attributes, vitals, *power)
		}
		response.JSON(w, http.StatusOK, 0, "ok", map[string]any{"qq_number": qq, "qq_nickname": "", "profile": profile, "vitals": vitals, "attributes": attributes, "metric_changes": metricChanges, "power": power, "baseline": baseline, "dominant_type": dominantType, "power_change": powerChange, "fatigue": fatigue})
	}
}

func petProfilesHandler(db *sql.DB, client *mokant.Client, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		rows, err := db.QueryContext(r.Context(), "SELECT qq_number FROM qq_bindings WHERE user_id = ? ORDER BY id DESC", user.ID)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "宠物资料加载失败", nil)
			return
		}
		defer rows.Close()
		var qqNumbers []string
		for rows.Next() {
			var qq string
			if err := rows.Scan(&qq); err != nil {
				response.JSON(w, http.StatusInternalServerError, 9002, "宠物资料加载失败", nil)
				return
			}
			qqNumbers = append(qqNumbers, qq)
		}
		if err := rows.Err(); err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "宠物资料加载失败", nil)
			return
		}
		botsData, err := client.ListBots(r.Context())
		if err != nil {
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "在线 QQ 列表获取失败", nil)
			return
		}
		bots, valid := parseMokantBots(botsData)
		if !valid {
			response.JSON(w, http.StatusBadGateway, 5033, "听雨框架返回的 QQ 列表格式无效", nil)
			return
		}
		bound := make(map[string]bool, len(qqNumbers))
		for _, qq := range qqNumbers {
			bound[qq] = true
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		items := make([]petProfileItem, 0)
		for _, bot := range bots {
			qq := strconv.FormatInt(bot.SelfID, 10)
			if bot.Status != 1 || !bound[qq] {
				continue
			}
			data, profileErr := client.GetPetProfile(ctx, bot.SelfID)
			if profileErr != nil || len(data) == 0 {
				continue
			}
			var profile any
			if err := json.Unmarshal(data, &profile); err != nil {
				logger.Error("听雨框架宠物资料原始响应格式无效", "action", "get_pet_profile", "self_id", bot.SelfID, "error", err, "raw_response", string(data))
				continue
			}
			identity, err := syncPetIdentity(ctx, db, user.ID, qq, data)
			if err != nil {
				logger.Error("宠物资料保存失败", "qq", qq, "error", err)
				continue
			}
			var vitals any
			if vitalsData, vitalsErr := client.GetPetVitals(ctx, bot.SelfID, identity.ID); vitalsErr == nil && len(vitalsData) > 0 {
				_ = json.Unmarshal(vitalsData, &vitals)
			}
			var attributes any
			if attributesData, attributesErr := client.GetPetAttributes(ctx, bot.SelfID, identity.ID); attributesErr == nil && len(attributesData) > 0 {
				_ = json.Unmarshal(attributesData, &attributes)
			}
			var bindingID int64
			_ = db.QueryRowContext(ctx, "SELECT id FROM qq_bindings WHERE qq_number = ? AND user_id = ?", qq, user.ID).Scan(&bindingID)
			fatigue, fatigueErr := loadPetFatigueStatus(ctx, db, bindingID)
			if fatigueErr != nil {
				logger.Error("宠物今日执行统计读取失败", "binding_id", bindingID, "error", fatigueErr)
			}
			power, baseline, dominantType, powerChange, _ := refreshPetPower(ctx, db, client, bindingID, bot.SelfID, identity.ID)
			if power != nil {
				_ = savePetDailySnapshot(ctx, db, bindingID, attributes, vitals, *power)
			}
			items = append(items, petProfileItem{QQNumber: qq, QQNickname: bot.Nickname, Profile: profile, Vitals: vitals, Attributes: attributes, Power: power, BaselinePower: baseline, DominantType: dominantType, PowerChange: powerChange, Fatigue: fatigue})
		}
		response.JSON(w, http.StatusOK, 0, "ok", items)
	}
}

func findCatalogID(data []byte, name, idKey string) string {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return ""
	}
	var walk func(any) string
	walk = func(item any) string {
		switch typed := item.(type) {
		case map[string]any:
			itemName, _ := typed["name"].(string)
			if strings.TrimSpace(itemName) == strings.TrimSpace(name) {
				switch id := typed[idKey].(type) {
				case string:
					return id
				case float64:
					return strconv.FormatInt(int64(id), 10)
				}
			}
			for _, child := range typed {
				if found := walk(child); found != "" {
					return found
				}
			}
		case []any:
			for _, child := range typed {
				if found := walk(child); found != "" {
					return found
				}
			}
		}
		return ""
	}
	if found := walk(value); found != "" {
		return found
	}
	return name
}

func parsePetIdentity(data []byte) (petIdentity, error) {
	var profile map[string]any
	if err := json.Unmarshal(data, &profile); err != nil {
		return petIdentity{}, err
	}
	petID, ok := profile["pet_id"]
	if !ok || petID == nil {
		return petIdentity{}, errors.New("pet_id missing")
	}
	petIDValue := strings.TrimSpace(fmt.Sprint(petID))
	if petIDValue == "" || petIDValue == "<nil>" {
		return petIdentity{}, errors.New("pet_id empty")
	}
	petName, _ := profile["pet_name"].(string)
	return petIdentity{ID: petIDValue, Name: strings.TrimSpace(petName)}, nil
}

func syncPetIdentity(ctx context.Context, db *sql.DB, userID int64, qq string, data []byte) (petIdentity, error) {
	identity, err := parsePetIdentity(data)
	if err != nil {
		return petIdentity{}, err
	}
	var bindingID int64
	if err := db.QueryRowContext(ctx, "SELECT id FROM qq_bindings WHERE qq_number = ? AND user_id = ?", qq, userID).Scan(&bindingID); err != nil {
		return petIdentity{}, err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO pet_profiles (qq_binding_id, pet_id, pet_name, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(qq_binding_id) DO UPDATE SET
		pet_id = excluded.pet_id,
		pet_name = excluded.pet_name,
		updated_at = CURRENT_TIMESTAMP`, bindingID, identity.ID, identity.Name)
	if err != nil {
		return petIdentity{}, err
	}
	return identity, nil
}

func ensurePetIdentity(ctx context.Context, db *sql.DB, client *mokant.Client, userID int64, qq string, selfID int64) (petIdentity, error) {
	var identity petIdentity
	err := db.QueryRowContext(ctx, `
		SELECT pet_id, pet_name
		FROM pet_profiles
		WHERE qq_binding_id = (SELECT id FROM qq_bindings WHERE qq_number = ? AND user_id = ?)`, qq, userID).Scan(&identity.ID, &identity.Name)
	if err == nil && identity.ID != "" {
		return identity, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return petIdentity{}, err
	}
	data, err := client.GetPetProfile(ctx, selfID)
	if err != nil {
		return petIdentity{}, err
	}
	return syncPetIdentity(ctx, db, userID, qq, data)
}

func refreshPetPower(ctx context.Context, db *sql.DB, client *mokant.Client, bindingID, selfID int64, petID string) (*int64, *int64, *int64, int64, error) {
	data, err := client.GetPetPKPower(ctx, selfID, petID)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, nil, nil, 0, err
	}
	power, dominantType := petPKPowerValues(payload)
	if power == nil {
		return nil, nil, nil, 0, errors.New("pk power missing")
	}
	var baseline sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT baseline_pk_power FROM pet_profiles WHERE qq_binding_id = ?", bindingID).Scan(&baseline); err != nil {
		return nil, nil, nil, 0, err
	}
	if !baseline.Valid {
		baseline = sql.NullInt64{Int64: *power, Valid: true}
	}
	change := *power - baseline.Int64
	now := timeutil.Now()
	_, err = db.ExecContext(ctx, "UPDATE pet_profiles SET pk_power = ?, baseline_pk_power = ?, dominant_type = ?, pk_updated_at = ?, updated_at = ? WHERE qq_binding_id = ?", *power, baseline.Int64, dominantType, now, now, bindingID)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	return power, &baseline.Int64, dominantType, change, nil
}

func rankingHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.QueryContext(r.Context(), `SELECT p.pk_power, p.pk_updated_at, p.pet_name, q.qq_number, p.dominant_type FROM pet_profiles p JOIN qq_bindings q ON q.id = p.qq_binding_id WHERE p.pk_power IS NOT NULL ORDER BY p.pk_power DESC, p.pk_updated_at DESC`)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "排行榜加载失败", nil)
			return
		}
		defer rows.Close()
		items := make([]map[string]any, 0)
		for rows.Next() {
			var power int64
			var updated, name, qq string
			var dominantType sql.NullInt64
			if rows.Scan(&power, &updated, &name, &qq, &dominantType) == nil {
				if parsed, parseErr := time.Parse("2006-01-02 15:04:05", updated); parseErr == nil {
					updated = parsed.In(timeutil.Beijing).Format(time.RFC3339)
				}
				var dominantTypeValue any
				if dominantType.Valid {
					dominantTypeValue = dominantType.Int64
				}
				items = append(items, map[string]any{"power": power, "updated_at": updated, "pet_name": name, "qq_number": qq, "dominant_type": dominantTypeValue})
			}
		}
		response.JSON(w, http.StatusOK, 0, "ok", items)
	}
}

func validatePetActivityRequest(db *sql.DB, r *http.Request) (int64, int64, error) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		return 0, 0, errors.New("unauthorized")
	}
	qq := strings.TrimSpace(r.PathValue("qq"))
	selfID, err := strconv.ParseInt(qq, 10, 64)
	if err != nil || !validQQ(qq) {
		return 0, 0, errors.New("invalid qq")
	}
	var bindingID int64
	if err := db.QueryRowContext(r.Context(), "SELECT id FROM qq_bindings WHERE qq_number = ? AND user_id = ?", qq, user.ID).Scan(&bindingID); err != nil {
		return 0, 0, err
	}
	return selfID, bindingID, nil
}

func petPKPowerValues(payload any) (*int64, *int64) {
	var power, dominantType *int64
	var walk func(any)
	walk = func(value any) {
		switch item := value.(type) {
		case map[string]any:
			if power == nil {
				for _, key := range []string{"power", "pk_power", "pet_power"} {
					if number, ok := item[key].(float64); ok {
						parsed := int64(number)
						power = &parsed
						break
					}
				}
			}
			if dominantType == nil {
				if number, ok := item["dominant_type"].(float64); ok {
					parsed := int64(number)
					dominantType = &parsed
				}
			}
			for _, child := range item {
				walk(child)
			}
		case []any:
			for _, child := range item {
				walk(child)
			}
		}
	}
	walk(payload)
	return power, dominantType
}

func petPKStrangerItems(payload any) []map[string]any {
	switch value := payload.(type) {
	case []any:
		items := make([]map[string]any, 0, len(value))
		for _, raw := range value {
			if item, ok := raw.(map[string]any); ok {
				items = append(items, item)
			}
		}
		return items
	case map[string]any:
		for _, key := range []string{"friends", "strangers", "pets", "data", "items"} {
			if child, ok := value[key]; ok {
				return petPKStrangerItems(child)
			}
		}
	case string:
		var decoded any
		if json.Unmarshal([]byte(value), &decoded) == nil {
			return petPKStrangerItems(decoded)
		}
	}
	return []map[string]any{}
}

func syncPetPKStrangers(ctx context.Context, db *sql.DB, userID, bindingID int64, payload any) ([]map[string]any, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := timeutil.Now()
	for _, item := range petPKStrangerItems(payload) {
		strangerUserID := strings.TrimSpace(fmt.Sprint(item["user_id"]))
		if strangerUserID == "" || strangerUserID == "<nil>" {
			continue
		}
		raw, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO pet_pk_strangers
			(user_id, pet_id, pet_name, nickname, power, dominant_type, raw_data, imported_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(user_id) DO UPDATE SET
				pet_id = excluded.pet_id,
				pet_name = excluded.pet_name,
				nickname = excluded.nickname,
				raw_data = excluded.raw_data,
				updated_at = excluded.updated_at`,
			strangerUserID, strings.TrimSpace(fmt.Sprint(item["pet_id"])), strings.TrimSpace(fmt.Sprint(item["pet_name"])), strings.TrimSpace(fmt.Sprint(item["nickname"])), item["power"], item["dominant_type"], string(raw), now, now)
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return loadPetPKStrangers(ctx, db, userID, &bindingID)
}

func loadPetPKStrangers(ctx context.Context, db *sql.DB, _ int64, _ *int64) ([]map[string]any, error) {
	query := `SELECT s.user_id, s.pet_id, s.pet_name, s.nickname, s.power, s.dominant_type,
		s.raw_data, s.imported_at, s.updated_at, s.power_updated_at
		FROM pet_pk_strangers s ORDER BY s.id ASC`
	args := []any{}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var strangerUserID, petID, petName, nickname, rawData, importedAt, updatedAt string
		var power, dominantType sql.NullInt64
		var powerUpdatedAt sql.NullString
		if err := rows.Scan(&strangerUserID, &petID, &petName, &nickname, &power, &dominantType, &rawData, &importedAt, &updatedAt, &powerUpdatedAt); err != nil {
			return nil, err
		}
		item := make(map[string]any)
		_ = json.Unmarshal([]byte(rawData), &item)
		item["user_id"] = strangerUserID
		item["pet_id"] = petID
		item["pet_name"] = petName
		item["nickname"] = nickname
		item["power"] = nil
		item["dominant_type"] = nil
		if power.Valid {
			item["power"] = power.Int64
		}
		if dominantType.Valid {
			item["dominant_type"] = dominantType.Int64
		}
		item["imported_at"] = importedAt
		item["updated_at"] = updatedAt
		item["power_updated_at"] = nil
		if powerUpdatedAt.Valid {
			item["power_updated_at"] = powerUpdatedAt.String
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func petPKCacheHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		items, err := loadPetPKStrangers(r.Context(), db, user.ID, nil)
		if err != nil {
			slog.Default().Error("陌生人宠物缓存查询失败", "user_id", user.ID, "error", err)
			response.JSON(w, http.StatusInternalServerError, 9002, "陌生人宠物缓存加载失败: "+err.Error(), nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", items)
	}
}

func updatePetPKStrangerPower(ctx context.Context, db *sql.DB, ownerUserID int64, petID string, power int64, dominantType *int64) (int64, error) {
	now := timeutil.Now()
	result, err := db.ExecContext(ctx, `UPDATE pet_pk_strangers
		SET power = ?, dominant_type = ?, power_updated_at = ?, updated_at = ?
		WHERE pet_id = ?`, power, dominantType, now, now, petID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func petPKPowerHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, _, err := validatePetActivityRequest(db, r)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
				return
			}
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号不合法", nil)
			return
		}
		petID := strings.TrimSpace(r.URL.Query().Get("pet_id"))
		if petID == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "宠物 ID 不能为空", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		data, err := client.GetPetPKPower(ctx, selfID, petID)
		if err != nil {
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "宠物战力更新失败", nil)
			return
		}
		var payload any
		if err := json.Unmarshal(data, &payload); err != nil {
			response.JSON(w, http.StatusBadGateway, 5033, "宠物战力数据格式无效", nil)
			return
		}
		power, dominantType := petPKPowerValues(payload)
		if power == nil {
			response.JSON(w, http.StatusBadGateway, 5033, "宠物战力数据格式无效", nil)
			return
		}
		user, _ := auth.UserFromContext(r.Context())
		affected, err := updatePetPKStrangerPower(r.Context(), db, user.ID, petID, *power, dominantType)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "宠物战力缓存更新失败", nil)
			return
		}
		if affected == 0 {
			response.JSON(w, http.StatusNotFound, 4004, "该宠物不在当前 QQ 的陌生人缓存中", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", payload)
	}
}

func writePetPKResult(w http.ResponseWriter, data json.RawMessage, errorMessage string) {
	data = bytes.TrimPrefix(bytes.TrimSpace(data), []byte{0xef, 0xbb, 0xbf})
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		response.JSON(w, http.StatusBadGateway, 5033, "宠物 PK 数据格式无效", nil)
		return
	}
	for depth := 0; depth < 2; depth++ {
		encoded, ok := result.(string)
		if !ok {
			break
		}
		encodedData := bytes.TrimPrefix(bytes.TrimSpace([]byte(encoded)), []byte{0xef, 0xbb, 0xbf})
		if err := json.Unmarshal(encodedData, &result); err != nil {
			response.JSON(w, http.StatusBadGateway, 5033, "宠物 PK 数据格式无效", nil)
			return
		}
	}
	response.JSON(w, http.StatusOK, 0, errorMessage, result)
}

func startPetPKHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		var input struct {
			FriendID    string `json:"friend_id"`
			FriendPetID string `json:"friend_pet_id"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.FriendID) == "" || strings.TrimSpace(input.FriendPetID) == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "宠物 PK 参数不完整", nil)
			return
		}
		_, err = strconv.ParseInt(strings.TrimSpace(input.FriendID), 10, 64)
		if err != nil || !validQQ(strings.TrimSpace(input.FriendID)) {
			response.JSON(w, http.StatusBadRequest, 4003, "对手 QQ 不合法", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		petID := ""
		_ = db.QueryRowContext(ctx, "SELECT pet_id FROM pet_profiles WHERE qq_binding_id = (SELECT id FROM qq_bindings WHERE qq_number = ?)", strconv.FormatInt(selfID, 10)).Scan(&petID)
		data, err := client.StartPetPK(ctx, selfID, petID, strings.TrimSpace(input.FriendID), strings.TrimSpace(input.FriendPetID))
		if err != nil {
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "宠物 PK 开始失败", nil)
			return
		}
		writePetPKResult(w, data, "ok")
	}
}

func petPKStatusHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		storyID := strings.TrimSpace(r.URL.Query().Get("story_id"))
		if storyID == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "story_id 不能为空", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		petID := ""
		_ = db.QueryRowContext(ctx, "SELECT pet_id FROM pet_profiles WHERE qq_binding_id = (SELECT id FROM qq_bindings WHERE qq_number = ?)", strconv.FormatInt(selfID, 10)).Scan(&petID)
		data, err := client.GetPetPKStatus(ctx, selfID, petID, storyID)
		if err != nil {
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "宠物 PK 状态加载失败", nil)
			return
		}
		writePetPKResult(w, data, "ok")
	}
}

func settlePetPKHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		var input struct {
			StoryID string `json:"story_id"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.StoryID) == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "story_id 不能为空", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		petID := ""
		_ = db.QueryRowContext(ctx, "SELECT pet_id FROM pet_profiles WHERE qq_binding_id = (SELECT id FROM qq_bindings WHERE qq_number = ?)", strconv.FormatInt(selfID, 10)).Scan(&petID)
		data, err := client.SettlePetPK(ctx, selfID, petID, strings.TrimSpace(input.StoryID))
		if err != nil {
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "宠物 PK 结算失败", nil)
			return
		}
		writePetPKResult(w, data, "ok")
	}
}

func petPKStrangersHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, bindingID, err := validatePetActivityRequest(db, r)
		if err != nil {
			if err.Error() == "unauthorized" {
				response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
				return
			}
			if errors.Is(err, sql.ErrNoRows) {
				response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
				return
			}
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号不合法", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
		mode := int64(-1)
		if value := strings.TrimSpace(r.URL.Query().Get("mode")); value != "" {
			if mode, err = strconv.ParseInt(value, 10, 64); err != nil {
				response.JSON(w, http.StatusBadRequest, 4003, "mode 不合法", nil)
				return
			}
		}
		data, err := client.GetPetPKStrangers(ctx, selfID, cursor, mode)
		if err != nil {
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "陌生人宠物数据加载失败", nil)
			return
		}
		data = bytes.TrimPrefix(bytes.TrimSpace(data), []byte{0xef, 0xbb, 0xbf})
		var payload any
		var encoded string
		if json.Unmarshal(data, &encoded) == nil {
			if fileData, fileErr := os.ReadFile(encoded); fileErr == nil {
				data = bytes.TrimPrefix(bytes.TrimSpace(fileData), []byte{0xef, 0xbb, 0xbf})
			} else {
				data = []byte(encoded)
			}
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			path := strings.TrimSpace(string(data))
			if fileData, fileErr := os.ReadFile(path); fileErr == nil {
				data = bytes.TrimPrefix(bytes.TrimSpace(fileData), []byte{0xef, 0xbb, 0xbf})
			}
			if err := json.Unmarshal(data, &payload); err != nil {
				start, end := bytes.IndexAny(data, "[{"), bytes.LastIndexAny(data, "]}")
				if start >= 0 && end > start {
					_ = json.Unmarshal(data[start:end+1], &payload)
				}
			}
			if payload == nil {
				payload = map[string]any{"friends": []any{}}
			}
		}
		user, _ := auth.UserFromContext(r.Context())
		userIDs, err := syncPetPKStrangers(r.Context(), db, user.ID, bindingID, payload)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "陌生人宠物数据保存失败", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", userIDs)
	}
}

func petMedalGalleryHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, bindingID, err := validatePetActivityRequest(db, r)
		if err != nil {
			if err.Error() == "unauthorized" {
				response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
				return
			}
			if errors.Is(err, sql.ErrNoRows) {
				response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
				return
			}
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号不合法", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		var petID string
		if err := db.QueryRowContext(ctx, "SELECT pet_id FROM pet_profiles WHERE qq_binding_id = ?", bindingID).Scan(&petID); err != nil || strings.TrimSpace(petID) == "" {
			response.JSON(w, http.StatusBadGateway, 5032, "宠物资料读取失败", nil)
			return
		}
		data, err := client.GetPetMedalGallery(ctx, selfID, petID)
		if err != nil {
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "宠物勋章加载失败", nil)
			return
		}
		var result any
		if err := json.Unmarshal(data, &result); err != nil {
			response.JSON(w, http.StatusBadGateway, 5033, "宠物勋章格式无效", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", result)
	}
}

func petInteractionMessagesHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, _, err := validatePetActivityRequest(db, r)
		if err != nil {
			if err.Error() == "unauthorized" {
				response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
				return
			}
			if errors.Is(err, sql.ErrNoRows) {
				response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
				return
			}
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号不合法", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		limit := int64(0)
		if value := strings.TrimSpace(r.URL.Query().Get("limit")); value != "" {
			if limit, err = strconv.ParseInt(value, 10, 64); err != nil || limit < 1 {
				response.JSON(w, http.StatusBadRequest, 4003, "limit 不合法", nil)
				return
			}
		}
		data, err := client.GetPetInteractionMessages(ctx, selfID, limit)
		if err != nil {
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "互动记录加载失败", nil)
			return
		}
		var result any
		if err := json.Unmarshal(data, &result); err != nil {
			response.JSON(w, http.StatusBadGateway, 5033, "互动记录格式无效", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", result)
	}
}

func activityRequestData(db *sql.DB, r *http.Request) (int64, string, string, error) {
	selfID, _, err := validatePetActivityRequest(db, r)
	if err != nil {
		return 0, "", "", err
	}
	activity := strings.TrimSpace(r.URL.Query().Get("activity"))
	if activity != "school" && activity != "work" && activity != "adventure" {
		return 0, "", "", errors.New("activity_invalid")
	}
	petID := ""
	_ = db.QueryRowContext(r.Context(), "SELECT pet_id FROM pet_profiles WHERE qq_binding_id = (SELECT id FROM qq_bindings WHERE qq_number = ?)", strconv.FormatInt(selfID, 10)).Scan(&petID)
	return selfID, petID, activity, nil
}

func writeActivityRequestError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if err.Error() == "unauthorized" {
		response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
		return true
	}
	if errors.Is(err, sql.ErrNoRows) {
		response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
		return true
	}
	response.JSON(w, http.StatusBadRequest, 4003, "活动类型不合法", nil)
	return true
}

func petActivityOverviewHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, petID, activity, err := activityRequestData(db, r)
		if writeActivityRequestError(w, err) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		data, err := client.GetPetActivityOverview(ctx, selfID, petID, activity)
		if err != nil {
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
			} else {
				response.JSON(w, http.StatusBadGateway, 5032, "活动概览加载失败", nil)
			}
			return
		}
		var result any
		if err := json.Unmarshal(data, &result); err != nil {
			response.JSON(w, http.StatusBadGateway, 5033, "活动概览格式无效", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", result)
	}
}

func petActivityOptionsHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, petID, activity, err := activityRequestData(db, r)
		if writeActivityRequestError(w, err) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		careerType := int64(0)
		var overview any
		overviewData, overviewErr := client.GetPetActivityOverview(ctx, selfID, petID, activity)
		if overviewErr == nil {
			_ = json.Unmarshal(overviewData, &overview)
			careerType = int64(findJSONNumber(overview, "current_career_type"))
		}
		if value := strings.TrimSpace(r.URL.Query().Get("career_type")); value != "" {
			careerType, err = strconv.ParseInt(value, 10, 64)
			if err != nil {
				response.JSON(w, http.StatusBadRequest, 4003, "career_type 不合法", nil)
				return
			}
		}
		data, err := client.GetPetActivityOptions(ctx, selfID, petID, activity, careerType)
		if err != nil {
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "活动选项加载失败", nil)
			return
		}
		var result any
		if err := json.Unmarshal(data, &result); err != nil {
			response.JSON(w, http.StatusBadGateway, 5033, "活动选项格式无效", nil)
			return
		}
		if object, ok := result.(map[string]any); ok {
			object["overview"] = overview
			object["current_career_type"] = careerType
		}
		response.JSON(w, http.StatusOK, 0, "ok", result)
	}
}

func findJSONNumber(value any, key string) float64 {
	switch item := value.(type) {
	case map[string]any:
		if number, ok := item[key].(float64); ok {
			return number
		}
		for _, child := range item {
			if number := findJSONNumber(child, key); number != 0 {
				return number
			}
		}
	case []any:
		for _, child := range item {
			if number := findJSONNumber(child, key); number != 0 {
				return number
			}
		}
	}
	return 0
}

func startPetActivityHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, bindingID, err := validatePetActivityRequest(db, r)
		if err != nil {
			if err.Error() == "unauthorized" {
				response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
				return
			}
			if errors.Is(err, sql.ErrNoRows) {
				response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
				return
			}
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号不合法", nil)
			return
		}
		var input struct {
			Activity     string `json:"activity"`
			OptionName   string `json:"option_name"`
			SubEventType int64  `json:"sub_event_type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			response.JSON(w, http.StatusBadRequest, 4003, "活动参数不合法", nil)
			return
		}
		if (input.Activity != "school" && input.Activity != "work" && input.Activity != "adventure") || strings.TrimSpace(input.OptionName) == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "活动参数不合法", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		petID := ""
		_ = db.QueryRowContext(ctx, "SELECT pet_id FROM pet_profiles WHERE qq_binding_id = ?", bindingID).Scan(&petID)
		data, err := client.StartPetActivity(ctx, selfID, petID, input.Activity, strings.TrimSpace(input.OptionName), input.SubEventType)
		if err != nil {
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "活动开始失败", nil)
			return
		}
		var result map[string]any
		if err := json.Unmarshal(data, &result); err != nil {
			response.JSON(w, http.StatusBadGateway, 5033, "活动结果格式无效", nil)
			return
		}
		storyID, _ := result["story_id"].(string)
		if strings.TrimSpace(storyID) == "" {
			response.JSON(w, http.StatusBadGateway, 5033, "活动结果缺少 story_id", nil)
			return
		}
		if _, err := db.ExecContext(r.Context(), "UPDATE pet_profiles SET story_id = ?, updated_at = CURRENT_TIMESTAMP WHERE qq_binding_id = ?", storyID, bindingID); err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "活动状态保存失败", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "活动开始成功", result)
	}
}

func petActivityStatusHandler(db *sql.DB, client *mokant.Client, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, _, err := validatePetActivityRequest(db, r)
		if err != nil {
			if err.Error() == "unauthorized" {
				response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
				return
			}
			if errors.Is(err, sql.ErrNoRows) {
				response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
				return
			}
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号不合法", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		petID := ""
		_ = db.QueryRowContext(ctx, "SELECT pet_id FROM pet_profiles WHERE qq_binding_id = (SELECT id FROM qq_bindings WHERE qq_number = ?)", strconv.FormatInt(selfID, 10)).Scan(&petID)
		data, err := client.GetPetActivityStatus(ctx, selfID, petID)
		if err != nil {
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "活动状态加载失败", nil)
			return
		}
		var result any
		if err := json.Unmarshal(data, &result); err != nil {
			response.JSON(w, http.StatusBadGateway, 5033, "活动状态格式无效", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", result)
	}
}

func encouragePetActivityHandler(db *sql.DB, client *mokant.Client, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, _, err := validatePetActivityRequest(db, r)
		if err != nil {
			if err.Error() == "unauthorized" {
				response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
				return
			}
			if errors.Is(err, sql.ErrNoRows) {
				response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
				return
			}
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号不合法", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		petID := ""
		storyID := ""
		_ = db.QueryRowContext(ctx, "SELECT pet_id, COALESCE(story_id, '') FROM pet_profiles WHERE qq_binding_id = (SELECT id FROM qq_bindings WHERE qq_number = ?)", strconv.FormatInt(selfID, 10)).Scan(&petID, &storyID)
		data, err := client.EncouragePetActivity(ctx, selfID, petID, storyID)
		if err != nil {
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "鼓励活动失败", nil)
			return
		}
		var result any
		if err := json.Unmarshal(data, &result); err != nil {
			response.JSON(w, http.StatusBadGateway, 5033, "鼓励结果格式无效", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "鼓励成功", result)
	}
}

func feedPetHandler(db *sql.DB, client *mokant.Client, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		qq := strings.TrimSpace(r.PathValue("qq"))
		foodName := strings.TrimSpace(r.URL.Query().Get("food_name"))
		selfID, err := strconv.ParseInt(qq, 10, 64)
		if err != nil || !validQQ(qq) || foodName == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号或食物名称不合法", nil)
			return
		}
		var boundQQ string
		if err := db.QueryRowContext(r.Context(), "SELECT qq_number FROM qq_bindings WHERE qq_number = ? AND user_id = ?", qq, user.ID).Scan(&boundQQ); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
				return
			}
			response.JSON(w, http.StatusInternalServerError, 9002, "QQ 绑定校验失败", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		identity, err := ensurePetIdentity(ctx, db, client, user.ID, qq, selfID)
		if err != nil {
			logger.Error("宠物资料读取失败", "action", "get_pet_profile", "self_id", selfID, "error", err)
			response.JSON(w, http.StatusBadGateway, 5032, "宠物资料读取失败", nil)
			return
		}
		foodID := foodName
		if catalog, catalogErr := client.GetPetFoodCatalog(ctx, selfID); catalogErr == nil {
			foodID = findCatalogID(catalog, foodName, "food_id")
		}
		data, err := client.FeedPet(ctx, selfID, identity.ID, foodID)
		if err != nil {
			logger.Error("听雨框架喂食请求失败", "action", "feed_pet", "error", err)
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "喂食请求失败", nil)
			return
		}
		var result any
		if len(data) > 0 && json.Unmarshal(data, &result) != nil {
			result = string(data)
		}
		response.JSON(w, http.StatusOK, 0, "喂食成功", result)
	}
}

func bathePetHandler(db *sql.DB, client *mokant.Client, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		qq, itemName := strings.TrimSpace(r.PathValue("qq")), strings.TrimSpace(r.URL.Query().Get("item_name"))
		selfID, err := strconv.ParseInt(qq, 10, 64)
		if err != nil || !validQQ(qq) || itemName == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号或洗护用品不合法", nil)
			return
		}
		var boundQQ string
		if err := db.QueryRowContext(r.Context(), "SELECT qq_number FROM qq_bindings WHERE qq_number = ? AND user_id = ?", qq, user.ID).Scan(&boundQQ); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
				return
			}
			response.JSON(w, http.StatusInternalServerError, 9002, "QQ 绑定校验失败", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		identity, err := ensurePetIdentity(ctx, db, client, user.ID, qq, selfID)
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "宠物资料读取失败", nil)
			return
		}
		itemID := itemName
		if catalog, catalogErr := client.GetPetBathCatalog(ctx, selfID); catalogErr == nil {
			itemID = findCatalogID(catalog, itemName, "item_id")
		}
		data, err := client.BathePet(ctx, selfID, identity.ID, itemID, 1)
		if err != nil {
			logger.Error("听雨框架洗护请求失败", "action", "bathe_pet", "error", err)
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "洗护请求失败", nil)
			return
		}
		var result any
		if len(data) > 0 && json.Unmarshal(data, &result) != nil {
			result = string(data)
		}
		response.JSON(w, http.StatusOK, 0, "洗护成功", result)
	}
}

func buyPetFoodHandler(db *sql.DB, client *mokant.Client, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		qq := strings.TrimSpace(r.PathValue("qq"))
		selfID, err := strconv.ParseInt(qq, 10, 64)
		if err != nil || !validQQ(qq) {
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号不合法", nil)
			return
		}
		var boundQQ string
		if err := db.QueryRowContext(r.Context(), "SELECT qq_number FROM qq_bindings WHERE qq_number = ? AND user_id = ?", qq, user.ID).Scan(&boundQQ); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
				return
			}
			response.JSON(w, http.StatusInternalServerError, 9002, "QQ 绑定校验失败", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		data, err := client.BuyPetFood(ctx, selfID, 1)
		if err != nil {
			logger.Error("听雨框架购买食物请求失败", "action", "buy_pet_food", "error", err)
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "购买食物失败", nil)
			return
		}
		var result any
		if len(data) > 0 && json.Unmarshal(data, &result) != nil {
			result = string(data)
		}
		response.JSON(w, http.StatusOK, 0, "购买食物成功", result)
	}
}

func buyPetBathItemHandler(db *sql.DB, client *mokant.Client, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		qq, itemName := strings.TrimSpace(r.PathValue("qq")), strings.TrimSpace(r.URL.Query().Get("item_name"))
		selfID, err := strconv.ParseInt(qq, 10, 64)
		if err != nil || !validQQ(qq) || itemName == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号或洗护用品不合法", nil)
			return
		}
		var boundQQ string
		if err := db.QueryRowContext(r.Context(), "SELECT qq_number FROM qq_bindings WHERE qq_number = ? AND user_id = ?", qq, user.ID).Scan(&boundQQ); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
				return
			}
			response.JSON(w, http.StatusInternalServerError, 9002, "QQ 绑定校验失败", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		identity, err := ensurePetIdentity(ctx, db, client, user.ID, qq, selfID)
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "宠物资料读取失败", nil)
			return
		}
		catalog, err := client.GetPetBathCatalog(ctx, selfID)
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "洗护目录读取失败", nil)
			return
		}
		itemID := findCatalogID(catalog, itemName, "item_id")
		if itemID == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "洗护用品名称不存在", nil)
			return
		}
		data, err := client.BuyPetBathItem(ctx, selfID, identity.ID, itemID, 1)
		if err != nil {
			logger.Error("听雨框架购买洗护用品请求失败", "action", "buy_pet_bath_item", "error", err)
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "购买洗护用品失败", nil)
			return
		}
		var result any
		if len(data) > 0 && json.Unmarshal(data, &result) != nil {
			result = string(data)
		}
		response.JSON(w, http.StatusOK, 0, "购买成功", result)
	}
}

type qqAccountItem struct {
	QQNumber         string `json:"qq_number"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	Nickname         string `json:"nickname"`
	Status           int    `json:"status"`
	ProtocolID       int64  `json:"protocol_id"`
	DeviceProfileID  int64  `json:"device_profile_id"`
	ServiceMonths    int64  `json:"service_months"`
	ServiceExpiresAt string `json:"service_expires_at"`
	Expired          bool   `json:"expired"`
	NodeID           int64  `json:"node_id"`
	NodeName         string `json:"node_name"`
}

func qqAccountHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		rows, err := db.QueryContext(r.Context(), "SELECT b.qq_number, b.created_at, b.updated_at, b.service_months, COALESCE(b.service_expires_at, ''), b.login_node_id, b.login_node_name FROM qq_bindings b WHERE b.user_id = ? ORDER BY b.id DESC", user.ID)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "QQ 列表加载失败", nil)
			return
		}
		defer rows.Close()
		items := make([]qqAccountItem, 0)
		for rows.Next() {
			var item qqAccountItem
			if err := rows.Scan(&item.QQNumber, &item.CreatedAt, &item.UpdatedAt, &item.ServiceMonths, &item.ServiceExpiresAt, &item.NodeID, &item.NodeName); err != nil {
				response.JSON(w, http.StatusInternalServerError, 9002, "QQ 列表加载失败", nil)
				return
			}
			item.Expired = accountExpired(item.ServiceExpiresAt)
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "QQ 列表加载失败", nil)
			return
		}
		if data, listErr := client.ListBots(r.Context()); listErr == nil {
			if bots, valid := parseMokantBots(data); valid {
				for index := range items {
					for _, bot := range bots {
						if strconv.FormatInt(bot.SelfID, 10) == items[index].QQNumber {
							items[index].Nickname = bot.Nickname
							items[index].Status = bot.Status
							items[index].ProtocolID = bot.ProtocolID
							items[index].DeviceProfileID = bot.DeviceProfileID
							items[index].NodeID = bot.NodeID
							break
						}
					}
				}
			}
		}
		if nodeData, nodeErr := client.ListLoginNodes(r.Context()); nodeErr == nil {
			nodeNames := make(map[int64]string)
			for _, node := range parseMokantLoginNodes(nodeData) {
				nodeNames[node.ID] = node.Name
			}
			for index := range items {
				if name := nodeNames[items[index].NodeID]; name != "" {
					items[index].NodeName = name
					_, _ = db.ExecContext(r.Context(), "UPDATE qq_bindings SET login_node_id = ?, login_node_name = ? WHERE qq_number = ?", items[index].NodeID, name, items[index].QQNumber)
				}
			}
		}
		response.JSON(w, http.StatusOK, 0, "ok", items)
	}
}

func qqAccountInfoHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		qq := strings.TrimSpace(r.PathValue("qq"))
		selfID, err := strconv.ParseInt(qq, 10, 64)
		if err != nil || !validQQ(qq) {
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号不合法", nil)
			return
		}
		var boundQQ string
		if err := db.QueryRowContext(r.Context(), "SELECT qq_number FROM qq_bindings WHERE qq_number = ? AND user_id = ?", qq, user.ID).Scan(&boundQQ); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
				return
			}
			response.JSON(w, http.StatusInternalServerError, 9002, "QQ 绑定校验失败", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		data, err := client.GetBotInfo(ctx, selfID)
		if err != nil {
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "获取 QQ 账号信息失败", nil)
			return
		}
		var info any
		if len(data) == 0 || json.Unmarshal(data, &info) != nil {
			response.JSON(w, http.StatusBadGateway, 5033, "听雨框架返回的 QQ 账号信息格式无效", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", info)
	}
}

type qqAccountOption struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type qqLoginNode struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	Enabled            bool   `json:"enabled"`
	ProxyEnabled       bool   `json:"proxy_enabled"`
	ProxyType          string `json:"proxy_type"`
	Remark             string `json:"remark"`
	LastCheck          string `json:"last_check"`
	AccountCount       int64  `json:"account_count"`
	OnlineAccountCount int64  `json:"online_account_count"`
}

func qqAccountNodesHandler(client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		data, err := client.ListLoginNodes(ctx)
		if err != nil {
			response.JSON(w, http.StatusServiceUnavailable, 5031, "萌卡 NT 尚未连接，登录节点加载失败", nil)
			return
		}
		nodes := parseMokantLoginNodes(data)
		if len(nodes) == 0 {
			response.JSON(w, http.StatusBadGateway, 5033, "萌卡 NT 没有返回登录节点", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", nodes)
	}
}

func qqAccountOptionsHandler(client *mokant.Client, _ ...*mokant.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		protocolData, err := client.ListProtocols(ctx)
		if err != nil {
			response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
			return
		}
		deviceData, err := client.ListDeviceProfiles(ctx)
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "获取设备指纹列表失败", nil)
			return
		}
		protocols, devices := parseMokantProtocols(protocolData), parseMokantOptions(deviceData)
		if len(protocols) == 0 || len(devices) == 0 {
			response.JSON(w, http.StatusBadGateway, 5033, "听雨框架没有可用的协议或设备指纹", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", map[string]any{"protocols": protocols, "device_profiles": devices})
	}
}

func qqAccountPriceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var price int64
		if err := db.QueryRowContext(r.Context(), "SELECT price FROM qq_plans WHERE id = 1").Scan(&price); err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "QQ 价格读取失败", nil)
			return
		}
		if price < 0 {
			response.JSON(w, http.StatusInternalServerError, 9002, "QQ 价格配置无效", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", map[string]any{"monthly_price": price})
	}
}

type qqPlan struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Price       int64  `json:"price"`
	ServiceDays int64  `json:"service_days"`
	Description string `json:"description"`
}

func qqPlansHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		plans, err := loadQQPlans(r.Context(), db, false)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "QQ 套餐读取失败", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", plans)
	}
}

func loadQQPlans(ctx context.Context, db *sql.DB, includeInactive bool) ([]qqPlan, error) {
	plans := make([]qqPlan, 0)
	query := "SELECT id, name, price, service_days, description FROM qq_plans WHERE status = 'active' ORDER BY sort_order, id"
	if includeInactive {
		query = "SELECT id, name, price, service_days, description FROM qq_plans ORDER BY sort_order, id"
	}
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var plan qqPlan
		if err := rows.Scan(&plan.ID, &plan.Name, &plan.Price, &plan.ServiceDays, &plan.Description); err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, rows.Err()
}

func loadQQPlan(ctx context.Context, db *sql.DB, id int64) (qqPlan, error) {
	plans, err := loadQQPlans(ctx, db, false)
	if err != nil {
		return qqPlan{}, err
	}
	for _, plan := range plans {
		if plan.ID == id {
			return plan, nil
		}
	}
	return qqPlan{}, sql.ErrNoRows
}

func qqAccountAddHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		var input struct {
			QQNumber        string `json:"qq_number"`
			Password        string `json:"password"`
			ProtocolID      int64  `json:"protocol_id"`
			DeviceProfileID int64  `json:"device_profile_id"`
			ServiceMonths   int64  `json:"service_months"`
			PlanID          int64  `json:"plan_id"`
			NodeID          int64  `json:"node_id"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || !validQQ(input.QQNumber) || strings.TrimSpace(input.Password) == "" || input.ProtocolID < 0 || input.DeviceProfileID < 0 || input.NodeID < 1 || input.PlanID < 1 && (input.ServiceMonths < 1 || input.ServiceMonths > 12) {
			response.JSON(w, http.StatusBadRequest, 4003, "请输入有效的 QQ 号、密码、登录节点、协议和设备指纹", nil)
			return
		}
		plan, err := loadQQPlan(r.Context(), db, input.PlanID)
		if input.PlanID == 0 {
			plan, err = loadQQPlan(r.Context(), db, 1)
			plan.ServiceDays = input.ServiceMonths * 30
		}
		if err != nil || plan.Price < 0 || plan.ServiceDays < 1 {
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 套餐不可用", nil)
			return
		}
		var ownerID sql.NullInt64
		if err := db.QueryRowContext(r.Context(), "SELECT user_id FROM qq_bindings WHERE qq_number = ?", input.QQNumber).Scan(&ownerID); err == nil {
			if ownerID.Valid && ownerID.Int64 != user.ID {
				response.JSON(w, http.StatusConflict, 4006, "该 QQ 已绑定其他用户", nil)
			} else {
				response.JSON(w, http.StatusConflict, 4007, "该 QQ 已添加，账号无法修改", nil)
			}
			return
		}
		totalPrice := plan.Price
		var coins int64
		if err := db.QueryRowContext(r.Context(), "SELECT coins FROM wallets WHERE user_id = ?", user.ID).Scan(&coins); err != nil || coins < totalPrice {
			response.JSON(w, http.StatusPaymentRequired, 4020, "金币余额不足", map[string]any{"required_coins": totalPrice, "coins": coins})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		nodeData, err := client.ListLoginNodes(ctx)
		if err != nil {
			response.JSON(w, http.StatusServiceUnavailable, 5031, "萌卡 NT 尚未连接", nil)
			return
		}
		loginNode, found := findMokantLoginNode(parseMokantLoginNodes(nodeData), input.NodeID)
		if !found || !loginNode.Enabled {
			response.JSON(w, http.StatusBadRequest, 4003, "所选萌卡登录节点不存在或已停用", nil)
			return
		}
		protocolData, err := client.ListProtocols(ctx)
		if err != nil {
			response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
			return
		}
		deviceData, err := client.ListDeviceProfiles(ctx)
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "获取设备指纹列表失败", nil)
			return
		}
		if !optionContains(parseMokantProtocols(protocolData), input.ProtocolID) || !optionContains(parseMokantOptions(deviceData), input.DeviceProfileID) {
			response.JSON(w, http.StatusBadRequest, 4003, "协议或设备指纹不可用", nil)
			return
		}
		selfID, _ := strconv.ParseInt(input.QQNumber, 10, 64)
		result, err := client.AddAccount(ctx, selfID, input.Password, input.ProtocolID, input.DeviceProfileID, input.NodeID)
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "添加 QQ 账号失败", nil)
			return
		}
		now := timeutil.Now()
		expiresAt := time.Now().In(timeutil.Beijing).AddDate(0, 0, int(plan.ServiceDays)).Format(time.RFC3339)
		tx, err := db.BeginTx(r.Context(), nil)
		if err == nil {
			var walletID, balance int64
			err = tx.QueryRowContext(r.Context(), "SELECT id, coins FROM wallets WHERE user_id = ?", user.ID).Scan(&walletID, &balance)
			if err == nil && balance < totalPrice {
				err = wallet.ErrInsufficient
			}
			if err == nil {
				_, err = tx.ExecContext(r.Context(), "UPDATE wallets SET coins = coins - ?, updated_at = ? WHERE id = ? AND coins >= ?", totalPrice, now, walletID, totalPrice)
			}
			if err == nil {
				_, err = tx.ExecContext(r.Context(), "INSERT INTO wallet_transactions (wallet_id, user_id, amount, balance_after, transaction_type, reference_id, description, created_at) VALUES (?, ?, ?, ?, 'qq_subscription', ?, ?, ?)", walletID, user.ID, -totalPrice, balance-totalPrice, input.QQNumber, fmt.Sprintf("购买 QQ 套餐：%s（%d 天）", plan.Name, plan.ServiceDays), now)
			}
			if err == nil {
				var connectionNodeID int64
				err = tx.QueryRowContext(r.Context(), "SELECT id FROM mokant_nodes WHERE is_default = 1 ORDER BY id LIMIT 1").Scan(&connectionNodeID)
				if err == nil {
					_, err = tx.ExecContext(r.Context(), "INSERT INTO qq_bindings (qq_number, user_id, node_id, login_node_id, login_node_name, service_months, service_expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", input.QQNumber, user.ID, connectionNodeID, input.NodeID, loginNode.Name, (plan.ServiceDays+29)/30, expiresAt, now, now)
				}
			}
			if err == nil {
				err = tx.Commit()
			} else {
				_ = tx.Rollback()
			}
		}
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "QQ 添加成功，但服务开通失败，金币未扣除", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "QQ 添加成功，当前尚未登录", map[string]any{"qq_number": input.QQNumber, "plan_id": plan.ID, "result": json.RawMessage(result)})
	}
}

func qqAccountUpdateHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		qqNumber := strings.TrimSpace(r.PathValue("qq"))
		if !validQQ(qqNumber) {
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号不合法", nil)
			return
		}
		var input struct {
			Password        string `json:"password"`
			ProtocolID      int64  `json:"protocol_id"`
			DeviceProfileID int64  `json:"device_profile_id"`
			NodeID          int64  `json:"node_id"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.Password) == "" || input.ProtocolID < 0 || input.DeviceProfileID < 0 || input.NodeID < 1 {
			response.JSON(w, http.StatusBadRequest, 4003, "请输入 QQ 密码、登录节点、协议和设备指纹", nil)
			return
		}
		var ownerID int64
		if err := db.QueryRowContext(r.Context(), "SELECT user_id FROM qq_bindings WHERE qq_number = ?", qqNumber).Scan(&ownerID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.JSON(w, http.StatusNotFound, 4004, "QQ 账号不存在", nil)
			} else {
				response.JSON(w, http.StatusInternalServerError, 9002, "QQ 账号读取失败", nil)
			}
			return
		}
		if ownerID != user.ID {
			response.JSON(w, http.StatusForbidden, 1003, "无权修改该 QQ 账号", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		nodeData, err := client.ListLoginNodes(ctx)
		if err != nil {
			response.JSON(w, http.StatusServiceUnavailable, 5031, "萌卡 NT 尚未连接", nil)
			return
		}
		loginNode, found := findMokantLoginNode(parseMokantLoginNodes(nodeData), input.NodeID)
		if !found || !loginNode.Enabled {
			response.JSON(w, http.StatusBadRequest, 4003, "所选萌卡登录节点不存在或已停用", nil)
			return
		}
		result, err := client.UpdateAccount(ctx, mustParseQQ(qqNumber), input.Password, input.ProtocolID, input.DeviceProfileID, input.NodeID)
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "QQ 账号修改失败，请确认账号已下线", nil)
			return
		}
		if decodeMokantCode(result) != 0 {
			response.JSON(w, http.StatusBadGateway, 5032, "QQ 账号修改失败，请确认账号已下线", result)
			return
		}
		_, _ = db.ExecContext(r.Context(), "UPDATE qq_bindings SET login_node_id = ?, login_node_name = ?, updated_at = ? WHERE qq_number = ? AND user_id = ?", input.NodeID, loginNode.Name, timeutil.Now(), qqNumber, user.ID)
		response.JSON(w, http.StatusOK, 0, "QQ 账号修改成功", result)
	}
}

func completeQQLogin(ctx context.Context, db *sql.DB, scheduler *autocontrol.Scheduler, userID int64, qq string) {
	now := timeutil.Now()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		slog.Default().Error("QQ 登录成功后开启自动控制失败", "qq", qq, "error", err)
		return
	}
	defer tx.Rollback()
	var bindingID int64
	if err = tx.QueryRowContext(ctx, "SELECT id FROM qq_bindings WHERE qq_number = ? AND user_id = ?", qq, userID).Scan(&bindingID); err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE qq_bindings SET updated_at = ? WHERE id = ?", now, bindingID)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO pet_auto_control_configs (qq_binding_id, enabled, created_at, updated_at) VALUES (?, 1, ?, ?) ON CONFLICT(qq_binding_id) DO UPDATE SET enabled = 1, updated_at = excluded.updated_at`, bindingID, now, now)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO pet_auto_control_states (qq_binding_id, message, updated_at) VALUES (?, 'QQ 登录成功，自动控制已开启', ?) ON CONFLICT(qq_binding_id) DO UPDATE SET message = excluded.message, last_error = '', updated_at = excluded.updated_at`, bindingID, now)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		slog.Default().Error("QQ 登录成功后开启自动控制失败", "qq", qq, "error", err)
		return
	}
	if scheduler != nil {
		scheduler.StartBinding(context.Background(), bindingID, qq)
	}
}

func qqAccountLoginHandler(db *sql.DB, client *mokant.Client, scheduler *autocontrol.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		qq := strings.TrimSpace(r.PathValue("qq"))
		selfID, err := strconv.ParseInt(qq, 10, 64)
		if err != nil || !validQQ(qq) {
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号不合法", nil)
			return
		}
		var boundQQ string
		if err := db.QueryRowContext(r.Context(), "SELECT qq_number FROM qq_bindings WHERE qq_number = ? AND user_id = ?", qq, user.ID).Scan(&boundQQ); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
				return
			}
			response.JSON(w, http.StatusInternalServerError, 9002, "QQ 绑定校验失败", nil)
			return
		}
		var expiresAt string
		if err := db.QueryRowContext(r.Context(), "SELECT COALESCE(service_expires_at, '') FROM qq_bindings WHERE qq_number = ? AND user_id = ?", qq, user.ID).Scan(&expiresAt); err == nil && accountExpired(expiresAt) {
			response.JSON(w, http.StatusForbidden, 4021, "QQ 服务已到期，请先续费", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if cacheData, cacheErr := client.CheckCache(ctx, selfID); cacheErr == nil && mokantCacheValid(cacheData) {
			if result, err := client.CacheLogin(ctx, selfID); err == nil {
				loginResult := decodeMokantLoginResult(result)
				if loginResult.Code == 0 {
					completeQQLogin(r.Context(), db, scheduler, user.ID, qq)
					response.JSON(w, http.StatusOK, 0, "QQ 缓存登录成功", loginResult)
					return
				}
			}
		}
		result, err := client.LoginAccount(ctx, selfID)
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "QQ 密码登录请求失败", nil)
			return
		}
		loginResult := decodeMokantLoginResult(result)
		if loginResult.Code == 0 {
			completeQQLogin(r.Context(), db, scheduler, user.ID, qq)
		}
		response.JSON(w, http.StatusOK, 0, loginResult.Message, loginResult)
	}
}

func qqSecurityMethodsHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		reasonData, _ := client.OpenAccountSecurityAccess(ctx, selfID, "QueryVerifyReason", nil)
		listData, err := client.OpenAccountSecurityAccess(ctx, selfID, "QueryVerifyList", nil)
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "安全验证方式查询失败", nil)
			return
		}
		var reason, methods map[string]any
		_ = json.Unmarshal(reasonData, &reason)
		if json.Unmarshal(listData, &methods) != nil {
			response.JSON(w, http.StatusBadGateway, 5033, "安全验证方式响应无效", nil)
			return
		}
		prompt := firstString(reason, "prompt")
		if prompt == "" {
			prompt = firstString(methods, "prompt")
		}
		response.JSON(w, http.StatusOK, 0, "安全验证方式查询成功", map[string]any{"prompt": prompt, "methods": methods})
	}
}

func qqSecurityQRHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		data, err := client.OpenAccountSecurityAccess(ctx, selfID, "CreateGuarantee", nil)
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "登录二维码创建失败", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "登录二维码创建成功", data)
	}
}

func qqSecurityQRStatusHandler(db *sql.DB, client *mokant.Client, scheduler *autocontrol.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		var input struct {
			GuaranteeToken string `json:"guarantee_token"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.GuaranteeToken) == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "二维码验证参数不完整", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		data, err := client.OpenAccountSecurityAccess(ctx, selfID, "QueryGuaranteeStatus", map[string]any{"guarantee_token": strings.TrimSpace(input.GuaranteeToken)})
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "二维码状态查询失败", nil)
			return
		}
		var query map[string]any
		_ = json.Unmarshal(data, &query)
		status := nestedNumber(query, "query_rsp", "uint32_guarantee_status")
		result := map[string]any{"status_code": status, "status": guaranteeStatusName(status), "raw": query}
		if status == 1 {
			loginData, retryErr := client.RetryAccountSecurityVerify(ctx, selfID, 0, nil)
			if retryErr != nil {
				response.JSON(w, http.StatusBadGateway, 5032, "扫码确认后继续登录失败", nil)
				return
			}
			loginResult := decodeMokantLoginResult(loginData)
			if loginResult.Code == 0 {
				completeQQLogin(r.Context(), db, scheduler, user.ID, strings.TrimSpace(r.PathValue("qq")))
			}
			result["login_result"] = loginResult
		}
		response.JSON(w, http.StatusOK, 0, "二维码状态查询成功", result)
	}
}

func qqSecurityConfirmHandler(db *sql.DB, client *mokant.Client, scheduler *autocontrol.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		data, err := client.RetryAccountSecurityVerify(ctx, selfID, 0, nil)
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "继续 QQ 登录失败，请确认已完成扫码验证", nil)
			return
		}
		result := decodeMokantLoginResult(data)
		if result.Code == 0 {
			completeQQLogin(r.Context(), db, scheduler, user.ID, strings.TrimSpace(r.PathValue("qq")))
		}
		response.JSON(w, http.StatusOK, 0, result.Message, result)
	}
}

func qqSecuritySMSHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		var input struct {
			VerifyType int    `json:"verify_type"`
			Sign       string `json:"sign"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || (input.VerifyType != 3 && input.VerifyType != 4) || strings.TrimSpace(input.Sign) == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "短信验证参数不合法", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		data, err := client.OpenAccountSecurityAccess(ctx, selfID, "GetSMS", map[string]any{"verify_type": input.VerifyType, "sign": strings.TrimSpace(input.Sign)})
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "短信验证请求失败", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "短信验证请求成功", data)
	}
}

func qqSecuritySMSCheckHandler(db *sql.DB, client *mokant.Client, scheduler *autocontrol.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		var input struct {
			VerifyType int    `json:"verify_type"`
			Sign       string `json:"sign"`
			Code       string `json:"code"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || (input.VerifyType != 3 && input.VerifyType != 4) || strings.TrimSpace(input.Sign) == "" || input.VerifyType == 4 && strings.TrimSpace(input.Code) == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "短信验证参数不合法", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		params := map[string]any{"verify_type": input.VerifyType, "sign": strings.TrimSpace(input.Sign)}
		if input.VerifyType == 4 {
			params["code"] = strings.TrimSpace(input.Code)
		}
		data, err := client.OpenAccountSecurityAccess(ctx, selfID, "CheckSMS", params)
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "短信验证提交失败", nil)
			return
		}
		var checked map[string]any
		_ = json.Unmarshal(data, &checked)
		verificationState := nestedNumber(checked, "result", "state")
		if verificationState == 0 {
			verificationState = nestedNumber(checked, "result", "code")
		}
		if verificationState != 1 {
			response.JSON(w, http.StatusOK, 0, "短信验证结果已返回", map[string]any{"verified": false, "result": checked})
			return
		}
		verifySign := firstString(checked, "verify_sign")
		if verifySign == "" {
			if nested, ok := checked["result"].(map[string]any); ok {
				verifySign = firstString(nested, "verify_sign")
			}
		}
		if verifySign == "" {
			response.JSON(w, http.StatusBadGateway, 5033, "短信验证结果缺少登录凭据", nil)
			return
		}
		loginData, err := client.RetryAccountSecurityVerify(ctx, selfID, 2, map[string]any{"verify_sign": verifySign})
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "短信验证通过后继续登录失败", nil)
			return
		}
		loginResult := decodeMokantLoginResult(loginData)
		if loginResult.Code == 0 {
			completeQQLogin(r.Context(), db, scheduler, user.ID, strings.TrimSpace(r.PathValue("qq")))
		}
		response.JSON(w, http.StatusOK, 0, "短信验证结果已返回", map[string]any{"verified": true, "login_result": loginResult})
	}
}

func qqIdentityCaptchaHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		var input struct {
			Ticket  string `json:"ticket"`
			Randstr string `json:"randstr"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.Ticket) == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "身份验证参数不完整", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		data, err := client.SubmitAccountIdentityCaptcha(ctx, selfID, strings.TrimSpace(input.Ticket), strings.TrimSpace(input.Randstr))
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "身份验证提交失败", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "身份验证已提交", data)
	}
}

func qqIdentityPhoneHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		var input struct {
			Mobile   string `json:"mobile"`
			AreaCode string `json:"area_code"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.Mobile) == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "请输入绑定手机号", nil)
			return
		}
		areaCode := strings.TrimPrefix(strings.TrimSpace(input.AreaCode), "+")
		if areaCode == "" {
			areaCode = "86"
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		data, err := client.SubmitAccountIdentityPhone(ctx, selfID, strings.TrimSpace(input.Mobile), areaCode)
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "身份验证短信请求失败", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "身份验证短信已返回", data)
	}
}

func qqIdentitySMSConfirmHandler(db *sql.DB, client *mokant.Client, scheduler *autocontrol.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		var input struct {
			Mobile   string `json:"mobile"`
			AreaCode string `json:"area_code"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.Mobile) == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "身份验证参数不完整", nil)
			return
		}
		areaCode := strings.TrimPrefix(strings.TrimSpace(input.AreaCode), "+")
		if areaCode == "" {
			areaCode = "86"
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if _, err := client.ConfirmAccountIdentitySMS(ctx, selfID, strings.TrimSpace(input.Mobile), areaCode); err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "身份验证短信确认失败", nil)
			return
		}
		data, err := client.RetryAccountIdentityVerify(ctx, selfID)
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "身份验证通过后继续登录失败", nil)
			return
		}
		result := decodeMokantLoginResult(data)
		if result.Code == 0 {
			completeQQLogin(r.Context(), db, scheduler, user.ID, strings.TrimSpace(r.PathValue("qq")))
		}
		response.JSON(w, http.StatusOK, 0, result.Message, result)
	}
}

func qqSecuritySliderHandler(db *sql.DB, client *mokant.Client, scheduler *autocontrol.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		var input struct {
			Ticket  string `json:"ticket"`
			Randstr string `json:"randstr"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.Ticket) == "" || strings.TrimSpace(input.Randstr) == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "滑块验证参数不完整", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		data, err := client.SubmitSlider(ctx, selfID, strings.TrimSpace(input.Ticket), strings.TrimSpace(input.Randstr))
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "滑块验证提交失败", nil)
			return
		}
		result := decodeMokantLoginResult(data)
		if result.Code == 0 {
			completeQQLogin(r.Context(), db, scheduler, user.ID, strings.TrimSpace(r.PathValue("qq")))
		}
		response.JSON(w, http.StatusOK, 0, result.Message, result)
	}
}

func qqSecuritySliderScriptHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		var input struct {
			SliderURL string `json:"slider_url"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			response.JSON(w, http.StatusBadRequest, 4003, "滑块验证参数不合法", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		data, err := client.RegisterCaptchaProxy(ctx, selfID, strings.TrimSpace(input.SliderURL), "/api/qq/captcha-proxy")
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "滑块验证页面加载失败", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "滑块验证页面加载成功", data)
	}
}

func qqSecuritySliderJavaScriptHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		sliderURL := strings.TrimSpace(r.URL.Query().Get("slider_url"))
		if sliderURL == "" {
			http.Error(w, "missing slider_url", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		data, err := client.RegisterCaptchaProxy(ctx, selfID, sliderURL, "/api/qq/captcha-proxy")
		if err != nil {
			http.Error(w, "captcha script unavailable", http.StatusBadGateway)
			return
		}
		var result struct {
			Script string `json:"script"`
		}
		if json.Unmarshal(data, &result) != nil || strings.TrimSpace(result.Script) == "" {
			http.Error(w, "invalid captcha script", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, result.Script)
	}
}

func qqCaptchaProxyHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		var input struct {
			URL     string            `json:"url"`
			Method  string            `json:"method"`
			Headers map[string]string `json:"headers"`
			Body    string            `json:"body"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || !captchaProxyURLAllowed(input.URL) {
			response.JSON(w, http.StatusBadRequest, 4003, "验证码代理地址不合法", nil)
			return
		}
		method := strings.ToUpper(strings.TrimSpace(input.Method))
		if method == "" {
			method = http.MethodGet
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		data, err := client.CaptchaProxy(ctx, selfID, strings.TrimSpace(input.URL), method, input.Headers, input.Body)
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "验证码代理请求失败", nil)
			return
		}
		var result struct {
			Status  int               `json:"status"`
			Headers map[string]string `json:"headers"`
			Body    string            `json:"result"`
		}
		if json.Unmarshal(data, &result) != nil || result.Body == "" {
			response.JSON(w, http.StatusBadGateway, 5032, "验证码代理响应无效", nil)
			return
		}
		decoded, err := base64.StdEncoding.DecodeString(result.Body)
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "验证码代理响应解码失败", nil)
			return
		}
		for key, value := range result.Headers {
			if strings.EqualFold(key, "content-type") || strings.EqualFold(key, "cache-control") {
				w.Header().Set(key, value)
			}
		}
		status := result.Status
		if status < 100 || status > 599 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write(decoded)
	}
}

func qqCaptchaBrowserProxyHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		path := strings.TrimLeft(chi.URLParam(r, "*"), "/")
		if path == "" {
			http.Error(w, "missing captcha path", http.StatusBadRequest)
			return
		}
		target := "https://t.captcha.qq.com/" + path
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
		if err != nil {
			http.Error(w, "captcha request too large", http.StatusRequestEntityTooLarge)
			return
		}
		headers := make(map[string]string)
		for _, name := range []string{"Accept", "Content-Type", "Origin", "Referer", "User-Agent", "X-Requested-With"} {
			if value := strings.TrimSpace(r.Header.Get(name)); value != "" {
				headers[name] = value
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		data, err := client.CaptchaProxy(ctx, selfID, target, r.Method, headers, string(body))
		if err != nil {
			http.Error(w, "captcha proxy request failed", http.StatusBadGateway)
			return
		}
		writeCaptchaProxyResponse(w, data)
	}
}

func writeCaptchaProxyResponse(w http.ResponseWriter, data json.RawMessage) {
	var result struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"result"`
	}
	if json.Unmarshal(data, &result) != nil || result.Body == "" {
		http.Error(w, "invalid captcha proxy response", http.StatusBadGateway)
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(result.Body)
	if err != nil {
		http.Error(w, "invalid captcha proxy body", http.StatusBadGateway)
		return
	}
	contentType := ""
	for key, value := range result.Headers {
		if strings.EqualFold(key, "content-type") || strings.EqualFold(key, "cache-control") {
			w.Header().Set(key, value)
		}
		if strings.EqualFold(key, "content-type") {
			contentType = value
		}
	}
	if strings.Contains(strings.ToLower(contentType), "text/html") {
		decoded = allowRelativeCaptchaProxy(decoded)
	}
	status := result.Status
	if status < 100 || status > 599 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(decoded)
}

func allowRelativeCaptchaProxy(body []byte) []byte {
	const marker = "var matches=[/localhost/,"
	const replacement = `var matches=[/^\/api\/qq\/captcha-proxy\//,/localhost/,`
	return bytes.Replace(body, []byte(marker), []byte(replacement), 1)
}

func captchaProxyURLAllowed(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && parsed.Scheme == "https" && strings.EqualFold(parsed.Hostname(), "t.captcha.qq.com")
}

func verifyQQAccess(r *http.Request, db *sql.DB) (int64, error) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		return 0, errors.New("unauthorized")
	}
	qq := strings.TrimSpace(r.PathValue("qq"))
	selfID, err := strconv.ParseInt(qq, 10, 64)
	if err != nil || !validQQ(qq) {
		return 0, errors.New("invalid_qq")
	}
	var bound string
	if err := db.QueryRowContext(r.Context(), "SELECT qq_number FROM qq_bindings WHERE qq_number = ? AND user_id = ?", qq, user.ID).Scan(&bound); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, errors.New("not_bound")
		}
		return 0, errors.New("database")
	}
	return selfID, nil
}

func writeQQAccessError(w http.ResponseWriter, err error) {
	switch err.Error() {
	case "unauthorized":
		response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
	case "invalid_qq":
		response.JSON(w, http.StatusBadRequest, 4003, "QQ 号不合法", nil)
	case "not_bound":
		response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
	default:
		response.JSON(w, http.StatusInternalServerError, 9002, "QQ 绑定校验失败", nil)
	}
}

func accountExpired(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, value)
	return err == nil && !t.After(timeutil.Current())
}

func qqAccountOfflineHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		qq := strings.TrimSpace(r.PathValue("qq"))
		selfID, err := strconv.ParseInt(qq, 10, 64)
		if err != nil || !validQQ(qq) {
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号不合法", nil)
			return
		}
		var bound string
		if err := db.QueryRowContext(r.Context(), "SELECT qq_number FROM qq_bindings WHERE qq_number = ? AND user_id = ?", qq, user.ID).Scan(&bound); err != nil {
			response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		data, err := client.OfflineAccount(ctx, selfID)
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "停止 QQ 登录会话失败", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "QQ 登录会话已停止", data)
	}
}

func qqAccountRenewHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		qq := strings.TrimSpace(r.PathValue("qq"))
		var input struct {
			ServiceMonths int64 `json:"service_months"`
			PlanID        int64 `json:"plan_id"`
		}
		if !validQQ(qq) || json.NewDecoder(r.Body).Decode(&input) != nil || input.PlanID < 1 && (input.ServiceMonths < 1 || input.ServiceMonths > 12) {
			response.JSON(w, http.StatusBadRequest, 4003, "续费月数不合法", nil)
			return
		}
		plan := qqPlan{}
		var err error
		if input.PlanID > 0 {
			plan, err = loadQQPlan(r.Context(), db, input.PlanID)
		} else {
			plan, err = loadQQPlan(r.Context(), db, 1)
			plan.ServiceDays = input.ServiceMonths * 30
		}
		if err != nil || plan.Price < 0 || plan.ServiceDays < 1 {
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 套餐不可用", nil)
			return
		}
		var expiresAt string
		if err := db.QueryRowContext(r.Context(), "SELECT COALESCE(service_expires_at, '') FROM qq_bindings WHERE qq_number = ? AND user_id = ?", qq, user.ID).Scan(&expiresAt); err != nil {
			response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
			return
		}
		base := timeutil.Current()
		if !accountExpired(expiresAt) {
			if parsed, parseErr := time.Parse(time.RFC3339, expiresAt); parseErr == nil {
				base = parsed.In(timeutil.Beijing)
			}
		}
		newExpires := base.AddDate(0, 0, int(plan.ServiceDays)).Format(time.RFC3339)
		total := plan.Price
		now := timeutil.Now()
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "续费失败", nil)
			return
		}
		defer tx.Rollback()
		var walletID, balance int64
		if err = tx.QueryRowContext(r.Context(), "SELECT id, coins FROM wallets WHERE user_id = ?", user.ID).Scan(&walletID, &balance); err != nil || balance < total {
			response.JSON(w, http.StatusPaymentRequired, 4020, "金币余额不足", map[string]any{"required_coins": total, "coins": balance})
			return
		}
		if _, err = tx.ExecContext(r.Context(), "UPDATE wallets SET coins = coins - ?, updated_at = ? WHERE id = ?", total, now, walletID); err == nil {
			_, err = tx.ExecContext(r.Context(), "INSERT INTO wallet_transactions (wallet_id, user_id, amount, balance_after, transaction_type, reference_id, description, created_at) VALUES (?, ?, ?, ?, 'qq_subscription', ?, ?, ?)", walletID, user.ID, -total, balance-total, qq, fmt.Sprintf("购买 QQ 套餐：%s（%d 天）", plan.Name, plan.ServiceDays), now)
		}
		if err == nil {
			_, err = tx.ExecContext(r.Context(), "UPDATE qq_bindings SET service_months = service_months + ?, service_expires_at = ?, updated_at = ? WHERE qq_number = ? AND user_id = ?", (plan.ServiceDays+29)/30, newExpires, now, qq, user.ID)
		}
		if err != nil || tx.Commit() != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "续费失败，金币未扣除", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "QQ 套餐购买成功", map[string]any{"qq_number": qq, "plan_id": plan.ID, "service_expires_at": newExpires, "service_days": plan.ServiceDays, "total_coins": total})
	}
}

func mokantCacheValid(data json.RawMessage) bool {
	var result struct {
		Valid    bool `json:"valid"`
		HasCache bool `json:"hasCache"`
		HasSnake bool `json:"has_cache"`
	}
	return json.Unmarshal(data, &result) == nil && (result.Valid || result.HasCache || result.HasSnake)
}

type mokantLoginResult struct {
	Code           int    `json:"code"`
	Message        string `json:"message"`
	SliderURL      string `json:"slider_url,omitempty"`
	IdentityURL    string `json:"identity_url,omitempty"`
	SecurityURL    string `json:"security_url,omitempty"`
	SecurityVerify any    `json:"security_verify,omitempty"`
}

func decodeMokantLoginResult(data json.RawMessage) mokantLoginResult {
	var result mokantLoginResult
	var raw struct {
		Data struct {
			SliderURL        string `json:"sliderUrl"`
			SliderURLSnake   string `json:"slider_url"`
			IdentityURL      string `json:"identityUrl"`
			IdentityURLSnake string `json:"identity_url"`
			SecurityURL      string `json:"securityUrl"`
			SecurityURLSnake string `json:"security_url"`
		} `json:"data"`
	}
	_ = json.Unmarshal(data, &result)
	_ = json.Unmarshal(data, &raw)
	if result.SliderURL == "" {
		result.SliderURL = raw.Data.SliderURL
		if result.SliderURL == "" {
			result.SliderURL = raw.Data.SliderURLSnake
		}
	}
	if result.IdentityURL == "" {
		result.IdentityURL = raw.Data.IdentityURL
		if result.IdentityURL == "" {
			result.IdentityURL = raw.Data.IdentityURLSnake
		}
	}
	if raw.Data.SecurityURL != "" {
		result.SecurityURL = raw.Data.SecurityURL
	} else if raw.Data.SecurityURLSnake != "" {
		result.SecurityURL = raw.Data.SecurityURLSnake
	}
	if result.Message == "" {
		result.Message = "QQ 登录未完成"
	}
	return result
}

func firstString(value map[string]any, key string) string {
	if current, ok := value[key]; ok {
		if text, ok := current.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	for _, current := range value {
		if nested, ok := current.(map[string]any); ok {
			if text := firstString(nested, key); text != "" {
				return text
			}
		}
	}
	return ""
}

func nestedNumber(value map[string]any, parent, key string) int {
	nested, ok := value[parent].(map[string]any)
	if !ok {
		return 0
	}
	switch number := nested[key].(type) {
	case float64:
		return int(number)
	case int:
		return number
	case json.Number:
		parsed, _ := strconv.Atoi(number.String())
		return parsed
	default:
		return 0
	}
}

func guaranteeStatusName(status int) string {
	switch status {
	case 1:
		return "confirmed"
	case 3:
		return "scanned"
	default:
		return "waiting"
	}
}

func decodeMokantCode(data json.RawMessage) int {
	var result struct {
		Code int `json:"code"`
	}
	if json.Unmarshal(data, &result) != nil {
		return 1
	}
	return result.Code
}

func mustParseQQ(value string) int64 {
	result, _ := strconv.ParseInt(value, 10, 64)
	return result
}

func parseMokantOptions(data json.RawMessage) []qqAccountOption {
	var raw any
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	if object, ok := raw.(map[string]any); ok {
		if value, exists := object["data"]; exists {
			raw = value
		} else if value, exists := object["items"]; exists {
			raw = value
		}
	}
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	options := make([]qqAccountOption, 0, len(list))
	for _, item := range list {
		option := qqAccountOption{}
		switch value := item.(type) {
		case float64:
			option.ID = int64(value)
		case map[string]any:
			for _, key := range []string{"id", "protocol_id", "device_profile_id"} {
				if number, ok := value[key].(float64); ok {
					option.ID = int64(number)
					break
				}
			}
			for _, key := range []string{"name", "title", "description", "model"} {
				if name, ok := value[key].(string); ok && name != "" {
					option.Name = name
					break
				}
			}
		}
		if option.ID >= 0 {
			options = append(options, option)
		}
	}
	return options
}

func parseMokantProtocols(data json.RawMessage) []qqAccountOption {
	var raw any
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	if object, ok := raw.(map[string]any); ok {
		if value, exists := object["data"]; exists {
			raw = value
		} else if value, exists := object["items"]; exists {
			raw = value
		}
	}
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	options := make([]qqAccountOption, 0, len(list))
	for index, item := range list {
		value, ok := item.(map[string]any)
		if !ok {
			continue
		}
		protocolType, _ := value["type"].(string)
		version, _ := value["ver"].(string)
		name := strings.TrimSpace(strings.Join([]string{protocolType, version}, " "))
		if name == "" {
			name = fmt.Sprintf("协议 %d", index)
		}
		options = append(options, qqAccountOption{ID: int64(index), Name: name})
	}
	return options
}

func optionContains(options []qqAccountOption, id int64) bool {
	for _, option := range options {
		if option.ID == id {
			return true
		}
	}
	return false
}

func parseMokantLoginNodes(data json.RawMessage) []qqLoginNode {
	var raw any
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	if object, ok := raw.(map[string]any); ok {
		if value, exists := object["data"]; exists {
			raw = value
		} else if value, exists := object["items"]; exists {
			raw = value
		} else if value, exists := object["nodes"]; exists {
			raw = value
		}
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var nodes []qqLoginNode
	if json.Unmarshal(encoded, &nodes) != nil {
		return nil
	}
	return nodes
}

func findMokantLoginNode(nodes []qqLoginNode, id int64) (qqLoginNode, bool) {
	for _, node := range nodes {
		if node.ID == id {
			return node, true
		}
	}
	return qqLoginNode{}, false
}

func validQQ(value string) bool {
	if len(value) < 5 || len(value) > 10 {
		return false
	}
	_, err := strconv.ParseInt(value, 10, 64)
	return err == nil
}

type mokantBot struct {
	DeviceProfileID  int64   `json:"device_profile_id"`
	ExtraInfo        string  `json:"extra_info"`
	FriendCount      int64   `json:"friend_count"`
	GroupCount       int64   `json:"group_count"`
	LastActive       int64   `json:"last_active"`
	Level            int64   `json:"level"`
	LoginTime        int64   `json:"login_time"`
	Nickname         string  `json:"nickname"`
	OnlineTime       int64   `json:"online_time"`
	ProtocolID       int64   `json:"protocol_id"`
	Received         int64   `json:"received"`
	SelfID           int64   `json:"self_id"`
	Sent             int64   `json:"sent"`
	Status           int     `json:"status"`
	BoundUserID      *int64  `json:"bound_user_id"`
	BoundUsername    *string `json:"bound_username"`
	ServiceExpiresAt string  `json:"service_expires_at"`
	NodeID           int64   `json:"node_id"`
	NodeName         string  `json:"node_name"`
	FrameworkPresent bool    `json:"framework_present"`
	FrameworkChecked bool    `json:"framework_checked"`
	DatabasePresent  bool    `json:"database_present"`
}

func mokantDebugHandler(client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Action string         `json:"action"`
			Params map[string]any `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.Action) == "" || input.Params == nil {
			response.JSON(w, http.StatusBadRequest, 4003, "调试请求参数不合法", nil)
			return
		}
		if selfID, ok := input.Params["self_id"].(float64); ok {
			input.Params["self_id"] = int64(selfID)
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		data, err := client.RawAction(ctx, strings.TrimSpace(input.Action), input.Params)
		if err != nil {
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "听雨框架调试请求失败", nil)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}

func mokantBotsHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		bots := []mokantBot{}
		frameworkChecked := false
		if data, err := client.ListBots(ctx); err == nil {
			if parsed, ok := parseMokantBots(data); ok {
				bots = parsed
				frameworkChecked = true
			}
		}

		byQQ := make(map[string]int, len(bots))
		for index := range bots {
			bots[index].FrameworkPresent = true
			bots[index].FrameworkChecked = frameworkChecked
			byQQ[strconv.FormatInt(bots[index].SelfID, 10)] = index
		}

		rows, err := db.QueryContext(r.Context(), "SELECT b.qq_number, b.user_id, u.username, COALESCE(b.service_expires_at, ''), b.login_node_id, b.login_node_name FROM qq_bindings b LEFT JOIN users u ON u.id = b.user_id ORDER BY b.id")
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "读取本地 QQ 列表失败", nil)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var qq, serviceExpiresAt, nodeName string
			var userID sql.NullInt64
			var username sql.NullString
			var nodeID int64
			if err := rows.Scan(&qq, &userID, &username, &serviceExpiresAt, &nodeID, &nodeName); err != nil {
				response.JSON(w, http.StatusInternalServerError, 9002, "读取本地 QQ 列表失败", nil)
				return
			}
			index, exists := byQQ[qq]
			if !exists {
				selfID, parseErr := strconv.ParseInt(qq, 10, 64)
				if parseErr != nil {
					continue
				}
				bots = append(bots, mokantBot{SelfID: selfID, FrameworkChecked: frameworkChecked})
				index = len(bots) - 1
				byQQ[qq] = index
			}
			bots[index].DatabasePresent = true
			bots[index].ServiceExpiresAt = serviceExpiresAt
			if bots[index].NodeID == 0 {
				bots[index].NodeID = nodeID
			}
			if bots[index].NodeName == "" {
				bots[index].NodeName = nodeName
			}
			if userID.Valid {
				bots[index].BoundUserID = &userID.Int64
			}
			if username.Valid {
				bots[index].BoundUsername = &username.String
			}
		}
		if err := rows.Err(); err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "读取本地 QQ 列表失败", nil)
			return
		}
		if nodeData, nodeErr := client.ListLoginNodes(r.Context()); nodeErr == nil {
			nodeNames := make(map[int64]string)
			for _, node := range parseMokantLoginNodes(nodeData) {
				nodeNames[node.ID] = node.Name
			}
			for index := range bots {
				if name := nodeNames[bots[index].NodeID]; name != "" {
					bots[index].NodeName = name
				}
			}
		}
		response.JSON(w, http.StatusOK, 0, "ok", bots)
	}
}

func parseMokantBots(data []byte) ([]mokantBot, bool) {
	var raw any
	if len(data) == 0 || json.Unmarshal(data, &raw) != nil {
		return nil, false
	}
	if object, ok := raw.(map[string]any); ok {
		if value, exists := object["data"]; exists {
			raw = value
		} else if value, exists := object["bots"]; exists {
			raw = value
		}
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	var bots []mokantBot
	if json.Unmarshal(encoded, &bots) != nil {
		return nil, false
	}
	return bots, true
}

func qqBindingUpdateHandler(db *sql.DB, scheduler *autocontrol.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		qq := strings.TrimSpace(r.PathValue("qq"))
		if _, err := strconv.ParseInt(qq, 10, 64); err != nil || qq == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号不合法", nil)
			return
		}
		var input struct {
			UserID int64 `json:"user_id"`
			PlanID int64 `json:"plan_id"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.UserID < 1 || input.PlanID < 1 {
			response.JSON(w, http.StatusBadRequest, 4003, "请选择有效用户和套餐", nil)
			return
		}
		adminUser, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		plan, err := loadQQPlan(r.Context(), db, input.PlanID)
		if err != nil || plan.Price < 0 || plan.ServiceDays < 1 {
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 套餐不可用", nil)
			return
		}
		var username, status string
		if err := db.QueryRowContext(r.Context(), "SELECT username, status FROM users WHERE id = ?", input.UserID).Scan(&username, &status); err != nil || status != "active" {
			response.JSON(w, http.StatusBadRequest, 4003, "目标用户不存在或已禁用", nil)
			return
		}
		var oldUserID sql.NullInt64
		var oldExpiresAt string
		_ = db.QueryRowContext(r.Context(), "SELECT user_id, COALESCE(service_expires_at, '') FROM qq_bindings WHERE qq_number = ?", qq).Scan(&oldUserID, &oldExpiresAt)
		now := timeutil.Now()
		base := timeutil.Current()
		if !accountExpired(oldExpiresAt) {
			if parsed, parseErr := time.Parse(time.RFC3339, oldExpiresAt); parseErr == nil {
				base = parsed.In(timeutil.Beijing)
			}
		}
		newExpiresAt := base.AddDate(0, 0, int(plan.ServiceDays)).Format(time.RFC3339)
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "绑定 QQ 失败", nil)
			return
		}
		defer tx.Rollback()
		var walletID, balance int64
		if err = tx.QueryRowContext(r.Context(), "SELECT id, coins FROM wallets WHERE user_id = ?", adminUser.ID).Scan(&walletID, &balance); err != nil || balance < int64(plan.Price) {
			response.JSON(w, http.StatusPaymentRequired, 4020, "管理员金币余额不足", map[string]any{"required_coins": plan.Price, "coins": balance})
			return
		}
		if _, err = tx.ExecContext(r.Context(), "UPDATE wallets SET coins = coins - ?, updated_at = ? WHERE id = ? AND coins >= ?", plan.Price, now, walletID, plan.Price); err == nil {
			_, err = tx.ExecContext(r.Context(), "INSERT INTO wallet_transactions (wallet_id, user_id, amount, balance_after, transaction_type, reference_id, description, created_at) VALUES (?, ?, ?, ?, 'admin_qq_binding', ?, ?, ?)", walletID, adminUser.ID, -plan.Price, balance-int64(plan.Price), qq, fmt.Sprintf("管理员绑定 QQ 套餐：%s（%d 天）", plan.Name, plan.ServiceDays), now)
		}
		if err == nil {
			_, err = tx.ExecContext(r.Context(), "INSERT INTO qq_bindings (qq_number, user_id, service_months, service_expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(qq_number) DO UPDATE SET user_id = excluded.user_id, service_months = qq_bindings.service_months + excluded.service_months, service_expires_at = excluded.service_expires_at, updated_at = excluded.updated_at", qq, input.UserID, (plan.ServiceDays+29)/30, newExpiresAt, now, now)
		}
		if err != nil || tx.Commit() != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "绑定失败，金币未扣除", nil)
			return
		}
		if scheduler != nil {
			var bindingID int64
			if db.QueryRowContext(r.Context(), "SELECT id FROM qq_bindings WHERE qq_number = ?", qq).Scan(&bindingID) == nil {
				config, configErr := autocontrol.LoadConfig(r.Context(), db, bindingID)
				if configErr == nil && config.Enabled {
					scheduler.StartBinding(context.Background(), bindingID, qq)
				}
			}
		}
		details, _ := json.Marshal(map[string]any{"qq": qq, "old_user_id": nullableInt64(oldUserID), "new_user_id": input.UserID, "new_username": username, "plan_id": plan.ID, "service_days": plan.ServiceDays, "service_expires_at": newExpiresAt})
		_, _ = db.ExecContext(r.Context(), "INSERT INTO admin_audit_logs (admin_user_id, action, resource_type, resource_id, details, created_at) VALUES (?, ?, ?, ?, ?, ?)", adminUser.ID, "qq.binding.update", "qq", qq, string(details), now)
		response.JSON(w, http.StatusOK, 0, "QQ 绑定已更新", map[string]any{"qq_number": qq, "user_id": input.UserID, "username": username, "plan_id": plan.ID, "service_days": plan.ServiceDays, "service_expires_at": newExpiresAt})
	}
}

func qqBindingDeleteHandler(db *sql.DB, client *mokant.Client, scheduler *autocontrol.Scheduler, pkScheduler *autopk.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		qq := strings.TrimSpace(r.PathValue("qq"))
		selfID, err := strconv.ParseInt(qq, 10, 64)
		if err != nil || qq == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号不合法", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		data, err := client.ListBots(ctx)
		if err != nil {
			response.JSON(w, http.StatusBadGateway, 5032, "无法确认框架 QQ 列表，暂不能删除", nil)
			return
		}
		bots, ok := parseMokantBots(data)
		if !ok {
			response.JSON(w, http.StatusBadGateway, 5033, "框架 QQ 列表格式无效，暂不能删除", nil)
			return
		}
		for _, bot := range bots {
			if bot.SelfID == selfID {
				response.JSON(w, http.StatusConflict, 4091, "该 QQ 仍存在于框架中，不能删除本地记录", nil)
				return
			}
		}
		var bindingID int64
		var oldUserID sql.NullInt64
		if err := db.QueryRowContext(r.Context(), "SELECT id, user_id FROM qq_bindings WHERE qq_number = ?", qq).Scan(&bindingID, &oldUserID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.JSON(w, http.StatusNotFound, 4004, "QQ 尚未绑定用户", nil)
				return
			}
			response.JSON(w, http.StatusInternalServerError, 9002, "解除绑定失败", nil)
			return
		}
		if scheduler != nil {
			scheduler.StopBinding(bindingID)
		}
		if pkScheduler != nil {
			pkScheduler.StopBinding(bindingID)
		}
		now := timeutil.Now()
		if _, err := db.ExecContext(r.Context(), "DELETE FROM qq_bindings WHERE qq_number = ?", qq); err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "删除本地 QQ 记录失败", nil)
			return
		}
		adminUser, _ := auth.UserFromContext(r.Context())
		details, _ := json.Marshal(map[string]any{"qq": qq, "old_user_id": nullableInt64(oldUserID)})
		_, _ = db.ExecContext(r.Context(), "INSERT INTO admin_audit_logs (admin_user_id, action, resource_type, resource_id, details, created_at) VALUES (?, ?, ?, ?, ?, ?)", adminUser.ID, "qq.binding.delete", "qq", qq, string(details), now)
		response.JSON(w, http.StatusOK, 0, "本地 QQ 记录及关联数据已删除", nil)
	}
}

func nullableInt64(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}

func mokantConfigGet(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var host, port, token string
		_ = db.QueryRowContext(r.Context(), "SELECT COALESCE((SELECT setting_value FROM system_settings WHERE setting_key = 'mokant_host'), '127.0.0.1'), COALESCE((SELECT setting_value FROM system_settings WHERE setting_key = 'mokant_port'), '3001'), COALESCE((SELECT setting_value FROM system_settings WHERE setting_key = 'mokant_token'), '')").Scan(&host, &port, &token)
		p, _ := strconv.Atoi(port)
		response.JSON(w, http.StatusOK, 0, "ok", map[string]any{"mokant_host": host, "mokant_port": p, "mokant_token": token, "mokant_token_set": token != ""})
	}
}

func mokantConfigUpdateManager(db *sql.DB, manager *mokant.Manager) http.HandlerFunc {
	client, err := manager.DefaultClient(context.Background())
	if err != nil {
		return func(w http.ResponseWriter, r *http.Request) {
			response.JSON(w, http.StatusServiceUnavailable, 5031, "默认听雨节点不存在", nil)
		}
	}
	return mokantConfigUpdate(db, client)
}

func mokantManagerStatusHandler(manager *mokant.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, err := manager.DefaultClient(r.Context())
		if err != nil {
			response.JSON(w, http.StatusServiceUnavailable, 5031, "默认听雨节点不存在", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", client.Status())
	}
}

func mokantManagerConnectionHandler(manager *mokant.Manager, connect bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, err := manager.DefaultClient(r.Context())
		if err != nil {
			response.JSON(w, http.StatusServiceUnavailable, 5031, "默认听雨节点不存在", nil)
			return
		}
		if connect {
			_ = client.Connect(context.Background())
		} else {
			client.Disconnect()
		}
		response.JSON(w, http.StatusOK, 0, "ok", client.Status())
	}
}

func mokantConfigUpdate(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Host  string `json:"mokant_host"`
			Port  int    `json:"mokant_port"`
			Token string `json:"mokant_token"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.Host) == "" || input.Port < 1 || input.Port > 65535 {
			response.JSON(w, http.StatusBadRequest, 4004, "听雨框架连接配置不合法", nil)
			return
		}
		var oldToken string
		_ = db.QueryRowContext(r.Context(), "SELECT COALESCE((SELECT setting_value FROM system_settings WHERE setting_key = 'mokant_token'), '')").Scan(&oldToken)
		if strings.TrimSpace(input.Token) == "" {
			input.Token = oldToken
		}
		now := timeutil.Now()
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			response.JSON(w, 500, 9002, "服务暂时不可用", nil)
			return
		}
		defer tx.Rollback()
		for key, value := range map[string]string{"mokant_host": strings.TrimSpace(input.Host), "mokant_port": strconv.Itoa(input.Port), "mokant_token": input.Token} {
			if _, err = tx.ExecContext(r.Context(), "INSERT INTO system_settings (setting_key, setting_value, updated_at) VALUES (?, ?, ?) ON CONFLICT(setting_key) DO UPDATE SET setting_value = excluded.setting_value, updated_at = excluded.updated_at", key, value, now); err != nil {
				response.JSON(w, 500, 9002, "服务暂时不可用", nil)
				return
			}
		}
		if _, err = tx.ExecContext(r.Context(), "UPDATE mokant_nodes SET host = ?, port = ?, token = ?, updated_at = ? WHERE is_default = 1", strings.TrimSpace(input.Host), input.Port, input.Token, now); err != nil {
			response.JSON(w, 500, 9002, "服务暂时不可用", nil)
			return
		}
		if err = tx.Commit(); err != nil {
			response.JSON(w, 500, 9002, "服务暂时不可用", nil)
			return
		}
		client.Configure(strings.TrimSpace(input.Host), input.Port, input.Token)
		response.JSON(w, http.StatusOK, 0, "听雨框架连接配置已更新，正在连接", map[string]any{"mokant_host": input.Host, "mokant_port": input.Port, "mokant_token_set": input.Token != ""})
	}
}

func mokantConnectHandler(client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client.Connect(context.Background())
		response.JSON(w, http.StatusOK, 0, "听雨框架正在连接", client.Status())
	}
}

func mokantDisconnectHandler(client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client.Disconnect()
		response.JSON(w, http.StatusOK, 0, "听雨框架已断开", client.Status())
	}
}

func mokantNodesHandler(db *sql.DB, manager *mokant.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodes, err := manager.Nodes(r.Context())
		if err != nil {
			response.JSON(w, 500, 9002, "节点列表加载失败", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", nodes)
	}
}

func decodeNodeInput(r *http.Request) (string, string, int, string, bool, error) {
	var input struct {
		Name      string `json:"name"`
		Host      string `json:"host"`
		Port      int    `json:"port"`
		Token     string `json:"token"`
		IsDefault bool   `json:"is_default"`
	}
	err := json.NewDecoder(r.Body).Decode(&input)
	input.Name, input.Host = strings.TrimSpace(input.Name), strings.TrimSpace(input.Host)
	if err == nil && (input.Name == "" || input.Host == "" || input.Port < 1 || input.Port > 65535) {
		err = errors.New("invalid node")
	}
	return input.Name, input.Host, input.Port, input.Token, input.IsDefault, err
}

func mokantNodeCreateHandler(db *sql.DB, manager *mokant.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name, host, port, token, def, err := decodeNodeInput(r)
		if err != nil {
			response.JSON(w, 400, 4004, "节点配置不合法", nil)
			return
		}
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			response.JSON(w, 500, 9002, "节点创建失败", nil)
			return
		}
		defer tx.Rollback()
		if def {
			_, err = tx.ExecContext(r.Context(), "UPDATE mokant_nodes SET is_default = 0")
		}
		var result sql.Result
		if err == nil {
			result, err = tx.ExecContext(r.Context(), "INSERT INTO mokant_nodes(name,host,port,token,is_default,created_at,updated_at) VALUES(?,?,?,?,?,?,?)", name, host, port, token, def, timeutil.Now(), timeutil.Now())
		}
		if err == nil {
			err = tx.Commit()
		}
		if err != nil {
			response.JSON(w, 409, 4006, "节点名称已存在", nil)
			return
		}
		id, _ := result.LastInsertId()
		_ = manager.Reload(r.Context())
		response.JSON(w, 201, 0, "节点已创建", map[string]any{"id": id})
	}
}

func mokantNodeUpdateHandler(db *sql.DB, manager *mokant.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		name, host, port, token, def, inputErr := decodeNodeInput(r)
		if err != nil || inputErr != nil {
			response.JSON(w, 400, 4004, "节点配置不合法", nil)
			return
		}
		var oldToken string
		if db.QueryRowContext(r.Context(), "SELECT token FROM mokant_nodes WHERE id = ?", id).Scan(&oldToken) != nil {
			response.JSON(w, 404, 4004, "节点不存在", nil)
			return
		}
		if token == "" {
			token = oldToken
		}
		tx, _ := db.BeginTx(r.Context(), nil)
		defer tx.Rollback()
		if def {
			_, err = tx.ExecContext(r.Context(), "UPDATE mokant_nodes SET is_default = 0")
		}
		if err == nil {
			_, err = tx.ExecContext(r.Context(), "UPDATE mokant_nodes SET name=?,host=?,port=?,token=?,is_default=?,updated_at=? WHERE id=?", name, host, port, token, def, timeutil.Now(), id)
		}
		if err == nil {
			err = tx.Commit()
		}
		if err != nil {
			response.JSON(w, 409, 4006, "节点更新失败", nil)
			return
		}
		_ = manager.Reload(r.Context())
		response.JSON(w, 200, 0, "节点已更新", nil)
	}
}

func mokantNodeDeleteHandler(db *sql.DB, manager *mokant.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			response.JSON(w, 400, 4004, "节点不存在", nil)
			return
		}
		var count int
		var def bool
		if db.QueryRowContext(r.Context(), "SELECT is_default,(SELECT COUNT(*) FROM qq_bindings WHERE node_id=?) FROM mokant_nodes WHERE id=?", id, id).Scan(&def, &count) != nil {
			response.JSON(w, 404, 4004, "节点不存在", nil)
			return
		}
		if def || count > 0 {
			response.JSON(w, 409, 4006, "默认节点或已有账号的节点不能删除", nil)
			return
		}
		_, err = db.ExecContext(r.Context(), "DELETE FROM mokant_nodes WHERE id=?", id)
		if err != nil {
			response.JSON(w, 500, 9002, "节点删除失败", nil)
			return
		}
		_ = manager.Reload(r.Context())
		response.JSON(w, 200, 0, "节点已删除", nil)
	}
}

func mokantNodeConnectionHandler(manager *mokant.Manager, connect bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		client, e := manager.Client(id)
		if err != nil || e != nil {
			response.JSON(w, 404, 4004, "节点不存在", nil)
			return
		}
		if connect {
			client.Connect(context.Background())
		} else {
			client.Disconnect()
		}
		response.JSON(w, 200, 0, "ok", client.Status())
	}
}
func mokantNodeStatusHandler(manager *mokant.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		client, e := manager.Client(id)
		if err != nil || e != nil {
			response.JSON(w, 404, 4004, "节点不存在", nil)
			return
		}
		response.JSON(w, 200, 0, "ok", client.Status())
	}
}

func adminSystemStatusHandler(db *sql.DB, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		err := db.PingContext(r.Context())
		info := map[string]any{"status": "ok", "database": "ok", "database_latency_ms": time.Since(start).Milliseconds(), "version": cfg.Version, "environment": cfg.Environment, "memory_alloc_mb": 0, "database_size_mb": 0, "checked_at": timeutil.Now()}
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		info["memory_alloc_mb"] = stats.Alloc / 1024 / 1024
		if file, statErr := os.Stat(cfg.DatabasePath); statErr == nil {
			info["database_size_mb"] = file.Size() / 1024 / 1024
		}
		if err != nil {
			info["status"] = "error"
			info["database"] = "error"
			response.JSON(w, http.StatusServiceUnavailable, 9001, "数据库不可用", info)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", info)
	}
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" || len(requestID) > 128 {
			requestID = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(r.Context()))
	})
}

func walletHandler(service *wallet.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, _ := auth.UserFromContext(r.Context())
		result, err := service.Get(user.ID)
		if err != nil {
			response.JSON(w, http.StatusNotFound, 3001, "钱包不存在", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", result)
	}
}
func transactionHandler(service *wallet.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, _ := auth.UserFromContext(r.Context())
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
		if page < 1 {
			page = 1
		}
		if size < 1 || size > 100 {
			size = 20
		}
		items, total, err := service.Transactions(user.ID, page, size)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", map[string]any{"items": items, "total": total, "page": page, "page_size": size})
	}
}

func transferHandler(service *wallet.Service, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, _ := auth.UserFromContext(r.Context())
		var input struct {
			RecipientID   int64  `json:"recipient_id"`
			RecipientName string `json:"recipient_name"`
			Amount        int64  `json:"amount"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.RecipientID < 1 || input.Amount <= 0 || strings.TrimSpace(input.RecipientName) == "" {
			response.JSON(w, http.StatusBadRequest, 3004, "转账信息不完整", nil)
			return
		}
		result, err := service.Transfer(user.ID, input.RecipientID, input.Amount, strings.TrimSpace(input.RecipientName))
		if err != nil {
			if errors.Is(err, wallet.ErrInsufficient) {
				response.JSON(w, http.StatusConflict, 3005, "钱包余额不足，无法转账", nil)
				return
			}
			if errors.Is(err, wallet.ErrTransfer) {
				response.JSON(w, http.StatusBadRequest, 3006, "收款用户 ID 或用户名不匹配，或不能给自己转账", nil)
				return
			}
			response.JSON(w, http.StatusInternalServerError, 9002, "转账失败，请稍后重试", nil)
			return
		}
		var senderName, recipientName string
		_ = db.QueryRowContext(r.Context(), "SELECT username FROM users WHERE id = ?", user.ID).Scan(&senderName)
		_ = db.QueryRowContext(r.Context(), "SELECT username FROM users WHERE id = ?", input.RecipientID).Scan(&recipientName)
		details := fmt.Sprintf(`{"sender":"%s","recipient":"%s","amount":%d,"balance_before":%d,"balance_after":%d}`, senderName, recipientName, input.Amount, result.Before, result.After)
		_, _ = db.ExecContext(r.Context(), "INSERT INTO admin_audit_logs (admin_user_id, action, resource_type, resource_id, details, created_at) VALUES (?, ?, ?, ?, ?, ?)", user.ID, "wallet.transfer", "user", strconv.FormatInt(input.RecipientID, 10), details, timeutil.Now())
		response.JSON(w, http.StatusOK, 0, "转账成功，已立即生效且无法撤回", result)
	}
}

func redeemHandler(service *wallet.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, _ := auth.UserFromContext(r.Context())
		var input struct {
			Code string `json:"code"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.Code) == "" {
			response.JSON(w, http.StatusBadRequest, 5002, "请输入卡密", nil)
			return
		}
		result, amount, err := service.Redeem(user.ID, input.Code)
		if err != nil {
			if errors.Is(err, wallet.ErrRedeemCode) {
				response.JSON(w, http.StatusConflict, 5003, "卡密不存在或已使用", nil)
				return
			}
			response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "兑换成功", map[string]any{"coins": result.Coins, "updated_at": result.UpdatedAt, "amount": amount})
	}
}

func healthHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			response.JSON(w, http.StatusServiceUnavailable, 9001, "数据库不可用", map[string]string{"status": "error", "database": "error"})
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", map[string]string{"status": "ok", "database": "ok"})
	}
}

func versionHandler(cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response.JSON(w, http.StatusOK, 0, "ok", map[string]string{"name": "qq-pet", "version": cfg.Version, "environment": cfg.Environment})
	}
}
