package mokant

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qq-pet/backend/internal/config"

	"github.com/gorilla/websocket"
	_ "modernc.org/sqlite"
)

func connectedTestClient(t *testing.T, replies func(map[string]any) string) *Client {
	t.Helper()
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		var auth map[string]any
		if err := conn.ReadJSON(&auth); err != nil {
			t.Error(err)
			return
		}
		if err := conn.WriteJSON(map[string]string{"type": "auth_ok"}); err != nil {
			t.Error(err)
			return
		}
		for {
			var request map[string]any
			if err := conn.ReadJSON(&request); err != nil {
				return
			}
			if request["type"] == "ping" {
				continue
			}
			if err := conn.WriteMessage(websocket.TextMessage, []byte(replies(request))); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	address := strings.TrimPrefix(server.URL, "http://")
	host, portText, _ := strings.Cut(address, ":")
	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatal(err)
	}
	client := NewClient(config.Config{Version: "test"}, slog.Default())
	client.Configure(host, port, "token")
	client.Start(context.Background())
	t.Cleanup(client.Stop)
	deadline := time.Now().Add(2 * time.Second)
	for !client.Status().Connected && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !client.Status().Connected {
		t.Fatal("test client did not connect")
	}
	return client
}

func TestRawActionPreservesBusinessFailureFrame(t *testing.T) {
	const failure = `{"type":"action_result","id":"1","ok":false,"error":"bad request","error_code":4003,"data":{"partial":true}}`
	client := connectedTestClient(t, func(request map[string]any) string {
		return failure
	})

	raw, err := client.RawAction(context.Background(), "failing_action", map[string]any{})
	if err != nil {
		t.Fatalf("RawAction returned business failure as error: %v", err)
	}
	if string(raw) != failure {
		t.Fatalf("RawAction changed failure frame:\nwant %s\n got %s", failure, raw)
	}
}

func TestActionBehaviorUnchanged(t *testing.T) {
	client := connectedTestClient(t, func(request map[string]any) string {
		if request["action"] == "failure" {
			return `{"type":"action_result","id":"` + request["id"].(string) + `","status":"failed","retcode":1,"message":"nope","data":{"ignored":true}}`
		}
		return `{"type":"action_result","id":"` + request["id"].(string) + `","status":"ok","retcode":0,"data":{"value":42}}`
	})

	data, err := client.Action(context.Background(), "success", map[string]any{})
	if err != nil || string(data) != `{"value":42}` {
		t.Fatalf("Action success = %s, %v", data, err)
	}
	if _, err := client.Action(context.Background(), "failure", map[string]any{}); err == nil {
		t.Fatal("Action should still reject business failures")
	}
}

func TestActionRejectsOKFalseFailureFrame(t *testing.T) {
	client := connectedTestClient(t, func(request map[string]any) string {
		return `{"type":"action_result","id":"` + request["id"].(string) + `","ok":false,"error":"bad request","error_code":4003}`
	})

	if _, err := client.Action(context.Background(), "failure", map[string]any{}); err == nil {
		t.Fatal("Action should reject ok=false business failures")
	}
}

func TestOfflineAccountStopsAndroidLogin(t *testing.T) {
	requests := make(chan map[string]any, 1)
	client := connectedTestClient(t, func(request map[string]any) string {
		requests <- request
		return `{"type":"action_result","id":"` + request["id"].(string) + `","status":"ok","retcode":0,"data":{"stopped":true}}`
	})

	data, err := client.OfflineAccount(context.Background(), 3456647043)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"stopped":true}` {
		t.Fatalf("OfflineAccount data = %s", data)
	}
	request := <-requests
	if request["action"] != "stop_account_login" {
		t.Fatalf("action = %v", request["action"])
	}
	params, ok := request["params"].(map[string]any)
	if !ok {
		t.Fatalf("params = %#v", request["params"])
	}
	if params["self_id"] != float64(3456647043) || params["client_type"] != "android" || params["protocol"] != "android" {
		t.Fatalf("params = %#v", params)
	}
}

func TestCaptchaProxyActionsUseAndroidClientAndRelativeBase(t *testing.T) {
	requests := make(chan map[string]any, 2)
	client := connectedTestClient(t, func(request map[string]any) string {
		requests <- request
		return `{"type":"action_result","id":"` + request["id"].(string) + `","status":"ok","retcode":0,"data":{}}`
	})

	if _, err := client.RegisterCaptchaProxy(context.Background(), 3456647043, "https://ti.qq.com/captcha", "/api/qq/captcha-proxy"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CaptchaProxy(context.Background(), 3456647043, "https://t.captcha.qq.com/cap_union_prehandle", http.MethodGet, nil, ""); err != nil {
		t.Fatal(err)
	}

	registerRequest := <-requests
	registerParams := registerRequest["params"].(map[string]any)
	if registerRequest["action"] != "register_captcha_proxy" || registerParams["client_type"] != "android" || registerParams["proxy_base"] != "/api/qq/captcha-proxy" {
		t.Fatalf("register request = %#v", registerRequest)
	}
	proxyRequest := <-requests
	proxyParams := proxyRequest["params"].(map[string]any)
	if proxyRequest["action"] != "captcha_proxy" || proxyParams["client_type"] != "android" || proxyParams["method"] != http.MethodGet {
		t.Fatalf("proxy request = %#v", proxyRequest)
	}
}

func TestRawActionRoutesSelfIDThroughDefaultConnection(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("CREATE TABLE mokant_nodes (id INTEGER, is_default INTEGER); INSERT INTO mokant_nodes VALUES (7, 1)"); err != nil {
		t.Fatal(err)
	}
	target := connectedTestClient(t, func(request map[string]any) string {
		return `{"type":"action_result","id":"` + request["id"].(string) + `","ok":true,"node":7}`
	})
	manager := &Manager{db: db, clients: map[int64]*Client{7: target}}
	client := NewManagerClient(config.Config{}, slog.Default(), manager)

	raw, err := client.RawAction(context.Background(), "routed_action", map[string]any{"self_id": int64(123456)})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Node int `json:"node"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || result.Node != 7 {
		t.Fatalf("RawAction did not use default connection: %s, %v", raw, err)
	}
}

func TestListLoginNodesUsesGetNodeListWithoutParams(t *testing.T) {
	requests := make(chan map[string]any, 1)
	client := connectedTestClient(t, func(request map[string]any) string {
		requests <- request
		return `{"type":"action_result","id":"` + request["id"].(string) + `","ok":true,"data":[]}`
	})

	if _, err := client.ListLoginNodes(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := <-requests
	if request["action"] != "get_node_list" {
		t.Fatalf("action = %v", request["action"])
	}
	params, ok := request["params"].(map[string]any)
	if !ok || len(params) != 0 {
		t.Fatalf("params = %#v", request["params"])
	}
}

func TestAddAccountIncludesLoginNodeAndAndroidClient(t *testing.T) {
	requests := make(chan map[string]any, 1)
	client := connectedTestClient(t, func(request map[string]any) string {
		requests <- request
		return `{"type":"action_result","id":"` + request["id"].(string) + `","ok":true,"data":{"code":0}}`
	})

	if _, err := client.AddAccount(context.Background(), 3456647043, "secret", 1, 2, 9); err != nil {
		t.Fatal(err)
	}
	request := <-requests
	params := request["params"].(map[string]any)
	if request["action"] != "add_account" || params["node_id"] != float64(9) || params["client_type"] != "android" {
		t.Fatalf("request = %#v", request)
	}
}
