package autocontrol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestWithinLoginSyncGrace(t *testing.T) {
	now := time.Date(2026, 9, 17, 20, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	if !withinLoginSyncGrace(now.Add(-30*time.Second).Format(time.RFC3339), now) {
		t.Fatal("recent successful login should remain in the synchronization grace period")
	}
	if withinLoginSyncGrace(now.Add(-time.Minute).Format(time.RFC3339), now) {
		t.Fatal("stale login timestamp should not remain in the synchronization grace period")
	}
}

func TestSavedWorkOptionPreservesZeroSubEventType(t *testing.T) {
	zero := int64(0)
	item := PlanItem{Activity: "work", WorkOption: "厨师", WorkSubEventType: &zero}

	option, subEventType, saved := savedWorkOption(item)
	if !saved || option != "厨师" || subEventType != 0 {
		t.Fatalf("savedWorkOption() = %q, %d, %t", option, subEventType, saved)
	}

	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"id":"","activity":"work","control_mode":"","count":0,"hours":0,"work_option":"厨师","work_sub_event_type":0,"executed_count":0,"executed_seconds":0,"completed":false}` {
		t.Fatalf("unexpected serialized plan item: %s", encoded)
	}
}

func TestSavedWorkOptionFallsBackForLegacyConfig(t *testing.T) {
	if option, subEventType, saved := savedWorkOption(PlanItem{Activity: "work", WorkOption: "厨师"}); saved || option != "" || subEventType != 0 {
		t.Fatalf("legacy work option should require lookup, got %q, %d, %t", option, subEventType, saved)
	}
}

func TestPlanItemDefinitionIncludesWorkDetails(t *testing.T) {
	zero := int64(0)
	one := int64(1)
	base := PlanItem{Activity: "work", ControlMode: "count", Count: 1, WorkOption: "厨师", WorkSubEventType: &zero}
	changedOption := base
	changedOption.WorkOption = "园丁"
	changedCareer := base
	changedCareer.WorkCareerType = &one
	changedSubEvent := base
	changedSubEvent.WorkSubEventType = &one

	if planItemDefinitionEqual(base, changedOption) || planItemDefinitionEqual(base, changedCareer) || planItemDefinitionEqual(base, changedSubEvent) {
		t.Fatal("changed work details were treated as unchanged")
	}
}

func TestActivityInProgressUsesRemainingSeconds(t *testing.T) {
	completed := []byte(`{"story_id":"6700_f87394a0-e2cf-4bce-80f6-55c9df8cb046","state_code":101,"remaining_seconds":0,"duration_seconds":45}`)
	var status struct {
		StoryID          string `json:"story_id"`
		RemainingSeconds int64  `json:"remaining_seconds"`
	}
	if err := json.Unmarshal(completed, &status); err != nil {
		t.Fatal(err)
	}
	if activityInProgress(completed, status) {
		t.Fatal("completed activity was treated as in progress")
	}

	active := []byte(`{"story_id":"6700_f87394a0-e2cf-4bce-80f6-55c9df8cb046","state_code":101,"remaining_seconds":36}`)
	if err := json.Unmarshal(active, &status); err != nil {
		t.Fatal(err)
	}
	if !activityInProgress(active, status) {
		t.Fatal("active activity was not treated as in progress")
	}
}

func TestParseActivityStatusSupportsNestedResponse(t *testing.T) {
	status, ok := parseActivityStatus([]byte(`{"data":{"story_id":"story-2","state_code":101,"remaining_seconds":36,"duration_seconds":45}}`))
	if !ok {
		t.Fatal("nested activity status was not parsed")
	}
	if status.StoryID != "story-2" || status.StateCode != 101 || status.RemainingSeconds != 36 || status.DurationSeconds != 45 {
		t.Fatalf("unexpected nested activity status: %#v", status)
	}
}

func TestFindStoryID(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "direct", data: `{"story_id":"story-1"}`, want: "story-1"},
		{name: "nested", data: `{"data":{"story_id":"story-2"}}`, want: "story-2"},
		{name: "missing", data: `{"code":1,"message":"failed"}`, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := findStoryID([]byte(test.data)); got != test.want {
				t.Fatalf("findStoryID() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestActivityFromStateCode(t *testing.T) {
	for code, want := range map[int64]string{2: "school", 51: "work", 101: "adventure", 0: ""} {
		if got := activityFromStateCode(code); got != want {
			t.Fatalf("activityFromStateCode(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestNextActivityKeepsLearningUntilItsCountIsComplete(t *testing.T) {
	config := Config{
		ActivityOrder:     []string{"school", "work", "adventure"},
		SchoolRemaining:   2,
		WorkRemaining:     1,
		AdventureRemain:   1,
		SchoolExecuted:    1,
		WorkExecuted:      0,
		AdventureExecuted: 0,
	}
	activity, ok := nextActivity(config)
	if !ok || activity != "school" {
		t.Fatalf("nextActivity() = %q, %t, want school, true", activity, ok)
	}

	config.SchoolRemaining = 0
	config.SchoolExecuted = 3
	activity, ok = nextActivity(config)
	if !ok || activity != "work" {
		t.Fatalf("nextActivity() = %q, %t, want work, true", activity, ok)
	}
}

func TestNextActivitySupportsDurationControl(t *testing.T) {
	config := Config{ActivityOrder: []string{"school", "work", "adventure"}, SchoolControlMode: "duration", SchoolDuration: 3600, SchoolDurationUsed: 1800, WorkControlMode: "count", WorkRemaining: 1}
	activity, ok := nextActivity(config)
	if !ok || activity != "school" {
		t.Fatalf("nextActivity() = %q, %t, want school, true", activity, ok)
	}
	config.SchoolDurationUsed = 3600
	activity, ok = nextActivity(config)
	if !ok || activity != "work" {
		t.Fatalf("nextActivity() = %q, %t, want work, true", activity, ok)
	}
}

func TestSaveConfigRejectsDuplicatePlanIDs(t *testing.T) {
	config := Config{Plan: []PlanItem{{ID: "same", Activity: "school", Hours: 1}, {ID: "same", Activity: "work", Hours: 1, WorkOption: "厨师"}}}
	if err := validatePlanIDs(config.Plan); err == nil {
		t.Fatal("duplicate plan IDs were accepted")
	}
}

func TestNextPlanItemUsesOrderedItems(t *testing.T) {
	config := Config{Plan: []PlanItem{{ID: "a", Activity: "school", ControlMode: "duration", Hours: 2}, {ID: "b", Activity: "work", ControlMode: "duration", Hours: 1}}}
	item, ok := nextPlanItem(config)
	if !ok || item.ID != "a" {
		t.Fatalf("nextPlanItem() = %#v, %t", item, ok)
	}
	config.Plan[0].ExecutedSeconds = 7200
	config.Plan[0].Completed = true
	item, ok = nextPlanItem(config)
	if !ok || item.ID != "b" {
		t.Fatalf("nextPlanItem() = %#v, %t, want b", item, ok)
	}
}

func TestNextPlanItemKeepsProgressPerItem(t *testing.T) {
	config := Config{Plan: []PlanItem{
		{ID: "first", Activity: "school", ControlMode: "count", Count: 2, ExecutedCount: 1},
		{ID: "second", Activity: "school", ControlMode: "count", Count: 1},
	}}
	item, ok := nextPlanItem(config)
	if !ok || item.ID != "first" {
		t.Fatalf("nextPlanItem() = %#v, %t, want first", item, ok)
	}
	config.Plan[0].ExecutedCount = 2
	config.Plan[0].Completed = true
	item, ok = nextPlanItem(config)
	if !ok || item.ID != "second" {
		t.Fatalf("nextPlanItem() = %#v, %t, want second", item, ok)
	}
}

func TestCompletedActivityDuration(t *testing.T) {
	tests := []struct {
		name           string
		cachedDuration int64
		statusDuration int64
		want           int64
	}{
		{name: "cached duration takes precedence", cachedDuration: 1800, statusDuration: 3600, want: 1800},
		{name: "status duration used when cache is zero", cachedDuration: 0, statusDuration: 3600, want: 3600},
		{name: "zero when both durations are unavailable", cachedDuration: 0, statusDuration: 0, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := completedActivityDuration(test.cachedDuration, test.statusDuration); got != test.want {
				t.Fatalf("completedActivityDuration(%d, %d) = %d, want %d", test.cachedDuration, test.statusDuration, got, test.want)
			}
		})
	}
}

func TestTrackedActivityEndedWhenStoryChangesOrClears(t *testing.T) {
	tests := []struct {
		name            string
		currentStoryID  string
		reportedStoryID string
		inProgress      bool
		want            bool
	}{
		{name: "same story running", currentStoryID: "story-1", reportedStoryID: "story-1", inProgress: true, want: false},
		{name: "same story completed", currentStoryID: "story-1", reportedStoryID: "story-1", inProgress: false, want: true},
		{name: "story changed", currentStoryID: "story-1", reportedStoryID: "story-2", inProgress: true, want: true},
		{name: "story cleared", currentStoryID: "story-1", reportedStoryID: "", inProgress: false, want: true},
		{name: "no tracked activity", currentStoryID: "", reportedStoryID: "story-2", inProgress: true, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := trackedActivityEnded(test.currentStoryID, test.reportedStoryID, test.inProgress); got != test.want {
				t.Fatalf("trackedActivityEnded() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestCompletePlanItemOnlyAdvancesAfterConfirmedCompletion(t *testing.T) {
	plan := []PlanItem{{ID: "work-1", Activity: "work", ControlMode: "duration", Hours: 1}}
	if !completePlanItem(plan, "work-1", 1800) {
		t.Fatal("matching plan item was not updated")
	}
	if plan[0].ExecutedCount != 1 || plan[0].ExecutedSeconds != 1800 || plan[0].Completed {
		t.Fatalf("first completion produced unexpected progress: %#v", plan[0])
	}
	if !completePlanItem(plan, "work-1", 1800) {
		t.Fatal("matching plan item was not updated the second time")
	}
	if plan[0].ExecutedCount != 2 || plan[0].ExecutedSeconds != 3600 || !plan[0].Completed {
		t.Fatalf("second completion produced unexpected progress: %#v", plan[0])
	}
}

func TestCompletePlanItemIgnoresExternalActivity(t *testing.T) {
	plan := []PlanItem{{ID: "work-1", Activity: "work", ControlMode: "count", Count: 1}}
	if completePlanItem(plan, "external", 3600) {
		t.Fatal("external activity updated the automatic plan")
	}
	if plan[0].ExecutedCount != 0 || plan[0].ExecutedSeconds != 0 || plan[0].Completed {
		t.Fatalf("external activity changed plan progress: %#v", plan[0])
	}
}

func TestPickOptionUsesSelectedWork(t *testing.T) {
	data := []byte(`{"options":[{"name":"搬砖","can_do":true},{"name":"厨师","can_do":true}]}`)
	if got := pickOption(data, "work", "", "厨师"); got != "厨师" {
		t.Fatalf("pickOption() = %q, want 厨师", got)
	}
}

func TestFindActivityDuration(t *testing.T) {
	options := []byte(`{"options":[{"name":"厨师","duration_seconds":1800}]}`)
	if got := findActivityDuration([]byte(`{"story_id":"1"}`), options, "厨师"); got != 1800 {
		t.Fatalf("duration = %d", got)
	}
	if got := findActivityDuration([]byte(`{"story_id":"1","duration_seconds":900}`), options, "厨师"); got != 900 {
		t.Fatalf("duration = %d", got)
	}
}

func TestFindItemStockSupportsBathInventoryShapes(t *testing.T) {
	tests := []struct {
		name  string
		data  string
		want  int64
		found bool
	}{
		{name: "array with num", data: `[{"item_name":" 香皂片 ","num":"4"}]`, want: 4, found: true},
		{name: "nested items", data: `{"data":{"items":[{"name":"香皂片","count":8}]}}`, want: 8, found: true},
		{name: "unknown item", data: `[{"name":"饼干","count":1}]`, want: 0, found: false},
		{name: "invalid json", data: `{`, want: 0, found: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, found := findItemStock([]byte(test.data), "香皂片")
			if got != test.want || found != test.found {
				t.Fatalf("findItemStock() = %d, %t, want %d, %t", got, found, test.want, test.found)
			}
		})
	}
}

func TestFindItemStockByIDSupportsBathInventory(t *testing.T) {
	tests := []struct {
		name   string
		data   string
		itemID string
		want   int64
		found  bool
	}{
		{name: "string id", data: `[{"item_id":"1","count":8}]`, itemID: "1", want: 8, found: true},
		{name: "numeric id", data: `{"data":{"items":[{"item_id":2,"count":"4"}]}}`, itemID: "2", want: 4, found: true},
		{name: "zero stock", data: `[{"item_id":"1","count":0}]`, itemID: "1", want: 0, found: true},
		{name: "unknown id", data: `[{"item_id":"2","count":8}]`, itemID: "1", want: 0, found: false},
		{name: "empty id", data: `[{"item_id":"1","count":8}]`, itemID: "", want: 0, found: false},
		{name: "invalid json", data: `{`, itemID: "1", want: 0, found: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, found := findItemStockByID([]byte(test.data), test.itemID, "item_id")
			if got != test.want || found != test.found {
				t.Fatalf("findItemStockByID() = %d, %t, want %d, %t", got, found, test.want, test.found)
			}
		})
	}
}
