package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"log/slog"
	"os"
	"qq-pet/backend/internal/config"
	"qq-pet/backend/internal/database"
)

func openPetPKTestDB(t *testing.T) (*sql.DB, int64, int64, int64) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	result, err := db.Exec("INSERT INTO users (username, password_hash) VALUES ('owner', 'hash')")
	if err != nil {
		t.Fatal(err)
	}
	ownerID, _ := result.LastInsertId()
	result, err = db.Exec("INSERT INTO qq_bindings (qq_number, user_id) VALUES ('123456', ?)", ownerID)
	if err != nil {
		t.Fatal(err)
	}
	firstBinding, _ := result.LastInsertId()
	result, err = db.Exec("INSERT INTO qq_bindings (qq_number, user_id) VALUES ('654321', ?)", ownerID)
	if err != nil {
		t.Fatal(err)
	}
	secondBinding, _ := result.LastInsertId()
	return db, ownerID, firstBinding, secondBinding
}

func TestSyncPetPKStrangersIsGlobalByUserIDAndPreservesPower(t *testing.T) {
	db, ownerID, firstBinding, secondBinding := openPetPKTestDB(t)
	defer db.Close()
	ctx := context.Background()
	first := []any{
		map[string]any{"user_id": "shared", "pet_id": "pet-1", "pet_name": "旧名称", "power": float64(88), "dominant_type": float64(2)},
		map[string]any{"user_id": "stale", "pet_id": "pet-stale"},
	}
	if _, err := syncPetPKStrangers(ctx, db, ownerID, firstBinding, first); err != nil {
		t.Fatal(err)
	}
	items, err := syncPetPKStrangers(ctx, db, ownerID, secondBinding, []any{map[string]any{"user_id": "shared", "pet_id": "pet-1", "pet_name": "新名称"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("全局陌生人缓存应返回全部候选: %#v", items)
	}
	all, err := loadPetPKStrangers(ctx, db, ownerID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("同一 user_id 应全局唯一: %#v", all)
	}
	for _, item := range all {
		if item["user_id"] == "shared" && (item["power"] != int64(88) || item["dominant_type"] != int64(2)) {
			t.Fatalf("同步更新不应覆盖已有战力和类型: %#v", item)
		}
	}
}

func TestFriendProfileHasPetUsesBusinessCodes(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		hasPet     bool
		recognized bool
	}{
		{"has pet", `{"ok":false,"error":"宠物服务拒绝请求: code=1000120"}`, true, true},
		{"no pet", `{"ok":false,"error":"宠物服务拒绝请求: code=1000100"}`, false, true},
		{"nested has pet response", `{"code":0,"data":{"result":{"error":"other_pet_query_failed: OIDB errorCode=1000120 message=请升级客户端版本后体验宠物; 可直接提供 pet_id 跳过此查询","ok":false}}}`, true, true},
		{"nested no pet response", `{"code":0,"data":{"result":{"error":"other_pet_query_failed: OIDB errorCode=1000100 message=用户没有宠物; 可直接提供 pet_id 跳过此查询","ok":false}}}`, false, true},
		{"unknown code", `{"ok":false,"error":"code=9999999"}`, false, false},
		{"action success only", `{"ok":true,"data":{"success":true}}`, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasPet, recognized := friendProfileHasPet([]byte(tt.raw))
			if hasPet != tt.hasPet || recognized != tt.recognized {
				t.Fatalf("friendProfileHasPet() = (%v, %v), want (%v, %v)", hasPet, recognized, tt.hasPet, tt.recognized)
			}
		})
	}
}

func TestFriendListTotalCount(t *testing.T) {
	if got := friendListTotalCount([]byte(`{"total_count":1865,"data":[]}`)); got != 1865 {
		t.Fatalf("friendListTotalCount() = %d, want 1865", got)
	}
	if got := friendListTotalCount([]byte(`{"data":{"total_count":1865,"friends":[]}}`)); got != 1865 {
		t.Fatalf("nested friendListTotalCount() = %d, want 1865", got)
	}
}

func TestPokeResultHasNoPet(t *testing.T) {
	var value any
	if err := json.Unmarshal([]byte(`{"code":0,"data":{"result":{"error":"OIDB errorCode=136201 message=没有宠物，不能点赞哦","ok":false}}}`), &value); err != nil {
		t.Fatal(err)
	}
	noPet, message := pokeResultHasNoPet(value)
	if !noPet || !strings.Contains(message, "没有宠物") {
		t.Fatalf("pokeResultHasNoPet() = %v, %q", noPet, message)
	}
}

func TestFriendPetProfileValue(t *testing.T) {
	var success any
	if err := json.Unmarshal([]byte(`{"code":0,"data":{"result":{"ok":true,"data":{"friend_uin":"654321","pet_id":"pet-1","vitals":{"mood":80}}}}}`), &success); err != nil {
		t.Fatal(err)
	}
	data, ok, message := friendPetProfileValue(success)
	if !ok || message != "" || data["friend_uin"] != "654321" || data["pet_id"] != "pet-1" {
		t.Fatalf("unexpected success result: data=%#v ok=%v message=%q", data, ok, message)
	}

	var mismatch any
	if err := json.Unmarshal([]byte(`{"data":{"result":{"ok":false,"error":"pet_owner_mismatch: pet_id 与目标 QQ 不匹配"}}}`), &mismatch); err != nil {
		t.Fatal(err)
	}
	data, ok, message = friendPetProfileValue(mismatch)
	if data != nil || ok || !strings.Contains(message, "pet_owner_mismatch") {
		t.Fatalf("unexpected mismatch result: data=%#v ok=%v message=%q", data, ok, message)
	}
}

func TestUpdatePetPKStrangerPowerUsesGlobalPetID(t *testing.T) {
	db, ownerID, firstBinding, _ := openPetPKTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := syncPetPKStrangers(ctx, db, ownerID, firstBinding, []any{map[string]any{"user_id": "shared", "pet_id": "pet-1"}}); err != nil {
		t.Fatal(err)
	}
	dominantType := int64(10)
	affected, err := updatePetPKStrangerPower(ctx, db, ownerID, "pet-1", 999, &dominantType)
	if err != nil {
		t.Fatal(err)
	}
	if affected != 1 {
		t.Fatalf("全局陌生人记录应只更新一次，实际 %d", affected)
	}
	items, err := loadPetPKStrangers(ctx, db, ownerID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0]["power"] != int64(999) || items[0]["dominant_type"] != int64(10) {
		t.Fatalf("未读取到更新后的战力和类型: %#v", items)
	}
}

func TestSystemEndpoints(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}

	router := NewRouter(db, config.Config{Version: "test", Environment: "test"}, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	for _, path := range []string{"/healthz", "/api/version"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("%s returned %d", path, res.Code)
		}
	}
}

func TestMokantCacheValidSupportsCurrentResponse(t *testing.T) {
	if !mokantCacheValid(json.RawMessage(`{"hasCache":true}`)) {
		t.Fatal("hasCache=true should enable cache login")
	}
	if mokantCacheValid(json.RawMessage(`{"hasCache":false}`)) {
		t.Fatal("hasCache=false should not enable cache login")
	}
}

func TestDecodeMokantLoginResultReadsNestedVerificationURLs(t *testing.T) {
	result := decodeMokantLoginResult(json.RawMessage(`{
		"code":140022008,
		"message":"需要滑块验证",
		"data":{"sliderUrl":"https://captcha.qq.com/slider","identity_url":"https://captcha.qq.com/identity","securityUrl":"https://accounts.qq.com/login/attack"}
	}`))
	if result.Code != 140022008 || result.SliderURL != "https://captcha.qq.com/slider" || result.IdentityURL != "https://captcha.qq.com/identity" || result.SecurityURL != "https://accounts.qq.com/login/attack" {
		t.Fatalf("unexpected login result: %#v", result)
	}
}

func TestAllowRelativeCaptchaProxy(t *testing.T) {
	input := []byte(`var matches=[/localhost/,/t\.captcha\.qq\.com/]`)
	result := string(allowRelativeCaptchaProxy(input))
	if !strings.Contains(result, `/^\/api\/qq\/captcha-proxy\//,/localhost/`) {
		t.Fatalf("relative captcha proxy was not allowed: %s", result)
	}
}

func TestCompleteQQLoginEnablesAutoControlAndPreservesConfig(t *testing.T) {
	db, ownerID, bindingID, _ := openPetPKTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO pet_auto_control_configs (qq_binding_id, enabled, learning_target, plan_json, created_at, updated_at) VALUES (?, 0, '魅力', '[{"id":"plan-1","activity":"school","control_mode":"count","count":2}]', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, bindingID); err != nil {
		t.Fatal(err)
	}

	completeQQLogin(ctx, db, nil, ownerID, "123456")

	var enabled int
	var target, plan string
	if err := db.QueryRowContext(ctx, "SELECT enabled, learning_target, plan_json FROM pet_auto_control_configs WHERE qq_binding_id = ?", bindingID).Scan(&enabled, &target, &plan); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 || target != "魅力" || !strings.Contains(plan, "plan-1") {
		t.Fatalf("automatic control config was not enabled safely: enabled=%d target=%q plan=%q", enabled, target, plan)
	}
}

func TestCompleteQQLoginCreatesDefaultAutoControlConfig(t *testing.T) {
	db, ownerID, _, secondBinding := openPetPKTestDB(t)
	defer db.Close()

	completeQQLogin(context.Background(), db, nil, ownerID, "654321")

	var enabled int
	if err := db.QueryRow("SELECT enabled FROM pet_auto_control_configs WHERE qq_binding_id = ?", secondBinding).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("default automatic control config enabled=%d, want 1", enabled)
	}
}
