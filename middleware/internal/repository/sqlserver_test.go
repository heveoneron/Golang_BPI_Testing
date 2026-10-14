package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/joho/godotenv"
	_ "github.com/microsoft/go-mssqldb"
)

func TestExtractDataOnlyValidDetails(t *testing.T) {
	_ = godotenv.Load("../../.env")
	connStr := os.Getenv("SQL_CONN_STRING")
	if connStr == "" {
		t.Skip("SQL_CONN_STRING not set")
	}

	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		t.Fatalf("db open err: %v", err)
	}
	defer db.Close()

	repo := NewSQLServerRepository(db)
	ctx := context.Background()

	records, err := repo.ExtractData(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractData error: %v", err)
	}

	t.Logf("Extracted records count: %d", len(records))
	for _, r := range records {
		t.Logf("Doc: %s | Type: %s | TransID: %s", r.DocumentNo, r.DocType, r.TransID)
		t.Logf("Payload: %s", string(r.PayloadJSON))
	}
}
