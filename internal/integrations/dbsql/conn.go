package dbsql

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"homehub/internal/integration"
	"homehub/internal/metrics"
)

// ---- normalized model ------------------------------------------------------

// ServerStats is the live health snapshot.
type ServerStats struct {
	Version          string  `json:"version"`
	Flavor           string  `json:"flavor"` // MySQL | MariaDB
	UptimeSeconds    int64   `json:"uptimeSeconds"`
	ThreadsConnected int64   `json:"threadsConnected"`
	MaxConnections   int64   `json:"maxConnections"`
	QuestionsTotal   int64   `json:"questionsTotal"`
	SlowQueriesTotal int64   `json:"slowQueriesTotal"`
	QPS              float64 `json:"qps"` // derived from counter deltas
}

// Database is one schema with aggregate size.
type Database struct {
	Name      string `json:"name"`
	Tables    int64  `json:"tables"`
	SizeBytes int64  `json:"sizeBytes"`
}

// Table is one table's catalog row.
type Table struct {
	Name      string `json:"name"`
	Engine    string `json:"engine"`
	Rows      int64  `json:"rows"` // storage-engine estimate
	SizeBytes int64  `json:"sizeBytes"`
}

// Session is one row of the live processlist.
type Session struct {
	ID       int64  `json:"id"`
	User     string `json:"user"`
	Host     string `json:"host"`
	Database string `json:"database,omitempty"`
	Command  string `json:"command"`
	TimeSec  int64  `json:"timeSec"`
	State    string `json:"state,omitempty"`
	Query    string `json:"query,omitempty"`
}

// QueryResult is a capped console result.
type QueryResult struct {
	Columns      []string   `json:"columns"`
	Rows         [][]string `json:"rows"`
	RowsAffected int64      `json:"rowsAffected"`
	Truncated    bool       `json:"truncated"`
	DurationMS   float64    `json:"durationMs"`
}

const maxConsoleRows = 500

// systemSchemas are hidden from listings and denied to the console's USE.
var systemSchemas = map[string]bool{
	"information_schema": true, "performance_schema": true, "mysql": true, "sys": true,
}

// IsSystemSchema reports whether a schema is one of the server's own — the
// ones holding accounts and password hashes (mysql.user) and live query text.
func IsSystemSchema(name string) bool { return systemSchemas[strings.ToLower(name)] }

// ---- connection ------------------------------------------------------------

type Conn struct {
	inst integration.Instance
	db   *sql.DB

	mu            sync.Mutex
	lastQuestions int64
	lastSlow      int64
	lastSample    time.Time
}

func (c *Conn) globalStatus(ctx context.Context, names ...string) (map[string]int64, error) {
	// SHOW GLOBAL STATUS works on both engines; the information_schema table
	// is MariaDB-only (MySQL moved it to performance_schema).
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "'" + n + "'"
	}
	rows, err := c.db.QueryContext(ctx,
		"SHOW GLOBAL STATUS WHERE Variable_name IN ("+strings.Join(quoted, ",")+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return nil, err
		}
		v, _ := strconv.ParseInt(value, 10, 64)
		out[strings.ToUpper(name)] = v
	}
	return out, rows.Err()
}

// Stats gathers the health snapshot; QPS derives from Questions deltas
// between calls (0 on the first).
func (c *Conn) Stats(ctx context.Context) (*ServerStats, error) {
	var version string
	if err := c.db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil {
		return nil, err
	}
	status, err := c.globalStatus(ctx, "Questions", "Slow_queries", "Threads_connected", "Uptime")
	if err != nil {
		return nil, err
	}
	var maxConn int64
	_ = c.db.QueryRowContext(ctx, "SELECT @@max_connections").Scan(&maxConn)

	stats := &ServerStats{
		Version:          version,
		Flavor:           "MySQL",
		UptimeSeconds:    status["UPTIME"],
		ThreadsConnected: status["THREADS_CONNECTED"],
		MaxConnections:   maxConn,
		QuestionsTotal:   status["QUESTIONS"],
		SlowQueriesTotal: status["SLOW_QUERIES"],
	}
	if strings.Contains(strings.ToLower(version), "mariadb") {
		stats.Flavor = "MariaDB"
	}

	c.mu.Lock()
	now := time.Now()
	if !c.lastSample.IsZero() && stats.QuestionsTotal >= c.lastQuestions {
		elapsed := now.Sub(c.lastSample).Seconds()
		if elapsed > 0 {
			stats.QPS = float64(stats.QuestionsTotal-c.lastQuestions) / elapsed
		}
	}
	c.lastQuestions = stats.QuestionsTotal
	c.lastSlow = stats.SlowQueriesTotal
	c.lastSample = now
	c.mu.Unlock()
	return stats, nil
}

// Databases lists user schemas with table counts and sizes.
func (c *Conn) Databases(ctx context.Context) ([]Database, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT s.schema_name,
		       COUNT(t.table_name),
		       COALESCE(SUM(t.data_length + t.index_length), 0)
		FROM information_schema.schemata s
		LEFT JOIN information_schema.tables t ON t.table_schema = s.schema_name
		GROUP BY s.schema_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Database{}
	for rows.Next() {
		var d Database
		if err := rows.Scan(&d.Name, &d.Tables, &d.SizeBytes); err != nil {
			return nil, err
		}
		if systemSchemas[d.Name] {
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, rows.Err()
}

// Tables lists one schema's tables.
func (c *Conn) Tables(ctx context.Context, database string) ([]Table, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT table_name, COALESCE(engine, ''), COALESCE(table_rows, 0),
		       COALESCE(data_length + index_length, 0)
		FROM information_schema.tables
		WHERE table_schema = ?
		ORDER BY table_name`, database)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Table{}
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Name, &t.Engine, &t.Rows, &t.SizeBytes); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Sessions returns the live processlist.
func (c *Conn) Sessions(ctx context.Context) ([]Session, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT id, user, host, COALESCE(db, ''), command, time, COALESCE(state, ''), COALESCE(info, '')
		FROM information_schema.processlist
		ORDER BY time DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.User, &s.Host, &s.Database, &s.Command, &s.TimeSec, &s.State, &s.Query); err != nil {
			return nil, err
		}
		if len(s.Query) > 2000 {
			s.Query = s.Query[:2000] + "…"
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Kill terminates a session.
func (c *Conn) Kill(ctx context.Context, sessionID int64) error {
	_, err := c.db.ExecContext(ctx, fmt.Sprintf("KILL %d", sessionID))
	if err != nil {
		return fmt.Errorf("kill failed: %s", friendlyError(err))
	}
	return nil
}

// Query runs one console statement, optionally scoped to a database, with
// row capping. The caller audits; this only executes.
func (c *Conn) Query(ctx context.Context, database, statement string) (*QueryResult, error) {
	conn, err := c.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if database != "" {
		if systemSchemas[database] {
			return nil, fmt.Errorf("system schema %q is not available in the console", database)
		}
		if _, err := conn.ExecContext(ctx, "USE "+quoteIdent(database)); err != nil {
			return nil, fmt.Errorf("unknown database %q", database)
		}
	}
	start := time.Now()
	rows, err := conn.QueryContext(ctx, statement)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := &QueryResult{Columns: cols, Rows: [][]string{}}
	raw := make([]sql.RawBytes, len(cols))
	scan := make([]any, len(cols))
	for i := range raw {
		scan[i] = &raw[i]
	}
	for rows.Next() {
		if len(result.Rows) >= maxConsoleRows {
			result.Truncated = true
			break
		}
		if err := rows.Scan(scan...); err != nil {
			return nil, err
		}
		row := make([]string, len(cols))
		for i, cell := range raw {
			if cell == nil {
				row[i] = "NULL"
			} else {
				v := string(cell)
				if len(v) > 1000 {
					v = v[:1000] + "…"
				}
				row[i] = v
			}
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result.RowsAffected = int64(len(result.Rows))
	result.DurationMS = float64(time.Since(start).Microseconds()) / 1000
	return result, nil
}

func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// ---- integration.Connection ------------------------------------------------

func (c *Conn) Health(ctx context.Context) integration.Health {
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.db.PingContext(pctx); err != nil {
		return integration.Health{Status: integration.StatusUnreachable, Message: friendlyError(err)}
	}
	return integration.Health{Status: integration.StatusConnected}
}

// Resources publishes one node per user database for per-database grants.
func (c *Conn) Resources(ctx context.Context) ([]integration.Resource, error) {
	dbs, err := c.Databases(ctx)
	if err != nil {
		return nil, err
	}
	root := c.inst.ResourcePath()
	out := make([]integration.Resource, 0, len(dbs))
	for _, d := range dbs {
		out = append(out, integration.Resource{
			Path: root + "/database/" + d.Name,
			Kind: "database",
			Name: d.Name,
		})
	}
	return out, nil
}

func (c *Conn) Collect(ctx context.Context) ([]metrics.Sample, error) {
	stats, err := c.Stats(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	res := c.inst.ResourcePath()
	return []metrics.Sample{
		{Resource: res, Metric: "qps", Unit: "count", TS: now, Value: stats.QPS},
		{Resource: res, Metric: "threads_connected", Unit: "count", TS: now, Value: float64(stats.ThreadsConnected)},
		{Resource: res, Metric: "slow_queries_total", Unit: "count", TS: now, Value: float64(stats.SlowQueriesTotal)},
	}, nil
}

func (c *Conn) Close() {
	_ = c.db.Close()
}
