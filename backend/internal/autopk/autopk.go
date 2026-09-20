package autopk

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"qq-pet/backend/internal/timeutil"
)

const dailyOpponentLimit = 3

const generatedOpponentQQ = "2587495862"

type Client interface {
	GetPetPKPower(context.Context, int64, string) (json.RawMessage, error)
	StartPetPK(context.Context, int64, string, string, string) (json.RawMessage, error)
	GetPetPKStatus(context.Context, int64, string, string) (json.RawMessage, error)
	SettlePetPK(context.Context, int64, string, string) (json.RawMessage, error)
	GetPetProfile(context.Context, int64) (json.RawMessage, error)
	GetPetFoodCatalog(context.Context, int64) (json.RawMessage, error)
	GetPetBathInventory(context.Context, int64) (json.RawMessage, error)
	GetPetBathCatalog(context.Context, int64) (json.RawMessage, error)
	FeedPet(context.Context, int64, string, string) (json.RawMessage, error)
	BuyPetFood(context.Context, int64, int64) (json.RawMessage, error)
	BathePet(context.Context, int64, string, string, int64) (json.RawMessage, error)
	BuyPetBathItem(context.Context, int64, string, string, int64) (json.RawMessage, error)
	GetPetVitals(context.Context, int64, string) (json.RawMessage, error)
}

type Config struct {
	Enabled      bool   `json:"enabled"`
	TargetStarts int64  `json:"target_starts"`
	StartTime    string `json:"start_time"`
}

type State struct {
	Running            bool   `json:"running"`
	SuccessfulStarts   int64  `json:"successful_starts"`
	TodayStarts        int64  `json:"today_starts"`
	TargetStarts       int64  `json:"target_starts"`
	StartTime          string `json:"start_time"`
	LastOpponentUserID string `json:"last_opponent_user_id,omitempty"`
	Message            string `json:"message"`
	LastError          string `json:"last_error,omitempty"`
	LastRunAt          string `json:"last_run_at,omitempty"`
}

type Log struct {
	ID             int64  `json:"id"`
	Action         string `json:"action"`
	OpponentUserID string `json:"opponent_user_id,omitempty"`
	OpponentPetID  string `json:"opponent_pet_id,omitempty"`
	OpponentPower  int64  `json:"opponent_power"`
	Success        bool   `json:"success"`
	Message        string `json:"message"`
	Details        string `json:"details,omitempty"`
	CreatedAt      string `json:"created_at"`
}

type Scheduler struct {
	db       *sql.DB
	client   Client
	log      *slog.Logger
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	workers  map[int64]*worker
	wg       sync.WaitGroup
	interval time.Duration
	wait     func(context.Context, time.Duration) error
}

type worker struct {
	cancel        context.CancelFunc
	done          chan struct{}
	inflight      *inflight
	rounds        int
	date          string
	dailyFinished bool
	needsInternal bool
	internalPetID int64
}

type inflight struct {
	opponentUserID     string
	opponentPetID      string
	opponentPower      int64
	selfFinalPower     int64
	opponentFinalPower int64
	storyID            string
	selfPetID          string
	internal           bool
	quiet              bool
}

type candidate struct {
	ID           int64
	UserID       string
	PetID        string
	Power        int64
	DominantType int64
	Count        int64
}

func NewScheduler(db *sql.DB, client Client, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		db: db, client: client, log: logger, workers: make(map[int64]*worker), interval: 15 * time.Second,
		wait: func(ctx context.Context, duration time.Duration) error {
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	root := s.ctx
	s.mu.Unlock()

	rows, err := s.db.QueryContext(root, `SELECT q.id, q.qq_number FROM qq_bindings q JOIN pet_auto_pk_configs c ON c.qq_binding_id = q.id WHERE c.enabled = 1 ORDER BY q.id`)
	if err != nil {
		s.log.Error("自动 PK 启动恢复失败", "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var bindingID int64
		var qq string
		if rows.Scan(&bindingID, &qq) == nil {
			s.StartBinding(bindingID, qq)
		}
	}
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel, s.ctx = nil, nil
	workers := make([]context.CancelFunc, 0, len(s.workers))
	for id, item := range s.workers {
		workers = append(workers, item.cancel)
		delete(s.workers, id)
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	for _, stop := range workers {
		stop()
	}
	s.wg.Wait()
}

func (s *Scheduler) StartBinding(bindingID int64, qq string) {
	s.mu.Lock()
	if s.ctx == nil {
		s.mu.Unlock()
		return
	}
	if _, exists := s.workers[bindingID]; exists {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	item := &worker{cancel: cancel}
	s.workers[bindingID] = item
	s.wg.Add(1)
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			if s.workers[bindingID] == item {
				delete(s.workers, bindingID)
			}
			s.mu.Unlock()
		}()
		for {
			keepRunning := s.runBindingWorker(ctx, bindingID, qq, item)
			if !keepRunning {
				return
			}
			if s.wait(ctx, s.interval) != nil {
				return
			}
		}
	}()
}

func (s *Scheduler) StopBinding(bindingID int64) {
	s.mu.Lock()
	item := s.workers[bindingID]
	if item != nil {
		delete(s.workers, bindingID)
	}
	s.mu.Unlock()
	if item != nil {
		item.cancel()
	}
}

func (s *Scheduler) Enable(ctx context.Context, bindingID int64, qq string) error {
	config, err := LoadConfig(ctx, s.db, bindingID)
	if err != nil {
		return err
	}
	config.Enabled = true
	now := timeutil.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO pet_auto_pk_configs (qq_binding_id, enabled, target_starts, start_time, updated_at) VALUES (?, 1, ?, ?, ?) ON CONFLICT(qq_binding_id) DO UPDATE SET enabled = 1, target_starts = excluded.target_starts, start_time = excluded.start_time, updated_at = excluded.updated_at`, bindingID, config.TargetStarts, config.StartTime, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pet_auto_pk_states (qq_binding_id, running, successful_starts, last_opponent_user_id, message, last_error, last_run_at, updated_at) VALUES (?, 1, 0, '', '自动 PK 已启动', '', ?, ?) ON CONFLICT(qq_binding_id) DO UPDATE SET running = 1, successful_starts = 0, last_opponent_user_id = '', message = '自动 PK 已启动', last_error = '', last_run_at = excluded.last_run_at, updated_at = excluded.updated_at`, bindingID, now, now); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.record(bindingID, "start_scheduler", "", "", true, "自动 PK 已启动", fmt.Sprintf("target_starts=%d start_time=%s", config.TargetStarts, config.StartTime))
	s.StartBinding(bindingID, qq)
	return nil
}

func (s *Scheduler) Disable(ctx context.Context, bindingID int64, message string) error {
	now := timeutil.Now()
	if _, err := s.db.ExecContext(ctx, "UPDATE pet_auto_pk_configs SET enabled = 0, updated_at = ? WHERE qq_binding_id = ?", now, bindingID); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO pet_auto_pk_states (qq_binding_id, running, message, updated_at) VALUES (?, 0, ?, ?) ON CONFLICT(qq_binding_id) DO UPDATE SET running = 0, message = excluded.message, updated_at = excluded.updated_at`, bindingID, message, now); err != nil {
		return err
	}
	s.StopBinding(bindingID)
	s.record(bindingID, "stop_scheduler", "", "", true, message, "")
	return nil
}

func (s *Scheduler) runBinding(ctx context.Context, bindingID int64, qq string) bool {
	return s.runBindingWorker(ctx, bindingID, qq, &worker{})
}

func (s *Scheduler) runBindingWorker(ctx context.Context, bindingID int64, qq string, item *worker) bool {
	// #region debug-point A:worker-entry
	debugReport("A", "[DEBUG] 自动 PK worker 检查", map[string]any{"binding_id": bindingID, "qq": qq})
	// #endregion
	config, err := LoadConfig(ctx, s.db, bindingID)
	// #region debug-point A:config-gate
	debugReport("A", "[DEBUG] 自动 PK 配置检查", map[string]any{"binding_id": bindingID, "enabled": config.Enabled, "target_starts": config.TargetStarts, "start_time": config.StartTime, "load_error": errorText(err)})
	// #endregion
	if err != nil || !config.Enabled {
		return false
	}
	if config.TargetStarts == 0 {
		// #region debug-point C:target-disabled
		debugReport("C", "[DEBUG] 自动 PK 目标次数为 0", map[string]any{"binding_id": bindingID})
		// #endregion
		s.finish(bindingID, "自动 PK 已关闭（设置次数为 0）", "disabled")
		return false
	}
	currentTime := timeutil.Current()
	currentDate := currentTime.Format("2006-01-02")
	if item.date != currentDate {
		item.date = currentDate
		item.rounds = 0
		item.dailyFinished = false
		item.needsInternal = true
		item.internalPetID = 0
	}
	if item.dailyFinished {
		return true
	}
	if !startTimeReached(config.StartTime, currentTime) {
		// #region debug-point B:time-gate
		debugReport("B", "[DEBUG] 自动 PK 尚未到开始时间", map[string]any{"binding_id": bindingID, "configured_time": config.StartTime, "current_time": currentTime.Format("15:04:05"), "location": currentTime.Location().String()})
		// #endregion
		now := timeutil.Now()
		_, _ = s.db.ExecContext(ctx, `INSERT INTO pet_auto_pk_states (qq_binding_id, running, message, last_run_at, updated_at) VALUES (?, 1, ?, ?, ?) ON CONFLICT(qq_binding_id) DO UPDATE SET running = 1, message = excluded.message, last_run_at = excluded.last_run_at, updated_at = excluded.updated_at`, bindingID, fmt.Sprintf("等待自动 PK 开始时间 %s", config.StartTime), now, now)
		return true
	}
	state, err := LoadState(ctx, s.db, bindingID)
	if err != nil {
		s.fail(bindingID, "state", "自动 PK 状态读取失败", err)
		return true
	}
	selfID, err := strconv.ParseInt(qq, 10, 64)
	if err != nil {
		s.finish(bindingID, "QQ 号无效，自动 PK 已停止", "invalid_qq")
		return false
	}
	var selfPetID string
	if err := s.db.QueryRowContext(ctx, "SELECT pet_id FROM pet_profiles WHERE qq_binding_id = ?", bindingID).Scan(&selfPetID); err != nil || strings.TrimSpace(selfPetID) == "" {
		s.fail(bindingID, "profile", "自身宠物资料不完整", err)
		return true
	}
	if item.inflight != nil {
		fight := item.inflight
		status, statusErr := s.client.GetPetPKStatus(ctx, selfID, fight.selfPetID, fight.storyID)
		if statusErr != nil {
			if fight.internal || fight.quiet {
				return true
			}
			s.recordWithPower(bindingID, "status", fight.opponentUserID, fight.opponentPetID, fight.opponentPower, false, "PK 状态查询失败", statusErr.Error())
			return true
		}
		if !fight.internal && !fight.quiet {
			s.recordWithPower(bindingID, "status", fight.opponentUserID, fight.opponentPetID, fight.opponentPower, true, "PK 状态查询成功", string(status))
		}
		if !responseEmpty(status) {
			return true
		}
		settled, settleErr := s.client.SettlePetPK(ctx, selfID, fight.selfPetID, fight.storyID)
		if !fight.internal && !fight.quiet {
			s.recordWithPower(bindingID, "settle", fight.opponentUserID, fight.opponentPetID, fight.opponentPower, settleErr == nil, resultMessage(settleErr, "PK 结算成功"), string(settled))
		}
		if settleErr != nil {
			return true
		}
		item.inflight = nil
		if fight.internal {
			return true
		}
		item.needsInternal = true
		state, err = LoadState(ctx, s.db, bindingID)
		if err != nil {
			s.fail(bindingID, "state", "自动 PK 状态读取失败", err)
			return true
		}
		if state.TodayStarts > 0 && state.TodayStarts%2 == 0 {
			s.runPetCare(ctx, bindingID, selfID)
		}
		// 每轮调度只推进一个 PK 阶段。结算完成后等待下一次 15 秒调度，
		// 避免在同一轮中立即提交下一场 start_pet_pk。
		return true
	}
	if state.TodayStarts >= config.TargetStarts {
		// #region debug-point C:target-complete
		debugReport("C", "[DEBUG] 自动 PK 今日目标已完成", map[string]any{"binding_id": bindingID, "today_starts": state.TodayStarts, "successful_starts": state.SuccessfulStarts, "target_starts": config.TargetStarts})
		// #endregion
		s.finishDaily(bindingID, "目标 start 次数已完成", "complete")
		item.dailyFinished = true
		return true
	}
	if config.TargetStarts == 10 {
		petID := generatedOpponentPetID()
		result, startErr := s.client.StartPetPK(ctx, selfID, selfPetID, generatedOpponentQQ, petID)
		if startErr != nil {
			return true
		}
		storyID := findString(result, "story_id")
		if storyID == "" {
			return true
		}
		if _, updateErr := s.recordSuccessfulStart(ctx, bindingID, generatedOpponentQQ); updateErr != nil {
			s.fail(bindingID, "start_save", "PK start 已成功但状态保存失败", updateErr)
			return true
		}
		item.inflight = &inflight{opponentUserID: generatedOpponentQQ, opponentPetID: petID, storyID: storyID, selfPetID: selfPetID, quiet: true}
		s.recordWithPower(bindingID, "start", generatedOpponentQQ, petID, 0, true, "PK已提交", "")
		return true
	}
	var selfPower, selfType sql.NullInt64
	if err := s.db.QueryRowContext(ctx, "SELECT pk_power, dominant_type FROM pet_profiles WHERE qq_binding_id = ?", bindingID).Scan(&selfPower, &selfType); err != nil || !selfPower.Valid || !selfType.Valid {
		s.fail(bindingID, "profile", "自身宠物资料或战力不完整", err)
		return true
	}
	if item.needsInternal {
		var targetID int64
		var targetQQ, targetPetID string
		err := s.db.QueryRowContext(ctx, `SELECT id, user_id, pet_id FROM pet_auto_pk_internal_pets WHERE id > ? ORDER BY id LIMIT 1`, item.internalPetID).Scan(&targetID, &targetQQ, &targetPetID)
		if errors.Is(err, sql.ErrNoRows) && item.internalPetID != 0 {
			err = s.db.QueryRowContext(ctx, `SELECT id, user_id, pet_id FROM pet_auto_pk_internal_pets ORDER BY id LIMIT 1`).Scan(&targetID, &targetQQ, &targetPetID)
		}
		if err == nil {
			item.internalPetID = targetID
			result, startErr := s.client.StartPetPK(ctx, selfID, selfPetID, targetQQ, targetPetID)
			if startErr == nil {
				storyID := findString(result, "story_id")
				if storyID != "" {
					item.inflight = &inflight{opponentUserID: targetQQ, opponentPetID: targetPetID, storyID: storyID, selfPetID: selfPetID, internal: true}
					item.needsInternal = false
					return true
				}
			}
			return true
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return true
		}
		item.needsInternal = false
	}
	candidates, err := s.loadCandidates(ctx, bindingID)
	if err != nil {
		s.fail(bindingID, "candidates", "PK 候选读取失败", err)
		return true
	}
	// #region debug-point D:candidates
	debugReport("D", "[DEBUG] 自动 PK 候选读取完成", map[string]any{"binding_id": bindingID, "candidate_count": len(candidates), "last_opponent": state.LastOpponentUserID})
	// #endregion
	candidates = rotateCandidates(candidates, state.LastOpponentUserID)
	for _, candidate := range candidates {
		if candidate.Count >= dailyOpponentLimit {
			continue
		}
		_, parseErr := strconv.ParseInt(candidate.UserID, 10, 64)
		if parseErr != nil || strings.TrimSpace(candidate.PetID) == "" {
			continue
		}
		powerData, powerErr := s.client.GetPetPKPower(ctx, selfID, candidate.PetID)
		if powerErr != nil {
			s.record(bindingID, "power", candidate.UserID, candidate.PetID, false, "对手实时战力获取失败", powerErr.Error())
			continue
		}
		power, dominantType, ok := powerValues(powerData)
		if !ok {
			s.record(bindingID, "power", candidate.UserID, candidate.PetID, false, "对手实时战力格式无效", string(powerData))
			continue
		}
		now := timeutil.Now()
		if _, err := s.db.ExecContext(ctx, "UPDATE pet_pk_strangers SET power = ?, dominant_type = ?, power_updated_at = ?, updated_at = ? WHERE id = ?", power, dominantType, now, now, candidate.ID); err != nil {
			s.fail(bindingID, "power_save", "对手实时战力保存失败", err)
			return true
		}
		selfFinalPower := effectivePower(selfPower.Int64, selfType.Int64, dominantType)
		opponentFinalPower := effectivePower(power, dominantType, selfType.Int64)
		if selfFinalPower <= opponentFinalPower {
			// #region debug-point D:power-skip
			debugReport("D", "[DEBUG] 自动 PK 因最终战力不足跳过候选", map[string]any{"binding_id": bindingID, "opponent_user_id": candidate.UserID, "self_final_power": selfFinalPower, "opponent_final_power": opponentFinalPower})
			// #endregion
			s.recordWithPower(bindingID, "skip", candidate.UserID, candidate.PetID, power, true, "最终战力不足，已跳过", fmt.Sprintf("self_final_power=%d opponent_final_power=%d", selfFinalPower, opponentFinalPower))
			continue
		}
		result, startErr := s.client.StartPetPK(ctx, selfID, selfPetID, candidate.UserID, candidate.PetID)
		if startErr != nil {
			s.recordWithPower(bindingID, "start", candidate.UserID, candidate.PetID, power, false, "PK start 失败", startErr.Error())
			continue
		}
		storyID := findString(result, "story_id")
		if storyID == "" {
			s.recordWithPower(bindingID, "start", candidate.UserID, candidate.PetID, power, false, "PK start 结果缺少 story_id", string(result))
			continue
		}
		if _, updateErr := s.recordSuccessfulStart(ctx, bindingID, candidate.UserID); updateErr != nil {
			s.fail(bindingID, "start_save", "PK start 已成功但状态保存失败", updateErr)
			return true
		}
		item.inflight = &inflight{
			opponentUserID: candidate.UserID, opponentPetID: candidate.PetID, opponentPower: power,
			selfFinalPower: selfFinalPower, opponentFinalPower: opponentFinalPower, storyID: storyID, selfPetID: selfPetID,
		}
		details := fmt.Sprintf("story_id=%s self_final_power=%d opponent_final_power=%d response=%s", storyID, selfFinalPower, opponentFinalPower, string(result))
		s.recordWithPower(bindingID, "start", candidate.UserID, candidate.PetID, power, true, "PK start 成功", details)
		return true
	}
	item.rounds++
	if item.rounds < 3 {
		return true
	}
	s.finishDaily(bindingID, "库内陌生人PK完成", "exhausted")
	item.dailyFinished = true
	return true
}

func (s *Scheduler) loadCandidates(ctx context.Context, bindingID int64) ([]candidate, error) {
	today := timeutil.Current().Format("2006-01-02")
	rows, err := s.db.QueryContext(ctx, `SELECT s.id, s.user_id, s.pet_id, COALESCE(d.start_count, 0)
		FROM pet_pk_strangers s
		LEFT JOIN pet_auto_pk_daily_opponents d ON d.qq_binding_id = ? AND d.opponent_user_id = s.user_id AND d.run_date = ?
		ORDER BY s.id`, bindingID, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]candidate, 0)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.ID, &item.UserID, &item.PetID, &item.Count); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func rotateCandidates(items []candidate, last string) []candidate {
	if len(items) < 2 || last == "" {
		return items
	}
	index := len(items)
	for i := range items {
		if items[i].UserID == last {
			index = i
			break
		}
	}
	if index >= len(items) || items[index].UserID != last {
		return items
	}
	rotated := append([]candidate{}, items[index+1:]...)
	return append(rotated, items[:index+1]...)
}

func startTimeReached(value string, now time.Time) bool {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return false
	}
	minutes := now.Hour()*60 + now.Minute()
	return minutes >= parsed.Hour()*60+parsed.Minute()
}

func generatedOpponentPetID() string {
	raw := fmt.Sprintf("%s-2-2-%d", generatedOpponentQQ, time.Now().UnixMilli())
	return base64.RawStdEncoding.EncodeToString([]byte(raw))
}

func effectivePower(power, ownType, opponentType int64) int64 {
	if (ownType == 1 && opponentType == 2) || (ownType == 2 && opponentType == 3) || (ownType == 3 && opponentType == 1) {
		return power * 12 / 10
	}
	return power
}

func powerValues(data []byte) (int64, int64, bool) {
	value := decode(data)
	power, powerOK := findNumber(value, "power", "pk_power", "pet_power")
	dominantType, typeOK := findNumber(value, "dominant_type")
	return power, dominantType, powerOK && typeOK
}

func decode(data []byte) any {
	data = bytes.TrimPrefix(bytes.TrimSpace(data), []byte{0xef, 0xbb, 0xbf})
	var value any
	if json.Unmarshal(data, &value) != nil {
		return nil
	}
	for i := 0; i < 2; i++ {
		text, ok := value.(string)
		if !ok || json.Unmarshal([]byte(text), &value) != nil {
			break
		}
	}
	return value
}

func findNumber(value any, keys ...string) (int64, bool) {
	keySet := make(map[string]bool, len(keys))
	for _, key := range keys {
		keySet[key] = true
	}
	var walk func(any) (int64, bool)
	walk = func(item any) (int64, bool) {
		switch typed := item.(type) {
		case map[string]any:
			for key, child := range typed {
				if keySet[key] {
					switch number := child.(type) {
					case float64:
						return int64(number), true
					case json.Number:
						parsed, err := number.Int64()
						return parsed, err == nil
					}
				}
			}
			for _, child := range typed {
				if found, ok := walk(child); ok {
					return found, true
				}
			}
		case []any:
			for _, child := range typed {
				if found, ok := walk(child); ok {
					return found, true
				}
			}
		}
		return 0, false
	}
	return walk(value)
}

func responseEmpty(data []byte) bool {
	if len(bytes.TrimSpace(data)) == 0 {
		return true
	}
	value := decode(data)
	var walk func(any) bool
	walk = func(item any) bool {
		switch typed := item.(type) {
		case map[string]any:
			if empty, ok := typed["response_empty"].(bool); ok && empty {
				return true
			}
			for _, child := range typed {
				if walk(child) {
					return true
				}
			}
		case []any:
			for _, child := range typed {
				if walk(child) {
					return true
				}
			}
		}
		return false
	}
	return walk(value)
}

func findString(data []byte, key string) string {
	var walk func(any) string
	walk = func(item any) string {
		switch typed := item.(type) {
		case map[string]any:
			if text, ok := typed[key].(string); ok {
				return strings.TrimSpace(text)
			}
			for _, child := range typed {
				if found := walk(child); found != "" {
					return found
				}
			}
		case []any:
			for _, child := range typed {
				if found := walk(child); found != "" {
					return found
				}
			}
		}
		return ""
	}
	return walk(decode(data))
}

func findItemStock(data []byte, name string) (int64, bool) {
	return findItemStockValue(decode(data), strings.TrimSpace(name))
}

func findItemID(data []byte, name, idKey string) string {
	var walk func(any) string
	walk = func(item any) string {
		switch typed := item.(type) {
		case map[string]any:
			if itemName, _ := typed["name"].(string); strings.TrimSpace(itemName) == strings.TrimSpace(name) {
				switch id := typed[idKey].(type) {
				case string:
					return strings.TrimSpace(id)
				case float64:
					return strconv.FormatInt(int64(id), 10)
				}
			}
			for _, child := range typed {
				if found := walk(child); found != "" {
					return found
				}
			}
		case []any:
			for _, child := range typed {
				if found := walk(child); found != "" {
					return found
				}
			}
		}
		return ""
	}
	return walk(decode(data))
}

func findItemStockValue(value any, name string) (int64, bool) {
	if object, ok := value.(map[string]any); ok {
		itemName := ""
		for _, key := range []string{"name", "food_name", "item_name", "title"} {
			if text, ok := object[key].(string); ok {
				itemName = strings.TrimSpace(text)
				break
			}
		}
		if itemName == name {
			for _, key := range []string{"stock", "count", "quantity", "balance", "amount", "num"} {
				if number, ok := stockNumber(object[key]); ok {
					return int64(number), true
				}
			}
		}
		for _, child := range object {
			if stock, found := findItemStockValue(child, name); found {
				return stock, true
			}
		}
	}
	if items, ok := value.([]any); ok {
		for _, child := range items {
			if stock, found := findItemStockValue(child, name); found {
				return stock, true
			}
		}
	}
	return 0, false
}

func stockNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case json.Number:
		result, err := number.Float64()
		return result, err == nil
	case string:
		result, err := strconv.ParseFloat(number, 64)
		return result, err == nil
	default:
		return 0, false
	}
}

func (s *Scheduler) runPetCare(ctx context.Context, bindingID, selfID int64) {
	const food = "饼干"
	const bathItem = "香皂片"

	var petID string
	if err := s.db.QueryRowContext(ctx, "SELECT pet_id FROM pet_profiles WHERE qq_binding_id = ?", bindingID).Scan(&petID); err != nil || strings.TrimSpace(petID) == "" {
		details := "pet_id 为空"
		if err != nil {
			details = err.Error()
		}
		s.record(bindingID, "care_feed", "", food, false, "自动喂食失败：宠物资料中未找到 pet_id", details)
	} else {
		s.feedPet(ctx, bindingID, selfID, petID, food)
	}
	s.bathePet(ctx, bindingID, selfID, petID, bathItem)
}

func (s *Scheduler) feedPet(ctx context.Context, bindingID, selfID int64, petID, food string) {
	inventory, err := s.client.GetPetFoodCatalog(ctx, selfID)
	if err != nil {
		s.record(bindingID, "care_feed", "", food, false, "自动喂食失败：食物库存读取失败", err.Error())
		return
	}
	stock, found := findItemStock(inventory, food)
	foodID := findItemID(inventory, food, "food_id")
	if foodID == "" {
		s.record(bindingID, "care_feed", "", food, false, "自动喂食失败：食物目录中未找到 food_id", string(inventory))
		return
	}
	if !found || stock < 1 {
		bought, buyErr := s.client.BuyPetFood(ctx, selfID, 10)
		if buyErr != nil {
			s.record(bindingID, "care_buy_food", "", food, false, "自动购买食物失败，已跳过本次喂食", buyErr.Error())
			return
		}
		s.record(bindingID, "care_buy_food", "", food, true, "食物库存不足，已自动购买 10 个饼干", string(bought))
	}
	fed, err := s.client.FeedPet(ctx, selfID, petID, foodID)
	message := "已自动使用饼干喂食"
	if err != nil {
		message = "自动喂食失败：" + err.Error()
	}
	s.record(bindingID, "care_feed", "", food, err == nil, message, string(fed))
}

func (s *Scheduler) bathePet(ctx context.Context, bindingID, selfID int64, petID, item string) {
	inventory, err := s.client.GetPetBathInventory(ctx, selfID)
	if err != nil {
		s.record(bindingID, "care_bathe", "", item, false, "自动洗护失败：洗护用品库存读取失败", err.Error())
		return
	}
	stock, found := findItemStock(inventory, item)
	catalog, catalogErr := s.client.GetPetBathCatalog(ctx, selfID)
	if catalogErr != nil {
		s.record(bindingID, "care_bathe", "", item, false, "自动洗护失败：洗护目录读取失败", catalogErr.Error())
		return
	}
	itemID := findItemID(catalog, item, "item_id")
	if itemID == "" {
		s.record(bindingID, "care_bathe", "", item, false, "自动洗护失败：洗护目录中未找到 item_id", string(catalog))
		return
	}
	if !found || stock < 1 {
		bought, buyErr := s.client.BuyPetBathItem(ctx, selfID, petID, itemID, 10)
		if buyErr != nil {
			s.record(bindingID, "care_buy_bath", "", item, false, "自动购买洗护用品失败，已跳过本次洗护", buyErr.Error())
			return
		}
		s.record(bindingID, "care_buy_bath", "", item, true, "洗护用品库存不足，已自动购买 10 个香皂片", string(bought))
	}
	bathed, err := s.client.BathePet(ctx, selfID, petID, itemID, 1)
	message := "已自动使用香皂片洗护"
	if err != nil {
		message = "自动洗护失败：" + err.Error()
	}
	s.record(bindingID, "care_bathe", "", item, err == nil, message, string(bathed))
}

func (s *Scheduler) recordSuccessfulStart(ctx context.Context, bindingID int64, opponentUserID string) (int64, error) {
	today, now := timeutil.Current().Format("2006-01-02"), timeutil.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO pet_auto_pk_daily_opponents (qq_binding_id, opponent_user_id, run_date, start_count, updated_at) VALUES (?, ?, ?, 1, ?) ON CONFLICT(qq_binding_id, opponent_user_id, run_date) DO UPDATE SET start_count = start_count + 1, updated_at = excluded.updated_at`, bindingID, opponentUserID, today, now); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pet_auto_pk_states (qq_binding_id, running, successful_starts, last_opponent_user_id, message, last_run_at, updated_at) VALUES (?, 1, 1, ?, 'PK start 成功', ?, ?) ON CONFLICT(qq_binding_id) DO UPDATE SET running = 1, successful_starts = successful_starts + 1, last_opponent_user_id = excluded.last_opponent_user_id, message = excluded.message, last_error = '', last_run_at = excluded.last_run_at, updated_at = excluded.updated_at`, bindingID, opponentUserID, now, now); err != nil {
		return 0, err
	}
	var starts int64
	if err = tx.QueryRowContext(ctx, "SELECT successful_starts FROM pet_auto_pk_states WHERE qq_binding_id = ?", bindingID).Scan(&starts); err != nil {
		return 0, err
	}
	return starts, tx.Commit()
}

func (s *Scheduler) finish(bindingID int64, message, action string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := timeutil.Now()
	_, err := s.db.ExecContext(ctx, "UPDATE pet_auto_pk_configs SET enabled = 0, updated_at = ? WHERE qq_binding_id = ?", now, bindingID)
	if err == nil {
		_, err = s.db.ExecContext(ctx, `INSERT INTO pet_auto_pk_states (qq_binding_id, running, message, last_run_at, updated_at) VALUES (?, 0, ?, ?, ?) ON CONFLICT(qq_binding_id) DO UPDATE SET running = 0, message = excluded.message, last_error = '', last_run_at = excluded.last_run_at, updated_at = excluded.updated_at`, bindingID, message, now, now)
	}
	if err != nil {
		s.log.Error("自动 PK 停止状态保存失败", "binding_id", bindingID, "error", err)
	}
	s.record(bindingID, action, "", "", true, message, "")
}

func (s *Scheduler) finishDaily(bindingID int64, message, action string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := timeutil.Now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO pet_auto_pk_states (qq_binding_id, running, message, last_run_at, updated_at) VALUES (?, 1, ?, ?, ?) ON CONFLICT(qq_binding_id) DO UPDATE SET running = 1, message = excluded.message, last_error = '', last_run_at = excluded.last_run_at, updated_at = excluded.updated_at`, bindingID, message, now, now)
	if err != nil {
		s.log.Error("自动 PK 今日完成状态保存失败", "binding_id", bindingID, "error", err)
	}
	s.record(bindingID, action, "", "", true, message, "")
}

func (s *Scheduler) fail(bindingID int64, action, message string, err error) {
	details := ""
	if err != nil {
		details = err.Error()
	}
	now := timeutil.Now()
	_, _ = s.db.Exec(`INSERT INTO pet_auto_pk_states (qq_binding_id, running, message, last_error, last_run_at, updated_at) VALUES (?, 1, ?, ?, ?, ?) ON CONFLICT(qq_binding_id) DO UPDATE SET running = 1, message = excluded.message, last_error = excluded.last_error, last_run_at = excluded.last_run_at, updated_at = excluded.updated_at`, bindingID, message, details, now, now)
	s.record(bindingID, action, "", "", false, message, details)
}

func (s *Scheduler) record(bindingID int64, action, opponentUserID, opponentPetID string, success bool, message, details string) {
	s.recordWithPower(bindingID, action, opponentUserID, opponentPetID, 0, success, message, details)
}

func (s *Scheduler) recordWithPower(bindingID int64, action, opponentUserID, opponentPetID string, opponentPower int64, success bool, message, details string) {
	if strings.TrimSpace(opponentUserID) == "" {
		opponentUserID = "0"
	}
	_, err := s.db.Exec("INSERT INTO pet_auto_pk_logs (qq_binding_id, action, opponent_user_id, opponent_pet_id, opponent_power, success, message, details, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", bindingID, action, opponentUserID, opponentPetID, opponentPower, boolInt(success), message, details, timeutil.Now())
	if err != nil {
		s.log.Error("自动 PK 日志保存失败", "binding_id", bindingID, "error", err)
	}
}

func LoadConfig(ctx context.Context, db *sql.DB, bindingID int64) (Config, error) {
	var config Config
	var enabled int
	err := db.QueryRowContext(ctx, "SELECT enabled, target_starts, COALESCE(start_time, '00:00') FROM pet_auto_pk_configs WHERE qq_binding_id = ?", bindingID).Scan(&enabled, &config.TargetStarts, &config.StartTime)
	if errors.Is(err, sql.ErrNoRows) {
		return Config{TargetStarts: 10, StartTime: "01:00"}, nil
	}
	if config.StartTime == "" {
		config.StartTime = "00:00"
	}
	config.Enabled = enabled == 1
	return config, err
}

func SaveConfig(ctx context.Context, db *sql.DB, bindingID int64, config Config) error {
	_, err := db.ExecContext(ctx, `INSERT INTO pet_auto_pk_configs (qq_binding_id, enabled, target_starts, start_time, updated_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT(qq_binding_id) DO UPDATE SET target_starts = excluded.target_starts, start_time = excluded.start_time, updated_at = excluded.updated_at`, bindingID, boolInt(config.Enabled), config.TargetStarts, config.StartTime, timeutil.Now())
	return err
}

func LoadState(ctx context.Context, db *sql.DB, bindingID int64) (State, error) {
	var state State
	var running int
	err := db.QueryRowContext(ctx, `SELECT s.running, s.successful_starts, s.last_opponent_user_id, s.message, s.last_error, COALESCE(s.last_run_at, ''), COALESCE(c.target_starts, 0), COALESCE(c.start_time, '00:00') FROM pet_auto_pk_states s LEFT JOIN pet_auto_pk_configs c ON c.qq_binding_id = s.qq_binding_id WHERE s.qq_binding_id = ?`, bindingID).Scan(&running, &state.SuccessfulStarts, &state.LastOpponentUserID, &state.Message, &state.LastError, &state.LastRunAt, &state.TargetStarts, &state.StartTime)
	if errors.Is(err, sql.ErrNoRows) {
		config, configErr := LoadConfig(ctx, db, bindingID)
		return State{Message: "未启动自动 PK", TargetStarts: config.TargetStarts, StartTime: config.StartTime}, configErr
	}
	state.Running = running == 1
	if err == nil {
		today := timeutil.Current().Format("2006-01-02")
		err = db.QueryRowContext(ctx, "SELECT COALESCE(SUM(start_count), 0) FROM pet_auto_pk_daily_opponents WHERE qq_binding_id = ? AND run_date = ?", bindingID, today).Scan(&state.TodayStarts)
	}
	return state, err
}

func LoadLogs(ctx context.Context, db *sql.DB, bindingID int64, limit int) ([]Log, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := db.QueryContext(ctx, "SELECT id, action, opponent_user_id, opponent_pet_id, opponent_power, success, message, details, created_at FROM pet_auto_pk_logs WHERE qq_binding_id = ? AND action = 'start' AND success = 1 ORDER BY id DESC LIMIT ?", bindingID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Log, 0)
	for rows.Next() {
		var item Log
		var success int
		if err := rows.Scan(&item.ID, &item.Action, &item.OpponentUserID, &item.OpponentPetID, &item.OpponentPower, &success, &item.Message, &item.Details, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Success = success == 1
		item.Message = "PK已提交"
		item.Details = ""
		items = append(items, item)
	}
	return items, rows.Err()
}

// #region debug-point A:reporting
func debugReport(hypothesis string, message string, data map[string]any) {
	payload := map[string]any{"sessionId": "auto-pk-not-starting", "runId": "pre-fix", "hypothesisId": hypothesis, "location": "backend/internal/autopk/autopk.go", "msg": message, "data": data, "ts": time.Now().UnixMilli()}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	request, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:7777/event", bytes.NewReader(body))
	if err != nil {
		return
	}
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 500 * time.Millisecond}
	_, _ = client.Do(request)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// #endregion

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func resultMessage(err error, success string) string {
	if err != nil {
		return err.Error()
	}
	return success
}
