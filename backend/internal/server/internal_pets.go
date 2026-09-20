package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"qq-pet/backend/internal/response"
	"qq-pet/backend/internal/timeutil"
)

type internalPetItem struct {
	ID        int64  `json:"id"`
	UserID    string `json:"user_id"`
	PetID     string `json:"pet_id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func internalPetListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.QueryContext(r.Context(), "SELECT id, user_id, pet_id, created_at, updated_at FROM pet_auto_pk_internal_pets ORDER BY id")
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "内部宠物读取失败", nil)
			return
		}
		defer rows.Close()
		items := make([]internalPetItem, 0)
		for rows.Next() {
			var item internalPetItem
			if err := rows.Scan(&item.ID, &item.UserID, &item.PetID, &item.CreatedAt, &item.UpdatedAt); err != nil {
				response.JSON(w, http.StatusInternalServerError, 9002, "内部宠物读取失败", nil)
				return
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "内部宠物读取失败", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", items)
	}
}

func internalPetCreateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			UserID string `json:"user_id"`
			PetID  string `json:"pet_id"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			response.JSON(w, http.StatusBadRequest, 4003, "内部宠物参数不合法", nil)
			return
		}
		input.UserID = strings.TrimSpace(input.UserID)
		input.PetID = strings.TrimSpace(input.PetID)
		if input.UserID == "" || input.PetID == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 和 pet_id 不能为空", nil)
			return
		}
		now := timeutil.Now()
		result, err := db.ExecContext(r.Context(), "INSERT INTO pet_auto_pk_internal_pets (user_id, pet_id, created_at, updated_at) VALUES (?, ?, ?, ?)", input.UserID, input.PetID, now, now)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "内部宠物添加失败", nil)
			return
		}
		id, _ := result.LastInsertId()
		response.JSON(w, http.StatusOK, 0, "内部宠物已添加", internalPetItem{ID: id, UserID: input.UserID, PetID: input.PetID, CreatedAt: now, UpdatedAt: now})
	}
}

func internalPetDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
		if err != nil || id < 1 {
			response.JSON(w, http.StatusBadRequest, 4003, "内部宠物编号不合法", nil)
			return
		}
		result, err := db.ExecContext(r.Context(), "DELETE FROM pet_auto_pk_internal_pets WHERE id = ?", id)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "内部宠物删除失败", nil)
			return
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			response.JSON(w, http.StatusNotFound, 4004, "内部宠物不存在", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "内部宠物已删除", nil)
	}
}
