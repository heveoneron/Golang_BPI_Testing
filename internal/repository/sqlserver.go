package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type SQLServerRepository struct {
	DB *sql.DB
}

type ExtractedRecord struct {
	DocType        string
	DocumentNo     string
	PayloadJSON    json.RawMessage
	LastUpdatedAt  time.Time
}

func NewSQLServerRepository(db *sql.DB) *SQLServerRepository {
	return &SQLServerRepository{DB: db}
}

// ExtractData menarik data delta dari SQL Server Maximo berbasis watermark lastSync
// Dalam implementasi riil BPI, ini menjalankan query ke tabel axmxpo, axreceipt, axitem, axuom
// atau stored procedure sp_GetMaximoOutboundPayload @lastSync
func (r *SQLServerRepository) ExtractData(ctx context.Context, docType string, lastSync time.Time) ([]ExtractedRecord, time.Time, error) {
	if r.DB == nil {
		// Mock offline safe: kembalikan slice kosong jika DB belum terhubung
		return nil, lastSync, nil
	}

	// Query delta dari tabel staging SQL Server dengan filter mxout_inter_trans.ax = 0
	query := `
		SELECT t.ifacetbname, t.transid, p.payload_json, t.transdate
		FROM mxout_inter_trans t
		LEFT JOIN v_maximo_payload p ON t.transid = p.transid
		WHERE t.ax = 0 AND t.transdate >= @p1
		ORDER BY t.transdate ASC
	`
	rows, err := r.DB.QueryContext(ctx, query, lastSync)
	if err != nil {
		return nil, lastSync, fmt.Errorf("error query sql server: %w", err)
	}
	defer rows.Close()

	var records []ExtractedRecord
	maxUpdated := lastSync

	for rows.Next() {
		var tbName, transID, payloadStr string
		var transDate time.Time
		if err := rows.Scan(&tbName, &transID, &payloadStr, &transDate); err != nil {
			continue
		}

		records = append(records, ExtractedRecord{
			DocType:       tbName,
			DocumentNo:    transID,
			PayloadJSON:   json.RawMessage(payloadStr),
			LastUpdatedAt: transDate,
		})

		if transDate.After(maxUpdated) {
			maxUpdated = transDate
		}
	}

	return records, maxUpdated, nil
}
