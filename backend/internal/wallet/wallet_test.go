package wallet

import (
	"database/sql"
	"path/filepath"
	"testing"

	"qq-pet/backend/internal/database"
)

func testDB(t *testing.T) *sql.DB {
	db, err := database.Open(filepath.Join(t.TempDir(), "wallet.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCreateForUserTxAndAdjust(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	result, err := db.Exec("INSERT INTO users (username, password_hash) VALUES ('wallet-user', 'hash')")
	if err != nil {
		t.Fatal(err)
	}
	userID, _ := result.LastInsertId()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := CreateForUserTx(tx, userID, 50); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	service := NewService(db)
	current, err := service.Get(userID)
	if err != nil || current.Coins != 50 {
		t.Fatalf("wallet=%+v err=%v", current, err)
	}
	items, total, err := service.Transactions(userID, 1, 20)
	if err != nil || total != 1 || len(items) != 1 || items[0].Amount != 50 {
		t.Fatalf("transactions=%+v total=%d err=%v", items, total, err)
	}
	current, err = service.Adjust(userID, 7, -20, "测试扣除")
	if err != nil || current.Coins != 30 {
		t.Fatalf("adjust=%+v err=%v", current, err)
	}
	if _, err := service.Adjust(userID, 7, -31, "余额不足"); err != ErrInsufficient {
		t.Fatalf("expected insufficient error, got %v", err)
	}
	current, _ = service.Get(userID)
	if current.Coins != 30 {
		t.Fatalf("balance changed after failed adjustment: %d", current.Coins)
	}
}

func TestGenerateAndRedeemCodeOnce(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	admin, _ := db.Exec("INSERT INTO users (username, password_hash, role) VALUES ('code-admin', 'hash', 'admin')")
	adminID, _ := admin.LastInsertId()
	user, _ := db.Exec("INSERT INTO users (username, password_hash) VALUES ('code-user', 'hash')")
	userID, _ := user.LastInsertId()
	tx, _ := db.Begin()
	if err := CreateForUserTx(tx, userID, 0); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	service := NewService(db)
	codes, err := service.GenerateCodes(adminID, 80, 1)
	if err != nil || len(codes) != 1 {
		t.Fatalf("codes=%v err=%v", codes, err)
	}
	var stored string
	if err := db.QueryRow("SELECT code_hash FROM redeem_codes").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == codes[0] {
		t.Fatal("plaintext redeem code was stored")
	}
	wallet, amount, err := service.Redeem(userID, codes[0])
	if err != nil || amount != 80 || wallet.Coins != 80 {
		t.Fatalf("wallet=%+v amount=%d err=%v", wallet, amount, err)
	}
	if _, _, err := service.Redeem(userID, codes[0]); err != ErrRedeemCode {
		t.Fatalf("expected redeemed error, got %v", err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM wallet_transactions WHERE user_id = ? AND transaction_type = 'redeem_code'", userID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("redeem transactions=%d err=%v", count, err)
	}
}
