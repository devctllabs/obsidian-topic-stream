//go:build integration && (performance || live)

package deps

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type capacitySQLProfile struct{ conn *pgx.Conn }

func (profile capacitySQLProfile) resetStatements(t *testing.T) {
	t.Helper()
	if os.Getenv("TOPIC_STREAM_CAPTURE_STATEMENTS") != "true" {
		return
	}
	_, err := profile.conn.Exec(t.Context(), `SELECT pg_stat_statements_reset()`)
	require.NoError(t, err)
}

func (profile capacitySQLProfile) statements(t *testing.T) json.RawMessage {
	t.Helper()
	if os.Getenv("TOPIC_STREAM_CAPTURE_STATEMENTS") != "true" {
		return nil
	}
	var encoded json.RawMessage
	err := profile.conn.QueryRow(t.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(statement_info)),'[]'::jsonb) FROM (
 SELECT query,calls,total_plan_time,total_exec_time,rows,shared_blks_hit,shared_blks_read,shared_blks_dirtied,
 shared_blk_read_time,shared_blk_write_time,temp_blks_read,temp_blks_written,wal_records,wal_fpi,wal_bytes
 FROM pg_stat_statements WHERE dbid=(SELECT oid FROM pg_database WHERE datname=current_database())
 AND query NOT LIKE '%pg_stat_statements%' ORDER BY total_exec_time DESC LIMIT 40) statement_info`).Scan(&encoded)
	require.NoError(t, err)
	return encoded
}
