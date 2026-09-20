package autocontrol

import (
	// #region debug-point AC-SEND:async-debug-event-sender
	"bytes"
	// #endregion
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	// #region debug-point AC-SEND:async-debug-event-sender
	"net/http"
	// #endregion
	"strconv"
	"strings"
	"sync"
	"time"

	"qq-pet/backend/internal/mokant"
	"qq-pet/backend/internal/timeutil"
)

var validActivities = map[string]bool{"school": true, "work": true, "adventure": true}

var errBindingOffline = errors.New("QQ 当前不在线")
var errBindingServiceCheck = errors.New("QQ 服务期限查询失败")
var errBindingLoginSync = errors.New("QQ 登录状态正在同步")

const loginSyncGracePeriod = 45 * time.Second

type Notifier interface {
	NotifyActivityStarted(context.Context, string, string, string, string) error
	NotifyActivityCompleted(context.Context, string, string, string, string, string) error
	NotifyOffline(context.Context, string, string) error
}

// #region debug-point AC-SEND:async-debug-event-sender
var sendDebugEvent = func(hypothesisID, location, msg string, data map[string]any) {
	payload, err := json.Marshal(map[string]any{
		"sessionId":    "recalled-pet-plan-stall",
		"runId":        "pre-fix",
		"hypothesisId": hypothesisID,
		"location":     location,
		"msg":          "[DEBUG] " + msg,
		"data":         data,
		"ts":           time.Now().UnixMilli(),
	})
	if err != nil {
		return
	}
	go func() {
		request, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:7777/event", bytes.NewReader(payload))
		if err != nil {
			return
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := (&http.Client{Timeout: 2 * time.Second}).Do(request)
		if err == nil {
			_ = response.Body.Close()
		}
	}()
}

// #endregion

type PlanItem struct {
	ID               string `json:"id"`
	Activity         string `json:"activity"`
	ControlMode      string `json:"control_mode"`
	Count            int64  `json:"count"`
	Hours            int64  `json:"hours"`
	LearningTarget   string `json:"learning_target,omitempty"`
	WorkCareerType   *int64 `json:"work_career_type,omitempty"`
	WorkOption       string `json:"work_option,omitempty"`
	WorkSubEventType *int64 `json:"work_sub_event_type,omitempty"`
	ExecutedCount    int64  `json:"executed_count"`
	ExecutedSeconds  int64  `json:"executed_seconds"`
	Completed        bool   `json:"completed"`
	LastNotifiedDate string `json:"last_notified_date,omitempty"`
}

type Config struct {
	Enabled               bool       `json:"enabled"`
	Plan                  []PlanItem `json:"plan"`
	LearningTarget        string     `json:"learning_target,omitempty"`
	ActivityOrder         []string   `json:"activity_order"`
	SchoolCount           int64      `json:"school_count"`
	WorkCount             int64      `json:"work_count"`
	AdventureCount        int64      `json:"adventure_count"`
	SchoolRemaining       int64      `json:"school_remaining"`
	WorkRemaining         int64      `json:"work_remaining"`
	AdventureRemain       int64      `json:"adventure_remaining"`
	SchoolExecuted        int64      `json:"school_executed"`
	WorkExecuted          int64      `json:"work_executed"`
	AdventureExecuted     int64      `json:"adventure_executed"`
	SchoolControlMode     string     `json:"school_control_mode"`
	WorkControlMode       string     `json:"work_control_mode"`
	AdventureControlMode  string     `json:"adventure_control_mode"`
	SchoolDuration        int64      `json:"school_duration_seconds"`
	WorkDuration          int64      `json:"work_duration_seconds"`
	AdventureDuration     int64      `json:"adventure_duration_seconds"`
	SchoolDurationUsed    int64      `json:"school_duration_executed"`
	WorkDurationUsed      int64      `json:"work_duration_executed"`
	AdventureDurationUsed int64      `json:"adventure_duration_executed"`
	WorkOption            string     `json:"work_option"`
	AutoFeedEnabled       bool       `json:"auto_feed_enabled"`
	AutoFeedThreshold     int64      `json:"auto_feed_threshold"`
	AutoFeedFood          string     `json:"auto_feed_food"`
	AutoBatheEnabled      bool       `json:"auto_bathe_enabled"`
	AutoBatheThreshold    int64      `json:"auto_bathe_threshold"`
	AutoBatheItem         string     `json:"auto_bathe_item"`
	EncourageProbability  int64      `json:"encourage_probability"`
	AutoPokeEnabled       bool       `json:"auto_poke_enabled"`
	AutoPokeStartTime     string     `json:"auto_poke_start_time"`
	AutoPokeDailyLimit    int64      `json:"auto_poke_daily_limit"`
	AutoPokeDailyCount    int64      `json:"auto_poke_daily_count"`
	AutoPokeInterval      int64      `json:"auto_poke_interval_seconds"`
	AutoPokeLastRunAt     string     `json:"auto_poke_last_run_at,omitempty"`
	AutoPokeLastFriendQQ  string     `json:"auto_poke_last_friend_qq,omitempty"`
	LastResetDate         string     `json:"last_reset_date"`
}

type State struct {
	Running         bool   `json:"running"`
	CurrentActivity string `json:"current_activity"`
	CurrentOption   string `json:"current_option"`
	CurrentStoryID  string `json:"-"`
	CurrentPlanID   string `json:"-"`
	CurrentDuration int64  `json:"-"`
	Message         string `json:"message"`
	LastAction      string `json:"last_action"`
	LastError       string `json:"last_error,omitempty"`
	LastRunAt       string `json:"last_run_at,omitempty"`
}

type Log struct {
	ID         int64  `json:"id"`
	Action     string `json:"action"`
	Activity   string `json:"activity"`
	OptionName string `json:"option_name"`
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	Details    string `json:"details"`
	CreatedAt  string `json:"created_at"`
}

type Scheduler struct {
	db       *sql.DB
	client   *mokant.Client
	log      *slog.Logger
	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
	workers  map[int64]*bindingWorker
	workerWG sync.WaitGroup
	notifier Notifier
}

type bindingWorker struct {
	cancel context.CancelFunc
}

const startupRestoreBatchSize = 10

const startupRestoreDelay = 5 * time.Second

func NewScheduler(db *sql.DB, client *mokant.Client, logger *slog.Logger, notifier ...Notifier) *Scheduler {
	scheduler := &Scheduler{db: db, client: client, log: logger, workers: make(map[int64]*bindingWorker)}
	if len(notifier) > 0 {
		scheduler.notifier = notifier[0]
	}
	return scheduler
}

func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.done = make(chan struct{})
	done := s.done
	s.mu.Unlock()
	go func() {
		defer close(done)
		timer := time.NewTimer(startupRestoreDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		s.restore(ctx)
	}()
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.cancel, s.done = nil, nil
	workerCancels := make([]context.CancelFunc, 0, len(s.workers))
	for bindingID, workerCancel := range s.workers {
		workerCancels = append(workerCancels, workerCancel.cancel)
		delete(s.workers, bindingID)
	}
	s.mu.Unlock()
	for _, workerCancel := range workerCancels {
		workerCancel()
	}
	if cancel != nil {
		cancel()
		<-done
	}
	s.workerWG.Wait()
}

func (s *Scheduler) StartBinding(parent context.Context, bindingID int64, qq string) {
	s.mu.Lock()
	if s.cancel == nil {
		s.mu.Unlock()
		return
	}
	if _, exists := s.workers[bindingID]; exists {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	worker := &bindingWorker{cancel: cancel}
	s.workers[bindingID] = worker
	s.workerWG.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.workerWG.Done()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		defer func() {
			s.mu.Lock()
			if s.workers[bindingID] == worker {
				delete(s.workers, bindingID)
			}
			s.mu.Unlock()
		}()
		go func() {
			pokeTicker := time.NewTicker(15 * time.Second)
			defer pokeTicker.Stop()
			s.runAutoPokeTick(ctx, bindingID, qq)
			for {
				select {
				case <-ctx.Done():
					return
				case <-pokeTicker.C:
					s.runAutoPokeTick(ctx, bindingID, qq)
				}
			}
		}()
		s.runBinding(ctx, bindingID, qq)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runBinding(ctx, bindingID, qq)
			}
		}
	}()
}

func (s *Scheduler) StopBinding(bindingID int64) {
	s.mu.Lock()
	worker := s.workers[bindingID]
	if worker != nil {
		delete(s.workers, bindingID)
	}
	s.mu.Unlock()
	if worker != nil {
		worker.cancel()
	}
}

func (s *Scheduler) restore(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, qq_number FROM qq_bindings WHERE EXISTS (SELECT 1 FROM pet_auto_control_configs c WHERE c.qq_binding_id = qq_bindings.id AND (c.enabled = 1 OR c.auto_poke_enabled = 1)) ORDER BY id")
	if err != nil {
		s.log.Error("自动控制启动恢复扫描失败", "error", err)
		return
	}
	defer rows.Close()
	batch := make([]struct {
		bindingID int64
		qq        string
	}, 0, startupRestoreBatchSize)
	for rows.Next() {
		var item struct {
			bindingID int64
			qq        string
		}
		if rows.Scan(&item.bindingID, &item.qq) != nil {
			continue
		}
		batch = append(batch, item)
		if len(batch) == startupRestoreBatchSize {
			if !s.runRestoreBatch(ctx, batch) {
				return
			}
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		s.runRestoreBatch(ctx, batch)
	}
}

func (s *Scheduler) runRestoreBatch(ctx context.Context, batch []struct {
	bindingID int64
	qq        string
}) bool {
	for _, item := range batch {
		s.StartBinding(ctx, item.bindingID, item.qq)
	}
	if len(batch) < startupRestoreBatchSize {
		return true
	}
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *Scheduler) runBinding(parent context.Context, bindingID int64, qq string) {
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	config, err := LoadConfig(ctx, s.db, bindingID)
	// #region debug-point AC-01:runBinding-loop-entry
	sendDebugEvent("AC-01", "autocontrol.runBinding", "runBinding loop entry", map[string]any{"binding_id": bindingID, "qq": qq, "config_enabled": config.Enabled, "plan_count": len(config.Plan)})
	// #endregion
	if err != nil || !config.Enabled {
		return
	}
	if reason, err := s.checkBindingReady(ctx, bindingID, qq); err != nil {
		if errors.Is(err, errBindingServiceCheck) || errors.Is(err, errBindingLoginSync) {
			s.record(bindingID, "precheck", "", "", false, reason, err.Error())
			return
		}
		s.disableBinding(bindingID, reason)
		if errors.Is(err, errBindingOffline) {
			s.notifyOffline(qq, reason)
		}
		return
	}
	if config.EncourageProbability < 0 || config.EncourageProbability > 100 {
		config.EncourageProbability = 100
	}
	today := timeutil.Current().Format("2006-01-02")
	if config.LastResetDate != today {
		if err := resetDailyConfig(ctx, s.db, bindingID, today); err != nil {
			s.record(bindingID, "reset", "", "", false, "每日任务重置失败", err.Error())
			return
		}
		config.SchoolRemaining = config.SchoolCount
		config.WorkRemaining = config.WorkCount
		config.AdventureRemain = config.AdventureCount
		config.SchoolExecuted = 0
		config.WorkExecuted = 0
		config.AdventureExecuted = 0
		config.SchoolDurationUsed = 0
		config.WorkDurationUsed = 0
		config.AdventureDurationUsed = 0
		config.AutoPokeDailyCount = 0
		config.AutoPokeLastRunAt = ""
		config.AutoPokeLastFriendQQ = ""
		for index := range config.Plan {
			config.Plan[index].ExecutedCount = 0
			config.Plan[index].ExecutedSeconds = 0
			config.Plan[index].Completed = false
			config.Plan[index].LastNotifiedDate = ""
		}
		config.LastResetDate = today
		if err := savePlanProgress(ctx, s.db, bindingID, config.Plan); err != nil {
			s.record(bindingID, "reset", "", "", false, "每日计划重置失败", err.Error())
			return
		}
		s.updateState(bindingID, false, "", "", "每日00点已重置任务次数", "reset", "")
	}
	selfID := mustInt64(qq)
	petID := s.petID(ctx, selfID)
	if petID == "" {
		return
	}
	statusData, err := s.client.GetPetActivityStatus(ctx, selfID, petID)
	if err != nil {
		s.record(bindingID, "status", "", "", false, err.Error(), "")
		return
	}
	status, ok := parseActivityStatus(statusData)
	if !ok {
		s.record(bindingID, "status", "", "", false, "活动状态格式无效", string(statusData))
		return
	}
	state, stateErr := LoadState(ctx, s.db, bindingID)
	if stateErr != nil {
		s.record(bindingID, "state", "", "", false, "自动控制状态读取失败", stateErr.Error())
		return
	}
	inProgress := activityInProgress(statusData, struct {
		StoryID          string `json:"story_id"`
		RemainingSeconds int64  `json:"remaining_seconds"`
	}{StoryID: strings.TrimSpace(status.StoryID), RemainingSeconds: status.RemainingSeconds})
	// #region debug-point AC-02:activity-status-parsed
	sendDebugEvent("AC-02", "autocontrol.runBinding", "pet activity status parsed", map[string]any{"reported_story_id": status.StoryID, "state": status.StateCode, "remaining": status.RemainingSeconds, "in_progress": inProgress})
	// #endregion
	// #region debug-point AC-03:tracked-activity-end-decision
	trackedEnded := trackedActivityEnded(state.CurrentStoryID, status.StoryID, inProgress)
	sendDebugEvent("AC-03", "autocontrol.runBinding", "tracked activity end decision", map[string]any{"local_story_id": state.CurrentStoryID, "reported_story_id": status.StoryID, "equal": strings.TrimSpace(state.CurrentStoryID) == strings.TrimSpace(status.StoryID), "in_progress": inProgress, "trackedEnded": trackedEnded})
	// #endregion
	if trackedEnded {
		if !validActivities[state.CurrentActivity] {
			s.clearRunningActivity(bindingID, "已清理失效的活动状态，将继续执行计划", "recover")
		} else if err := s.completeActivity(ctx, bindingID, qq, state.CurrentPlanID, state.CurrentActivity, state.CurrentOption, completedActivityDuration(state.CurrentDuration, status.DurationSeconds)); err != nil {
			s.record(bindingID, "complete", state.CurrentActivity, state.CurrentOption, false, "活动完成状态更新失败", err.Error())
			return
		}
		config, err = LoadConfig(ctx, s.db, bindingID)
		if err != nil {
			s.record(bindingID, "config", "", "", false, "活动完成后配置读取失败", err.Error())
			return
		}
		state = State{}
	}
	if inProgress {
		_ = s.runPetCare(ctx, bindingID, qq, config)
		if state.CurrentStoryID == "" || status.StoryID != state.CurrentStoryID {
			// #region debug-point AC-04:external-activity-wait-return
			reason := "reported_story_differs_from_tracked"
			if state.CurrentStoryID == "" {
				reason = "no_tracked_story"
			}
			sendDebugEvent("AC-04", "autocontrol.runBinding", "waiting for external activity", map[string]any{"reason": reason})
			// #endregion
			s.clearRunningActivity(bindingID, "宠物正在被雇佣或执行其他活动，结束后将自动继续计划", "occupied")
			return
		}
		currentActivity := activityFromStateCode(status.StateCode)
		if currentActivity == "" {
			currentActivity = state.CurrentActivity
		}
		if currentActivity == "adventure" || rand.Int63n(100) >= config.EncourageProbability {
			// #region debug-point AC-04:in-progress-early-return
			reason := "encourage_probability_not_met"
			if currentActivity == "adventure" {
				reason = "adventure_not_encouraged"
			}
			sendDebugEvent("AC-04", "autocontrol.runBinding", "in-progress activity early return", map[string]any{"reason": reason})
			// #endregion
			return
		}
		_, err = s.client.EncouragePetActivity(ctx, selfID, petID, state.CurrentStoryID)
		s.record(bindingID, "encourage", currentActivity, state.CurrentOption, err == nil, resultMessage(err, "活动进行中，已发送鼓励"), "")
		return
	}
	item, ok := nextPlanItem(config)
	// #region debug-point AC-05:next-plan-item-selection
	sendDebugEvent("AC-05", "autocontrol.runBinding", "next plan item selected", map[string]any{"selected": ok, "plan_id": item.ID, "activity": item.Activity, "count_progress": map[string]any{"executed": item.ExecutedCount, "target": item.Count}, "hours_progress": map[string]any{"executed_seconds": item.ExecutedSeconds, "target_hours": item.Hours}})
	// #endregion
	if !ok {
		s.runPetCare(ctx, bindingID, qq, config)
		s.updateState(bindingID, false, "", "", "今日计划已完成", "complete", "")
		return
	}
	activity := item.Activity
	option, subEventType, saved := savedWorkOption(item)
	var optionsData []byte
	if !saved {
		var careerType int64
		if activity == "work" {
			overviewData, overviewErr := s.client.GetPetActivityOverview(ctx, selfID, petID, activity)
			if overviewErr != nil {
				s.record(bindingID, "overview", activity, "", false, overviewErr.Error(), "")
				return
			}
			if number, ok := findNumber(decodeObject(overviewData), "current_career_type"); ok {
				careerType = int64(number)
			}
		}
		optionsData, err = s.client.GetPetActivityOptions(ctx, selfID, petID, activity, careerType)
		if err != nil {
			s.record(bindingID, "options", activity, "", false, err.Error(), "")
			return
		}
		option, subEventType = pickOptionDetails(optionsData, activity, item.LearningTarget, item.WorkOption)
		if option == "" {
			s.runPetCare(ctx, bindingID, qq, config)
			s.record(bindingID, "options", activity, "", false, "暂无符合条件的项目，已检查宠物状态并将在下轮重试", string(optionsData))
			return
		}
	}
	result, err := s.client.StartPetActivity(ctx, selfID, petID, activity, option, subEventType)
	// #region debug-point AC-06:start-activity-result
	startError := ""
	if err != nil {
		startError = err.Error()
	}
	sendDebugEvent("AC-06", "autocontrol.runBinding", "start activity returned", map[string]any{"activity": activity, "option": option, "start_error": startError, "story_id": findStoryID(result)})
	// #endregion
	if err != nil {
		s.record(bindingID, "start", activity, option, false, err.Error(), "")
		return
	}
	storyID := findStoryID(result)
	if storyID == "" {
		s.record(bindingID, "start", activity, option, false, "活动启动结果缺少 story_id，未记录运行状态", string(result))
		return
	}
	duration := findActivityDuration(result, optionsData, option)
	// #region debug-point AC-07:start-state-save
	sendDebugEvent("AC-07", "autocontrol.runBinding", "saving started activity state", map[string]any{"binding_id": bindingID, "story_id": storyID, "plan_id": item.ID, "duration": duration})
	// #endregion
	firstPlanStart := item.LastNotifiedDate != today
	if err := s.startPlanActivity(ctx, bindingID, item.ID, storyID, activity, option, duration); err != nil {
		// #region debug-point AC-07:start-state-save
		sendDebugEvent("AC-07", "autocontrol.runBinding", "started activity state save failed", map[string]any{"binding_id": bindingID, "error": err.Error()})
		// #endregion
		s.record(bindingID, "start", activity, option, false, "活动已启动但运行状态保存失败", err.Error())
		return
	}
	// #region debug-point AC-07:start-state-save
	sendDebugEvent("AC-07", "autocontrol.runBinding", "started activity state saved", map[string]any{"binding_id": bindingID, "story_id": storyID})
	// #endregion
	// #region debug-point AC-08:start-record
	sendDebugEvent("AC-08", "autocontrol.runBinding", "recording activity start", map[string]any{"binding_id": bindingID, "story_id": storyID})
	// #endregion
	s.record(bindingID, "start", activity, option, true, "活动开始成功", string(result))
	if firstPlanStart {
		if err := s.notifyActivityStarted(qq, activity, option, planExpectation(item)); err != nil {
			s.log.Error("计划任务开始推送失败", "qq", qq, "error", err)
		} else if err := markPlanNotified(ctx, s.db, bindingID, config.Plan, item.ID, today); err != nil {
			s.log.Error("计划任务推送状态保存失败", "qq", qq, "error", err)
		}
	}
	// #region debug-point AC-08:start-record
	sendDebugEvent("AC-08", "autocontrol.runBinding", "activity start recorded", map[string]any{"binding_id": bindingID, "story_id": storyID})
	// #endregion
	if activity != "adventure" && config.EncourageProbability > 0 && rand.Int63n(100) < config.EncourageProbability {
		_, err = s.client.EncouragePetActivity(ctx, selfID, petID, storyID)
		s.record(bindingID, "encourage", activity, option, err == nil, resultMessage(err, "活动开始后已发送鼓励"), "")
	}
}

func resetDailyConfig(ctx context.Context, db *sql.DB, bindingID int64, today string) error {
	_, err := db.ExecContext(ctx, `UPDATE pet_auto_control_configs SET school_remaining = school_count, work_remaining = work_count, adventure_remaining = adventure_count, school_executed = 0, work_executed = 0, adventure_executed = 0, school_duration_executed = 0, work_duration_executed = 0, adventure_duration_executed = 0, auto_poke_daily_count = 0, auto_poke_last_run_at = NULL, auto_poke_last_friend_qq = '', last_reset_date = ?, updated_at = ? WHERE qq_binding_id = ?`, today, timeutil.Now(), bindingID)
	return err
}

func autoPokeStarted(startTime string) bool {
	if startTime == "" {
		startTime = "00:00"
	}
	planned, err := time.ParseInLocation("15:04", startTime, timeutil.Beijing)
	if err != nil {
		return false
	}
	now := timeutil.Current()
	return !now.Before(time.Date(now.Year(), now.Month(), now.Day(), planned.Hour(), planned.Minute(), 0, 0, timeutil.Beijing))
}

func (s *Scheduler) runAutoPokeTick(parent context.Context, bindingID int64, qq string) {
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	config, err := LoadConfig(ctx, s.db, bindingID)
	if err != nil || !config.AutoPokeEnabled || !autoPokeStarted(config.AutoPokeStartTime) {
		return
	}
	// #region debug-point AC-09:auto-poke-block
	sendDebugEvent("AC-09", "autocontrol.StartBinding", "auto poke tick entered", map[string]any{"binding_id": bindingID, "enabled": config.AutoPokeEnabled, "daily_count": config.AutoPokeDailyCount, "daily_limit": config.AutoPokeDailyLimit})
	// #endregion
	s.runAutoPoke(ctx, bindingID, qq, config)
	// #region debug-point AC-09:auto-poke-block
	sendDebugEvent("AC-09", "autocontrol.StartBinding", "auto poke tick returned", map[string]any{"binding_id": bindingID})
	// #endregion
}

func (s *Scheduler) runAutoPoke(ctx context.Context, bindingID int64, qq string, config Config) bool {
	if !config.AutoPokeEnabled {
		return false
	}
	if config.AutoPokeDailyLimit > 0 && config.AutoPokeDailyLimit <= config.AutoPokeDailyCount {
		return false
	}
	const interval = 15 * time.Second
	if config.AutoPokeLastRunAt != "" {
		last, err := time.Parse(time.RFC3339, config.AutoPokeLastRunAt)
		if err == nil && timeutil.Current().Before(last.Add(interval)) {
			return false
		}
	}
	var friendQQ string
	today := timeutil.Current()
	todayStart := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, timeutil.Beijing).Format(time.RFC3339)
	err := s.db.QueryRowContext(ctx, `SELECT f.friend_qq FROM qq_friends f WHERE f.qq_binding_id = ? AND f.has_pet = 1 AND f.friend_qq != ? AND NOT EXISTS (SELECT 1 FROM pet_auto_control_logs l WHERE l.qq_binding_id = f.qq_binding_id AND l.action = 'poke' AND l.success = 1 AND l.option_name = f.friend_qq AND l.created_at >= ?) ORDER BY f.updated_at ASC LIMIT 1`, bindingID, qq, todayStart).Scan(&friendQQ)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		s.record(bindingID, "poke", "", "", false, "自动踩一踩候选好友读取失败", err.Error())
		return true
	}
	result, err := s.client.PokeFriendPet(ctx, mustInt64(qq), friendQQ)
	if err != nil {
		s.record(bindingID, "poke", "", friendQQ, false, "自动踩一踩失败", err.Error())
		return true
	}
	now := timeutil.Now()
	_, err = s.db.ExecContext(ctx, "UPDATE pet_auto_control_configs SET auto_poke_daily_count = auto_poke_daily_count + 1, auto_poke_last_run_at = ?, auto_poke_last_friend_qq = ?, updated_at = ? WHERE qq_binding_id = ?", now, friendQQ, now, bindingID)
	s.record(bindingID, "poke", "", friendQQ, err == nil, "自动踩一踩成功", string(result))
	return true
}

func nextPlanItem(config Config) (PlanItem, bool) {
	if len(config.Plan) == 0 {
		activity, ok := nextActivity(config)
		if !ok {
			return PlanItem{}, false
		}
		return PlanItem{ID: activity, Activity: activity, ControlMode: "duration", Hours: 1, LearningTarget: config.LearningTarget, WorkOption: config.WorkOption, ExecutedSeconds: durationUsed(config, activity)}, true
	}
	for _, item := range config.Plan {
		if !validActivities[item.Activity] || item.Completed {
			continue
		}
		mode := item.ControlMode
		if mode == "" {
			mode = "duration"
		}
		if mode == "count" && item.Count > item.ExecutedCount {
			return item, true
		}
		if mode == "duration" && item.Hours > 0 && item.ExecutedSeconds < item.Hours*3600 {
			return item, true
		}
	}
	return PlanItem{}, false
}

func durationUsed(config Config, activity string) int64 {
	return map[string]int64{"school": config.SchoolDurationUsed, "work": config.WorkDurationUsed, "adventure": config.AdventureDurationUsed}[activity]
}

func (s *Scheduler) startPlanActivity(ctx context.Context, bindingID int64, itemID, storyID, activity, option string, duration int64) error {
	if !validActivities[activity] {
		return errors.New("活动类型无效")
	}
	if duration < 0 {
		duration = 0
	}
	now := timeutil.Now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO pet_auto_control_states (qq_binding_id, running, current_activity, current_option, current_story_id, current_plan_item_id, current_duration_seconds, message, last_action, last_error, last_run_at, updated_at) VALUES (?, 1, ?, ?, ?, ?, ?, '活动开始成功', 'start', '', ?, ?) ON CONFLICT(qq_binding_id) DO UPDATE SET running = 1, current_activity = excluded.current_activity, current_option = excluded.current_option, current_story_id = excluded.current_story_id, current_plan_item_id = excluded.current_plan_item_id, current_duration_seconds = excluded.current_duration_seconds, message = excluded.message, last_action = excluded.last_action, last_error = '', last_run_at = excluded.last_run_at, updated_at = excluded.updated_at`, bindingID, activity, option, storyID, itemID, duration, now, now)
	return err
}

func savePlanProgress(ctx context.Context, db *sql.DB, bindingID int64, plan []PlanItem) error {
	encoded, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, "UPDATE pet_auto_control_configs SET plan_json = ?, updated_at = ? WHERE qq_binding_id = ?", string(encoded), timeutil.Now(), bindingID)
	return err
}

func markPlanNotified(ctx context.Context, db *sql.DB, bindingID int64, plan []PlanItem, itemID, date string) error {
	for index := range plan {
		if plan[index].ID == itemID {
			plan[index].LastNotifiedDate = date
			return savePlanProgress(ctx, db, bindingID, plan)
		}
	}
	return errors.New("每日计划条目不存在")
}

func (s *Scheduler) startActivity(ctx context.Context, bindingID int64, activity string, duration int64) error {
	if !validActivities[activity] {
		return errors.New("活动类型无效")
	}
	_, err := s.db.ExecContext(ctx, "UPDATE pet_auto_control_configs SET "+activity+"_remaining = MAX("+activity+"_remaining - 1, 0), "+activity+"_duration_executed = "+activity+"_duration_executed + ?, updated_at = ? WHERE qq_binding_id = ?", duration, timeutil.Now(), bindingID)
	return err
}

func (s *Scheduler) completeActivity(ctx context.Context, bindingID int64, qq, itemID, activity, option string, duration int64) error {
	if !validActivities[activity] {
		return errors.New("活动类型无效")
	}
	config, err := LoadConfig(ctx, s.db, bindingID)
	if err != nil {
		return err
	}
	var completedPlan *PlanItem
	for index := range config.Plan {
		if config.Plan[index].ID == itemID {
			completedPlan = &config.Plan[index]
			break
		}
	}
	wasCompleted := completedPlan != nil && completedPlan.Completed
	if completePlanItem(config.Plan, itemID, duration) {
		if err := savePlanProgress(ctx, s.db, bindingID, config.Plan); err != nil {
			return err
		}
	}
	planJustCompleted := completedPlan != nil && !wasCompleted && completedPlan.Completed
	_, err = s.db.ExecContext(ctx, "UPDATE pet_auto_control_configs SET "+activity+"_remaining = MAX("+activity+"_remaining - 1, 0), "+activity+"_executed = "+activity+"_executed + 1, "+activity+"_duration_executed = "+activity+"_duration_executed + ?, updated_at = ? WHERE qq_binding_id = ?", duration, timeutil.Now(), bindingID)
	if err != nil {
		return err
	}
	s.record(bindingID, "complete", activity, option, true, "活动已完成", "")
	if planJustCompleted {
		updated, loadErr := LoadConfig(ctx, s.db, bindingID)
		if loadErr == nil {
			s.notifyActivityCompleted(qq, activity, option, planExpectation(*completedPlan), todaySummary(updated))
		}
	}
	s.clearRunningActivity(bindingID, "活动已完成", "complete")
	return nil
}

func completePlanItem(plan []PlanItem, itemID string, duration int64) bool {
	for index := range plan {
		if plan[index].ID != itemID {
			continue
		}
		item := &plan[index]
		item.ExecutedCount++
		item.ExecutedSeconds += max(duration, 0)
		if item.ControlMode == "count" {
			item.Completed = item.ExecutedCount >= item.Count
		} else {
			item.Completed = item.ExecutedSeconds >= item.Hours*3600
		}
		return true
	}
	return false
}

func activityFromStateCode(code int64) string {
	switch code {
	case 2:
		return "school"
	case 51:
		return "work"
	case 101:
		return "adventure"
	default:
		return ""
	}
}

func (s *Scheduler) checkBindingReady(ctx context.Context, bindingID int64, qq string) (string, error) {
	var expiresAt string
	var updatedAt string
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(service_expires_at, ''), updated_at FROM qq_bindings WHERE id = ?", bindingID).Scan(&expiresAt, &updatedAt); err != nil {
		return "QQ 服务期限检查失败", fmt.Errorf("%w: %v", errBindingServiceCheck, err)
	}
	if strings.TrimSpace(expiresAt) != "" {
		expires, err := time.Parse(time.RFC3339, expiresAt)
		if err != nil {
			return "QQ 服务期限格式无效", err
		}
		if !expires.After(timeutil.Current()) {
			return "QQ 服务已到期，自动控制已关闭", errors.New("QQ 服务已到期")
		}
	}
	botsData, err := s.client.ListBots(ctx)
	if err != nil {
		return "QQ 服务在线检查失败，自动控制已关闭", err
	}
	var raw any
	if err := json.Unmarshal(botsData, &raw); err != nil {
		return "QQ 在线状态格式无效，自动控制已关闭", err
	}
	if object, ok := raw.(map[string]any); ok {
		if value, exists := object["data"]; exists {
			raw = value
		} else if value, exists := object["bots"]; exists {
			raw = value
		}
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return "QQ 在线状态格式无效，自动控制已关闭", err
	}
	var bots []struct {
		SelfID int64 `json:"self_id"`
		Status int   `json:"status"`
	}
	if err := json.Unmarshal(encoded, &bots); err != nil {
		return "QQ 在线状态格式无效，自动控制已关闭", err
	}
	selfID := mustInt64(qq)
	for _, bot := range bots {
		if bot.SelfID == selfID && bot.Status == 1 {
			return "", nil
		}
	}
	if withinLoginSyncGrace(updatedAt, timeutil.Current()) {
		return "QQ 登录成功，等待在线状态同步", errBindingLoginSync
	}
	return "QQ 当前不在线，自动控制已关闭", errBindingOffline
}

func withinLoginSyncGrace(updatedAt string, now time.Time) bool {
	updated, err := time.Parse(time.RFC3339, strings.TrimSpace(updatedAt))
	if err != nil {
		return false
	}
	elapsed := now.Sub(updated)
	return elapsed >= 0 && elapsed <= loginSyncGracePeriod
}

type activityStatus struct {
	StoryID          string
	StateCode        int64
	RemainingSeconds int64
	DurationSeconds  int64
}

func parseActivityStatus(data []byte) (activityStatus, bool) {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return activityStatus{}, false
	}
	status := activityStatus{StoryID: findString(value, "story_id")}
	if number, ok := findNumber(value, "state_code"); ok {
		status.StateCode = int64(number)
	}
	if number, ok := findNumber(value, "remaining_seconds"); ok {
		status.RemainingSeconds = int64(number)
	}
	if number, ok := findNumber(value, "duration_seconds"); ok {
		status.DurationSeconds = int64(number)
	}
	return status, true
}

func trackedActivityEnded(currentStoryID, reportedStoryID string, inProgress bool) bool {
	currentStoryID = strings.TrimSpace(currentStoryID)
	reportedStoryID = strings.TrimSpace(reportedStoryID)
	return currentStoryID != "" && (reportedStoryID != currentStoryID || !inProgress)
}

func completedActivityDuration(cachedDuration, statusDuration int64) int64 {
	if cachedDuration > 0 {
		return cachedDuration
	}
	return statusDuration
}

func activityInProgress(data []byte, status struct {
	StoryID          string `json:"story_id"`
	RemainingSeconds int64  `json:"remaining_seconds"`
}) bool {
	if status.RemainingSeconds > 0 && status.StoryID != "" {
		return true
	}
	var value any
	if json.Unmarshal(data, &value) != nil {
		return false
	}
	return activityValueInProgress(value)
}

func activityValueInProgress(value any) bool {
	switch item := value.(type) {
	case map[string]any:
		storyID, hasStory := item["story_id"].(string)
		remaining, hasRemaining := numberFrom(item["remaining_seconds"])
		if !hasRemaining {
			remaining, hasRemaining = numberFrom(item["remainingSeconds"])
		}
		if hasRemaining {
			return remaining > 0 && strings.TrimSpace(storyID) != ""
		}
		if hasStory && strings.TrimSpace(storyID) != "" {
			if state, ok := numberFrom(item["state_code"]); ok && state > 0 {
				return true
			}
			if state, ok := numberFrom(item["stateCode"]); ok && state > 0 {
				return true
			}
			if active, ok := item["active"].(bool); ok && active {
				return true
			}
		}
		for _, key := range []string{"data", "activity", "status", "result"} {
			if activityValueInProgress(item[key]) {
				return true
			}
		}
	case []any:
		for _, child := range item {
			if activityValueInProgress(child) {
				return true
			}
		}
	}
	return false
}

func (s *Scheduler) runPetCare(ctx context.Context, bindingID int64, qq string, config Config) bool {
	if !config.AutoFeedEnabled && !config.AutoBatheEnabled {
		return false
	}
	selfID := mustInt64(qq)
	petID := s.petID(ctx, selfID)
	if petID == "" {
		s.record(bindingID, "vitals", "", "", false, "宠物资料读取失败", "")
		return true
	}
	vitalsData, err := s.client.GetPetVitals(ctx, selfID, petID)
	if err != nil {
		s.record(bindingID, "vitals", "", "", false, err.Error(), "")
		return true
	}
	vitals := decodeObject(vitalsData)
	if config.AutoFeedEnabled {
		if hunger, ok := findNumber(vitals, "hunger"); ok && hunger <= float64(config.AutoFeedThreshold) {
			return s.feedIfNeeded(ctx, bindingID, qq, config.AutoFeedFood, hunger)
		}
	}
	if config.AutoBatheEnabled {
		if cleanliness, ok := findNumber(vitals, "cleanliness"); ok && cleanliness <= float64(config.AutoBatheThreshold) {
			return s.batheIfNeeded(ctx, bindingID, qq, config.AutoBatheItem, cleanliness)
		}
	}
	return false
}

func (s *Scheduler) feedIfNeeded(ctx context.Context, bindingID int64, qq, food string, hunger float64) bool {
	selfID := mustInt64(qq)
	petID := s.petID(ctx, selfID)
	if petID == "" {
		s.record(bindingID, "feed", "", food, false, "宠物资料读取失败", "")
		return true
	}
	inventory, err := s.client.GetPetFoodCatalog(ctx, selfID)
	if err != nil {
		s.record(bindingID, "feed_inventory", "", food, false, err.Error(), "")
		return true
	}
	stock, found := findItemStock(inventory, food)
	foodID := findItemID(inventory, food, "food_id")
	if !found || foodID == "" {
		s.record(bindingID, "feed_inventory", "", food, false, "食物目录中未找到目标物品、数量或 food_id", string(inventory))
		return true
	}
	if stock < 5 {
		if _, err = s.client.BuyPetFood(ctx, selfID, 10); err != nil {
			s.record(bindingID, "buy_food", "", food, false, err.Error(), "")
			return true
		}
		s.record(bindingID, "buy_food", "", food, true, "库存低于 5，已自动购买 10 个", "")
	}
	_, err = s.client.FeedPet(ctx, selfID, petID, foodID)
	s.record(bindingID, "feed", "", food, err == nil, resultMessage(err, fmt.Sprintf("饥饿值 %.0f，已自动喂食", hunger)), "")
	return true
}

func (s *Scheduler) batheIfNeeded(ctx context.Context, bindingID int64, qq, item string, cleanliness float64) bool {
	selfID := mustInt64(qq)
	petID := s.petID(ctx, selfID)
	if petID == "" {
		s.record(bindingID, "bathe", "", item, false, "宠物资料读取失败", "")
		return true
	}
	inventory, err := s.client.GetPetBathInventory(ctx, selfID)
	if err != nil {
		s.record(bindingID, "bath_inventory", "", item, false, err.Error(), "")
		return true
	}
	catalog, err := s.client.GetPetBathCatalog(ctx, selfID)
	if err != nil {
		s.record(bindingID, "bath_catalog", "", item, false, err.Error(), "")
		return true
	}
	itemID := findItemID(catalog, item, "item_id")
	if itemID == "" {
		s.record(bindingID, "bath_catalog", "", item, false, "洗护目录中未找到目标物品或 item_id", string(catalog))
		return true
	}
	stock, found := findItemStockByID(inventory, itemID, "item_id")
	if !found {
		s.record(bindingID, "bath_inventory", "", item, false, "洗护库存中未找到目标 item_id 或数量", string(inventory))
		return true
	}
	if stock < 5 {
		if _, err = s.client.BuyPetBathItem(ctx, selfID, petID, itemID, 10); err != nil {
			s.record(bindingID, "buy_bath", "", item, false, err.Error(), "")
			return true
		}
		s.record(bindingID, "buy_bath", "", item, true, "库存低于 5，已自动购买 10 个", "")
	}
	_, err = s.client.BathePet(ctx, selfID, petID, itemID, 1)
	s.record(bindingID, "bathe", "", item, err == nil, resultMessage(err, fmt.Sprintf("清洁值 %.0f，已自动洗护", cleanliness)), "")
	return true
}

func (s *Scheduler) petID(ctx context.Context, selfID int64) string {
	data, err := s.client.GetPetProfile(ctx, selfID)
	if err != nil {
		return ""
	}
	value := decodeObject(data)
	for _, key := range []string{"pet_id", "id"} {
		if text, ok := value[key].(string); ok && text != "" {
			return text
		}
	}
	return ""
}

func decodeObject(data []byte) map[string]any {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return map[string]any{}
	}
	if object, ok := value.(map[string]any); ok {
		return object
	}
	return map[string]any{}
}

func findStoryID(data []byte) string {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return ""
	}
	return findString(value, "story_id")
}

func findActivityDuration(result, options []byte, optionName string) int64 {
	var value any
	if json.Unmarshal(result, &value) == nil {
		if duration, ok := findNumber(value, "duration_seconds"); ok && duration > 0 {
			return int64(duration)
		}
	}
	var payload struct {
		Options []struct {
			Name     string `json:"name"`
			Duration int64  `json:"duration_seconds"`
		} `json:"options"`
	}
	if json.Unmarshal(options, &payload) == nil {
		for _, option := range payload.Options {
			if option.Name == optionName && option.Duration > 0 {
				return option.Duration
			}
		}
	}
	return 0
}

func findString(value any, key string) string {
	switch item := value.(type) {
	case map[string]any:
		if text, ok := item[key].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
		for _, child := range item {
			if text := findString(child, key); text != "" {
				return text
			}
		}
	case []any:
		for _, child := range item {
			if text := findString(child, key); text != "" {
				return text
			}
		}
	}
	return ""
}

func findNumber(value any, key string) (float64, bool) {
	if object, ok := value.(map[string]any); ok {
		if number, ok := numberFrom(object[key]); ok {
			return number, true
		}
		for _, child := range object {
			if number, ok := findNumber(child, key); ok {
				return number, true
			}
		}
	}
	return 0, false
}

func findItemStock(data []byte, name string) (int64, bool) {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return 0, false
	}
	return findItemStockValue(value, strings.TrimSpace(name))
}

func findItemStockByID(data []byte, itemID, idKey string) (int64, bool) {
	if strings.TrimSpace(itemID) == "" {
		return 0, false
	}
	var value any
	if json.Unmarshal(data, &value) != nil {
		return 0, false
	}
	return findItemStockByIDValue(value, strings.TrimSpace(itemID), idKey)
}

func findItemID(data []byte, name, idKey string) string {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return ""
	}
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
	return walk(value)
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
				if number, ok := numberFrom(object[key]); ok {
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

func findItemStockByIDValue(value any, itemID, idKey string) (int64, bool) {
	if object, ok := value.(map[string]any); ok {
		matched := false
		switch id := object[idKey].(type) {
		case string:
			matched = strings.TrimSpace(id) == itemID
		case float64:
			matched = strconv.FormatInt(int64(id), 10) == itemID
		}
		if matched {
			for _, key := range []string{"stock", "count", "quantity", "balance", "amount", "num"} {
				if number, ok := numberFrom(object[key]); ok {
					return int64(number), true
				}
			}
		}
		for _, child := range object {
			if stock, found := findItemStockByIDValue(child, itemID, idKey); found {
				return stock, true
			}
		}
	}
	if items, ok := value.([]any); ok {
		for _, child := range items {
			if stock, found := findItemStockByIDValue(child, itemID, idKey); found {
				return stock, true
			}
		}
	}
	return 0, false
}

func numberFrom(value any) (float64, bool) {
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

func (s *Scheduler) disableBinding(bindingID int64, message string) {
	now := timeutil.Now()
	if _, err := s.db.Exec("UPDATE pet_auto_control_configs SET enabled = 0, updated_at = ? WHERE qq_binding_id = ?", now, bindingID); err != nil {
		s.log.Error("自动控制关闭失败", "binding_id", bindingID, "error", err)
	}
	s.StopBinding(bindingID)
	s.record(bindingID, "precheck", "", "", false, message, "")
	s.updateState(bindingID, false, "", "", message, "precheck", message)
	s.log.Warn("自动控制任务前置检查失败，已持久化关闭", "binding_id", bindingID, "message", message)
}

func (s *Scheduler) notifyActivityStarted(qq, activity, option, expectation string) error {
	if s.notifier == nil {
		return nil
	}
	return s.notifier.NotifyActivityStarted(context.Background(), qq, activityName(activity), option, expectation)
}

func planExpectation(item PlanItem) string {
	if item.ControlMode == "count" {
		return fmt.Sprintf("本次预计执行%d次", item.Count)
	}
	return fmt.Sprintf("本次预计执行时间：%d小时", item.Hours)
}

func (s *Scheduler) notifyActivityCompleted(qq, activity, option, expectation, summary string) {
	if s.notifier == nil {
		return
	}
	go func() {
		if err := s.notifier.NotifyActivityCompleted(context.Background(), qq, activityName(activity), option, expectation, summary); err != nil {
			s.log.Error("计划任务完成推送失败", "qq", qq, "error", err)
		}
	}()
}

func (s *Scheduler) notifyOffline(qq, reason string) {
	if s.notifier == nil {
		return
	}
	go func() {
		if err := s.notifier.NotifyOffline(context.Background(), qq, reason); err != nil {
			s.log.Error("自动控制掉线推送失败", "qq", qq, "error", err)
		}
	}()
}

func activityName(activity string) string {
	return map[string]string{"school": "学习", "work": "打工", "adventure": "冒险"}[activity]
}

func todaySummary(config Config) string {
	return fmt.Sprintf("学习%d次，打工%d次，冒险%d次", config.SchoolExecuted, config.WorkExecuted, config.AdventureExecuted)
}

func (s *Scheduler) record(bindingID int64, action, activity, option string, success bool, message, details string) {
	now := timeutil.Now()
	_, _ = s.db.Exec("INSERT INTO pet_auto_control_logs (qq_binding_id, action, activity, option_name, success, message, details, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", bindingID, action, activity, option, boolInt(success), message, details, now)
	switch action {
	case "start", "encourage":
		if success {
			s.updateState(bindingID, true, activity, option, message, action, details)
		} else {
			s.updateState(bindingID, false, "", "", message, action, details)
		}
	case "complete":
		s.updateState(bindingID, false, "", "", message, action, details)
	case "precheck":
		s.updateState(bindingID, false, "", "", message, action, details)
	default:
		if !success {
			_, _ = s.db.Exec("UPDATE pet_auto_control_states SET last_action = ?, last_error = ?, last_run_at = ?, updated_at = ? WHERE qq_binding_id = ?", action, detailsOrMessage(details, message), now, now, bindingID)
		}
	}
}

func detailsOrMessage(details, message string) string {
	if strings.TrimSpace(details) != "" {
		return details
	}
	return message
}

func (s *Scheduler) updateState(bindingID int64, running bool, activity, option, message, action, lastError string) {
	if !running {
		activity = ""
		option = ""
	}
	_, _ = s.db.Exec("INSERT INTO pet_auto_control_states (qq_binding_id, running, current_activity, current_option, current_story_id, current_plan_item_id, current_duration_seconds, message, last_action, last_error, last_run_at, updated_at) VALUES (?, ?, ?, ?, '', '', 0, ?, ?, ?, ?, ?) ON CONFLICT(qq_binding_id) DO UPDATE SET running = excluded.running, current_activity = excluded.current_activity, current_option = excluded.current_option, current_story_id = CASE WHEN excluded.running = 1 THEN pet_auto_control_states.current_story_id ELSE '' END, current_plan_item_id = CASE WHEN excluded.running = 1 THEN pet_auto_control_states.current_plan_item_id ELSE '' END, current_duration_seconds = CASE WHEN excluded.running = 1 THEN pet_auto_control_states.current_duration_seconds ELSE 0 END, message = excluded.message, last_action = excluded.last_action, last_error = excluded.last_error, last_run_at = excluded.last_run_at, updated_at = excluded.updated_at", bindingID, boolInt(running), activity, option, message, action, lastError, timeutil.Now(), timeutil.Now())
}

func (s *Scheduler) clearRunningActivity(bindingID int64, message, action string) {
	now := timeutil.Now()
	_, _ = s.db.Exec("UPDATE pet_auto_control_states SET running = 0, current_activity = '', current_option = '', current_story_id = '', current_plan_item_id = '', current_duration_seconds = 0, message = ?, last_action = ?, last_error = '', last_run_at = ?, updated_at = ? WHERE qq_binding_id = ?", message, action, now, now, bindingID)
}

func nextActivity(config Config) (string, bool) {
	modes := map[string]string{"school": config.SchoolControlMode, "work": config.WorkControlMode, "adventure": config.AdventureControlMode}
	remaining := map[string]int64{"school": config.SchoolRemaining, "work": config.WorkRemaining, "adventure": config.AdventureRemain}
	durations := map[string]int64{"school": config.SchoolDuration, "work": config.WorkDuration, "adventure": config.AdventureDuration}
	used := map[string]int64{"school": config.SchoolDurationUsed, "work": config.WorkDurationUsed, "adventure": config.AdventureDurationUsed}
	for _, activity := range config.ActivityOrder {
		if !validActivities[activity] {
			continue
		}
		if modes[activity] == "duration" {
			if durations[activity] > used[activity] {
				return activity, true
			}
		} else if remaining[activity] > 0 {
			return activity, true
		}
	}
	return "", false
}

func pickOption(data []byte, activity, target, workOption string) string {
	option, _ := pickOptionDetails(data, activity, target, workOption)
	return option
}

func savedWorkOption(item PlanItem) (string, int64, bool) {
	if item.Activity != "work" || strings.TrimSpace(item.WorkOption) == "" || item.WorkSubEventType == nil {
		return "", 0, false
	}
	return strings.TrimSpace(item.WorkOption), *item.WorkSubEventType, true
}

func pickOptionDetails(data []byte, activity, target, workOption string) (string, int64) {
	var payload struct {
		Options []struct {
			Name         string `json:"name"`
			OptionName   string `json:"option_name"`
			CanDo        bool   `json:"can_do"`
			Reward       string `json:"reward"`
			SubEventType int64  `json:"sub_event_type"`
		} `json:"options"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return "", 0
	}
	for _, option := range payload.Options {
		name := strings.TrimSpace(option.OptionName)
		if name == "" {
			name = strings.TrimSpace(option.Name)
		}
		if !option.CanDo || name == "" {
			continue
		}
		if activity == "work" && strings.TrimSpace(workOption) != "" && name != strings.TrimSpace(workOption) {
			continue
		}
		if activity != "school" || target == "随机" || strings.Contains(option.Reward, target) {
			return name, option.SubEventType
		}
	}
	return "", 0
}

func LoadConfig(ctx context.Context, db *sql.DB, bindingID int64) (Config, error) {
	var config Config
	var enabled int
	var orderJSON string
	var planJSON string
	var autoPokeEnabled int
	err := db.QueryRowContext(ctx, `SELECT enabled, learning_target, activity_order, plan_json, school_count, work_count, adventure_count, school_remaining, work_remaining, adventure_remaining, school_executed, work_executed, adventure_executed, school_control_mode, work_control_mode, adventure_control_mode, school_duration_seconds, work_duration_seconds, adventure_duration_seconds, school_duration_executed, work_duration_executed, adventure_duration_executed, work_option, auto_feed_enabled, auto_feed_threshold, auto_feed_food, auto_bathe_enabled, auto_bathe_threshold, auto_bathe_item, encourage_probability, auto_poke_enabled, auto_poke_start_time, auto_poke_daily_limit, auto_poke_daily_count, auto_poke_interval_seconds, COALESCE(auto_poke_last_run_at, ''), auto_poke_last_friend_qq, last_reset_date FROM pet_auto_control_configs WHERE qq_binding_id = ?`, bindingID).Scan(&enabled, &config.LearningTarget, &orderJSON, &planJSON, &config.SchoolCount, &config.WorkCount, &config.AdventureCount, &config.SchoolRemaining, &config.WorkRemaining, &config.AdventureRemain, &config.SchoolExecuted, &config.WorkExecuted, &config.AdventureExecuted, &config.SchoolControlMode, &config.WorkControlMode, &config.AdventureControlMode, &config.SchoolDuration, &config.WorkDuration, &config.AdventureDuration, &config.SchoolDurationUsed, &config.WorkDurationUsed, &config.AdventureDurationUsed, &config.WorkOption, &config.AutoFeedEnabled, &config.AutoFeedThreshold, &config.AutoFeedFood, &config.AutoBatheEnabled, &config.AutoBatheThreshold, &config.AutoBatheItem, &config.EncourageProbability, &autoPokeEnabled, &config.AutoPokeStartTime, &config.AutoPokeDailyLimit, &config.AutoPokeDailyCount, &config.AutoPokeInterval, &config.AutoPokeLastRunAt, &config.AutoPokeLastFriendQQ, &config.LastResetDate)
	if errors.Is(err, sql.ErrNoRows) {
		return Config{LearningTarget: "力量", ActivityOrder: []string{"school", "work", "adventure"}}, nil
	}
	if err != nil {
		return Config{}, err
	}
	config.Enabled = enabled == 1
	config.AutoPokeEnabled = autoPokeEnabled == 1
	// 自动踩一踩统一在北京时间 00:00 开始，不使用历史配置中的自定义时间。
	config.AutoPokeStartTime = "00:00"
	if err := json.Unmarshal([]byte(orderJSON), &config.ActivityOrder); err != nil {
		config.ActivityOrder = []string{"school", "work", "adventure"}
	}
	if strings.TrimSpace(planJSON) != "" && json.Unmarshal([]byte(planJSON), &config.Plan) != nil {
		return Config{}, errors.New("每日计划格式无效")
	}
	if len(config.Plan) == 0 {
		config.Plan = legacyPlan(config)
	} else {
		for index := range config.Plan {
			item := &config.Plan[index]
			if item.ControlMode == "" {
				item.ControlMode = "duration"
			}
			if item.ControlMode == "count" {
				item.Completed = item.Count > 0 && item.ExecutedCount >= item.Count
			} else {
				item.Completed = item.Hours > 0 && item.ExecutedSeconds >= item.Hours*3600
			}
		}
	}
	return config, nil
}

func legacyPlan(config Config) []PlanItem {
	items := make([]PlanItem, 0)
	for _, activity := range config.ActivityOrder {
		mode := map[string]string{"school": config.SchoolControlMode, "work": config.WorkControlMode, "adventure": config.AdventureControlMode}[activity]
		hours := map[string]int64{"school": config.SchoolDuration, "work": config.WorkDuration, "adventure": config.AdventureDuration}[activity] / 3600
		count := map[string]int64{"school": config.SchoolCount, "work": config.WorkCount, "adventure": config.AdventureCount}[activity]
		if mode == "duration" && hours > 0 {
			items = append(items, PlanItem{ID: fmt.Sprintf("legacy-%d", len(items)), Activity: activity, ControlMode: "duration", Hours: hours, LearningTarget: config.LearningTarget, WorkOption: config.WorkOption, ExecutedSeconds: durationUsed(config, activity)})
		} else if count > 0 {
			executed := map[string]int64{"school": config.SchoolExecuted, "work": config.WorkExecuted, "adventure": config.AdventureExecuted}[activity]
			items = append(items, PlanItem{ID: fmt.Sprintf("legacy-%d", len(items)), Activity: activity, ControlMode: "count", Count: count, LearningTarget: config.LearningTarget, WorkOption: config.WorkOption, ExecutedCount: executed, Completed: executed >= count})
		}
	}
	return items
}

func planItemDefinitionEqual(left, right PlanItem) bool {
	return left.Activity == right.Activity &&
		left.ControlMode == right.ControlMode &&
		left.Count == right.Count &&
		left.Hours == right.Hours &&
		left.LearningTarget == right.LearningTarget &&
		int64PointersEqual(left.WorkCareerType, right.WorkCareerType) &&
		left.WorkOption == right.WorkOption &&
		int64PointersEqual(left.WorkSubEventType, right.WorkSubEventType)
}

func int64PointersEqual(left, right *int64) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func validatePlanIDs(plan []PlanItem) error {
	seen := map[string]bool{}
	for _, item := range plan {
		if item.ID == "" || seen[item.ID] {
			return errors.New("每日计划条目标识重复或为空")
		}
		seen[item.ID] = true
	}
	return nil
}

func SaveConfig(ctx context.Context, db *sql.DB, bindingID int64, config Config) error {
	if config.LearningTarget == "" {
		config.LearningTarget = "力量"
	}
	if len(config.Plan) == 0 {
		return errors.New("每日计划不能为空")
	}
	existing, err := LoadConfig(ctx, db, bindingID)
	if err != nil {
		return err
	}
	existingItems := make(map[string]PlanItem, len(existing.Plan))
	for _, item := range existing.Plan {
		existingItems[item.ID] = item
	}
	for index := range config.Plan {
		item := &config.Plan[index]
		if item.ID == "" {
			item.ID = fmt.Sprintf("plan-%d", index+1)
		}
		if !validActivities[item.Activity] {
			return errors.New("每日计划条目不合法")
		}
		if item.ControlMode == "" {
			item.ControlMode = "duration"
		}
		if item.ControlMode != "count" && item.ControlMode != "duration" {
			return errors.New("每日计划控制方式不合法")
		}
		if item.ControlMode == "count" {
			if item.Count < 1 {
				return errors.New("每日计划次数须大于 0")
			}
			item.Hours = 0
		} else {
			if item.Hours < 1 || item.Hours > 24 {
				return errors.New("每日计划小时数须为 1 到 24")
			}
			item.Count = 0
		}
		if previous, ok := existingItems[item.ID]; ok && planItemDefinitionEqual(previous, *item) {
			item.ExecutedCount = previous.ExecutedCount
			item.ExecutedSeconds = previous.ExecutedSeconds
			item.Completed = previous.Completed
			item.LastNotifiedDate = previous.LastNotifiedDate
		} else {
			item.ExecutedCount = 0
			item.ExecutedSeconds = 0
			item.Completed = false
			item.LastNotifiedDate = ""
		}
		if item.Activity == "work" {
			if strings.TrimSpace(item.WorkOption) == "" {
				return errors.New("请选择具体打工项目")
			}
			if item.WorkSubEventType == nil {
				return errors.New("打工项目缺少子事件类型")
			}
		} else {
			item.WorkCareerType = nil
			item.WorkOption = ""
			item.WorkSubEventType = nil
		}
		if item.Activity == "school" && strings.TrimSpace(item.LearningTarget) == "" {
			item.LearningTarget = config.LearningTarget
		}
	}
	if err := validatePlanIDs(config.Plan); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, activity := range config.ActivityOrder {
		if !validActivities[activity] || seen[activity] {
			return errors.New("活动顺序不合法")
		}
		seen[activity] = true
	}
	if config.SchoolControlMode == "" {
		config.SchoolControlMode = "count"
	}
	if config.WorkControlMode == "" {
		config.WorkControlMode = "count"
	}
	if config.AdventureControlMode == "" {
		config.AdventureControlMode = "count"
	}
	for _, mode := range []string{config.SchoolControlMode, config.WorkControlMode, config.AdventureControlMode} {
		if mode != "count" && mode != "duration" {
			return errors.New("活动控制方式不合法")
		}
	}
	if config.SchoolCount < 0 || config.WorkCount < 0 || config.AdventureCount < 0 || config.SchoolDuration < 0 || config.WorkDuration < 0 || config.AdventureDuration < 0 {
		return errors.New("活动目标不合法")
	}
	if config.WorkCount > 0 || config.WorkDuration > 0 {
		if strings.TrimSpace(config.WorkOption) == "" {
			return errors.New("请选择具体打工项目")
		}
	}
	config.AutoFeedFood = "饼干"
	config.AutoBatheItem = "香皂片"
	if config.AutoFeedThreshold < 0 || config.AutoBatheThreshold < 0 || config.EncourageProbability < 0 || config.EncourageProbability > 100 {
		return errors.New("配置参数不合法")
	}
	config.AutoPokeStartTime = "00:00"
	if config.AutoPokeInterval < 1 {
		return errors.New("自动踩一踩间隔须大于 0 秒")
	}
	order, _ := json.Marshal(config.ActivityOrder)
	plan, _ := json.Marshal(config.Plan)
	if config.LastResetDate == "" {
		config.LastResetDate = timeutil.Current().Format("2006-01-02")
	}
	now := timeutil.Now()
	_, err = db.ExecContext(ctx, `INSERT INTO pet_auto_control_configs (
		qq_binding_id, enabled, learning_target, activity_order, plan_json,
		school_count, work_count, adventure_count, school_remaining, work_remaining,
		adventure_remaining, school_executed, work_executed, adventure_executed,
		school_control_mode, work_control_mode, adventure_control_mode,
		school_duration_seconds, work_duration_seconds, adventure_duration_seconds,
		school_duration_executed, work_duration_executed, adventure_duration_executed,
		work_option, auto_feed_enabled, auto_feed_threshold, auto_feed_food,
		auto_bathe_enabled, auto_bathe_threshold, auto_bathe_item,
		encourage_probability, auto_poke_enabled, auto_poke_start_time, auto_poke_daily_limit,
		auto_poke_daily_count, auto_poke_interval_seconds, auto_poke_last_run_at,
		auto_poke_last_friend_qq, last_reset_date, created_at, updated_at
	) VALUES (
		?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		?
	)
	ON CONFLICT(qq_binding_id) DO UPDATE SET
		enabled=excluded.enabled, learning_target=excluded.learning_target,
		activity_order=excluded.activity_order, plan_json=excluded.plan_json,
		school_count=excluded.school_count, work_count=excluded.work_count,
		adventure_count=excluded.adventure_count,
		school_remaining=MAX(excluded.school_count-pet_auto_control_configs.school_executed,0),
		work_remaining=MAX(excluded.work_count-pet_auto_control_configs.work_executed,0),
		adventure_remaining=MAX(excluded.adventure_count-pet_auto_control_configs.adventure_executed,0),
		school_control_mode=excluded.school_control_mode,
		work_control_mode=excluded.work_control_mode,
		adventure_control_mode=excluded.adventure_control_mode,
		school_duration_seconds=excluded.school_duration_seconds,
		work_duration_seconds=excluded.work_duration_seconds,
		adventure_duration_seconds=excluded.adventure_duration_seconds,
		work_option=excluded.work_option, auto_feed_enabled=excluded.auto_feed_enabled,
		auto_feed_threshold=excluded.auto_feed_threshold, auto_feed_food=excluded.auto_feed_food,
		auto_bathe_enabled=excluded.auto_bathe_enabled,
		auto_bathe_threshold=excluded.auto_bathe_threshold, auto_bathe_item=excluded.auto_bathe_item,
		encourage_probability=excluded.encourage_probability,
		auto_poke_enabled=excluded.auto_poke_enabled,
		auto_poke_start_time=excluded.auto_poke_start_time,
		auto_poke_daily_limit=excluded.auto_poke_daily_limit,
		auto_poke_interval_seconds=excluded.auto_poke_interval_seconds,
		last_reset_date=excluded.last_reset_date, updated_at=excluded.updated_at`,
		bindingID, boolInt(config.Enabled), config.LearningTarget, string(order), string(plan),
		config.SchoolCount, config.WorkCount, config.AdventureCount,
		config.SchoolCount, config.WorkCount, config.AdventureCount,
		config.SchoolExecuted, config.WorkExecuted, config.AdventureExecuted,
		config.SchoolControlMode, config.WorkControlMode, config.AdventureControlMode,
		config.SchoolDuration, config.WorkDuration, config.AdventureDuration,
		config.SchoolDurationUsed, config.WorkDurationUsed, config.AdventureDurationUsed,
		strings.TrimSpace(config.WorkOption), boolInt(config.AutoFeedEnabled), config.AutoFeedThreshold,
		config.AutoFeedFood, boolInt(config.AutoBatheEnabled), config.AutoBatheThreshold,
		config.AutoBatheItem, config.EncourageProbability, boolInt(config.AutoPokeEnabled), config.AutoPokeStartTime,
		config.AutoPokeDailyLimit, existing.AutoPokeDailyCount, config.AutoPokeInterval,
		existing.AutoPokeLastRunAt, existing.AutoPokeLastFriendQQ, config.LastResetDate, now, now)
	if err == nil {
		_, _ = db.ExecContext(ctx, "INSERT INTO pet_auto_control_states (qq_binding_id, message, updated_at) VALUES (?, ?, ?) ON CONFLICT(qq_binding_id) DO UPDATE SET message = excluded.message, updated_at = excluded.updated_at", bindingID, map[bool]string{true: "自动控制已开启", false: "自动控制已关闭"}[config.Enabled], now)
	}
	return err
}

func LoadState(ctx context.Context, db *sql.DB, bindingID int64) (State, error) {
	var state State
	var running int
	err := db.QueryRowContext(ctx, "SELECT running, current_activity, current_option, current_story_id, current_plan_item_id, current_duration_seconds, message, last_action, last_error, COALESCE(last_run_at, '') FROM pet_auto_control_states WHERE qq_binding_id = ?", bindingID).Scan(&running, &state.CurrentActivity, &state.CurrentOption, &state.CurrentStoryID, &state.CurrentPlanID, &state.CurrentDuration, &state.Message, &state.LastAction, &state.LastError, &state.LastRunAt)
	if errors.Is(err, sql.ErrNoRows) {
		return State{Message: "未启动自动控制"}, nil
	}
	state.Running = running == 1
	return state, err
}

func LoadLogs(ctx context.Context, db *sql.DB, bindingID int64, limit int) ([]Log, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, action, activity, option_name, success, message, details, created_at FROM pet_auto_control_logs WHERE qq_binding_id = ? ORDER BY id DESC LIMIT ?", bindingID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := make([]Log, 0)
	for rows.Next() {
		var item Log
		var success int
		if err := rows.Scan(&item.ID, &item.Action, &item.Activity, &item.OptionName, &success, &item.Message, &item.Details, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Success = success == 1
		logs = append(logs, item)
	}
	return logs, rows.Err()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
func mustInt64(value string) int64 { var result int64; _, _ = fmt.Sscan(value, &result); return result }
func resultMessage(err error, success string) string {
	if err != nil {
		return err.Error()
	}
	return success
}
