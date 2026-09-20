package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"log/slog"
	"os"

	"qq-pet/backend/internal/auth"
	"qq-pet/backend/internal/config"
	"qq-pet/backend/internal/database"
)

func TestAdminEndpointsAndPermissions(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{CookieName: "admin_test_session", SessionHours: 1, LoginLimit: 20, LoginWindowSec: 60}
	if err := auth.EnsureAdmin(db, "admin", "adminpass123"); err != nil {
		t.Fatal(err)
	}
	adminPassword, _ := json.Marshal(map[string]string{"username": "admin", "password": "adminpass123"})
	if _, err := db.Exec("INSERT INTO users (username, password_hash) VALUES (?, ?)", "normal", "not-a-login-hash"); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(db, cfg, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	request := func(method, path string, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	if response := request(http.MethodGet, "/api/admin/users", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized returned %d", response.Code)
	}

	login := request(http.MethodPost, "/api/auth/login", string(adminPassword), nil)
	if login.Code != http.StatusOK {
		t.Fatalf("admin login returned %d: %s", login.Code, login.Body.String())
	}
	adminCookie := login.Result().Cookies()[0]
	if response := request(http.MethodGet, "/api/admin/users", "", adminCookie); response.Code != http.StatusOK {
		t.Fatalf("admin users returned %d: %s", response.Code, response.Body.String())
	}
	var usersResponse struct {
		Data struct {
			Items []struct {
				Username string `json:"username"`
			}
			Total int `json:"total"`
		} `json:"data"`
	}
	if err := json.NewDecoder(request(http.MethodGet, "/api/admin/users?page=1&page_size=20", "", adminCookie).Body).Decode(&usersResponse); err != nil {
		t.Fatal(err)
	}
	if usersResponse.Data.Total != 2 || len(usersResponse.Data.Items) != 2 {
		t.Fatalf("unexpected user page: total=%d items=%d", usersResponse.Data.Total, len(usersResponse.Data.Items))
	}
	if response := request(http.MethodPatch, "/api/admin/settings/registration", `{"registration_enabled":false,"register_gift_coins":25}`, adminCookie); response.Code != http.StatusOK {
		t.Fatalf("settings returned %d: %s", response.Code, response.Body.String())
	}

	register := request(http.MethodPost, "/api/auth/register", `{"username":"blockeduser","password":"password123"}`, nil)
	if register.Code != http.StatusForbidden {
		t.Fatalf("closed registration returned %d: %s", register.Code, register.Body.String())
	}
	logs := request(http.MethodGet, "/api/admin/audit-logs", "", adminCookie)
	if logs.Code != http.StatusOK || !strings.Contains(logs.Body.String(), "settings.registration.update") {
		t.Fatalf("audit response invalid: %d %s", logs.Code, logs.Body.String())
	}
}
