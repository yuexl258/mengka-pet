package admin

import (
	"bufio"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"qq-pet/backend/internal/auth"
	"qq-pet/backend/internal/response"
	"qq-pet/backend/internal/timeutil"
	"qq-pet/backend/internal/wallet"
)

type Service struct{ db *sql.DB }

type UserItem struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Coins     int64  `json:"coins"`
}

type Settings struct {
	RegistrationEnabled bool   `json:"registration_enabled"`
	RegisterGiftCoins   int64  `json:"register_gift_coins"`
	SystemName          string `json:"system_name"`
	MokantHost          string `json:"mokant_host"`
	MokantPort          int    `json:"mokant_port"`
	MokantTokenSet      bool   `json:"mokant_token_set"`
}

type QQPlan struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Price       int64  `json:"price"`
	ServiceDays int64  `json:"service_days"`
	Description string `json:"description"`
	Status      string `json:"status"`
	SortOrder   int64  `json:"sort_order"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type RedeemCodeItem struct {
	ID             int64  `json:"id"`
	Code           string `json:"code"`
	Amount         int64  `json:"amount"`
	Status         string `json:"status"`
	CreatedBy      int64  `json:"created_by"`
	CreatedByName  string `json:"created_by_name"`
	RedeemedBy     *int64 `json:"redeemed_by"`
	RedeemedByName string `json:"redeemed_by_name"`
	CreatedAt      string `json:"created_at"`
	RedeemedAt     string `json:"redeemed_at"`
}

func NewService(db *sql.DB) *Service { return &Service{db: db} }

func (s *Service) Users(w http.ResponseWriter, r *http.Request) {
	page, pageSize := pagination(r)
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	status := r.URL.Query().Get("status")
	role := r.URL.Query().Get("role")
	where := []string{"1 = 1"}
	args := []any{}
	if keyword != "" {
		where = append(where, "username LIKE ?")
		args = append(args, "%"+keyword+"%")
	}
	if status == "active" || status == "disabled" {
		where = append(where, "status = ?")
		args = append(args, status)
	}
	if role == "user" || role == "admin" {
		where = append(where, "role = ?")
		args = append(args, role)
	}
	condition := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM users WHERE "+condition, args...).Scan(&total); err != nil {
		internalError(w)
		return
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(r.Context(), "SELECT u.id, u.username, u.role, u.status, u.created_at, u.updated_at, COALESCE(w.coins, 0) FROM users u LEFT JOIN wallets w ON w.user_id = u.id WHERE "+strings.ReplaceAll(condition, "username", "u.username")+" ORDER BY u.id DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		internalError(w)
		return
	}
	defer rows.Close()
	items := make([]UserItem, 0)
	for rows.Next() {
		var item UserItem
		if err := rows.Scan(&item.ID, &item.Username, &item.Role, &item.Status, &item.CreatedAt, &item.UpdatedAt, &item.Coins); err != nil {
			internalError(w)
			return
		}
		items = append(items, item)
	}
	response.JSON(w, http.StatusOK, 0, "ok", map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (s *Service) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		response.JSON(w, http.StatusBadRequest, 4003, "用户编号不合法", nil)
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil || (input.Status != "active" && input.Status != "disabled") {
		response.JSON(w, http.StatusBadRequest, 4003, "用户状态不合法", nil)
		return
	}
	var oldRole, oldStatus string
	if err := s.db.QueryRowContext(r.Context(), "SELECT role, status FROM users WHERE id = ?", id).Scan(&oldRole, &oldStatus); err != nil {
		response.JSON(w, http.StatusNotFound, 4002, "用户不存在", nil)
		return
	}
	if input.Status == "disabled" && oldRole == "admin" && s.adminCount(r) <= 1 {
		response.JSON(w, http.StatusConflict, 4005, "不能禁用最后一个管理员", nil)
		return
	}
	if _, err := s.db.ExecContext(r.Context(), "UPDATE users SET status = ?, updated_at = ? WHERE id = ?", input.Status, timeutil.Now(), id); err != nil {
		internalError(w)
		return
	}
	s.audit(r, "user.status.update", "user", id, map[string]string{"from": oldStatus, "to": input.Status})
	response.JSON(w, http.StatusOK, 0, "用户状态已更新", nil)
}

func (s *Service) UpdateRole(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		response.JSON(w, http.StatusBadRequest, 4003, "用户编号不合法", nil)
		return
	}
	var input struct {
		Role string `json:"role"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil || (input.Role != "user" && input.Role != "admin") {
		response.JSON(w, http.StatusBadRequest, 4003, "用户角色不合法", nil)
		return
	}
	var oldRole, status string
	if err := s.db.QueryRowContext(r.Context(), "SELECT role, status FROM users WHERE id = ?", id).Scan(&oldRole, &status); err != nil {
		response.JSON(w, http.StatusNotFound, 4002, "用户不存在", nil)
		return
	}
	if input.Role == "user" && oldRole == "admin" && s.adminCount(r) <= 1 {
		response.JSON(w, http.StatusConflict, 4005, "不能降级最后一个管理员", nil)
		return
	}
	if _, err := s.db.ExecContext(r.Context(), "UPDATE users SET role = ?, updated_at = ? WHERE id = ?", input.Role, timeutil.Now(), id); err != nil {
		internalError(w)
		return
	}
	s.audit(r, "user.role.update", "user", id, map[string]string{"from": oldRole, "to": input.Role, "status": status})
	response.JSON(w, http.StatusOK, 0, "用户角色已更新", nil)
}

func (s *Service) ResetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		response.JSON(w, http.StatusBadRequest, 4003, "用户编号不合法", nil)
		return
	}
	var username string
	if err := s.db.QueryRowContext(r.Context(), "SELECT username FROM users WHERE id = ?", id).Scan(&username); err != nil {
		response.JSON(w, http.StatusNotFound, 4002, "用户不存在", nil)
		return
	}
	password, err := randomPassword(10)
	if err != nil {
		internalError(w)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		internalError(w)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), "UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?", string(hash), timeutil.Now(), id); err != nil {
		internalError(w)
		return
	}
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM sessions WHERE user_id = ?", id); err != nil || tx.Commit() != nil {
		internalError(w)
		return
	}
	s.audit(r, "user.password.reset", "user", id, map[string]string{"username": username})
	response.JSON(w, http.StatusOK, 0, "密码重置成功", map[string]string{"username": username, "password": password})
}

func randomPassword(length int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	for i := range bytes {
		bytes[i] = alphabet[int(bytes[i])%len(alphabet)]
	}
	return string(bytes), nil
}

type StrangerItem struct {
	ID             int64           `json:"id"`
	UserID         string          `json:"user_id"`
	PetID          string          `json:"pet_id"`
	PetName        string          `json:"pet_name"`
	Nickname       string          `json:"nickname"`
	Power          *int64          `json:"power"`
	DominantType   *int64          `json:"dominant_type"`
	RawData        json.RawMessage `json:"raw_data"`
	ImportedAt     string          `json:"imported_at"`
	UpdatedAt      string          `json:"updated_at"`
	PowerUpdatedAt string          `json:"power_updated_at"`
}

func (s *Service) StrangerPets(w http.ResponseWriter, r *http.Request) {
	page, pageSize := pagination(r)
	var total int
	if err := s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM pet_pk_strangers").Scan(&total); err != nil {
		response.JSON(w, http.StatusInternalServerError, 9002, "陌生人数据查询失败: "+err.Error(), nil)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, user_id, pet_id, pet_name, nickname, power, dominant_type, raw_data, imported_at, updated_at, COALESCE(power_updated_at, '') FROM pet_pk_strangers ORDER BY id ASC LIMIT ? OFFSET ?`, pageSize, (page-1)*pageSize)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, 9002, "陌生人数据查询失败: "+err.Error(), nil)
		return
	}
	defer rows.Close()
	items := make([]StrangerItem, 0)
	for rows.Next() {
		var item StrangerItem
		var rawData string
		if err := rows.Scan(&item.ID, &item.UserID, &item.PetID, &item.PetName, &item.Nickname, &item.Power, &item.DominantType, &rawData, &item.ImportedAt, &item.UpdatedAt, &item.PowerUpdatedAt); err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "陌生人数据读取失败: "+err.Error(), nil)
			return
		}
		item.RawData = json.RawMessage(rawData)
		items = append(items, item)
	}
	response.JSON(w, http.StatusOK, 0, "ok", map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (s *Service) StrangerPetDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		response.JSON(w, 400, 4003, "陌生人编号不合法", nil)
		return
	}
	var item StrangerItem
	var rawData string
	if err := s.db.QueryRowContext(r.Context(), `SELECT id, user_id, pet_id, pet_name, nickname, power, dominant_type, raw_data, imported_at, updated_at, COALESCE(power_updated_at, '') FROM pet_pk_strangers WHERE id = ?`, id).Scan(&item.ID, &item.UserID, &item.PetID, &item.PetName, &item.Nickname, &item.Power, &item.DominantType, &rawData, &item.ImportedAt, &item.UpdatedAt, &item.PowerUpdatedAt); err != nil {
		response.JSON(w, 404, 4004, "陌生人宠物不存在", nil)
		return
	}
	item.RawData = json.RawMessage(rawData)
	response.JSON(w, 200, 0, "ok", item)
}

func (s *Service) DeleteStrangerPet(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		response.JSON(w, 400, 4003, "陌生人编号不合法", nil)
		return
	}
	var userID string
	if err := s.db.QueryRowContext(r.Context(), "SELECT user_id FROM pet_pk_strangers WHERE id = ?", id).Scan(&userID); err != nil {
		response.JSON(w, 404, 4004, "陌生人宠物不存在", nil)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM pet_pk_strangers WHERE id = ?", id); err != nil {
		internalError(w)
		return
	}
	adminUser, _ := auth.UserFromContext(r.Context())
	details, _ := json.Marshal(map[string]any{"user_id": userID})
	if _, err = tx.ExecContext(r.Context(), "INSERT INTO admin_audit_logs (admin_user_id, action, resource_type, resource_id, details, created_at) VALUES (?, ?, ?, ?, ?, ?)", adminUser.ID, "stranger_pet.delete", "pet_pk_stranger", id, string(details), timeutil.Now()); err != nil || tx.Commit() != nil {
		internalError(w)
		return
	}
	response.JSON(w, 200, 0, "陌生人宠物已删除", nil)
}

func (s *Service) ImportStrangerPet(w http.ResponseWriter, r *http.Request) {
	var item map[string]any
	if json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&item) != nil {
		response.JSON(w, 400, 4003, "JSON 格式不合法", nil)
		return
	}
	if err := s.upsertStrangers(r, []map[string]any{item}, "json"); err != nil {
		response.JSON(w, 400, 4003, err.Error(), nil)
		return
	}
	response.JSON(w, 200, 0, "陌生人宠物已导入", nil)
}

func (s *Service) ImportStrangerTXT(w http.ResponseWriter, r *http.Request) {
	scanner := bufio.NewScanner(io.LimitReader(r.Body, 20<<20))
	scanner.Buffer(make([]byte, 4096), 2<<20)
	items := make([]map[string]any, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item map[string]any
		if json.Unmarshal([]byte(line), &item) != nil {
			response.JSON(w, 400, 4003, "TXT 中存在非法 JSON 行", nil)
			return
		}
		items = append(items, item)
	}
	if err := scanner.Err(); err != nil {
		internalError(w)
		return
	}
	if err := s.upsertStrangers(r, items, "txt"); err != nil {
		response.JSON(w, 400, 4003, err.Error(), nil)
		return
	}
	response.JSON(w, 200, 0, "陌生人宠物 TXT 导入成功", map[string]int{"count": len(items)})
}

func (s *Service) upsertStrangers(r *http.Request, items []map[string]any, source string) error {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := timeutil.Now()
	for _, item := range items {
		userID := strings.TrimSpace(fmt.Sprint(item["user_id"]))
		if userID == "" || userID == "<nil>" {
			return errors.New("user_id 不能为空")
		}
		raw, _ := json.Marshal(item)
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO pet_pk_strangers (user_id, pet_id, pet_name, nickname, power, dominant_type, raw_data, imported_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(user_id) DO UPDATE SET pet_id=excluded.pet_id, pet_name=excluded.pet_name, nickname=excluded.nickname, power=excluded.power, dominant_type=excluded.dominant_type, raw_data=excluded.raw_data, updated_at=excluded.updated_at`, userID, strings.TrimSpace(fmt.Sprint(item["pet_id"])), strings.TrimSpace(fmt.Sprint(item["pet_name"])), strings.TrimSpace(fmt.Sprint(item["nickname"])), item["power"], item["dominant_type"], string(raw), now, now); err != nil {
			return err
		}
	}
	adminUser, _ := auth.UserFromContext(r.Context())
	details, _ := json.Marshal(map[string]any{"count": len(items), "source": source})
	if _, err = tx.ExecContext(r.Context(), "INSERT INTO admin_audit_logs (admin_user_id, action, resource_type, resource_id, details, created_at) VALUES (?, ?, ?, ?, ?, ?)", adminUser.ID, "stranger_pet.import", "pet_pk_stranger", "batch", string(details), now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) ExportStrangerTXT(w http.ResponseWriter, r *http.Request) {
	query := "SELECT raw_data FROM pet_pk_strangers ORDER BY id ASC"
	args := []any{}
	if r.URL.Query().Get("all") != "true" {
		page, size := pagination(r)
		query += " LIMIT ? OFFSET ?"
		args = append(args, size, (page-1)*size)
	}
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		internalError(w)
		return
	}
	defer rows.Close()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=pet-pk-strangers.txt")
	for rows.Next() {
		var raw string
		if rows.Scan(&raw) == nil {
			_, _ = fmt.Fprintln(w, raw)
		}
	}
}

func (s *Service) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.settings(r)
	if err != nil {
		internalError(w)
		return
	}
	response.JSON(w, http.StatusOK, 0, "ok", settings)
}

func (s *Service) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var input Settings
	if json.NewDecoder(r.Body).Decode(&input) != nil || input.RegisterGiftCoins < 0 {
		response.JSON(w, http.StatusBadRequest, 4004, "系统设置不合法", nil)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w)
		return
	}
	defer tx.Rollback()
	user, _ := auth.UserFromContext(r.Context())
	if strings.TrimSpace(input.SystemName) == "" {
		if err := tx.QueryRowContext(r.Context(), "SELECT setting_value FROM system_settings WHERE setting_key = 'system_name'").Scan(&input.SystemName); err != nil {
			input.SystemName = "QQ 宠物"
		}
	}
	if len([]rune(input.SystemName)) > 50 {
		response.JSON(w, http.StatusBadRequest, 4004, "系统名称长度必须为 1 到 50 个字符", nil)
		return
	}
	updatedAt := timeutil.Now()
	if _, err = tx.ExecContext(r.Context(), "INSERT INTO system_settings (setting_key, setting_value, updated_by, updated_at) VALUES ('registration_enabled', ?, ?, ?) ON CONFLICT(setting_key) DO UPDATE SET setting_value = excluded.setting_value, updated_by = excluded.updated_by, updated_at = excluded.updated_at", strconv.FormatBool(input.RegistrationEnabled), user.ID, updatedAt); err != nil {
		internalError(w)
		return
	}
	if _, err = tx.ExecContext(r.Context(), "INSERT INTO system_settings (setting_key, setting_value, updated_by, updated_at) VALUES ('register_gift_coins', ?, ?, ?) ON CONFLICT(setting_key) DO UPDATE SET setting_value = excluded.setting_value, updated_by = excluded.updated_by, updated_at = excluded.updated_at", strconv.FormatInt(input.RegisterGiftCoins, 10), user.ID, updatedAt); err != nil {
		internalError(w)
		return
	}
	if _, err = tx.ExecContext(r.Context(), "INSERT INTO system_settings (setting_key, setting_value, updated_by, updated_at) VALUES ('system_name', ?, ?, ?) ON CONFLICT(setting_key) DO UPDATE SET setting_value = excluded.setting_value, updated_by = excluded.updated_by, updated_at = excluded.updated_at", strings.TrimSpace(input.SystemName), user.ID, updatedAt); err != nil {
		internalError(w)
		return
	}
	if err = tx.Commit(); err != nil {
		internalError(w)
		return
	}
	s.audit(r, "settings.registration.update", "system_settings", "registration", input)
	response.JSON(w, http.StatusOK, 0, "系统设置已更新", input)
}

func (s *Service) QQPlans(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), "SELECT id, name, price, service_days, description, status, sort_order, created_at, updated_at FROM qq_plans ORDER BY sort_order, id")
	if err != nil {
		internalError(w)
		return
	}
	defer rows.Close()
	items := make([]QQPlan, 0)
	for rows.Next() {
		var item QQPlan
		if err := rows.Scan(&item.ID, &item.Name, &item.Price, &item.ServiceDays, &item.Description, &item.Status, &item.SortOrder, &item.CreatedAt, &item.UpdatedAt); err != nil {
			internalError(w)
			return
		}
		items = append(items, item)
	}
	response.JSON(w, http.StatusOK, 0, "ok", items)
}

func (s *Service) CreateQQPlan(w http.ResponseWriter, r *http.Request) {
	var input QQPlan
	if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.Name) == "" || input.Price < 0 || input.ServiceDays < 1 || input.Status != "active" && input.Status != "inactive" {
		response.JSON(w, http.StatusBadRequest, 4003, "套餐参数不合法", nil)
		return
	}
	result, err := s.db.ExecContext(r.Context(), "INSERT INTO qq_plans (name, price, service_days, description, status, sort_order, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)", strings.TrimSpace(input.Name), input.Price, input.ServiceDays, strings.TrimSpace(input.Description), input.Status, input.SortOrder, timeutil.Now())
	if err != nil {
		internalError(w)
		return
	}
	input.ID, _ = result.LastInsertId()
	s.audit(r, "qq_plan.create", "qq_plan", input.ID, input)
	response.JSON(w, http.StatusOK, 0, "套餐已创建", input)
}

func (s *Service) UpdateQQPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		response.JSON(w, http.StatusBadRequest, 4003, "套餐编号不合法", nil)
		return
	}
	var input QQPlan
	if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.Name) == "" || input.Price < 0 || input.ServiceDays < 1 || input.Status != "active" && input.Status != "inactive" {
		response.JSON(w, http.StatusBadRequest, 4003, "套餐参数不合法", nil)
		return
	}
	if _, err := s.db.ExecContext(r.Context(), "UPDATE qq_plans SET name = ?, price = ?, service_days = ?, description = ?, status = ?, sort_order = ?, updated_at = ? WHERE id = ?", strings.TrimSpace(input.Name), input.Price, input.ServiceDays, strings.TrimSpace(input.Description), input.Status, input.SortOrder, timeutil.Now(), id); err != nil {
		internalError(w)
		return
	}
	s.audit(r, "qq_plan.update", "qq_plan", id, input)
	response.JSON(w, http.StatusOK, 0, "套餐已更新", nil)
}

func (s *Service) DeleteQQPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok || id == 1 {
		response.JSON(w, http.StatusBadRequest, 4003, "套餐编号不合法", nil)
		return
	}
	if _, err := s.db.ExecContext(r.Context(), "DELETE FROM qq_plans WHERE id = ?", id); err != nil {
		internalError(w)
		return
	}
	s.audit(r, "qq_plan.delete", "qq_plan", id, nil)
	response.JSON(w, http.StatusOK, 0, "套餐已删除", nil)
}

func (s *Service) AuditLogs(w http.ResponseWriter, r *http.Request) {
	page, pageSize := pagination(r)
	var total int
	if err := s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM admin_audit_logs").Scan(&total); err != nil {
		internalError(w)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT a.id, a.admin_user_id, u.username, a.action, a.resource_type, a.resource_id, a.details, a.created_at FROM admin_audit_logs a JOIN users u ON u.id = a.admin_user_id ORDER BY a.id DESC LIMIT ? OFFSET ?`, pageSize, (page-1)*pageSize)
	if err != nil {
		internalError(w)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, adminID int64
		var username, action, resourceType, resourceID, details, createdAt string
		if err := rows.Scan(&id, &adminID, &username, &action, &resourceType, &resourceID, &details, &createdAt); err != nil {
			internalError(w)
			return
		}
		items = append(items, map[string]any{"id": id, "admin_user_id": adminID, "admin_username": username, "action": action, "resource_type": resourceType, "resource_id": resourceID, "details": details, "created_at": createdAt})
	}
	response.JSON(w, http.StatusOK, 0, "ok", map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (s *Service) AdjustWallet(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseID(r.PathValue("id"))
	if !ok {
		response.JSON(w, http.StatusBadRequest, 3002, "用户编号不合法", nil)
		return
	}
	var input struct {
		Amount      int64  `json:"amount"`
		Description string `json:"description"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil || input.Amount == 0 {
		response.JSON(w, http.StatusBadRequest, 3003, "金币数量不能为零", nil)
		return
	}
	description := "管理员操作"
	if strings.TrimSpace(input.Description) != "" {
		description += "：" + strings.TrimSpace(input.Description)
	}
	adminUser, _ := auth.UserFromContext(r.Context())
	result, err := wallet.NewService(s.db).Adjust(userID, adminUser.ID, input.Amount, description)
	if err != nil {
		if errors.Is(err, wallet.ErrInsufficient) {
			response.JSON(w, http.StatusConflict, 3004, "金币余额不足或调整数量不合法", nil)
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			response.JSON(w, http.StatusNotFound, 3001, "钱包不存在", nil)
			return
		}
		internalError(w)
		return
	}
	s.audit(r, "wallet.adjustment", "wallet", userID, map[string]any{"amount": input.Amount, "description": description, "balance_after": result.Coins})
	response.JSON(w, http.StatusOK, 0, "金币调整成功", result)
}

func (s *Service) GenerateRedeemCodes(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Amount   int64 `json:"amount"`
		Quantity int64 `json:"quantity"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil || input.Amount <= 0 || input.Quantity < 1 || input.Quantity > 1000 {
		response.JSON(w, http.StatusBadRequest, 5001, "卡密面额必须为正整数，数量范围为 1 到 1000", nil)
		return
	}
	adminUser, _ := auth.UserFromContext(r.Context())
	codes, err := wallet.NewService(s.db).GenerateCodes(adminUser.ID, input.Amount, input.Quantity)
	if err != nil {
		internalError(w)
		return
	}
	s.audit(r, "redeem_code.generate", "redeem_code", "batch", map[string]any{"amount": input.Amount, "quantity": input.Quantity})
	response.JSON(w, http.StatusCreated, 0, "卡密生成成功", map[string]any{"amount": input.Amount, "quantity": input.Quantity, "codes": codes})
}

func (s *Service) settings(r *http.Request) (Settings, error) {
	var enabled, coins string
	var systemName string
	if err := s.db.QueryRowContext(r.Context(), "SELECT setting_value FROM system_settings WHERE setting_key = 'registration_enabled'").Scan(&enabled); err != nil {
		return Settings{}, err
	}
	if err := s.db.QueryRowContext(r.Context(), "SELECT setting_value FROM system_settings WHERE setting_key = 'register_gift_coins'").Scan(&coins); err != nil {
		return Settings{}, err
	}
	value, err := strconv.ParseInt(coins, 10, 64)
	if err != nil {
		return Settings{}, err
	}
	if err := s.db.QueryRowContext(r.Context(), "SELECT setting_value FROM system_settings WHERE setting_key = 'system_name'").Scan(&systemName); err != nil {
		return Settings{}, err
	}
	var mokantHost, mokantPort, mokantToken string
	_ = s.db.QueryRowContext(r.Context(), "SELECT COALESCE((SELECT setting_value FROM system_settings WHERE setting_key = 'mokant_host'), '127.0.0.1')").Scan(&mokantHost)
	_ = s.db.QueryRowContext(r.Context(), "SELECT COALESCE((SELECT setting_value FROM system_settings WHERE setting_key = 'mokant_port'), '3001')").Scan(&mokantPort)
	_ = s.db.QueryRowContext(r.Context(), "SELECT COALESCE((SELECT setting_value FROM system_settings WHERE setting_key = 'mokant_token'), '')").Scan(&mokantToken)
	port, _ := strconv.Atoi(mokantPort)
	return Settings{RegistrationEnabled: enabled == "true", RegisterGiftCoins: value, SystemName: systemName, MokantHost: mokantHost, MokantPort: port, MokantTokenSet: mokantToken != ""}, nil
}

func (s *Service) PublicSettings(w http.ResponseWriter, r *http.Request) {
	var name string
	if err := s.db.QueryRowContext(r.Context(), "SELECT setting_value FROM system_settings WHERE setting_key = 'system_name'").Scan(&name); err != nil {
		internalError(w)
		return
	}
	response.JSON(w, http.StatusOK, 0, "ok", map[string]string{"system_name": name})
}

func (s *Service) RedeemCodes(w http.ResponseWriter, r *http.Request) {
	page, pageSize := pagination(r)
	status := r.URL.Query().Get("status")
	where := "1 = 1"
	args := []any{}
	if status == "unused" || status == "redeemed" {
		where += " AND c.status = ?"
		args = append(args, status)
	}
	var total int
	if err := s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM redeem_codes c WHERE "+where, args...).Scan(&total); err != nil {
		internalError(w)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT c.id, COALESCE(c.code_ciphertext, ''), c.amount, c.status, c.created_by, creator.username, c.redeemed_by, COALESCE(redeemer.username, ''), c.created_at, COALESCE(c.redeemed_at, '') FROM redeem_codes c JOIN users creator ON creator.id = c.created_by LEFT JOIN users redeemer ON redeemer.id = c.redeemed_by WHERE `+where+` ORDER BY c.id DESC LIMIT ? OFFSET ?`, append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		internalError(w)
		return
	}
	defer rows.Close()
	items := make([]RedeemCodeItem, 0)
	for rows.Next() {
		var item RedeemCodeItem
		var ciphertext string
		if err := rows.Scan(&item.ID, &ciphertext, &item.Amount, &item.Status, &item.CreatedBy, &item.CreatedByName, &item.RedeemedBy, &item.RedeemedByName, &item.CreatedAt, &item.RedeemedAt); err != nil {
			internalError(w)
			return
		}
		if ciphertext != "" {
			item.Code, _ = wallet.DecryptCode(ciphertext)
		}
		items = append(items, item)
	}
	response.JSON(w, http.StatusOK, 0, "ok", map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (s *Service) RedeemCodeStats(w http.ResponseWriter, r *http.Request) {
	var total, unused, redeemed int
	if err := s.db.QueryRowContext(r.Context(), "SELECT COUNT(*), COALESCE(SUM(status = 'unused'), 0), COALESCE(SUM(status = 'redeemed'), 0) FROM redeem_codes").Scan(&total, &unused, &redeemed); err != nil {
		internalError(w)
		return
	}
	response.JSON(w, http.StatusOK, 0, "ok", map[string]int{"total": total, "unused": unused, "redeemed": redeemed})
}

func (s *Service) UserStatistics(w http.ResponseWriter, r *http.Request) {
	days := 30
	if r.URL.Query().Get("range") == "7d" {
		days = 7
	} else if r.URL.Query().Get("range") == "90d" {
		days = 90
	}
	today := time.Now().In(timeutil.Beijing)
	start := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, timeutil.Beijing).AddDate(0, 0, -(days - 1))
	rows, err := s.db.QueryContext(r.Context(), "SELECT created_at FROM users")
	if err != nil {
		internalError(w)
		return
	}
	defer rows.Close()
	counts := make(map[string]int)
	for rows.Next() {
		var created string
		if err := rows.Scan(&created); err != nil {
			internalError(w)
			return
		}
		if parsed, err := parseStoredTime(created); err == nil {
			day := parsed.In(timeutil.Beijing).Format("2006-01-02")
			counts[day]++
		}
	}
	items := make([]map[string]any, 0, days)
	var total int
	if err := s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM users WHERE created_at < ?", start.Format(time.RFC3339)).Scan(&total); err != nil {
		internalError(w)
		return
	}
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		registered := counts[day]
		total += registered
		items = append(items, map[string]any{"date": day, "registered": registered, "total": total})
	}
	response.JSON(w, http.StatusOK, 0, "ok", map[string]any{"range": r.URL.Query().Get("range"), "items": items})
}

func parseStoredTime(value string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	return time.ParseInLocation("2006-01-02 15:04:05", value, timeutil.Beijing)
}

func (s *Service) adminCount(r *http.Request) int {
	var count int
	_ = s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM users WHERE role = 'admin' AND status = 'active'").Scan(&count)
	return count
}
func (s *Service) audit(r *http.Request, action, resourceType string, resourceID any, details any) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		return
	}
	raw, _ := json.Marshal(details)
	_, _ = s.db.ExecContext(r.Context(), "INSERT INTO admin_audit_logs (admin_user_id, action, resource_type, resource_id, details, created_at) VALUES (?, ?, ?, ?, ?, ?)", user.ID, action, resourceType, formatID(resourceID), string(raw), timeutil.Now())
}
func parseID(value string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err == nil && id > 0
}
func formatID(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return strconv.FormatInt(value.(int64), 10)
}
func pagination(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	return page, size
}
func internalError(w http.ResponseWriter) {
	response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
}
