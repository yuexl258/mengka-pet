package announcement

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"qq-pet/backend/internal/auth"
	"qq-pet/backend/internal/response"
	"qq-pet/backend/internal/timeutil"
)

type Service struct{ db *sql.DB }

type Item struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Status    string `json:"status"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func NewService(db *sql.DB) *Service { return &Service{db: db} }

func (s *Service) Public(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, title, content, created_at, updated_at FROM announcements WHERE status = 'published' ORDER BY id DESC LIMIT 20`)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
		return
	}
	defer rows.Close()
	items := make([]Item, 0)
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.Title, &item.Content, &item.CreatedAt, &item.UpdatedAt); err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
			return
		}
		item.Status = "published"
		items = append(items, item)
	}
	response.JSON(w, http.StatusOK, 0, "ok", items)
}

func (s *Service) AdminList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT a.id, a.title, a.content, a.status, u.username, a.created_at, a.updated_at FROM announcements a JOIN users u ON u.id = a.created_by WHERE a.status IN ('published', 'archived') ORDER BY a.id DESC`)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
		return
	}
	defer rows.Close()
	items := make([]Item, 0)
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.Title, &item.Content, &item.Status, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
			return
		}
		items = append(items, item)
	}
	response.JSON(w, http.StatusOK, 0, "ok", items)
}

func (s *Service) Create(w http.ResponseWriter, r *http.Request) { s.save(w, r, 0) }

func (s *Service) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		response.JSON(w, http.StatusBadRequest, 4003, "公告编号不合法", nil)
		return
	}
	s.save(w, r, id)
}

func (s *Service) save(w http.ResponseWriter, r *http.Request, id int64) {
	var input struct {
		Title   string `json:"title"`
		Content string `json:"content"`
		Status  string `json:"status"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Content) == "" || (input.Status != "published" && input.Status != "archived") {
		response.JSON(w, http.StatusBadRequest, 7001, "公告标题、内容或状态不合法", nil)
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	now := timeutil.Now()
	var err error
	if id > 0 {
		var current string
		if err := s.db.QueryRowContext(r.Context(), "SELECT status FROM announcements WHERE id = ?", id).Scan(&current); err != nil || current == "archived" {
			response.JSON(w, http.StatusConflict, 7002, "已归档公告禁止修改", nil)
			return
		}
	}
	if id == 0 {
		_, err = s.db.ExecContext(r.Context(), "INSERT INTO announcements (title, content, status, created_by, updated_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)", strings.TrimSpace(input.Title), input.Content, input.Status, user.ID, user.ID, now, now)
	} else {
		_, err = s.db.ExecContext(r.Context(), "UPDATE announcements SET title = ?, content = ?, status = ?, updated_by = ?, updated_at = ? WHERE id = ?", strings.TrimSpace(input.Title), input.Content, input.Status, user.ID, now, id)
	}
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
		return
	}
	response.JSON(w, http.StatusOK, 0, "公告已保存", nil)
}
