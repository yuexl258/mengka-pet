package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"qq-pet/backend/internal/auth"
	"qq-pet/backend/internal/autocontrol"
	"qq-pet/backend/internal/mokant"
	"qq-pet/backend/internal/response"
)

func autoControlInventoryHandler(db *sql.DB, client *mokant.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		food, foodErr := client.GetPetFoodCatalog(ctx, selfID)
		bath, bathErr := client.GetPetBathInventory(ctx, selfID)
		bathCatalog, bathCatalogErr := client.GetPetBathCatalog(ctx, selfID)
		if foodErr != nil || bathErr != nil || bathCatalogErr != nil {
			if errors.Is(foodErr, mokant.ErrNotConnected) || errors.Is(bathErr, mokant.ErrNotConnected) || errors.Is(bathCatalogErr, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
			} else {
				response.JSON(w, http.StatusBadGateway, 5032, "宠物库存加载失败", nil)
			}
			return
		}
		var foodInventory, bathInventory, parsedBathCatalog any
		if json.Unmarshal(food, &foodInventory) != nil || json.Unmarshal(bath, &bathInventory) != nil || json.Unmarshal(bathCatalog, &parsedBathCatalog) != nil {
			response.JSON(w, http.StatusBadGateway, 5033, "宠物库存格式无效", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", map[string]any{"food_inventory": foodInventory, "bath_inventory": bathInventory, "bath_catalog": parsedBathCatalog})
	}
}

func autoControlConfigHandler(db *sql.DB, update bool, scheduler *autocontrol.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		bindingID, err := userBindingID(r, db, user.ID)
		if err != nil {
			writeAutoControlBindingError(w, err)
			return
		}
		if !update {
			config, loadErr := autocontrol.LoadConfig(r.Context(), db, bindingID)
			if loadErr != nil {
				response.JSON(w, http.StatusInternalServerError, 9002, "自动控制配置读取失败", nil)
				return
			}
			response.JSON(w, http.StatusOK, 0, "ok", config)
			return
		}
		var config autocontrol.Config
		if json.NewDecoder(r.Body).Decode(&config) != nil {
			response.JSON(w, http.StatusBadRequest, 4003, "自动控制配置不合法", nil)
			return
		}
		if err := autocontrol.SaveConfig(r.Context(), db, bindingID, config); err != nil {
			response.JSON(w, http.StatusBadRequest, 4003, err.Error(), nil)
			return
		}
		savedConfig, err := autocontrol.LoadConfig(r.Context(), db, bindingID)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "自动控制配置读取失败", nil)
			return
		}
		if scheduler != nil {
			qq := strings.TrimSpace(r.PathValue("qq"))
			if savedConfig.Enabled || savedConfig.AutoPokeEnabled {
				scheduler.StartBinding(context.Background(), bindingID, qq)
			} else {
				scheduler.StopBinding(bindingID)
			}
		}
		response.JSON(w, http.StatusOK, 0, "自动控制配置已保存", savedConfig)
	}
}

func autoControlStateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		bindingID, err := userBindingID(r, db, user.ID)
		if err != nil {
			writeAutoControlBindingError(w, err)
			return
		}
		state, err := autocontrol.LoadState(r.Context(), db, bindingID)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "自动控制状态读取失败", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", state)
	}
}

func autoControlLogsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		bindingID, err := userBindingID(r, db, user.ID)
		if err != nil {
			writeAutoControlBindingError(w, err)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit < 1 || limit > 100 {
			limit = 20
		}
		logs, err := autocontrol.LoadLogs(r.Context(), db, bindingID, limit)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "自动控制日志读取失败", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", logs)
	}
}

func userBindingID(r *http.Request, db *sql.DB, userID int64) (int64, error) {
	qq := strings.TrimSpace(r.PathValue("qq"))
	if !validQQ(qq) {
		return 0, errors.New("invalid_qq")
	}
	var bindingID int64
	err := db.QueryRowContext(r.Context(), "SELECT id FROM qq_bindings WHERE qq_number = ? AND user_id = ?", qq, userID).Scan(&bindingID)
	return bindingID, err
}

func writeAutoControlBindingError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
		return
	}
	if err.Error() == "invalid_qq" {
		response.JSON(w, http.StatusBadRequest, 4003, "QQ 号不合法", nil)
		return
	}
	response.JSON(w, http.StatusInternalServerError, 9002, "QQ 绑定校验失败", nil)
}
