package autopk

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"qq-pet/backend/internal/database"
	"qq-pet/backend/internal/timeutil"
)

type fakeClient struct {
	powers          map[string]json.RawMessage
	statusResponses []json.RawMessage
	foodInventory   json.RawMessage
	bathInventory   json.RawMessage
	feedErr         error
	batheErr        error
	started         []string
	status          []string
	settled         []string
	fed             []string
	bathed          []string
	boughtFood      []string
	boughtBath      []string
	events          []string
}

func (f *fakeClient) GetPetPKPower(_ context.Context, _ int64, petID string) (json.RawMessage, error) {
	return f.powers[petID], nil
}

func (f *fakeClient) StartPetPK(_ context.Context, _ int64, selfPetID, friendUIN, petID string) (json.RawMessage, error) {
	started := fmt.Sprintf("%s:%s:%s", selfPetID, friendUIN, petID)
	f.started = append(f.started, started)
	f.events = append(f.events, "start:"+started)
	return json.RawMessage(fmt.Sprintf(`{"data":{"story_id":"story-%d"}}`, len(f.started))), nil
}

func (f *fakeClient) GetPetPKStatus(_ context.Context, _ int64, _, storyID string) (json.RawMessage, error) {
	f.status = append(f.status, storyID)
	f.events = append(f.events, "status:"+storyID)
	if len(f.statusResponses) > 0 {
		response := f.statusResponses[0]
		f.statusResponses = f.statusResponses[1:]
		return response, nil
	}
	return json.RawMessage(`{"response_empty":true}`), nil
}

func (f *fakeClient) SettlePetPK(_ context.Context, _ int64, _, storyID string) (json.RawMessage, error) {
	f.settled = append(f.settled, storyID)
	f.events = append(f.events, "settle:"+storyID)
	return json.RawMessage(`{"settled":true}`), nil
}

func (f *fakeClient) GetPetProfile(context.Context, int64) (json.RawMessage, error) {
	return json.RawMessage(`{"data":{"pet_id":"self-pet"}}`), nil
}

func (f *fakeClient) GetPetFoodCatalog(context.Context, int64) (json.RawMessage, error) {
	f.events = append(f.events, "food_inventory")
	if f.foodInventory != nil {
		return f.foodInventory, nil
	}
	return json.RawMessage(`[{"food_id":"food-1","name":"饼干","count":1}]`), nil
}

func (f *fakeClient) GetPetBathInventory(context.Context, int64) (json.RawMessage, error) {
	f.events = append(f.events, "bath_inventory")
	if f.bathInventory != nil {
		return f.bathInventory, nil
	}
	return json.RawMessage(`[{"item_id":"item-1","name":"香皂片","count":1}]`), nil
}

func (f *fakeClient) GetPetBathCatalog(context.Context, int64) (json.RawMessage, error) {
	return json.RawMessage(`[{"item_id":"item-1","name":"香皂片"}]`), nil
}

func (f *fakeClient) FeedPet(_ context.Context, _ int64, petID, food string) (json.RawMessage, error) {
	value := petID + ":" + food
	f.fed = append(f.fed, value)
	f.events = append(f.events, "feed:"+value)
	return json.RawMessage(`{"fed":true}`), f.feedErr
}

func (f *fakeClient) BuyPetFood(_ context.Context, _, count int64) (json.RawMessage, error) {
	value := fmt.Sprintf("count:%d", count)
	f.boughtFood = append(f.boughtFood, value)
	f.events = append(f.events, "buy_food:"+value)
	return json.RawMessage(`{"bought":true}`), nil
}

func (f *fakeClient) BathePet(_ context.Context, _ int64, _, item string, _ int64) (json.RawMessage, error) {
	f.bathed = append(f.bathed, item)
	f.events = append(f.events, "bathe:"+item)
	return json.RawMessage(`{"bathed":true}`), f.batheErr
}

func (f *fakeClient) BuyPetBathItem(_ context.Context, _ int64, _, item string, count int64) (json.RawMessage, error) {
	value := fmt.Sprintf("%s:%d", item, count)
	f.boughtBath = append(f.boughtBath, value)
	f.events = append(f.events, "buy_bath:"+value)
	return json.RawMessage(`{"bought":true}`), nil
}

func (f *fakeClient) GetPetVitals(context.Context, int64, string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func openTestDB(t *testing.T) (*sql.DB, int64) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "autopk.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (username, password_hash, role, status) VALUES ('autopk', 'hash', 'user', 'active')`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO qq_bindings (qq_number, user_id) VALUES ('123456', 1)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	var bindingID int64
	if err := db.QueryRow(`SELECT id FROM qq_bindings WHERE qq_number = '123456'`).Scan(&bindingID); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO pet_profiles (qq_binding_id, pet_id, pk_power, dominant_type) VALUES (?, 'self-pet', 100, 1)`, bindingID); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db, bindingID
}

func addCandidate(t *testing.T, db *sql.DB, _ int64, userID, petID string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO pet_pk_strangers (user_id, pet_id) VALUES (?, ?)`, userID, petID); err != nil {
		t.Fatal(err)
	}
}

func newTestScheduler(db *sql.DB, client Client) *Scheduler {
	scheduler := NewScheduler(db, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	scheduler.wait = func(context.Context, time.Duration) error { return nil }
	return scheduler
}

func TestEffectivePowerRespectsDominanceAndStrictComparison(t *testing.T) {
	if got := effectivePower(100, 1, 2); got != 120 {
		t.Fatalf("1 克制 2 应获得 1.2 倍，最终战力实际为 %d", got)
	}
	if got := effectivePower(100, 2, 1); got != 100 {
		t.Fatalf("2 不克制 1 不应加成，最终战力实际为 %d", got)
	}
	if effectivePower(100, 1, 2) <= effectivePower(119, 2, 1) {
		t.Fatal("加成后的己方战力应更高")
	}
	if effectivePower(100, 1, 3) > effectivePower(120, 3, 1) {
		t.Fatal("3 克制 1 时对手应获得加成")
	}
}

func TestSchedulerRotatesCandidatesAndCompletesTarget(t *testing.T) {
	db, bindingID := openTestDB(t)
	defer db.Close()
	addCandidate(t, db, bindingID, "200001", "pet-a")
	addCandidate(t, db, bindingID, "200002", "pet-b")
	client := &fakeClient{powers: map[string]json.RawMessage{
		"pet-a": json.RawMessage(`{"power":90,"dominant_type":2}`),
		"pet-b": json.RawMessage(`{"power":80,"dominant_type":2}`),
	}}
	scheduler := newTestScheduler(db, client)
	if err := SaveConfig(context.Background(), db, bindingID, Config{TargetStarts: 2}); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Enable(context.Background(), bindingID, "123456"); err != nil {
		t.Fatal(err)
	}
	scheduler.StopBinding(bindingID)

	item := &worker{}
	for i := 0; i < 4; i++ {
		if !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) {
			t.Fatalf("第 %d 轮不应停止调度", i+1)
		}
	}
	if !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) {
		t.Fatal("达到目标且最后一场结算完成后应保留每日调度")
	}
	if want := []string{"self-pet:200001:pet-a", "self-pet:200002:pet-b"}; !reflect.DeepEqual(client.started, want) {
		t.Fatalf("候选应轮询且不连续重复，实际 %#v", client.started)
	}
	if want := []string{"story-1", "story-2"}; !reflect.DeepEqual(client.status, want) || !reflect.DeepEqual(client.settled, want) {
		t.Fatalf("每次 start 后都应 status 并 settle，status=%#v settle=%#v", client.status, client.settled)
	}
	wantEvents := []string{
		"start:self-pet:200001:pet-a", "status:story-1", "settle:story-1",
		"start:self-pet:200002:pet-b", "status:story-2", "settle:story-2",
		"food_inventory", "feed:self-pet:food-1", "bath_inventory", "bathe:item-1",
	}
	if !reflect.DeepEqual(client.events, wantEvents) {
		t.Fatalf("PK 必须严格串行，实际调用顺序 %#v", client.events)
	}
	state, err := LoadState(context.Background(), db, bindingID)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Running || state.SuccessfulStarts != 2 || state.Message != "目标 start 次数已完成" {
		t.Fatalf("完成状态不正确: %#v", state)
	}
	config, _ := LoadConfig(context.Background(), db, bindingID)
	if !config.Enabled {
		t.Fatal("达到目标后配置仍应保持启用，以便次日继续执行")
	}
	var power, dominantType int64
	if err := db.QueryRow(`SELECT power, dominant_type FROM pet_pk_strangers WHERE user_id = '200002'`).Scan(&power, &dominantType); err != nil {
		t.Fatal(err)
	}
	if power != 80 || dominantType != 2 {
		t.Fatalf("实时战力未写回数据库: power=%d type=%d", power, dominantType)
	}
	var startDetails string
	if err := db.QueryRow(`SELECT details FROM pet_auto_pk_logs WHERE qq_binding_id = ? AND action = 'start' AND success = 1 AND opponent_power = 80 ORDER BY id DESC LIMIT 1`, bindingID).Scan(&startDetails); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(startDetails, "story_id=story-2") ||
		!strings.Contains(startDetails, "self_final_power=120") ||
		!strings.Contains(startDetails, "opponent_final_power=80") {
		t.Fatalf("start 日志应包含 Story ID 和双方最终战力，实际 %q", startDetails)
	}
}

func TestSchedulerAllowsIncreasingTargetAfterPreviousDailyStarts(t *testing.T) {
	db, bindingID := openTestDB(t)
	defer db.Close()
	addCandidate(t, db, bindingID, "200001", "pet-a")
	today := timeutil.Current().Format("2006-01-02")
	if _, err := db.Exec(`INSERT INTO pet_auto_pk_configs (qq_binding_id, enabled, target_starts, start_time) VALUES (?, 1, 100, '00:00')`, bindingID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO pet_auto_pk_states (qq_binding_id, running, successful_starts, message) VALUES (?, 1, 60, '目标 start 次数已完成')`, bindingID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO pet_auto_pk_daily_opponents (qq_binding_id, opponent_user_id, run_date, start_count) VALUES (?, 'previous', ?, 60)`, bindingID, today); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{powers: map[string]json.RawMessage{"pet-a": json.RawMessage(`{"power":80,"dominant_type":2}`)}}
	scheduler := newTestScheduler(db, client)
	item := &worker{}
	if !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) || item.inflight == nil {
		t.Fatal("目标从 60 增加到 100 后，不应因历史累计次数而停止")
	}
	if len(client.started) != 1 {
		t.Fatalf("应继续发起 PK，实际 started=%#v", client.started)
	}
}

func TestSchedulerWaitsForResponseEmptyBeforeSettling(t *testing.T) {
	db, bindingID := openTestDB(t)
	defer db.Close()
	addCandidate(t, db, bindingID, "200001", "pet-a")
	client := &fakeClient{
		powers: map[string]json.RawMessage{"pet-a": json.RawMessage(`{"power":90,"dominant_type":2}`)},
		statusResponses: []json.RawMessage{
			json.RawMessage(`{"response_empty":false}`),
			json.RawMessage(`{"response_empty":true}`),
		},
	}
	scheduler := newTestScheduler(db, client)
	if err := SaveConfig(context.Background(), db, bindingID, Config{Enabled: true, TargetStarts: 1}); err != nil {
		t.Fatal(err)
	}
	item := &worker{}
	if !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) || item.inflight == nil {
		t.Fatal("首轮应启动一场 PK")
	}
	if !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) || len(client.settled) != 0 || item.inflight == nil {
		t.Fatal("response_empty=false 时应保留 inflight 且不结算")
	}
	if !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) || len(client.settled) != 1 || item.inflight != nil {
		t.Fatal("response_empty=true 时应结算，达到目标后保留调度")
	}
	if len(client.started) != 1 || !reflect.DeepEqual(client.status, []string{"story-1", "story-1"}) {
		t.Fatalf("等待期间不得启动下一场，started=%#v status=%#v", client.started, client.status)
	}
}

func TestSchedulerWaitsForNextTickBeforeStartingAnotherPK(t *testing.T) {
	db, bindingID := openTestDB(t)
	defer db.Close()
	addCandidate(t, db, bindingID, "200001", "pet-a")
	addCandidate(t, db, bindingID, "200002", "pet-b")
	client := &fakeClient{powers: map[string]json.RawMessage{
		"pet-a": json.RawMessage(`{"power":80,"dominant_type":2}`),
		"pet-b": json.RawMessage(`{"power":80,"dominant_type":2}`),
	}}
	scheduler := newTestScheduler(db, client)
	if err := SaveConfig(context.Background(), db, bindingID, Config{Enabled: true, TargetStarts: 2}); err != nil {
		t.Fatal(err)
	}
	item := &worker{}
	if !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) {
		t.Fatal("第一轮应提交第一场 PK")
	}
	if !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) {
		t.Fatal("第二轮应结算第一场 PK")
	}
	if len(client.started) != 1 || len(client.settled) != 1 {
		t.Fatalf("结算轮次不得同时提交下一场，started=%#v settled=%#v", client.started, client.settled)
	}
	if !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) {
		t.Fatal("第三轮应提交第二场 PK")
	}
	if len(client.started) != 2 {
		t.Fatalf("下一轮应提交第二场 PK，started=%#v", client.started)
	}
}

func TestSchedulerRunsInternalPetBeforeEachRegularPKWithoutCountingOrLogging(t *testing.T) {
	db, bindingID := openTestDB(t)
	defer db.Close()
	addCandidate(t, db, bindingID, "200001", "pet-a")
	if _, err := db.Exec(`INSERT INTO pet_auto_pk_internal_pets (user_id, pet_id) VALUES ('900001', 'internal-pet')`); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{powers: map[string]json.RawMessage{
		"pet-a": json.RawMessage(`{"power":80,"dominant_type":2}`),
	}}
	scheduler := newTestScheduler(db, client)
	if err := SaveConfig(context.Background(), db, bindingID, Config{Enabled: true, TargetStarts: 1}); err != nil {
		t.Fatal(err)
	}
	item := &worker{}
	for i := 0; i < 4; i++ {
		if !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) {
			t.Fatalf("第 %d 轮不应停止调度", i+1)
		}
	}
	if want := []string{"self-pet:900001:internal-pet", "self-pet:200001:pet-a"}; !reflect.DeepEqual(client.started, want) {
		t.Fatalf("内部宠物应先于正式 PK，实际 %#v", client.started)
	}
	var starts int64
	if err := db.QueryRow(`SELECT successful_starts FROM pet_auto_pk_states WHERE qq_binding_id = ?`, bindingID).Scan(&starts); err != nil {
		t.Fatal(err)
	}
	if starts != 1 {
		t.Fatalf("内部 PK 不应计入正式次数，实际 %d", starts)
	}
	var logs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pet_auto_pk_logs WHERE qq_binding_id = ?`, bindingID).Scan(&logs); err != nil {
		t.Fatal(err)
	}
	if logs == 0 {
		t.Fatal("正式 PK 应保留日志，内部 PK 不应额外产生日志")
	}
	var internalLogs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pet_auto_pk_logs WHERE qq_binding_id = ? AND opponent_user_id = '900001'`, bindingID).Scan(&internalLogs); err != nil {
		t.Fatal(err)
	}
	if internalLogs != 0 {
		t.Fatalf("内部 PK 不应写入日志，实际 %d", internalLogs)
	}
}

func TestSchedulerUsesGeneratedOpponentForDailyTen(t *testing.T) {
	db, bindingID := openTestDB(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO pet_auto_pk_configs (qq_binding_id, enabled, target_starts, start_time) VALUES (?, 1, 10, '00:00')`, bindingID); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{}
	scheduler := newTestScheduler(db, client)
	item := &worker{}
	if !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) {
		t.Fatal("第一轮应提交 PK")
	}
	if len(client.started) != 1 {
		t.Fatalf("应提交一场 PK，实际 %#v", client.started)
	}
	parts := strings.Split(client.started[0], ":")
	if len(parts) != 3 || parts[1] != "2587495862" {
		t.Fatalf("应使用固定对手 QQ，实际 %#v", client.started)
	}
	decoded, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("生成的 pet_id 不是无填充 Base64，实际 %q: %v", parts[2], err)
	}
	value := string(decoded)
	if !strings.HasPrefix(value, "2587495862-2-2-") || len(strings.TrimPrefix(value, "2587495862-2-2-")) != 13 {
		t.Fatalf("解码后的 pet_id 格式不正确，实际 %q", value)
	}
	var count int64
	if err := db.QueryRow(`SELECT successful_starts FROM pet_auto_pk_states WHERE qq_binding_id = ?`, bindingID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("正式 PK 次数应计为 1，实际 %d", count)
	}
	logs, err := LoadLogs(context.Background(), db, bindingID, 20)
	if err != nil || len(logs) != 1 || logs[0].Message != "PK已提交" {
		t.Fatalf("PK 日志应只显示 PK已提交，logs=%#v err=%v", logs, err)
	}
}

func TestSchedulerStopsWhenDailyOpponentLimitExhausted(t *testing.T) {
	db, bindingID := openTestDB(t)
	defer db.Close()
	addCandidate(t, db, bindingID, "200001", "pet-a")
	today := timeutil.Current().Format("2006-01-02")
	if _, err := db.Exec(`INSERT INTO pet_auto_pk_configs (qq_binding_id, enabled, target_starts, start_time) VALUES (?, 1, 1, '00:00')`, bindingID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO pet_auto_pk_daily_opponents (qq_binding_id, opponent_user_id, run_date, start_count) VALUES (?, '200001', ?, 3)`, bindingID, today); err != nil {
		t.Fatal(err)
	}
	scheduler := newTestScheduler(db, &fakeClient{powers: map[string]json.RawMessage{}})
	item := &worker{}
	for i := 0; i < 3; i++ {
		if i < 2 && !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) {
			t.Fatal("三轮轮询完成前不应停止调度")
		}
		if i == 2 && !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) {
			t.Fatal("三轮轮询后应结束今日执行但保留调度")
		}
	}
	state, err := LoadState(context.Background(), db, bindingID)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Running || state.Message != "库内陌生人PK完成" {
		t.Fatalf("耗尽状态不正确: %#v", state)
	}
	config, err := LoadConfig(context.Background(), db, bindingID)
	if err != nil || !config.Enabled {
		t.Fatalf("候选耗尽后配置仍应保持启用: config=%#v err=%v", config, err)
	}
	var logCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pet_auto_pk_logs WHERE qq_binding_id = ? AND action = 'exhausted'`, bindingID).Scan(&logCount); err != nil {
		t.Fatal(err)
	}
	if logCount != 1 {
		t.Fatalf("应记录耗尽日志，实际数量 %d", logCount)
	}
}

func TestSchedulerCaresAfterEveryTwoSettlementsBeforeNextStart(t *testing.T) {
	db, bindingID := openTestDB(t)
	defer db.Close()
	addCandidate(t, db, bindingID, "200001", "pet-a")
	addCandidate(t, db, bindingID, "200002", "pet-b")
	addCandidate(t, db, bindingID, "200003", "pet-c")
	client := &fakeClient{powers: map[string]json.RawMessage{
		"pet-a": json.RawMessage(`{"power":80,"dominant_type":2}`),
		"pet-b": json.RawMessage(`{"power":80,"dominant_type":2}`),
		"pet-c": json.RawMessage(`{"power":80,"dominant_type":2}`),
	}}
	scheduler := newTestScheduler(db, client)
	if err := SaveConfig(context.Background(), db, bindingID, Config{Enabled: true, TargetStarts: 3}); err != nil {
		t.Fatal(err)
	}
	item := &worker{}
	if !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) {
		t.Fatal("首轮不应停止 worker")
	}
	if !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) {
		t.Fatal("第一场结算后不应停止 worker")
	}
	if strings.Contains(strings.Join(client.events, ","), "inventory") || len(client.fed) != 0 || len(client.bathed) != 0 {
		t.Fatalf("第一场结算后不应护理，实际调用 %#v", client.events)
	}
	if !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) {
		t.Fatal("第二场结算后不应停止 worker")
	}
	if !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) {
		t.Fatal("第二场结算后下一轮不应停止 worker")
	}
	if !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) {
		t.Fatal("护理完成后启动第三场不应停止 worker")
	}
	want := []string{
		"start:self-pet:200001:pet-a", "status:story-1", "settle:story-1",
		"start:self-pet:200002:pet-b", "status:story-2", "settle:story-2",
		"food_inventory", "feed:self-pet:food-1", "bath_inventory", "bathe:item-1", "start:self-pet:200003:pet-c",
	}
	if !reflect.DeepEqual(client.events, want) {
		t.Fatalf("每两场结算后应在下一场前护理，实际顺序 %#v", client.events)
	}
}

func TestSchedulerBuysTenItemsWhenCareStockIsInsufficient(t *testing.T) {
	db, bindingID := openTestDB(t)
	defer db.Close()
	addCandidate(t, db, bindingID, "200001", "pet-a")
	addCandidate(t, db, bindingID, "200002", "pet-b")
	client := &fakeClient{
		powers: map[string]json.RawMessage{
			"pet-a": json.RawMessage(`{"power":80,"dominant_type":2}`),
			"pet-b": json.RawMessage(`{"power":80,"dominant_type":2}`),
		},
		foodInventory: json.RawMessage(`{"data":{"foods":[{"food_id":"food-1","name":"饼干","quantity":"0"}]}}`),
		bathInventory: json.RawMessage(`{"result":{"items":[{"item_id":"item-1","name":"香皂片","balance":0}]}}`),
	}
	scheduler := newTestScheduler(db, client)
	if err := SaveConfig(context.Background(), db, bindingID, Config{Enabled: true, TargetStarts: 2}); err != nil {
		t.Fatal(err)
	}
	item := &worker{}
	for i := 0; i < 4; i++ {
		scheduler.runBindingWorker(context.Background(), bindingID, "123456", item)
	}
	if want := []string{"count:10"}; !reflect.DeepEqual(client.boughtFood, want) {
		t.Fatalf("食物库存不足应购买 10 个，实际 %#v", client.boughtFood)
	}
	if want := []string{"item-1:10"}; !reflect.DeepEqual(client.boughtBath, want) {
		t.Fatalf("洗护库存不足应购买 10 个，实际 %#v", client.boughtBath)
	}
	wantTail := []string{"food_inventory", "buy_food:count:10", "feed:self-pet:food-1", "bath_inventory", "buy_bath:item-1:10", "bathe:item-1"}
	if got := client.events[len(client.events)-len(wantTail):]; !reflect.DeepEqual(got, wantTail) {
		t.Fatalf("库存不足时应先购买再使用，实际顺序 %#v", got)
	}
}

func TestSchedulerCareFailureDoesNotStopWorker(t *testing.T) {
	db, bindingID := openTestDB(t)
	defer db.Close()
	addCandidate(t, db, bindingID, "200001", "pet-a")
	addCandidate(t, db, bindingID, "200002", "pet-b")
	addCandidate(t, db, bindingID, "200003", "pet-c")
	client := &fakeClient{
		powers: map[string]json.RawMessage{
			"pet-a": json.RawMessage(`{"power":80,"dominant_type":2}`),
			"pet-b": json.RawMessage(`{"power":80,"dominant_type":2}`),
			"pet-c": json.RawMessage(`{"power":80,"dominant_type":2}`),
		},
		feedErr:  errors.New("feed failed"),
		batheErr: errors.New("bathe failed"),
	}
	scheduler := newTestScheduler(db, client)
	if err := SaveConfig(context.Background(), db, bindingID, Config{Enabled: true, TargetStarts: 3}); err != nil {
		t.Fatal(err)
	}
	item := &worker{}
	for i := 0; i < 5; i++ {
		if !scheduler.runBindingWorker(context.Background(), bindingID, "123456", item) {
			t.Fatalf("护理失败后第 %d 轮不应停止 worker", i+1)
		}
	}
	if len(client.started) != 3 || client.started[2] != "self-pet:200003:pet-c" {
		t.Fatalf("护理失败后仍应启动下一场 PK，实际 %#v", client.started)
	}
	if got := client.events[len(client.events)-1]; got != "start:self-pet:200003:pet-c" {
		t.Fatalf("护理失败后调用顺序不正确，最后事件为 %q", got)
	}
}
