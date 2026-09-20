package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"qq-pet/backend/internal/auth"
	"qq-pet/backend/internal/mokant"
	"qq-pet/backend/internal/response"
	"qq-pet/backend/internal/timeutil"
)

var friendCodePattern = regexp.MustCompile(`(?i)\b(?:error[_-]?code|code)\s*[=:]\s*["']?([0-9]+)`)

func friendRefreshRoutes(db *sql.DB, client mokant.API) (http.HandlerFunc, http.HandlerFunc, http.HandlerFunc, http.HandlerFunc, http.HandlerFunc, http.HandlerFunc, http.HandlerFunc) {
	return friendsHandler(db), refreshFriendsHandler(db, client), filterPetFriendsHandler(db, client), friendRefreshStateHandler(db), pokeFriendPetHandler(db, client), bindFriendPetHandler(db, client), friendPetProfileHandler(db, client)
}

func friendsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		bindingID, err := userBinding(r.Context(), db, user.ID, r.PathValue("qq"))
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		_, _ = db.ExecContext(r.Context(), `UPDATE qq_friends SET has_pet=0, pet_data=NULL, updated_at=? WHERE qq_binding_id=? AND friend_qq LIKE '2854%' AND has_pet<>0`, timeutil.Now(), bindingID)
		rows, err := db.QueryContext(r.Context(), `SELECT friend_qq, nickname, has_pet, friend_data, pet_id, pet_data, updated_at FROM qq_friends WHERE qq_binding_id = ? ORDER BY id`, bindingID)
		if err != nil {
			response.JSON(w, 500, 9002, "好友列表加载失败", nil)
			return
		}
		defer rows.Close()
		items := make([]map[string]any, 0)
		for rows.Next() {
			var qq, nickname, friendData, petID, updated string
			var hasPet int
			var petData sql.NullString
			if rows.Scan(&qq, &nickname, &hasPet, &friendData, &petID, &petData, &updated) != nil {
				continue
			}
			item := map[string]any{"friend_qq": qq, "nickname": nickname, "has_pet": hasPet == 1, "pet_id": petID, "updated_at": updated}
			var friend, pet any
			if json.Unmarshal([]byte(friendData), &friend) == nil {
				item["friend_data"] = friend
			}
			if petData.Valid && json.Unmarshal([]byte(petData.String), &pet) == nil {
				item["pet_data"] = pet
			}
			items = append(items, item)
		}
		response.JSON(w, http.StatusOK, 0, "ok", items)
	}
}

func pokeFriendPetHandler(db *sql.DB, client mokant.API) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, err := verifyQQAccess(r, db)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		user, _ := auth.UserFromContext(r.Context())
		friendQQ := strings.TrimSpace(r.PathValue("friend_id"))
		_, err = strconv.ParseInt(friendQQ, 10, 64)
		if err != nil || !validQQ(friendQQ) {
			response.JSON(w, http.StatusBadRequest, 4003, "好友 QQ 不合法", nil)
			return
		}
		var bindingID, hasPet int64
		if err := db.QueryRowContext(r.Context(), "SELECT id FROM qq_bindings WHERE qq_number = ? AND user_id = ?", r.PathValue("qq"), user.ID).Scan(&bindingID); err != nil {
			response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
			return
		}
		if strings.HasPrefix(friendQQ, "2854") {
			_, _ = db.ExecContext(r.Context(), `UPDATE qq_friends SET has_pet=0, pet_data=NULL, updated_at=? WHERE qq_binding_id=? AND friend_qq=?`, timeutil.Now(), bindingID, friendQQ)
			response.JSON(w, http.StatusBadRequest, 4003, "官方机器人没有宠物", nil)
			return
		}
		err = db.QueryRowContext(r.Context(), "SELECT has_pet FROM qq_friends WHERE qq_binding_id = ? AND friend_qq = ?", bindingID, friendQQ).Scan(&hasPet)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.JSON(w, http.StatusBadRequest, 4003, "该好友没有可踩的宠物", nil)
				return
			}
			response.JSON(w, http.StatusInternalServerError, 9002, "好友缓存读取失败", nil)
			return
		}
		if hasPet != 1 {
			response.JSON(w, http.StatusBadRequest, 4003, "该好友没有可踩的宠物", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		data, err := client.PokeFriendPet(ctx, selfID, friendQQ)
		if err != nil {
			if errors.Is(err, mokant.ErrNotConnected) {
				response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
				return
			}
			response.JSON(w, http.StatusBadGateway, 5032, "好友踩一踩失败", nil)
			return
		}
		var result any
		if len(data) > 0 && json.Unmarshal(data, &result) != nil {
			result = string(data)
		}
		if noPet, message := pokeResultHasNoPet(result); noPet {
			_, _ = db.ExecContext(r.Context(), `UPDATE qq_friends SET has_pet=0, pet_data=NULL, updated_at=? WHERE qq_binding_id=? AND friend_qq=?`, timeutil.Now(), bindingID, friendQQ)
			response.JSON(w, http.StatusBadRequest, 4003, message, nil)
			return
		}
		if _, ok, message := friendPetProfileValue(result); !ok {
			if message == "" {
				message = "好友踩一踩失败"
			}
			response.JSON(w, http.StatusBadGateway, 5032, message, result)
			return
		}
		response.JSON(w, http.StatusOK, 0, "踩一踩成功", result)
	}
}

func bindFriendPetHandler(db *sql.DB, client mokant.API) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, bindingID, friendQQ, ok := friendRequestContext(w, r, db)
		if !ok {
			return
		}
		if strings.HasPrefix(friendQQ, "2854") {
			_, _ = db.ExecContext(r.Context(), `UPDATE qq_friends SET has_pet=0, pet_data=NULL, updated_at=? WHERE qq_binding_id=? AND friend_qq=?`, timeutil.Now(), bindingID, friendQQ)
			response.JSON(w, http.StatusBadRequest, 4003, "官方机器人没有宠物", nil)
			return
		}
		var input struct {
			PetID string `json:"pet_id"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.PetID) == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "Pet ID 不能为空", nil)
			return
		}
		petID := strings.TrimSpace(input.PetID)
		profile, err := queryFriendPetProfile(r.Context(), client, selfID, friendQQ, petID)
		if err != nil {
			writeFriendPetProfileError(w, err)
			return
		}
		if profile.FriendUIN != friendQQ || profile.PetID != petID {
			response.JSON(w, http.StatusBadRequest, 4003, "Pet ID 与目标好友不匹配", nil)
			return
		}
		raw, _ := json.Marshal(profile.Data)
		result, err := db.ExecContext(r.Context(), `UPDATE qq_friends SET pet_id=?,has_pet=1,pet_data=?,updated_at=? WHERE qq_binding_id=? AND friend_qq=?`, petID, string(raw), timeutil.Now(), bindingID, friendQQ)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "好友 Pet ID 保存失败", nil)
			return
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			response.JSON(w, http.StatusNotFound, 4004, "好友不存在，请先刷新好友列表", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "好友 Pet ID 绑定成功", profile.Data)
	}
}

func friendPetProfileHandler(db *sql.DB, client mokant.API) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		selfID, bindingID, friendQQ, ok := friendRequestContext(w, r, db)
		if !ok {
			return
		}
		var petID string
		if err := db.QueryRowContext(r.Context(), `SELECT pet_id FROM qq_friends WHERE qq_binding_id=? AND friend_qq=?`, bindingID, friendQQ).Scan(&petID); err != nil {
			response.JSON(w, http.StatusNotFound, 4004, "好友不存在，请先刷新好友列表", nil)
			return
		}
		if strings.TrimSpace(petID) == "" {
			response.JSON(w, http.StatusBadRequest, 4003, "该好友尚未绑定 Pet ID", nil)
			return
		}
		profile, err := queryFriendPetProfile(r.Context(), client, selfID, friendQQ, petID)
		if err != nil {
			writeFriendPetProfileError(w, err)
			return
		}
		if profile.FriendUIN != friendQQ || profile.PetID != petID {
			response.JSON(w, http.StatusBadRequest, 4003, "已绑定的 Pet ID 与目标好友不匹配", nil)
			return
		}
		raw, _ := json.Marshal(profile.Data)
		_, _ = db.ExecContext(r.Context(), `UPDATE qq_friends SET has_pet=1,pet_data=?,updated_at=? WHERE qq_binding_id=? AND friend_qq=?`, string(raw), timeutil.Now(), bindingID, friendQQ)
		response.JSON(w, http.StatusOK, 0, "ok", profile.Data)
	}
}

type friendPetProfileResult struct {
	FriendUIN string
	PetID     string
	Data      map[string]any
}

func queryFriendPetProfile(parent context.Context, client mokant.API, selfID int64, friendQQ, petID string) (friendPetProfileResult, error) {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	raw, err := client.GetFriendPetProfile(ctx, selfID, friendQQ, petID)
	if err != nil {
		return friendPetProfileResult{}, err
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return friendPetProfileResult{}, errors.New("好友宠物资料响应无法解析")
	}
	data, ok, message := friendPetProfileValue(value)
	if !ok {
		if message == "" {
			message = "好友宠物资料查询失败"
		}
		return friendPetProfileResult{}, errors.New(message)
	}
	return friendPetProfileResult{FriendUIN: strings.TrimSpace(fmt.Sprint(data["friend_uin"])), PetID: strings.TrimSpace(fmt.Sprint(data["pet_id"])), Data: data}, nil
}

func pokeResultHasNoPet(value any) (bool, string) {
	object, ok := value.(map[string]any)
	if !ok {
		return false, ""
	}
	if message := strings.TrimSpace(fmt.Sprint(object["error"])); strings.Contains(message, "errorCode=136201") && strings.Contains(message, "没有宠物") {
		return true, message
	}
	for _, key := range []string{"data", "result"} {
		if child, exists := object[key]; exists {
			if noPet, message := pokeResultHasNoPet(child); noPet {
				return true, message
			}
		}
	}
	return false, ""
}

func friendPetProfileValue(value any) (map[string]any, bool, string) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, false, ""
	}
	if success, exists := object["ok"].(bool); exists {
		if !success {
			return nil, false, strings.TrimSpace(fmt.Sprint(object["error"]))
		}
		if data, ok := object["data"].(map[string]any); ok {
			return data, true, ""
		}
	}
	for _, key := range []string{"data", "result"} {
		if child, exists := object[key]; exists {
			if data, success, message := friendPetProfileValue(child); data != nil || success || message != "" {
				return data, success, message
			}
		}
	}
	return nil, false, ""
}

func friendRequestContext(w http.ResponseWriter, r *http.Request, db *sql.DB) (int64, int64, string, bool) {
	selfID, err := verifyQQAccess(r, db)
	if err != nil {
		writeQQAccessError(w, err)
		return 0, 0, "", false
	}
	user, _ := auth.UserFromContext(r.Context())
	friendQQ := strings.TrimSpace(r.PathValue("friend_id"))
	if _, err := strconv.ParseInt(friendQQ, 10, 64); err != nil || !validQQ(friendQQ) {
		response.JSON(w, http.StatusBadRequest, 4003, "好友 QQ 不合法", nil)
		return 0, 0, "", false
	}
	var bindingID int64
	if err := db.QueryRowContext(r.Context(), `SELECT id FROM qq_bindings WHERE qq_number=? AND user_id=?`, r.PathValue("qq"), user.ID).Scan(&bindingID); err != nil {
		response.JSON(w, http.StatusNotFound, 4004, "该 QQ 未绑定到当前用户", nil)
		return 0, 0, "", false
	}
	return selfID, bindingID, friendQQ, true
}

func writeFriendPetProfileError(w http.ResponseWriter, err error) {
	if errors.Is(err, mokant.ErrNotConnected) {
		response.JSON(w, http.StatusServiceUnavailable, 5031, "听雨框架尚未连接", nil)
		return
	}
	response.JSON(w, http.StatusBadRequest, 4003, err.Error(), nil)
}

func refreshFriendsHandler(db *sql.DB, client mokant.API) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, 401, 1001, "请先登录", nil)
			return
		}
		qq := strings.TrimSpace(r.PathValue("qq"))
		selfID, err := strconv.ParseInt(qq, 10, 64)
		if err != nil || !validQQ(qq) {
			response.JSON(w, 400, 4003, "QQ 号不合法", nil)
			return
		}
		bindingID, err := userBinding(r.Context(), db, user.ID, qq)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		var status string
		_ = db.QueryRowContext(r.Context(), "SELECT status FROM qq_friend_refresh_states WHERE qq_binding_id = ?", bindingID).Scan(&status)
		if status == "running" {
			response.JSON(w, http.StatusConflict, 4090, "好友刷新正在进行", nil)
			return
		}
		now := timeutil.Now()
		_, err = db.ExecContext(r.Context(), `INSERT INTO qq_friend_refresh_states (qq_binding_id,status,message,error,started_at,finished_at,updated_at) VALUES (?, 'running','正在刷新好友','',?,NULL,?) ON CONFLICT(qq_binding_id) DO UPDATE SET status='running',message='正在刷新好友',error='',started_at=excluded.started_at,finished_at=NULL,updated_at=excluded.updated_at`, bindingID, now, now)
		if err != nil {
			response.JSON(w, 500, 9002, "好友刷新状态保存失败", nil)
			return
		}
		go refreshFriends(context.Background(), db, client, bindingID, selfID)
		response.JSON(w, http.StatusAccepted, 0, "好友刷新已开始", map[string]any{"status": "running"})
	}
}

func filterPetFriendsHandler(db *sql.DB, client mokant.API) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		qq := strings.TrimSpace(r.PathValue("qq"))
		selfID, err := strconv.ParseInt(qq, 10, 64)
		if err != nil || !validQQ(qq) {
			response.JSON(w, http.StatusBadRequest, 4003, "QQ 号不合法", nil)
			return
		}
		bindingID, err := userBinding(r.Context(), db, user.ID, qq)
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		var status string
		_ = db.QueryRowContext(r.Context(), "SELECT status FROM qq_friend_refresh_states WHERE qq_binding_id = ?", bindingID).Scan(&status)
		if status == "running" {
			response.JSON(w, http.StatusConflict, 4090, "好友刷新正在进行", nil)
			return
		}
		now := timeutil.Now()
		_, err = db.ExecContext(r.Context(), `INSERT INTO qq_friend_refresh_states (qq_binding_id,status,message,error,started_at,finished_at,updated_at) VALUES (?, 'running','正在过滤好友','',?,NULL,?) ON CONFLICT(qq_binding_id) DO UPDATE SET status='running',message='正在过滤好友',error='',started_at=excluded.started_at,finished_at=NULL,updated_at=excluded.updated_at`, bindingID, now, now)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "好友过滤状态保存失败", nil)
			return
		}
		go filterPetFriends(context.Background(), db, client, bindingID, selfID)
		response.JSON(w, http.StatusAccepted, 0, "好友过滤已开始", map[string]any{"status": "running"})
	}
}

func filterPetFriends(ctx context.Context, db *sql.DB, client mokant.API, bindingID, selfID int64) {
	rows, err := db.QueryContext(ctx, `SELECT friend_qq FROM qq_friends WHERE qq_binding_id=? AND has_pet=1 AND pet_id='' AND friend_qq NOT LIKE '2854%'`, bindingID)
	if err != nil {
		finishFriendRefresh(db, bindingID, "failed", "待过滤好友读取失败", err.Error())
		return
	}
	defer rows.Close()
	friends := make([]string, 0)
	for rows.Next() {
		var friendQQ string
		if rows.Scan(&friendQQ) == nil {
			friends = append(friends, friendQQ)
		}
	}
	checked, removed := 0, 0
	for _, friendQQ := range friends {
		petData, petErr := client.GetFriendPetProfile(ctx, selfID, friendQQ, "")
		if petErr == nil {
			hasPet, recognized := friendProfileHasPet(petData)
			if recognized {
				if hasPet {
					_, _ = db.ExecContext(ctx, `UPDATE qq_friends SET pet_data=?,updated_at=? WHERE qq_binding_id=? AND friend_qq=? AND pet_id=''`, string(petData), timeutil.Now(), bindingID, friendQQ)
				} else {
					_, _ = db.ExecContext(ctx, `UPDATE qq_friends SET has_pet=0,pet_data=NULL,updated_at=? WHERE qq_binding_id=? AND friend_qq=? AND pet_id=''`, timeutil.Now(), bindingID, friendQQ)
					removed++
				}
			}
		}
		checked++
	}
	finishFriendRefresh(db, bindingID, "completed", fmt.Sprintf("过滤完成，复查 %d 位好友，移除 %d 位无宠物好友", checked, removed), "")
}

func friendRefreshStateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, 401, 1001, "请先登录", nil)
			return
		}
		bindingID, err := userBinding(r.Context(), db, user.ID, r.PathValue("qq"))
		if err != nil {
			writeQQAccessError(w, err)
			return
		}
		var status, message, refreshErr, updated string
		var started, finished sql.NullString
		var totalCount int
		err = db.QueryRowContext(r.Context(), "SELECT status,message,error,started_at,finished_at,updated_at,total_count FROM qq_friend_refresh_states WHERE qq_binding_id = ?", bindingID).Scan(&status, &message, &refreshErr, &started, &finished, &updated, &totalCount)
		if errors.Is(err, sql.ErrNoRows) {
			status, message = "idle", "尚未刷新"
		} else if err != nil {
			response.JSON(w, 500, 9002, "好友刷新状态加载失败", nil)
			return
		}
		if status == "running" {
			if updatedAt, parseErr := time.Parse(time.RFC3339, updated); parseErr == nil && timeutil.Current().Sub(updatedAt) > 10*time.Minute {
				status = "failed"
				message = "好友刷新已超时，请重新点击刷新"
				refreshErr = message
				finishFriendRefresh(db, bindingID, status, message, refreshErr)
			}
		}
		response.JSON(w, 200, 0, "ok", map[string]any{"status": status, "message": message, "error": refreshErr, "started_at": nullableString(started), "finished_at": nullableString(finished), "updated_at": updated, "total_count": totalCount})
	}
}

func refreshFriends(ctx context.Context, db *sql.DB, client mokant.API, bindingID, selfID int64) {
	data, err := client.GetFriendList(ctx, selfID)
	if err != nil {
		finishFriendRefresh(db, bindingID, "failed", "好友列表获取失败", err.Error())
		return
	}
	friends := friendItems(data)
	totalCount := friendListTotalCount(data)
	if totalCount < len(friends) {
		totalCount = len(friends)
	}
	_, _ = db.ExecContext(ctx, "UPDATE qq_friend_refresh_states SET total_count=?,updated_at=? WHERE qq_binding_id=?", totalCount, timeutil.Now(), bindingID)
	type cachedFriend struct {
		qq string
	}
	cached := make([]cachedFriend, 0, len(friends))
	count := 0
	for _, friend := range friends {
		friendID := friendNumber(friend, "user_id", "friend_id", "qq", "uin")
		if friendID < 1 {
			continue
		}
		friendQQ := strconv.FormatInt(friendID, 10)
		nickname := strings.TrimSpace(fmt.Sprint(friend["nickname"]))
		raw, _ := json.Marshal(friend)
		if strings.HasPrefix(friendQQ, "2854") {
			_, err = db.ExecContext(ctx, `INSERT INTO qq_friends (qq_binding_id,friend_qq,nickname,has_pet,friend_data,pet_data,updated_at) VALUES (?,?,?,0,?,NULL,?) ON CONFLICT(qq_binding_id,friend_qq) DO UPDATE SET nickname=excluded.nickname,has_pet=0,friend_data=excluded.friend_data,pet_data=NULL,updated_at=excluded.updated_at`, bindingID, friendQQ, nickname, string(raw), timeutil.Now())
		} else {
			_, err = db.ExecContext(ctx, `INSERT INTO qq_friends (qq_binding_id,friend_qq,nickname,friend_data,updated_at) VALUES (?,?,?,?,?) ON CONFLICT(qq_binding_id,friend_qq) DO UPDATE SET nickname=excluded.nickname,friend_data=excluded.friend_data,updated_at=excluded.updated_at`, bindingID, friendQQ, nickname, string(raw), timeutil.Now())
		}
		if err == nil {
			if !strings.HasPrefix(friendQQ, "2854") {
				cached = append(cached, cachedFriend{qq: friendQQ})
			}
			count++
		}
	}

	// 先保存完整好友列表，再探测宠物状态，避免大量串行请求期间列表数量不完整。
	for _, friend := range cached {
		var petID string
		if db.QueryRowContext(ctx, `SELECT pet_id FROM qq_friends WHERE qq_binding_id=? AND friend_qq=?`, bindingID, friend.qq).Scan(&petID) != nil || petID != "" {
			continue
		}
		petData, petErr := client.GetFriendPetProfile(ctx, selfID, friend.qq, "")
		if petErr != nil {
			continue
		}
		hasPet, recognized := friendProfileHasPet(petData)
		if !recognized {
			continue
		}
		var encodedPet any
		if hasPet {
			encodedPet = string(petData)
		}
		_, _ = db.ExecContext(ctx, `UPDATE qq_friends SET has_pet=?,pet_data=?,updated_at=? WHERE qq_binding_id=? AND friend_qq=? AND pet_id=''`, hasPet, encodedPet, timeutil.Now(), bindingID, friend.qq)
	}
	finishFriendRefresh(db, bindingID, "completed", fmt.Sprintf("刷新完成，共处理 %d 位好友", count), "")
}

func finishFriendRefresh(db *sql.DB, bindingID int64, status, message, refreshErr string) {
	now := timeutil.Now()
	_, _ = db.Exec(`UPDATE qq_friend_refresh_states SET status=?,message=?,error=?,finished_at=?,updated_at=? WHERE qq_binding_id=?`, status, message, refreshErr, now, now, bindingID)
}
func friendProfileHasPet(raw []byte) (bool, bool) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false, false
	}
	return friendProfileValueHasPet(value)
}

func friendProfileValueHasPet(value any) (bool, bool) {
	switch current := value.(type) {
	case map[string]any:
		// 宠物服务用错误码区分“有宠物”和“没有宠物”，不能只看 ok 字段。
		errText := fmt.Sprint(current["error"]) + " " + fmt.Sprint(current["message"])
		if match := friendCodePattern.FindStringSubmatch(errText); len(match) == 2 {
			switch match[1] {
			case "1000120":
				return true, true
			case "1000100":
				return false, true
			}
		}
		for _, child := range current {
			if hasPet, recognized := friendProfileValueHasPet(child); recognized {
				return hasPet, true
			}
		}
	case []any:
		for _, child := range current {
			if hasPet, recognized := friendProfileValueHasPet(child); recognized {
				return hasPet, true
			}
		}
	}

	// 没有明确的宠物业务错误码时，不判定宠物状态，避免把 action 成功误认为拥有宠物。
	return false, false
}

func friendListTotalCount(data []byte) int {
	var raw any
	if json.Unmarshal(data, &raw) != nil {
		return 0
	}
	return friendListTotalCountValue(raw)
}

func friendListTotalCountValue(value any) int {
	if object, ok := value.(map[string]any); ok {
		if count, ok := object["total_count"].(float64); ok && count >= 0 {
			return int(count)
		}
		for _, key := range []string{"data", "friends", "friend_list", "items"} {
			if child, exists := object[key]; exists {
				if count := friendListTotalCountValue(child); count > 0 {
					return count
				}
			}
		}
	}
	return 0
}

func friendItems(data []byte) []map[string]any {
	var raw any
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	return friendItemsValue(raw)
}
func friendItemsValue(raw any) []map[string]any {
	if list, ok := raw.([]any); ok {
		result := make([]map[string]any, 0, len(list))
		for _, v := range list {
			if item, ok := v.(map[string]any); ok {
				result = append(result, item)
			}
		}
		return result
	}
	if object, ok := raw.(map[string]any); ok {
		for _, key := range []string{"data", "friends", "friend_list", "items"} {
			if child, exists := object[key]; exists {
				if result := friendItemsValue(child); len(result) > 0 {
					return result
				}
			}
		}
	}
	return nil
}
func friendNumber(item map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch value := item[key].(type) {
		case float64:
			return int64(value)
		case string:
			n, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if n > 0 {
				return n
			}
		}
	}
	return 0
}
func userBinding(ctx context.Context, db *sql.DB, userID int64, qq string) (int64, error) {
	if !validQQ(qq) {
		return 0, errors.New("invalid_qq")
	}
	var id int64
	err := db.QueryRowContext(ctx, "SELECT id FROM qq_bindings WHERE qq_number=? AND user_id=?", strings.TrimSpace(qq), userID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errors.New("not_bound")
	}
	return id, err
}
func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}
