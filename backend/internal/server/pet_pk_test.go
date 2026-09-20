package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"qq-pet/backend/internal/config"
	"qq-pet/backend/internal/database"
	"qq-pet/backend/internal/mokant"
)

func TestPetPKRoutes(t *testing.T) {
	actions := make(chan map[string]any, 3)
	upgrader := websocket.Upgrader{}
	framework := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		var authRequest map[string]any
		if err := conn.ReadJSON(&authRequest); err != nil {
			t.Error(err)
			return
		}
		if err := conn.WriteJSON(map[string]any{"type": "auth_ok"}); err != nil {
			t.Error(err)
			return
		}
		for index := 0; index < 3; index++ {
			var request map[string]any
			if err := conn.ReadJSON(&request); err != nil {
				t.Error(err)
				return
			}
			actions <- request
			if err := conn.WriteJSON(map[string]any{
				"type": "action_response", "id": request["id"], "status": "ok", "retcode": 0,
				"data": map[string]any{"action": request["action"], "nested": map[string]any{"value": index + 1}},
			}); err != nil {
				t.Error(err)
				return
			}
		}
	}))
	defer framework.Close()

	frameworkURL, err := url.Parse(framework.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, portText, found := strings.Cut(frameworkURL.Host, ":")
	if !found {
		t.Fatalf("invalid framework address %q", frameworkURL.Host)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}

	db, err := database.Open(filepath.Join(t.TempDir(), "pet-pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{CookieName: "pet_pk_session", SessionHours: 1, LoginLimit: 20, LoginWindowSec: 60, Version: "test"}
	client := mokant.NewClient(cfg, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	client.Configure(host, port, "test-token")
	client.Start(t.Context())
	defer client.Stop()
	deadline := time.Now().Add(2 * time.Second)
	for !client.Status().Connected && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !client.Status().Connected {
		t.Fatalf("mokant client did not connect: %#v", client.Status())
	}

	router := NewRouter(db, cfg, slog.New(slog.NewTextHandler(os.Stdout, nil)), client)
	request := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
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

	if res := request(http.MethodPost, "/api/pets/123456/pk-start", `{}`, nil); res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated route returned %d: %s", res.Code, res.Body.String())
	}
	if res := request(http.MethodPost, "/api/auth/register", `{"username":"pkuser","password":"password123"}`, nil); res.Code != http.StatusCreated {
		t.Fatalf("register returned %d: %s", res.Code, res.Body.String())
	}
	login := request(http.MethodPost, "/api/auth/login", `{"username":"pkuser","password":"password123"}`, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login returned %d: %s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	var userID int64
	if err := db.QueryRow("SELECT id FROM users WHERE username = 'pkuser'").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO qq_bindings (qq_number, user_id) VALUES ('123456', ?)", userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO pet_profiles (qq_binding_id, pet_id, pet_name) SELECT id, 'self-pet', '测试宠物' FROM qq_bindings WHERE qq_number = '123456'"); err != nil {
		t.Fatal(err)
	}

	invalid := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/pets/123456/pk-start", `{"friend_id":"friend"}`},
		{http.MethodGet, "/api/pets/123456/pk-status", ""},
		{http.MethodPost, "/api/pets/123456/pk-settle", `{}`},
	}
	for _, test := range invalid {
		if res := request(test.method, test.path, test.body, cookie); res.Code != http.StatusBadRequest {
			t.Fatalf("invalid %s returned %d: %s", test.path, res.Code, res.Body.String())
		}
	}
	if res := request(http.MethodGet, "/api/pets/654321/pk-status?story_id=story", "", cookie); res.Code != http.StatusNotFound {
		t.Fatalf("unbound QQ returned %d: %s", res.Code, res.Body.String())
	}

	tests := []struct {
		method string
		path   string
		body   string
		action string
		params map[string]string
	}{
		{http.MethodPost, "/api/pets/123456/pk-start", `{"friend_id":" 234567 ","friend_pet_id":" pet-2 "}`, "start_pet_pk", map[string]string{"pet_id": "self-pet", "friend_uin": "234567", "friend_pet_id": "pet-2"}},
		{http.MethodGet, "/api/pets/123456/pk-status?story_id=%20story-3%20", "", "get_pet_pk_status", map[string]string{"pet_id": "self-pet", "story_id": "story-3"}},
		{http.MethodPost, "/api/pets/123456/pk-settle", `{"story_id":" story-3 "}`, "settle_pet_pk", map[string]string{"pet_id": "self-pet", "story_id": "story-3"}},
	}
	for index, test := range tests {
		res := request(test.method, test.path, test.body, cookie)
		if res.Code != http.StatusOK {
			t.Fatalf("%s returned %d: %s", test.action, res.Code, res.Body.String())
		}
		var envelope struct {
			Data map[string]any `json:"data"`
		}
		if err := json.NewDecoder(res.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Data["action"] != test.action || envelope.Data["nested"].(map[string]any)["value"] != float64(index+1) {
			t.Fatalf("unexpected response payload for %s: %#v", test.action, envelope.Data)
		}
		frame := <-actions
		if frame["action"] != test.action {
			t.Fatalf("expected action %s, got %#v", test.action, frame["action"])
		}
		params := frame["params"].(map[string]any)
		if params["self_id"] != float64(123456) {
			t.Fatalf("unexpected self_id for %s: %#v", test.action, params)
		}
		for key, value := range test.params {
			actual := fmt.Sprint(params[key])
			if number, ok := params[key].(float64); ok {
				actual = strconv.FormatInt(int64(number), 10)
			}
			if actual != value {
				t.Fatalf("unexpected %s for %s: %#v", key, test.action, params)
			}
		}
	}
}
