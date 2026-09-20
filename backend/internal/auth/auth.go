package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"qq-pet/backend/internal/config"
	"qq-pet/backend/internal/response"
	"qq-pet/backend/internal/timeutil"
	"qq-pet/backend/internal/wallet"
)

type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type Service struct {
	db     *sql.DB
	cfg    config.Config
	limits *loginLimiter
}

type contextKey string

const userKey contextKey = "auth_user"

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_\x{4e00}-\x{9fff}]+$`)

var (
	errInvalidInput = errors.New("请求参数不合法")
	errUnauthorized = errors.New("用户名或密码错误")
	errDuplicate    = errors.New("用户名已存在")
	errDisabled     = errors.New("用户已被禁用")
)

func NewService(db *sql.DB, cfg config.Config) *Service {
	return &Service{db: db, cfg: cfg, limits: &loginLimiter{entries: make(map[string][]time.Time)}}
}

func EnsureAdmin(db *sql.DB, username, password string) error {
	if username == "" && password == "" {
		return nil
	}
	if !validUsername(username) || len(password) < 8 || len(password) > 72 {
		return errors.New("管理员账号配置不合法")
	}

	var role string
	err := db.QueryRow("SELECT role FROM users WHERE username = ?", username).Scan(&role)
	if err == nil {
		if role != "admin" {
			return errors.New("管理员账号已被普通用户占用")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := timeutil.Now()
	_, err = db.Exec("INSERT INTO users (username, password_hash, role, status, created_at, updated_at) VALUES (?, ?, 'admin', 'active', ?, ?) ON CONFLICT(username) DO NOTHING", username, string(hash), now, now)
	return err
}

func (s *Service) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/auth/me", s.me)
	mux.HandleFunc("GET /api/auth/session", s.session)
	mux.HandleFunc("POST /api/auth/password/change", s.changePassword)
	return mux
}

func (s *Service) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.userFromRequest(r)
		if err != nil {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	})
}

func (s *Service) RequireAdmin(next http.Handler) http.Handler {
	return s.RequireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _ := UserFromContext(r.Context())
		if user.Role != "admin" {
			response.JSON(w, http.StatusForbidden, 4001, "没有管理员权限", nil)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func UserFromContext(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(userKey).(User)
	return user, ok
}

func (s *Service) register(w http.ResponseWriter, r *http.Request) {
	var registrationEnabled string
	if err := s.db.QueryRowContext(r.Context(), "SELECT setting_value FROM system_settings WHERE setting_key = 'registration_enabled'").Scan(&registrationEnabled); err == nil && registrationEnabled != "true" {
		response.JSON(w, http.StatusForbidden, 2002, "注册功能已关闭", nil)
		return
	}
	var giftCoins int64
	if err := s.db.QueryRowContext(r.Context(), "SELECT setting_value FROM system_settings WHERE setting_key = 'register_gift_coins'").Scan(&registrationEnabled); err == nil {
		giftCoins, _ = strconv.ParseInt(registrationEnabled, 10, 64)
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || !validUsername(input.Username) || len(input.Password) < 8 || len(input.Password) > 72 {
		response.JSON(w, http.StatusBadRequest, 9000, errInvalidInput.Error(), nil)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
		return
	}
	defer tx.Rollback()
	now := timeutil.Now()
	result, err := tx.ExecContext(r.Context(), "INSERT INTO users (username, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?)", input.Username, string(hash), now, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			response.JSON(w, http.StatusConflict, 2001, errDuplicate.Error(), nil)
			return
		}
		response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
		return
	}
	userID, err := result.LastInsertId()
	if err != nil || wallet.CreateForUserTx(tx, userID, giftCoins) != nil {
		response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
		return
	}
	if err := tx.Commit(); err != nil {
		response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
		return
	}
	response.JSON(w, http.StatusCreated, 0, "注册成功", nil)
}

func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	key := clientKey(r)
	if !s.limits.allow(key, s.cfg.LoginLimit, time.Duration(s.cfg.LoginWindowSec)*time.Second) {
		response.JSON(w, http.StatusTooManyRequests, 1003, "登录尝试过于频繁，请稍后再试", nil)
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Username == "" || input.Password == "" {
		response.JSON(w, http.StatusBadRequest, 9000, errInvalidInput.Error(), nil)
		return
	}
	var id int64
	var username, hash, role, status, createdAt string
	err := s.db.QueryRowContext(r.Context(), "SELECT id, username, password_hash, role, status, created_at FROM users WHERE username = ?", input.Username).Scan(&id, &username, &hash, &role, &status, &createdAt)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(input.Password)) != nil {
		response.JSON(w, http.StatusUnauthorized, 1002, errUnauthorized.Error(), nil)
		return
	}
	if status != "active" {
		response.JSON(w, http.StatusForbidden, 1004, errDisabled.Error(), nil)
		return
	}
	token, err := randomToken()
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
		return
	}
	expires := timeutil.Current().Add(time.Duration(s.cfg.SessionHours) * time.Hour)
	if _, err = s.db.ExecContext(r.Context(), "INSERT INTO sessions (id, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)", token, id, expires.Format(time.RFC3339), timeutil.Now()); err != nil {
		response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
		return
	}
	http.SetCookie(w, s.cookie(token, expires))
	response.JSON(w, http.StatusOK, 0, "登录成功", User{ID: id, Username: username, Role: role, Status: status, CreatedAt: createdAt})
}

func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(s.cfg.CookieName); err == nil {
		_, _ = s.db.ExecContext(r.Context(), "DELETE FROM sessions WHERE id = ?", cookie.Value)
	}
	http.SetCookie(w, s.cookie("", time.Unix(0, 0)))
	response.JSON(w, http.StatusOK, 0, "已退出登录", nil)
}

func (s *Service) me(w http.ResponseWriter, r *http.Request) {
	user, err := s.userFromRequest(r)
	if err != nil {
		response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
		return
	}
	response.JSON(w, http.StatusOK, 0, "ok", user)
}

func (s *Service) session(w http.ResponseWriter, r *http.Request) {
	user, err := s.userFromRequest(r)
	if err != nil {
		response.JSON(w, http.StatusOK, 0, "ok", nil)
		return
	}
	response.JSON(w, http.StatusOK, 0, "ok", user)
}

func (s *Service) changePassword(w http.ResponseWriter, r *http.Request) {
	user, err := s.userFromRequest(r)
	if err != nil {
		response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
		return
	}
	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil || len(input.NewPassword) < 8 || len(input.NewPassword) > 72 || input.NewPassword != input.ConfirmPassword || input.NewPassword == input.CurrentPassword {
		response.JSON(w, http.StatusBadRequest, 1005, "新密码必须为 8 到 72 个字符，且两次输入一致并不同于原密码", nil)
		return
	}
	var hash string
	if err := s.db.QueryRowContext(r.Context(), "SELECT password_hash FROM users WHERE id = ?", user.ID).Scan(&hash); err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(input.CurrentPassword)) != nil {
		response.JSON(w, http.StatusBadRequest, 1006, "原密码不正确", nil)
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
		return
	}
	now := timeutil.Now()
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), "UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?", string(newHash), now, user.ID); err != nil {
		response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
		return
	}
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM sessions WHERE user_id = ?", user.ID); err != nil || tx.Commit() != nil {
		response.JSON(w, http.StatusInternalServerError, 9002, "服务暂时不可用", nil)
		return
	}
	response.JSON(w, http.StatusOK, 0, "密码修改成功，请重新登录", nil)
}

func (s *Service) userFromRequest(r *http.Request) (User, error) {
	cookie, err := r.Cookie(s.cfg.CookieName)
	if err != nil || cookie.Value == "" {
		return User{}, errUnauthorized
	}
	var user User
	var expires string
	err = s.db.QueryRowContext(r.Context(), `SELECT u.id, u.username, u.role, u.status, u.created_at, s.expires_at FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.id = ?`, cookie.Value).Scan(&user.ID, &user.Username, &user.Role, &user.Status, &user.CreatedAt, &expires)
	if err != nil || user.Status != "active" {
		return User{}, errUnauthorized
	}
	parsed, err := time.Parse(time.RFC3339, expires)
	if err != nil {
		parsed, err = time.ParseInLocation("2006-01-02 15:04:05", expires, timeutil.Beijing)
	}
	if err != nil || !parsed.After(timeutil.Current()) {
		_, _ = s.db.Exec("DELETE FROM sessions WHERE id = ?", cookie.Value)
		return User{}, errUnauthorized
	}
	return user, nil
}

func (s *Service) cookie(value string, expires time.Time) *http.Cookie {
	maxAge := int(time.Until(expires).Seconds())
	if value == "" {
		maxAge = -1
	}
	return &http.Cookie{Name: s.cfg.CookieName, Value: value, Path: "/", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: maxAge}
}

func validUsername(value string) bool {
	return len([]rune(value)) >= 3 && len([]rune(value)) <= 24 && usernamePattern.MatchString(value)
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
func clientKey(r *http.Request) string { return r.RemoteAddr }

type loginLimiter struct {
	sync.Mutex
	entries map[string][]time.Time
}

func (l *loginLimiter) allow(key string, limit int, window time.Duration) bool {
	l.Lock()
	defer l.Unlock()
	now := time.Now()
	recent := l.entries[key][:0]
	for _, value := range l.entries[key] {
		if now.Sub(value) < window {
			recent = append(recent, value)
		}
	}
	l.entries[key] = recent
	if len(recent) >= limit {
		return false
	}
	l.entries[key] = append(l.entries[key], now)
	return true
}
