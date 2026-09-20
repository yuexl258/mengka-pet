package mokant

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"sync"

	"qq-pet/backend/internal/config"
	"qq-pet/backend/internal/logstream"
)

type Node struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	TokenSet  bool   `json:"token_set"`
	IsDefault bool   `json:"is_default"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
	Status    Status `json:"status"`
}

type Manager struct {
	db           *sql.DB
	cfg          config.Config
	logger       *slog.Logger
	mu           sync.RWMutex
	clients      map[int64]*Client
	eventHandler func([]byte)
	publisher    *logstream.Hub
}

func NewManager(db *sql.DB, cfg config.Config, logger *slog.Logger, publishers ...*logstream.Hub) (*Manager, error) {
	var publisher *logstream.Hub
	if len(publishers) > 0 {
		publisher = publishers[0]
	}
	m := &Manager{db: db, cfg: cfg, logger: logger, clients: make(map[int64]*Client), publisher: publisher}
	if err := m.Reload(context.Background()); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) Reload(ctx context.Context) error {
	m.mu.RLock()
	eventHandler := m.eventHandler
	m.mu.RUnlock()

	rows, err := m.db.QueryContext(ctx, "SELECT id, host, port, token FROM mokant_nodes ORDER BY is_default DESC, id LIMIT 1")
	if err != nil {
		return err
	}
	defer rows.Close()
	configured := make(map[int64]*Client)
	for rows.Next() {
		var id int64
		var host, token string
		var port int
		if err := rows.Scan(&id, &host, &port, &token); err != nil {
			return err
		}
		client := NewClient(m.cfg, m.logger)
		client.setEventHandler(eventHandler)
		client.Configure(host, port, token)
		configured[id] = client
	}
	if err := rows.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	old := m.clients
	m.clients = configured
	m.mu.Unlock()
	for _, client := range old {
		client.Stop()
	}
	for _, client := range configured {
		client.Start(context.Background())
	}
	return nil
}

func (m *Manager) SetEventHandler(handler func([]byte)) {
	m.mu.Lock()
	m.eventHandler = handler
	for _, client := range m.clients {
		client.setEventHandler(handler)
	}
	m.mu.Unlock()
}

func (m *Manager) Start(ctx context.Context) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, client := range m.clients {
		client.Start(ctx)
	}
}

func (m *Manager) Stop() {
	m.mu.RLock()
	clients := make([]*Client, 0, len(m.clients))
	for _, client := range m.clients {
		clients = append(clients, client)
	}
	m.mu.RUnlock()
	for _, client := range clients {
		client.Stop()
	}
}

func (m *Manager) Client(nodeID int64) (*Client, error) {
	m.mu.RLock()
	client := m.clients[nodeID]
	m.mu.RUnlock()
	if client == nil {
		return nil, sql.ErrNoRows
	}
	return client, nil
}

func (m *Manager) DefaultClient(ctx context.Context) (*Client, error) {
	var id int64
	if err := m.db.QueryRowContext(ctx, "SELECT id FROM mokant_nodes WHERE is_default = 1 ORDER BY id LIMIT 1").Scan(&id); err != nil {
		return nil, err
	}
	return m.Client(id)
}

func (m *Manager) ClientForQQ(ctx context.Context, selfID int64) (*Client, error) {
	return m.DefaultClient(ctx)
}

func (m *Manager) Nodes(ctx context.Context) ([]Node, error) {
	rows, err := m.db.QueryContext(ctx, "SELECT id, name, host, port, token <> '', is_default, created_at, updated_at FROM mokant_nodes ORDER BY is_default DESC, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := make([]Node, 0)
	for rows.Next() {
		var node Node
		if err := rows.Scan(&node.ID, &node.Name, &node.Host, &node.Port, &node.TokenSet, &node.IsDefault, &node.CreatedAt, &node.UpdatedAt); err != nil {
			return nil, err
		}
		if client, err := m.Client(node.ID); err == nil {
			node.Status = client.Status()
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func (m *Manager) clientFor(ctx context.Context, selfID int64) (*Client, error) {
	return m.DefaultClient(ctx)
}

func (m *Manager) ListBots(ctx context.Context) (json.RawMessage, error) {
	client, err := m.DefaultClient(ctx)
	if err != nil {
		return nil, ErrNotConnected
	}
	return client.ListBots(ctx)
}

func (m *Manager) ListProtocols(ctx context.Context) (json.RawMessage, error) {
	c, e := m.DefaultClient(ctx)
	if e != nil {
		return nil, e
	}
	return c.ListProtocols(ctx)
}
func (m *Manager) ListDeviceProfiles(ctx context.Context) (json.RawMessage, error) {
	c, e := m.DefaultClient(ctx)
	if e != nil {
		return nil, e
	}
	return c.ListDeviceProfiles(ctx)
}
func (m *Manager) AddAccount(ctx context.Context, selfID int64, password string, protocolID, deviceProfileID, loginNodeID int64) (json.RawMessage, error) {
	c, e := m.DefaultClient(ctx)
	if e != nil {
		return nil, e
	}
	return c.AddAccount(ctx, selfID, password, protocolID, deviceProfileID, loginNodeID)
}
func (m *Manager) GetBotInfo(ctx context.Context, id int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.GetBotInfo(ctx, id)
}
func (m *Manager) GetFriendList(ctx context.Context, id int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.GetFriendList(ctx, id)
}
func (m *Manager) GetFriendPetProfile(ctx context.Context, id int64, friendID, petID string) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.GetFriendPetProfile(ctx, id, friendID, petID)
}
func (m *Manager) PokeFriendPet(ctx context.Context, id int64, friendID string) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.PokeFriendPet(ctx, id, friendID)
}
func (m *Manager) GetPetProfile(ctx context.Context, id int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.GetPetProfile(ctx, id)
}
func (m *Manager) GetPetVitals(ctx context.Context, id int64, petID string) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.GetPetVitals(ctx, id, petID)
}
func (m *Manager) GetPetAttributes(ctx context.Context, id int64, petID string) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.GetPetAttributes(ctx, id, petID)
}
func (m *Manager) GetPetFoodCatalog(ctx context.Context, id int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.GetPetFoodCatalog(ctx, id)
}
func (m *Manager) GetPetBathInventory(ctx context.Context, id int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.GetPetBathInventory(ctx, id)
}
func (m *Manager) GetPetBathCatalog(ctx context.Context, id int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.GetPetBathCatalog(ctx, id)
}
func (m *Manager) GetPetPKPower(ctx context.Context, id int64, petID string) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.GetPetPKPower(ctx, id, petID)
}
func (m *Manager) GetPetFatigueStatus(ctx context.Context, id int64, petID string) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.GetPetFatigueStatus(ctx, id, petID)
}
func (m *Manager) GetPetMedalGallery(ctx context.Context, id int64, petID string) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.GetPetMedalGallery(ctx, id, petID)
}
func (m *Manager) GetPetInteractionMessages(ctx context.Context, id int64, limit int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.GetPetInteractionMessages(ctx, id, limit)
}
func (m *Manager) GetPetActivityOverview(ctx context.Context, id int64, petID, activity string) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.GetPetActivityOverview(ctx, id, petID, activity)
}
func (m *Manager) GetPetActivityOptions(ctx context.Context, id int64, petID, activity string, careerType int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.GetPetActivityOptions(ctx, id, petID, activity, careerType)
}
func (m *Manager) StartPetActivity(ctx context.Context, id int64, petID, activity, optionName string, subEventType int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.StartPetActivity(ctx, id, petID, activity, optionName, subEventType)
}
func (m *Manager) GetPetActivityStatus(ctx context.Context, id int64, petID string) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.GetPetActivityStatus(ctx, id, petID)
}
func (m *Manager) EncouragePetActivity(ctx context.Context, id int64, petID, storyID string) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.EncouragePetActivity(ctx, id, petID, storyID)
}
func (m *Manager) FeedPet(ctx context.Context, id int64, petID, foodID string) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.FeedPet(ctx, id, petID, foodID)
}
func (m *Manager) BuyPetFood(ctx context.Context, id, count int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.BuyPetFood(ctx, id, count)
}
func (m *Manager) BathePet(ctx context.Context, id int64, petID, itemID string, count int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.BathePet(ctx, id, petID, itemID, count)
}
func (m *Manager) BuyPetBathItem(ctx context.Context, id int64, petID, itemID string, count int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.BuyPetBathItem(ctx, id, petID, itemID, count)
}
func (m *Manager) UpdateAccount(ctx context.Context, id int64, p string, a, b, loginNodeID int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.UpdateAccount(ctx, id, p, a, b, loginNodeID)
}
func (m *Manager) LoginAccount(ctx context.Context, id int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.LoginAccount(ctx, id)
}
func (m *Manager) OfflineAccount(ctx context.Context, id int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.OfflineAccount(ctx, id)
}
func (m *Manager) CheckCache(ctx context.Context, id int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.CheckCache(ctx, id)
}
func (m *Manager) CacheLogin(ctx context.Context, id int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.CacheLogin(ctx, id)
}
func (m *Manager) GetSecurityVerifyMethods(ctx context.Context, id int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.GetSecurityVerifyMethods(ctx, id)
}
func (m *Manager) CreateLoginQR(ctx context.Context, id int64) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.CreateLoginQR(ctx, id)
}
func (m *Manager) QueryLoginQRStatus(ctx context.Context, id int64, t string) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.QueryLoginQRStatus(ctx, id, t)
}
func (m *Manager) GetSMS(ctx context.Context, id int64, v int, s string) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.GetSMS(ctx, id, v, s)
}
func (m *Manager) CheckSMS(ctx context.Context, id int64, v int, s, code string) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.CheckSMS(ctx, id, v, s, code)
}
func (m *Manager) RegisterCaptchaProxy(ctx context.Context, id int64, u, b string) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.RegisterCaptchaProxy(ctx, id, u, b)
}
func (m *Manager) CaptchaProxy(ctx context.Context, id int64, u, method string, h map[string]string, b string) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.CaptchaProxy(ctx, id, u, method, h, b)
}
func (m *Manager) SubmitSlider(ctx context.Context, id int64, t, r string) (json.RawMessage, error) {
	c, e := m.clientFor(ctx, id)
	if e != nil {
		return nil, e
	}
	return c.SubmitSlider(ctx, id, t, r)
}
