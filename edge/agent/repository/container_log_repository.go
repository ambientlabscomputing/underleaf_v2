package repository

import (
	"database/sql"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

const defaultLogLimit = 100

// ContainerLogRepository persists and queries container log lines.
type ContainerLogRepository struct {
	db *sql.DB
}

func NewContainerLogRepository(db *sql.DB) *ContainerLogRepository {
	return &ContainerLogRepository{db: db}
}

// InsertLine writes a single log line to the database.
func (r *ContainerLogRepository) InsertLine(line *types.LogLine) error {
	_, err := r.db.Exec(
		`INSERT INTO container_logs (docker_id, ts_ms, stream, message) VALUES (?, ?, ?, ?)`,
		line.DockerID, line.TsMs, line.Stream, line.Message,
	)
	return err
}

// QueryLogsParams controls what QueryLines returns.
type QueryLogsParams struct {
	DockerID string
	SinceMs  int64 // inclusive; 0 = beginning of buffer
	UntilMs  int64 // inclusive; 0 = no upper bound
	Limit    int   // 0 = defaultLogLimit
	CursorID int64 // row id > CursorID; 0 = start from beginning
}

// LogPage is the result of a paginated log query.
type LogPage struct {
	Lines      []*types.LogLine
	NextCursor int64 // rowid of last returned line; 0 if no more pages
}

// QueryLines returns a page of log lines for a container.
func (r *ContainerLogRepository) QueryLines(p QueryLogsParams) (*LogPage, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = defaultLogLimit
	}

	query := `SELECT id, docker_id, ts_ms, stream, message
	          FROM container_logs
	          WHERE docker_id = ?
	            AND id > ?`
	args := []any{p.DockerID, p.CursorID}

	if p.SinceMs > 0 {
		query += ` AND ts_ms >= ?`
		args = append(args, p.SinceMs)
	}
	if p.UntilMs > 0 {
		query += ` AND ts_ms <= ?`
		args = append(args, p.UntilMs)
	}
	query += ` ORDER BY id ASC LIMIT ?`
	args = append(args, limit+1) // fetch one extra to detect next page

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	lines := make([]*types.LogLine, 0, limit)
	for rows.Next() {
		var l types.LogLine
		if err := rows.Scan(&l.RowID, &l.DockerID, &l.TsMs, &l.Stream, &l.Message); err != nil {
			return nil, err
		}
		lines = append(lines, &l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	page := &LogPage{Lines: lines}
	if len(lines) > limit {
		page.Lines = lines[:limit]
		page.NextCursor = lines[limit-1].RowID
	}
	return page, nil
}

// LatestTimestamp returns the ts_ms of the most recent log line for a container,
// or 0 if no lines exist.
func (r *ContainerLogRepository) LatestTimestamp(dockerID string) (int64, error) {
	var ts int64
	err := r.db.QueryRow(
		`SELECT COALESCE(MAX(ts_ms), 0) FROM container_logs WHERE docker_id = ?`,
		dockerID,
	).Scan(&ts)
	return ts, err
}

// PruneOlderThan deletes log lines older than the given duration.
func (r *ContainerLogRepository) PruneOlderThan(age time.Duration) error {
	cutoff := time.Now().Add(-age).UnixMilli()
	_, err := r.db.Exec(`DELETE FROM container_logs WHERE ts_ms < ?`, cutoff)
	return err
}
