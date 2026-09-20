package wallet

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"qq-pet/backend/internal/timeutil"
)

var ErrInsufficient = errors.New("金币余额不足")
var ErrRedeemCode = errors.New("卡密不存在或已使用")
var ErrTransfer = errors.New("转账参数不合法")

type Service struct{ db *sql.DB }

type Wallet struct {
	Coins     int64  `json:"coins"`
	UpdatedAt string `json:"updated_at"`
}

type Transaction struct {
	ID           int64  `json:"id"`
	Amount       int64  `json:"amount"`
	BalanceAfter int64  `json:"balance_after"`
	Type         string `json:"transaction_type"`
	ReferenceID  string `json:"reference_id"`
	Description  string `json:"description"`
	CreatedAt    string `json:"created_at"`
}

func NewService(db *sql.DB) *Service { return &Service{db: db} }

func CreateForUserTx(tx *sql.Tx, userID, gift int64) error {
	now := timeutil.Now()
	if _, err := tx.Exec("INSERT INTO wallets (user_id, coins, created_at, updated_at) VALUES (?, ?, ?, ?)", userID, gift, now, now); err != nil {
		return err
	}
	if gift > 0 {
		_, err := tx.Exec("INSERT INTO wallet_transactions (wallet_id, user_id, amount, balance_after, transaction_type, description, created_at) SELECT id, ?, ?, ?, 'register_gift', '注册赠送金币', ? FROM wallets WHERE user_id = ?", userID, gift, gift, now, userID)
		return err
	}
	return nil
}

func (s *Service) Get(userID int64) (Wallet, error) {
	var result Wallet
	err := s.db.QueryRow("SELECT coins, updated_at FROM wallets WHERE user_id = ?", userID).Scan(&result.Coins, &result.UpdatedAt)
	return result, err
}

func (s *Service) Transactions(userID int64, page, size int) ([]Transaction, int, error) {
	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM wallet_transactions WHERE user_id = ?", userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query("SELECT id, amount, balance_after, transaction_type, COALESCE(reference_id, ''), description, created_at FROM wallet_transactions WHERE user_id = ? ORDER BY id DESC LIMIT ? OFFSET ?", userID, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Transaction, 0)
	for rows.Next() {
		var item Transaction
		if err := rows.Scan(&item.ID, &item.Amount, &item.BalanceAfter, &item.Type, &item.ReferenceID, &item.Description, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *Service) Adjust(userID, adminID, amount int64, description string) (Wallet, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Wallet{}, err
	}
	defer tx.Rollback()
	var walletID, balance int64
	var updated string
	if err = tx.QueryRow("SELECT id, coins, updated_at FROM wallets WHERE user_id = ?", userID).Scan(&walletID, &balance, &updated); err != nil {
		return Wallet{}, err
	}
	newBalance := balance + amount
	if amount == 0 || newBalance < 0 {
		return Wallet{}, ErrInsufficient
	}
	updatedAt := timeutil.Now()
	if _, err = tx.Exec("UPDATE wallets SET coins = ?, updated_at = ? WHERE id = ?", newBalance, updatedAt, walletID); err != nil {
		return Wallet{}, err
	}
	if _, err = tx.Exec("INSERT INTO wallet_transactions (wallet_id, user_id, amount, balance_after, transaction_type, reference_id, description, created_at) VALUES (?, ?, ?, ?, 'admin_adjustment', ?, ?, ?)", walletID, userID, amount, newBalance, strconv.FormatInt(adminID, 10), description, updatedAt); err != nil {
		return Wallet{}, err
	}
	if err = tx.Commit(); err != nil {
		return Wallet{}, err
	}
	return Wallet{Coins: newBalance, UpdatedAt: updatedAt}, nil
}

type TransferResult struct {
	Wallet
	Before int64 `json:"balance_before"`
	After  int64 `json:"balance_after"`
}

func (s *Service) Transfer(senderID, recipientID, amount int64, recipientName string) (TransferResult, error) {
	if amount <= 0 || senderID == recipientID || strings.TrimSpace(recipientName) == "" {
		return TransferResult{}, ErrTransfer
	}
	tx, err := s.db.Begin()
	if err != nil {
		return TransferResult{}, err
	}
	defer tx.Rollback()
	var actualName string
	if err = tx.QueryRow("SELECT username FROM users WHERE id = ? AND status = 'active'", recipientID).Scan(&actualName); err != nil || actualName != recipientName {
		return TransferResult{}, ErrTransfer
	}
	var senderWalletID, senderBalance int64
	if err = tx.QueryRow("SELECT id, coins FROM wallets WHERE user_id = ?", senderID).Scan(&senderWalletID, &senderBalance); err != nil || senderBalance < amount {
		return TransferResult{}, ErrInsufficient
	}
	var recipientWalletID, recipientBalance int64
	if err = tx.QueryRow("SELECT id, coins FROM wallets WHERE user_id = ?", recipientID).Scan(&recipientWalletID, &recipientBalance); err != nil {
		return TransferResult{}, err
	}
	now := timeutil.Now()
	if _, err = tx.Exec("UPDATE wallets SET coins = ?, updated_at = ? WHERE id = ?", senderBalance-amount, now, senderWalletID); err != nil {
		return TransferResult{}, err
	}
	if _, err = tx.Exec("UPDATE wallets SET coins = ?, updated_at = ? WHERE id = ?", recipientBalance+amount, now, recipientWalletID); err != nil {
		return TransferResult{}, err
	}
	ref := strconv.FormatInt(recipientID, 10)
	if _, err = tx.Exec("INSERT INTO wallet_transactions (wallet_id, user_id, amount, balance_after, transaction_type, reference_id, description, created_at) VALUES (?, ?, ?, ?, 'transfer_out', ?, ?, ?)", senderWalletID, senderID, -amount, senderBalance-amount, ref, "转账给 "+recipientName, now); err != nil {
		return TransferResult{}, err
	}
	ref = strconv.FormatInt(senderID, 10)
	if _, err = tx.Exec("INSERT INTO wallet_transactions (wallet_id, user_id, amount, balance_after, transaction_type, reference_id, description, created_at) VALUES (?, ?, ?, ?, 'transfer_in', ?, ?, ?)", recipientWalletID, recipientID, amount, recipientBalance+amount, ref, "收到 "+strconv.FormatInt(senderID, 10)+" 的转账", now); err != nil {
		return TransferResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return TransferResult{}, err
	}
	return TransferResult{Wallet: Wallet{Coins: senderBalance - amount, UpdatedAt: now}, Before: senderBalance, After: senderBalance - amount}, nil
}

func (s *Service) GenerateCodes(adminID, amount, quantity int64) ([]string, error) {
	if amount <= 0 || quantity < 1 || quantity > 1000 {
		return nil, errors.New("卡密面额或数量不合法")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	codes := make([]string, 0, quantity)
	for i := int64(0); i < quantity; i++ {
		for {
			code, hash, err := newCode()
			if err != nil {
				return nil, err
			}
			ciphertext, err := encryptCode(code)
			if err != nil {
				return nil, err
			}
			if _, err = tx.Exec("INSERT INTO redeem_codes (code_hash, code_ciphertext, amount, created_by, created_at) VALUES (?, ?, ?, ?, ?)", hash, ciphertext, amount, adminID, timeutil.Now()); err == nil {
				codes = append(codes, code)
				break
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return codes, nil
}

func DecryptCode(ciphertext string) (string, error) {
	key, err := redeemCodeKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	data, err := base64.RawURLEncoding.DecodeString(ciphertext)
	if err != nil || len(data) < gcm.NonceSize() {
		return "", errors.New("invalid redeem code ciphertext")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func encryptCode(code string) (string, error) {
	key, err := redeemCodeKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	data := gcm.Seal(nil, nonce, []byte(code), nil)
	return base64.RawURLEncoding.EncodeToString(append(nonce, data...)), nil
}

func redeemCodeKey() ([]byte, error) {
	value := strings.TrimSpace(os.Getenv("REDEEM_CODE_ENCRYPTION_KEY"))
	if value == "" {
		value = "qq-pet-redeem-code-development-key"
	}
	digest := sha256.Sum256([]byte(value))
	return digest[:], nil
}

func (s *Service) Redeem(userID int64, input string) (Wallet, int64, error) {
	code := strings.ToUpper(strings.TrimSpace(input))
	if code == "" {
		return Wallet{}, 0, ErrRedeemCode
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(code)))
	tx, err := s.db.Begin()
	if err != nil {
		return Wallet{}, 0, err
	}
	defer tx.Rollback()
	var codeID, amount int64
	if err := tx.QueryRow("SELECT id, amount FROM redeem_codes WHERE code_hash = ? AND status = 'unused'", hash).Scan(&codeID, &amount); err != nil {
		return Wallet{}, 0, ErrRedeemCode
	}
	result, err := tx.Exec("UPDATE redeem_codes SET status = 'redeemed', redeemed_by = ?, redeemed_at = ? WHERE id = ? AND status = 'unused'", userID, timeutil.Now(), codeID)
	if err != nil {
		return Wallet{}, 0, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return Wallet{}, 0, ErrRedeemCode
	}
	var walletID, balance int64
	if err := tx.QueryRow("SELECT id, coins FROM wallets WHERE user_id = ?", userID).Scan(&walletID, &balance); err != nil {
		return Wallet{}, 0, err
	}
	newBalance := balance + amount
	if _, err = tx.Exec("UPDATE wallets SET coins = ?, updated_at = ? WHERE id = ?", newBalance, timeutil.Now(), walletID); err != nil {
		return Wallet{}, 0, err
	}
	if _, err = tx.Exec("INSERT INTO wallet_transactions (wallet_id, user_id, amount, balance_after, transaction_type, reference_id, description, created_at) VALUES (?, ?, ?, ?, 'redeem_code', ?, '卡密兑换金币', ?)", walletID, userID, amount, newBalance, strconv.FormatInt(codeID, 10), timeutil.Now()); err != nil {
		return Wallet{}, 0, err
	}
	if err = tx.Commit(); err != nil {
		return Wallet{}, 0, err
	}
	current, err := s.Get(userID)
	return current, amount, err
}

func newCode() (string, string, error) {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", err
	}
	parts := make([]string, 4)
	for i := range parts {
		for j := 0; j < 4; j++ {
			parts[i] += string(alphabet[int(bytes[i*4+j])%len(alphabet)])
		}
	}
	code := "QPET-" + strings.Join(parts, "-")
	return code, fmt.Sprintf("%x", sha256.Sum256([]byte(code))), nil
}
