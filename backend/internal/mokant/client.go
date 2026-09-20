package mokant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"qq-pet/backend/internal/config"
	"qq-pet/backend/internal/logstream"

	"github.com/gorilla/websocket"
)

type Status struct {
	State       string `json:"state"`
	Connected   bool   `json:"connected"`
	LastChecked string `json:"last_checked"`
	LastError   string `json:"last_error,omitempty"`
}

var ErrNotConnected = errors.New("听雨框架尚未连接")

type API interface {
	ListBots(context.Context) (json.RawMessage, error)
	GetBotInfo(context.Context, int64) (json.RawMessage, error)
	GetFriendList(context.Context, int64) (json.RawMessage, error)
	GetFriendPetProfile(context.Context, int64, string, string) (json.RawMessage, error)
	PokeFriendPet(context.Context, int64, string) (json.RawMessage, error)
	GetPetProfile(context.Context, int64) (json.RawMessage, error)
	GetPetVitals(context.Context, int64, string) (json.RawMessage, error)
	GetPetAttributes(context.Context, int64, string) (json.RawMessage, error)
	GetPetFoodCatalog(context.Context, int64) (json.RawMessage, error)
	GetPetBathInventory(context.Context, int64) (json.RawMessage, error)
	GetPetBathCatalog(context.Context, int64) (json.RawMessage, error)
	GetPetPKPower(context.Context, int64, string) (json.RawMessage, error)
	GetPetPKStrangers(context.Context, int64, string, int64) (json.RawMessage, error)
	StartPetPK(context.Context, int64, string, string, string) (json.RawMessage, error)
	GetPetPKStatus(context.Context, int64, string, string) (json.RawMessage, error)
	SettlePetPK(context.Context, int64, string, string) (json.RawMessage, error)
	GetPetFatigueStatus(context.Context, int64, string) (json.RawMessage, error)
	GetPetMedalGallery(context.Context, int64, string) (json.RawMessage, error)
	GetPetInteractionMessages(context.Context, int64, int64) (json.RawMessage, error)
	GetPetActivityOverview(context.Context, int64, string, string) (json.RawMessage, error)
	GetPetActivityOptions(context.Context, int64, string, string, int64) (json.RawMessage, error)
	StartPetActivity(context.Context, int64, string, string, string, int64) (json.RawMessage, error)
	GetPetActivityStatus(context.Context, int64, string) (json.RawMessage, error)
	EncouragePetActivity(context.Context, int64, string, string) (json.RawMessage, error)
	FeedPet(context.Context, int64, string, string) (json.RawMessage, error)
	BuyPetFood(context.Context, int64, int64) (json.RawMessage, error)
	BathePet(context.Context, int64, string, string, int64) (json.RawMessage, error)
	BuyPetBathItem(context.Context, int64, string, string, int64) (json.RawMessage, error)
	ListLoginNodes(context.Context) (json.RawMessage, error)
	AddAccount(context.Context, int64, string, int64, int64, int64) (json.RawMessage, error)
	UpdateAccount(context.Context, int64, string, int64, int64, int64) (json.RawMessage, error)
	LoginAccount(context.Context, int64) (json.RawMessage, error)
	OfflineAccount(context.Context, int64) (json.RawMessage, error)
	CheckCache(context.Context, int64) (json.RawMessage, error)
	CacheLogin(context.Context, int64) (json.RawMessage, error)
	GetSecurityVerifyMethods(context.Context, int64) (json.RawMessage, error)
	CreateLoginQR(context.Context, int64) (json.RawMessage, error)
	QueryLoginQRStatus(context.Context, int64, string) (json.RawMessage, error)
	GetSMS(context.Context, int64, int, string) (json.RawMessage, error)
	CheckSMS(context.Context, int64, int, string, string) (json.RawMessage, error)
	RegisterCaptchaProxy(context.Context, int64, string, string) (json.RawMessage, error)
	CaptchaProxy(context.Context, int64, string, string, map[string]string, string) (json.RawMessage, error)
	SubmitSlider(context.Context, int64, string, string) (json.RawMessage, error)
	ListProtocols(context.Context) (json.RawMessage, error)
	ListDeviceProfiles(context.Context) (json.RawMessage, error)
}

type actionResponse struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Status    string          `json:"status"`
	Retcode   int             `json:"retcode"`
	Message   string          `json:"message"`
	OK        *bool           `json:"ok"`
	Error     string          `json:"error"`
	ErrorCode int             `json:"error_code"`
	Data      json.RawMessage `json:"data"`
	Raw       json.RawMessage `json:"-"`
}

type Client struct {
	cfg          config.Config
	cfgMu        sync.RWMutex
	host         string
	port         int
	token        string
	log          *slog.Logger
	mu           sync.RWMutex
	status       Status
	stop         chan struct{}
	done         chan struct{}
	publisher    *logstream.Hub
	connMu       sync.RWMutex
	conn         *websocket.Conn
	writeMu      sync.Mutex
	pendingMu    sync.Mutex
	pending      map[string]chan actionResponse
	nextID       uint64
	lifecycleMu  sync.Mutex
	cancel       context.CancelFunc
	running      bool
	manager      *Manager
	eventMu      sync.RWMutex
	eventHandler func([]byte)
}

func NewManagerClient(cfg config.Config, logger *slog.Logger, manager *Manager) *Client {
	return &Client{cfg: cfg, log: logger, manager: manager, status: Status{State: "未配置"}, stop: make(chan struct{}), done: make(chan struct{}), pending: make(map[string]chan actionResponse)}
}

func NewClient(cfg config.Config, logger *slog.Logger, publishers ...*logstream.Hub) *Client {
	var publisher *logstream.Hub
	if len(publishers) > 0 {
		publisher = publishers[0]
	}
	return &Client{cfg: cfg, log: logger, publisher: publisher, status: Status{State: "未配置"}, stop: make(chan struct{}), done: make(chan struct{}), pending: make(map[string]chan actionResponse)}
}

func (c *Client) Start(ctx context.Context) {
	c.lifecycleMu.Lock()
	if c.running {
		c.lifecycleMu.Unlock()
		return
	}
	ctx, c.cancel = context.WithCancel(ctx)
	c.stop = make(chan struct{})
	c.done = make(chan struct{})
	c.running = true
	c.lifecycleMu.Unlock()
	go c.run(ctx)
}

func (c *Client) Configure(host string, port int, token string) {
	c.cfgMu.Lock()
	c.host, c.port, c.token = host, port, token
	c.cfgMu.Unlock()
}

func (c *Client) setEventHandler(handler func([]byte)) {
	c.eventMu.Lock()
	c.eventHandler = handler
	c.eventMu.Unlock()
}

func (c *Client) handleEvent(raw []byte) {
	c.eventMu.RLock()
	handler := c.eventHandler
	c.eventMu.RUnlock()
	if handler != nil {
		handler(raw)
	}
}

func (c *Client) Stop() {
	c.lifecycleMu.Lock()
	if !c.running {
		c.lifecycleMu.Unlock()
		return
	}
	stop := c.stop
	done := c.done
	cancel := c.cancel
	c.lifecycleMu.Unlock()
	select {
	case <-stop:
	default:
		close(stop)
	}
	if cancel != nil {
		cancel()
	}
	<-done
	c.setStatus("连接断开", false, "")
}

func (c *Client) Connect(ctx context.Context) error {
	c.Start(ctx)
	return nil
}

func (c *Client) Disconnect() { c.Stop() }

func (c *Client) Status() Status {
	if c.manager != nil {
		if target, err := c.manager.DefaultClient(context.Background()); err == nil {
			return target.Status()
		}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

func (c *Client) Node(nodeID int64) (*Client, error) {
	if c.manager == nil {
		return c, nil
	}
	return c.manager.Client(nodeID)
}

func (c *Client) run(ctx context.Context) {
	defer func() {
		c.lifecycleMu.Lock()
		c.running = false
		c.cancel = nil
		close(c.done)
		c.lifecycleMu.Unlock()
	}()
	c.cfgMu.RLock()
	token := c.token
	c.cfgMu.RUnlock()
	if token == "" {
		c.setStatus("未配置", false, "")
		return
	}
	select {
	case <-ctx.Done():
		c.setStatus("已停止", false, "")
		return
	case <-c.stop:
		c.setStatus("已停止", false, "")
		return
	default:
	}
	c.setStatus("连接中", false, "")
	c.publish("info", "connect_start", "正在连接听雨框架", nil)
	if err := c.connect(ctx); err != nil {
		c.setStatus("连接断开", false, err.Error())
		c.log.Warn("mokant connection failed", "error", err)
		c.publish("error", "connection_failed", "听雨框架连接失败，已停止重连", map[string]any{"error": err.Error()})
		return
	}
}

func (c *Client) connect(ctx context.Context) error {
	c.cfgMu.RLock()
	cfg := c.cfg
	host, port, token := c.host, c.port, c.token
	c.cfgMu.RUnlock()
	u := url.URL{Scheme: "ws", Host: fmt.Sprintf("%s:%d", host, port), Path: "/"}
	connectStarted := time.Now()
	c.publish("info", "dial_start", "开始拨号听雨框架", map[string]any{"address": u.Host, "path": u.Path})
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		c.publish("error", "dial_failed", "听雨框架 WebSocket 拨号失败", map[string]any{"address": u.Host, "path": u.Path, "elapsed_ms": time.Since(connectStarted).Milliseconds()})
		return err
	}
	defer conn.Close()
	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()
	defer func() {
		c.connMu.Lock()
		if c.conn == conn {
			c.conn = nil
		}
		c.connMu.Unlock()
	}()
	c.publish("info", "dial_connected", "听雨框架 WebSocket 已建立", map[string]any{"address": u.Host, "path": u.Path, "elapsed_ms": time.Since(connectStarted).Milliseconds()})
	auth := map[string]any{"type": "auth", "token": token, "name": "QQ宠物", "version": cfg.Version, "author": "小七", "permissions": map[string]bool{"group_message": true, "friend_message": true, "group_event": true, "friend_event": true, "bot_offline": true}}
	c.writeMu.Lock()
	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		c.writeMu.Unlock()
		return err
	}
	err = conn.WriteJSON(auth)
	c.writeMu.Unlock()
	if err != nil {
		return err
	}
	authStarted := time.Now()
	c.publish("info", "auth_sent", "已发送听雨框架认证请求，等待响应", map[string]any{"wait_timeout_seconds": 10})
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var authReply struct {
		Type string `json:"type"`
	}
	if err := conn.ReadJSON(&authReply); err != nil {
		c.publish("error", "auth_read_failed", "读取听雨框架认证响应失败", map[string]any{"elapsed_ms": time.Since(authStarted).Milliseconds(), "error_type": fmt.Sprintf("%T", err)})
		return err
	}
	c.publish("info", "auth_received", "已收到听雨框架认证响应", map[string]any{"message_type": authReply.Type, "elapsed_ms": time.Since(authStarted).Milliseconds()})
	if authReply.Type != "auth_ok" {
		c.publish("error", "auth_failed", "听雨框架认证失败", nil)
		return fmt.Errorf("认证失败")
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return err
	}
	c.setStatus("已连接", true, "")
	c.publish("info", "connected", "听雨框架连接并认证成功", nil)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	readErr := make(chan error, 1)
	go func() {
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				readErr <- err
				return
			}
			var event struct {
				EventType string `json:"event_type"`
			}
			if c.publisher != nil && json.Unmarshal(raw, &event) == nil && event.EventType == "account_offline" {
				c.publisher.PublishRaw(event.EventType, string(raw))
			}
			var frame actionResponse
			if err := json.Unmarshal(raw, &frame); err != nil {
				continue
			}
			frame.Raw = json.RawMessage(raw)
			c.handleEvent(raw)
			if frame.ID != "" {
				c.pendingMu.Lock()
				waiter := c.pending[frame.ID]
				delete(c.pending, frame.ID)
				c.pendingMu.Unlock()
				if waiter != nil {
					waiter <- frame
				}
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.stop:
			return nil
		case err := <-readErr:
			return err
		case <-ticker.C:
			c.writeMu.Lock()
			if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				c.writeMu.Unlock()
				return err
			}
			err := conn.WriteJSON(map[string]string{"type": "ping"})
			c.writeMu.Unlock()
			if err != nil {
				return err
			}
		}
	}
}

func (c *Client) Action(ctx context.Context, action string, params map[string]any) (json.RawMessage, error) {
	if c.manager != nil {
		if selfID, ok := params["self_id"].(int64); ok {
			target, err := c.manager.ClientForQQ(ctx, selfID)
			if err != nil {
				return nil, ErrNotConnected
			}
			return target.Action(ctx, action, params)
		}
		if action == "get_bot_list" {
			return c.manager.ListBots(ctx)
		}
		if target, err := c.manager.DefaultClient(ctx); err == nil {
			return target.Action(ctx, action, params)
		}
		return nil, ErrNotConnected
	}
	reply, err := c.action(ctx, action, params)
	if err != nil {
		return nil, err
	}
	if reply.OK != nil && !*reply.OK || reply.Status != "" && reply.Status != "ok" || reply.Retcode != 0 || reply.ErrorCode != 0 {
		message := reply.Message
		if message == "" {
			message = reply.Error
		}
		return nil, fmt.Errorf("听雨框架 action 失败: code=%d %s", max(reply.Retcode, reply.ErrorCode), message)
	}
	return reply.Data, nil
}

func (c *Client) RawAction(ctx context.Context, action string, params map[string]any) (json.RawMessage, error) {
	if c.manager != nil {
		if selfID, ok := params["self_id"].(int64); ok {
			target, err := c.manager.ClientForQQ(ctx, selfID)
			if err != nil {
				return nil, ErrNotConnected
			}
			return target.RawAction(ctx, action, params)
		}
		if action == "get_bot_list" {
			return c.manager.ListBots(ctx)
		}
		if target, err := c.manager.DefaultClient(ctx); err == nil {
			return target.RawAction(ctx, action, params)
		}
		return nil, ErrNotConnected
	}
	reply, err := c.action(ctx, action, params)
	if err != nil {
		return nil, err
	}
	return reply.Raw, nil
}

func (c *Client) action(ctx context.Context, action string, params map[string]any) (actionResponse, error) {
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()
	if conn == nil {
		return actionResponse{}, ErrNotConnected
	}
	id := strconv.FormatUint(atomic.AddUint64(&c.nextID, 1), 10)
	waiter := make(chan actionResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = waiter
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()
	request := map[string]any{"type": "action", "id": id, "action": action, "params": params}
	c.writeMu.Lock()
	err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err == nil {
		err = conn.WriteJSON(request)
	}
	if err == nil {
		err = conn.SetWriteDeadline(time.Time{})
	}
	c.writeMu.Unlock()
	if err != nil {
		return actionResponse{}, err
	}
	select {
	case <-ctx.Done():
		return actionResponse{}, ctx.Err()
	case reply := <-waiter:
		return reply, nil
	}
}

func (c *Client) ListBots(ctx context.Context) (json.RawMessage, error) {
	return c.Action(ctx, "get_bot_list", map[string]any{})
}

func (c *Client) ListLoginNodes(ctx context.Context) (json.RawMessage, error) {
	return c.Action(ctx, "get_node_list", map[string]any{})
}

func (c *Client) GetBotInfo(ctx context.Context, selfID int64) (json.RawMessage, error) {
	return c.Action(ctx, "get_bot_info", map[string]any{"self_id": selfID})
}

func (c *Client) GetFriendList(ctx context.Context, selfID int64) (json.RawMessage, error) {
	return c.Action(ctx, "get_friend_list", map[string]any{"self_id": selfID})
}

func (c *Client) GetFriendPetProfile(ctx context.Context, selfID int64, friendUIN, petID string) (json.RawMessage, error) {
	params := map[string]any{"self_id": selfID, "client_type": "android", "friend_uin": friendUIN}
	if strings.TrimSpace(petID) != "" {
		params["pet_id"] = strings.TrimSpace(petID)
	}
	return c.RawAction(ctx, "get_friend_pet_profile", params)
}
func (c *Client) PokeFriendPet(ctx context.Context, selfID int64, friendUIN string) (json.RawMessage, error) {
	return c.RawAction(ctx, "poke_friend_pet", map[string]any{"self_id": selfID, "client_type": "android", "friend_uin": friendUIN})
}
func (c *Client) GetPetProfile(ctx context.Context, selfID int64) (json.RawMessage, error) {
	return c.Action(ctx, "get_pet_profile", map[string]any{"self_id": selfID, "client_type": "android"})
}
func (c *Client) GetPetVitals(ctx context.Context, selfID int64, petID string) (json.RawMessage, error) {
	return c.Action(ctx, "get_pet_vitals", map[string]any{"self_id": selfID, "client_type": "android", "pet_id": petID})
}
func (c *Client) GetPetAttributes(ctx context.Context, selfID int64, petID string) (json.RawMessage, error) {
	return c.Action(ctx, "get_pet_attributes", map[string]any{"self_id": selfID, "client_type": "android", "pet_id": petID})
}
func (c *Client) GetPetFoodCatalog(ctx context.Context, selfID int64) (json.RawMessage, error) {
	return c.Action(ctx, "get_pet_food_catalog", map[string]any{"self_id": selfID, "client_type": "android"})
}
func (c *Client) GetPetBathInventory(ctx context.Context, selfID int64) (json.RawMessage, error) {
	return c.Action(ctx, "get_pet_bath_inventory", map[string]any{"self_id": selfID, "client_type": "android"})
}
func (c *Client) GetPetBathCatalog(ctx context.Context, selfID int64) (json.RawMessage, error) {
	return c.Action(ctx, "get_pet_bath_catalog", map[string]any{"self_id": selfID, "client_type": "android"})
}
func (c *Client) GetPetPKPower(ctx context.Context, selfID int64, petID string) (json.RawMessage, error) {
	return c.Action(ctx, "get_pet_pk_power", map[string]any{"self_id": selfID, "client_type": "android", "pet_id": petID})
}
func (c *Client) GetPetPKStrangers(ctx context.Context, selfID int64, cursor string, mode int64) (json.RawMessage, error) {
	p := map[string]any{"self_id": selfID, "client_type": "android"}
	if cursor != "" {
		p["cursor"] = cursor
	}
	if mode >= 0 {
		p["mode"] = mode
	}
	return c.Action(ctx, "get_pet_pk_strangers", p)
}
func (c *Client) StartPetPK(ctx context.Context, selfID int64, petID, friendUIN, friendPetID string) (json.RawMessage, error) {
	return c.Action(ctx, "start_pet_pk", map[string]any{"self_id": selfID, "client_type": "android", "pet_id": petID, "friend_uin": friendUIN, "friend_pet_id": friendPetID})
}
func (c *Client) GetPetPKStatus(ctx context.Context, selfID int64, petID, storyID string) (json.RawMessage, error) {
	return c.Action(ctx, "get_pet_pk_status", map[string]any{"self_id": selfID, "client_type": "android", "pet_id": petID, "story_id": storyID})
}
func (c *Client) SettlePetPK(ctx context.Context, selfID int64, petID, storyID string) (json.RawMessage, error) {
	return c.Action(ctx, "settle_pet_pk", map[string]any{"self_id": selfID, "client_type": "android", "pet_id": petID, "story_id": storyID})
}
func (c *Client) GetPetFatigueStatus(ctx context.Context, selfID int64, petID string) (json.RawMessage, error) {
	return c.Action(ctx, "get_pet_fatigue_status", map[string]any{"self_id": selfID, "client_type": "android", "pet_id": petID})
}
func (c *Client) GetPetMedalGallery(ctx context.Context, selfID int64, petID string) (json.RawMessage, error) {
	return c.Action(ctx, "get_pet_medal_gallery", map[string]any{"self_id": selfID, "client_type": "android", "pet_id": petID})
}
func (c *Client) GetPetInteractionMessages(ctx context.Context, selfID int64, limit int64) (json.RawMessage, error) {
	p := map[string]any{"self_id": selfID, "client_type": "android"}
	if limit > 0 {
		p["limit"] = limit
	}
	return c.Action(ctx, "get_pet_interaction_messages", p)
}
func (c *Client) GetPetActivityOverview(ctx context.Context, selfID int64, petID, activity string) (json.RawMessage, error) {
	return c.Action(ctx, "get_pet_activity_overview", map[string]any{"self_id": selfID, "client_type": "android", "pet_id": petID, "activity": activity})
}
func (c *Client) GetPetActivityOptions(ctx context.Context, selfID int64, petID, activity string, careerType int64) (json.RawMessage, error) {
	params := map[string]any{"self_id": selfID, "client_type": "android", "pet_id": petID, "activity": activity}
	if activity == "work" {
		params["career_type"] = careerType
	}
	return c.Action(ctx, "get_pet_activity_options", params)
}
func (c *Client) StartPetActivity(ctx context.Context, selfID int64, petID, activity, optionName string, subEventType int64) (json.RawMessage, error) {
	return c.Action(ctx, "start_pet_activity", map[string]any{"self_id": selfID, "client_type": "android", "pet_id": petID, "activity": activity, "option_name": optionName, "sub_event_type": subEventType})
}
func (c *Client) GetPetActivityStatus(ctx context.Context, selfID int64, petID string) (json.RawMessage, error) {
	return c.Action(ctx, "get_pet_activity_status", map[string]any{"self_id": selfID, "client_type": "android", "pet_id": petID})
}
func (c *Client) EncouragePetActivity(ctx context.Context, selfID int64, petID, storyID string) (json.RawMessage, error) {
	return c.Action(ctx, "encourage_pet_activity", map[string]any{"self_id": selfID, "client_type": "android", "pet_id": petID, "story_id": storyID})
}
func (c *Client) FeedPet(ctx context.Context, selfID int64, petID, foodID string) (json.RawMessage, error) {
	p := map[string]any{"self_id": selfID, "client_type": "android", "pet_id": petID}
	if foodID != "" {
		p["food_id"] = foodID
	}
	return c.Action(ctx, "feed_pet", p)
}
func (c *Client) BuyPetFood(ctx context.Context, selfID, count int64) (json.RawMessage, error) {
	return c.Action(ctx, "buy_pet_food", map[string]any{"self_id": selfID, "client_type": "android", "count": count})
}
func (c *Client) BathePet(ctx context.Context, selfID int64, petID, itemID string, count int64) (json.RawMessage, error) {
	return c.Action(ctx, "bathe_pet", map[string]any{"self_id": selfID, "client_type": "android", "pet_id": petID, "item_id": itemID, "count": count})
}
func (c *Client) BuyPetBathItem(ctx context.Context, selfID int64, petID, itemID string, count int64) (json.RawMessage, error) {
	return c.Action(ctx, "buy_pet_bath_item", map[string]any{"self_id": selfID, "client_type": "android", "pet_id": petID, "item_id": itemID, "count": count})
}

func (c *Client) AddAccount(ctx context.Context, selfID int64, password string, protocolID, deviceProfileID, loginNodeID int64) (json.RawMessage, error) {
	return c.Action(ctx, "add_account", map[string]any{
		"self_id": selfID, "password": password, "protocol_id": protocolID,
		"device_profile_id": deviceProfileID, "node_id": loginNodeID, "client_type": "android",
	})
}

func (c *Client) UpdateAccount(ctx context.Context, selfID int64, password string, protocolID, deviceProfileID, loginNodeID int64) (json.RawMessage, error) {
	return c.Action(ctx, "update_account", map[string]any{
		"self_id": selfID, "password": password, "protocol_id": protocolID,
		"device_profile_id": deviceProfileID, "node_id": loginNodeID, "client_type": "android",
	})
}

func (c *Client) LoginAccount(ctx context.Context, selfID int64) (json.RawMessage, error) {
	return c.Action(ctx, "login_account", map[string]any{"self_id": selfID, "client_type": "android"})
}

func (c *Client) OfflineAccount(ctx context.Context, selfID int64) (json.RawMessage, error) {
	return c.Action(ctx, "stop_account_login", map[string]any{
		"self_id": selfID, "client_type": "android", "protocol": "android",
	})
}

func (c *Client) CheckCache(ctx context.Context, selfID int64) (json.RawMessage, error) {
	return c.Action(ctx, "check_cache", map[string]any{"self_id": selfID, "client_type": "android"})
}

func (c *Client) CacheLogin(ctx context.Context, selfID int64) (json.RawMessage, error) {
	return c.Action(ctx, "cache_login", map[string]any{"self_id": selfID, "client_type": "android"})
}

func (c *Client) GetSecurityVerifyMethods(ctx context.Context, selfID int64) (json.RawMessage, error) {
	return c.Action(ctx, "get_security_verify_methods", map[string]any{"self_id": selfID, "client_type": "android"})
}

func (c *Client) CreateLoginQR(ctx context.Context, selfID int64) (json.RawMessage, error) {
	return c.Action(ctx, "create_login_qr", map[string]any{"self_id": selfID, "client_type": "android"})
}

func (c *Client) QueryLoginQRStatus(ctx context.Context, selfID int64, guaranteeToken string) (json.RawMessage, error) {
	return c.Action(ctx, "query_login_qr_status", map[string]any{"self_id": selfID, "client_type": "android", "guarantee_token": guaranteeToken})
}

func (c *Client) GetSMS(ctx context.Context, selfID int64, verifyType int, sign string) (json.RawMessage, error) {
	return c.Action(ctx, "get_sms", map[string]any{"self_id": selfID, "client_type": "android", "verify_type": verifyType, "sign": sign})
}

func (c *Client) CheckSMS(ctx context.Context, selfID int64, verifyType int, sign, code string) (json.RawMessage, error) {
	params := map[string]any{"self_id": selfID, "client_type": "android", "verify_type": verifyType, "sign": sign}
	if verifyType == 4 {
		params["code"] = code
	}
	return c.Action(ctx, "check_sms", params)
}

func (c *Client) RegisterCaptchaProxy(ctx context.Context, selfID int64, sliderURL, proxyBase string) (json.RawMessage, error) {
	params := map[string]any{"self_id": selfID, "client_type": "android"}
	if sliderURL != "" {
		params["url"] = sliderURL
	}
	if proxyBase != "" {
		params["proxy_base"] = proxyBase
	}
	return c.Action(ctx, "register_captcha_proxy", params)
}

func (c *Client) CaptchaProxy(ctx context.Context, selfID int64, url, method string, headers map[string]string, body string) (json.RawMessage, error) {
	params := map[string]any{"self_id": selfID, "client_type": "android", "url": url}
	if method != "" {
		params["method"] = method
	}
	if len(headers) > 0 {
		params["headers"] = headers
	}
	if body != "" {
		params["body"] = body
	}
	return c.Action(ctx, "captcha_proxy", params)
}

func (c *Client) SubmitSlider(ctx context.Context, selfID int64, ticket, randstr string) (json.RawMessage, error) {
	return c.Action(ctx, "submit_slider", map[string]any{"self_id": selfID, "client_type": "android", "ticket": ticket, "randstr": randstr})
}

func (c *Client) OpenAccountSecurityAccess(ctx context.Context, selfID int64, accessType string, data map[string]any) (json.RawMessage, error) {
	params := map[string]any{"self_id": selfID, "client_type": "android", "type": accessType}
	if len(data) > 0 {
		params["data"] = data
	}
	return c.Action(ctx, "open_account_security_access", params)
}

func (c *Client) RetryAccountSecurityVerify(ctx context.Context, selfID int64, loginType int, extra map[string]any) (json.RawMessage, error) {
	params := map[string]any{"self_id": selfID, "client_type": "android"}
	if loginType > 0 {
		params["login_type"] = loginType
	}
	if len(extra) > 0 {
		params["extra"] = extra
	}
	return c.Action(ctx, "retry_account_security_verify", params)
}

func (c *Client) SubmitAccountIdentityCaptcha(ctx context.Context, selfID int64, ticket, randstr string) (json.RawMessage, error) {
	return c.Action(ctx, "submit_account_identity_captcha", map[string]any{
		"self_id": selfID, "client_type": "android", "protocol": "android", "ticket": ticket, "randstr": randstr,
	})
}

func (c *Client) SubmitAccountIdentityPhone(ctx context.Context, selfID int64, mobile, areaCode string) (json.RawMessage, error) {
	return c.Action(ctx, "submit_account_identity_phone", map[string]any{
		"self_id": selfID, "client_type": "android", "protocol": "android", "mobile": mobile, "area_code": areaCode,
	})
}

func (c *Client) ConfirmAccountIdentitySMS(ctx context.Context, selfID int64, mobile, areaCode string) (json.RawMessage, error) {
	return c.Action(ctx, "confirm_account_identity_sms", map[string]any{
		"self_id": selfID, "client_type": "android", "protocol": "android", "mobile": mobile, "area_code": areaCode,
	})
}

func (c *Client) RetryAccountIdentityVerify(ctx context.Context, selfID int64) (json.RawMessage, error) {
	return c.Action(ctx, "retry_account_identity_verify", map[string]any{"self_id": selfID, "client_type": "android"})
}

func (c *Client) ListProtocols(ctx context.Context) (json.RawMessage, error) {
	return c.Action(ctx, "get_protocol_list", map[string]any{})
}

func (c *Client) ListDeviceProfiles(ctx context.Context) (json.RawMessage, error) {
	return c.Action(ctx, "get_device_profile_list", map[string]any{})
}

func (c *Client) publish(level, event, message string, details map[string]any) {
	if c.publisher != nil {
		c.publisher.Publish(level, event, message, details)
	}
}

func (c *Client) setStatus(state string, connected bool, lastError string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = Status{State: state, Connected: connected, LastChecked: time.Now().Format(time.RFC3339), LastError: lastError}
}
