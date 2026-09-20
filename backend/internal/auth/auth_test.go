package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"qq-pet/backend/internal/config"
	"qq-pet/backend/internal/database"

	"golang.org/x/crypto/bcrypt"
)

func testService(t *testing.T) *Service {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewService(db, config.Config{CookieName: "test_session", SessionHours: 1, LoginLimit: 10, LoginWindowSec: 60})
}

func TestSessionUnauthenticatedReturnsNull(t *testing.T) {
	service := testService(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	res := httptest.NewRecorder()
	service.Routes().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("session returned %d: %s", res.Code, res.Body.String())
	}
	var body struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != 0 || string(body.Data) != "null" {
		t.Fatalf("unexpected session response: %s", res.Body.String())
	}
}

func TestRegisterLoginMeLogout(t *testing.T) {
	service := testService(t)
	register := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"tester","password":"password123"}`))
	register.Header.Set("Content-Type", "application/json")
	registerResponse := httptest.NewRecorder()
	service.Routes().ServeHTTP(registerResponse, register)
	if registerResponse.Code != http.StatusCreated {
		t.Fatalf("register returned %d: %s", registerResponse.Code, registerResponse.Body.String())
	}

	login := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"tester","password":"password123"}`))
	login.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	service.Routes().ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login returned %d: %s", loginResponse.Code, loginResponse.Body.String())
	}
	cookie := loginResponse.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.Name != "test_session" || cookie.Value == "" {
		t.Fatalf("invalid session cookie: %+v", cookie)
	}

	me := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	me.AddCookie(cookie)
	meResponse := httptest.NewRecorder()
	service.Routes().ServeHTTP(meResponse, me)
	if meResponse.Code != http.StatusOK {
		t.Fatalf("me returned %d: %s", meResponse.Code, meResponse.Body.String())
	}
	var body struct {
		Data User `json:"data"`
	}
	if err := json.Unmarshal(meResponse.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Username != "tester" {
		t.Fatalf("unexpected user: %+v", body.Data)
	}

	session := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	session.AddCookie(cookie)
	sessionResponse := httptest.NewRecorder()
	service.Routes().ServeHTTP(sessionResponse, session)
	if sessionResponse.Code != http.StatusOK {
		t.Fatalf("session returned %d: %s", sessionResponse.Code, sessionResponse.Body.String())
	}
	var sessionBody struct {
		Data User `json:"data"`
	}
	if err := json.Unmarshal(sessionResponse.Body.Bytes(), &sessionBody); err != nil {
		t.Fatal(err)
	}
	if sessionBody.Data.Username != "tester" {
		t.Fatalf("unexpected session user: %+v", sessionBody.Data)
	}

	logout := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logout.AddCookie(cookie)
	logoutResponse := httptest.NewRecorder()
	service.Routes().ServeHTTP(logoutResponse, logout)
	if logoutResponse.Code != http.StatusOK {
		t.Fatalf("logout returned %d", logoutResponse.Code)
	}

	meAfterLogout := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meAfterLogout.AddCookie(cookie)
	meAfterLogoutResponse := httptest.NewRecorder()
	service.Routes().ServeHTTP(meAfterLogoutResponse, meAfterLogout)
	if meAfterLogoutResponse.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout returned %d", meAfterLogoutResponse.Code)
	}
}

func TestRegisterDuplicateAndInvalidPassword(t *testing.T) {
	service := testService(t)
	for _, payload := range []string{`{"username":"ab","password":"password123"}`, `{"username":"tester","password":"short"}`} {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(payload))
		res := httptest.NewRecorder()
		service.Routes().ServeHTTP(res, req)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("expected bad request, got %d", res.Code)
		}
	}
	first := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"tester","password":"password123"}`))
	firstResponse := httptest.NewRecorder()
	service.Routes().ServeHTTP(firstResponse, first)
	duplicate := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"tester","password":"password123"}`))
	duplicateResponse := httptest.NewRecorder()
	service.Routes().ServeHTTP(duplicateResponse, duplicate)
	if duplicateResponse.Code != http.StatusConflict {
		t.Fatalf("expected conflict, got %d", duplicateResponse.Code)
	}
}

func TestEnsureAdminCreatesAndSkipsExistingAdmin(t *testing.T) {
	service := testService(t)
	username := "admin"
	password := "adminpass123"

	if err := EnsureAdmin(service.db, username, password); err != nil {
		t.Fatal(err)
	}
	var count int
	var hash, role string
	if err := service.db.QueryRow("SELECT COUNT(*), password_hash, role FROM users WHERE username = ?", username).Scan(&count, &hash, &role); err != nil {
		t.Fatal(err)
	}
	if count != 1 || role != "admin" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		t.Fatalf("unexpected admin record: count=%d role=%s", count, role)
	}

	originalHash := hash
	if err := EnsureAdmin(service.db, username, password); err != nil {
		t.Fatal(err)
	}
	if err := service.db.QueryRow("SELECT password_hash FROM users WHERE username = ?", username).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash != originalHash {
		t.Fatal("existing admin password was changed")
	}
}

func TestEnsureAdminRejectsConflictAndInvalidConfig(t *testing.T) {
	service := testService(t)
	if _, err := service.db.Exec("INSERT INTO users (username, password_hash) VALUES (?, ?)", "admin", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAdmin(service.db, "admin", "adminpass123"); err == nil {
		t.Fatal("expected conflict error")
	}

	if err := EnsureAdmin(service.db, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAdmin(service.db, "admin", ""); err == nil {
		t.Fatal("expected partial config error")
	}
	if err := EnsureAdmin(service.db, "ad", "short"); err == nil {
		t.Fatal("expected invalid config error")
	}
}
