package database

import (
	"path/filepath"
	"testing"
)

func TestMigrateIsIdempotent(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}

	var table string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'wallets'").Scan(&table); err != nil {
		t.Fatal(err)
	}
	if table != "wallets" {
		t.Fatalf("expected wallets table, got %q", table)
	}
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'pet_profiles'").Scan(&table); err != nil {
		t.Fatal(err)
	}
	if table != "pet_profiles" {
		t.Fatalf("expected pet_profiles table, got %q", table)
	}
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'pet_pk_strangers'").Scan(&table); err != nil {
		t.Fatal(err)
	}
	if table != "pet_pk_strangers" {
		t.Fatalf("expected pet_pk_strangers table, got %q", table)
	}
	var userIDUnique int
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_index_list('pet_pk_strangers') WHERE \"unique\" = 1").Scan(&userIDUnique); err != nil {
		t.Fatal(err)
	}
	if userIDUnique != 1 {
		t.Fatalf("expected unique constraint/index for pet_pk_strangers.user_id")
	}
	for _, column := range []string{"user_id", "pet_id", "power", "dominant_type"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('pet_pk_strangers') WHERE name = ?", column).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected pet_pk_strangers.%s field", column)
		}
	}
	var friendPetIDColumn int
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('qq_friends') WHERE name = 'pet_id'").Scan(&friendPetIDColumn); err != nil {
		t.Fatal(err)
	}
	if friendPetIDColumn != 1 {
		t.Fatal("expected qq_friends.pet_id field")
	}
	for _, column := range []string{"login_node_id", "login_node_name"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('qq_bindings') WHERE name = ?", column).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected qq_bindings.%s field", column)
		}
	}
	for _, name := range []string{"pet_auto_pk_configs", "pet_auto_pk_states", "pet_auto_pk_daily_opponents", "pet_auto_pk_logs"} {
		if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", name).Scan(&table); err != nil {
			t.Fatal(err)
		}
		if table != name {
			t.Fatalf("expected %s table, got %q", name, table)
		}
	}
}
