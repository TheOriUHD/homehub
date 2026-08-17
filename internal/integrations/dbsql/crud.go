package dbsql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Column is one column's structure.
type Column struct {
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	Nullable     bool    `json:"nullable"`
	Key          string  `json:"key"` // PRI | UNI | MUL | ""
	Default      *string `json:"default"`
	Extra        string  `json:"extra"` // auto_increment, ...
	IsPrimaryKey bool    `json:"isPrimaryKey"`
}

// Index is one index on a table.
type Index struct {
	Name    string   `json:"name"`
	Unique  bool     `json:"unique"`
	Columns []string `json:"columns"`
}

// TableSchema is the full structure of one table.
type TableSchema struct {
	Columns     []Column `json:"columns"`
	Indexes     []Index  `json:"indexes"`
	PrimaryKey  []string `json:"primaryKey"`
	RowEstimate int64    `json:"rowEstimate"`
}

// RowFilter is one structured, parameterized predicate.
type RowFilter struct {
	Column   string
	Operator string // eq | ne | lt | lte | gt | gte | like | is_null | not_null
	Value    string
}

// RowPage is a page of table data.
type RowPage struct {
	Columns    []string   `json:"columns"`
	Rows       [][]*string `json:"rows"` // nil = SQL NULL
	Total      int64      `json:"total"`
	Limit      int        `json:"limit"`
	Offset     int        `json:"offset"`
	DurationMS float64    `json:"durationMs"`
}

// TableSchema loads columns, primary key, and indexes for one table.
func (c *Conn) TableSchema(ctx context.Context, database, table string) (*TableSchema, error) {
	cols, err := c.db.QueryContext(ctx, `
		SELECT column_name, column_type, is_nullable, column_key, column_default, extra
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position`, database, table)
	if err != nil {
		return nil, err
	}
	defer cols.Close()
	schema := &TableSchema{Columns: []Column{}, Indexes: []Index{}, PrimaryKey: []string{}}
	for cols.Next() {
		var col Column
		var nullable, key string
		if err := cols.Scan(&col.Name, &col.Type, &nullable, &key, &col.Default, &col.Extra); err != nil {
			return nil, err
		}
		col.Nullable = nullable == "YES"
		col.Key = key
		col.IsPrimaryKey = key == "PRI"
		if col.IsPrimaryKey {
			schema.PrimaryKey = append(schema.PrimaryKey, col.Name)
		}
		schema.Columns = append(schema.Columns, col)
	}
	if err := cols.Err(); err != nil {
		return nil, err
	}
	if len(schema.Columns) == 0 {
		return nil, fmt.Errorf("table %q not found in database %q", table, database)
	}

	idxRows, err := c.db.QueryContext(ctx, `
		SELECT index_name, non_unique, column_name
		FROM information_schema.statistics
		WHERE table_schema = ? AND table_name = ?
		ORDER BY index_name, seq_in_index`, database, table)
	if err != nil {
		return nil, err
	}
	defer idxRows.Close()
	byName := map[string]*Index{}
	var order []string
	for idxRows.Next() {
		var name, column string
		var nonUnique int
		if err := idxRows.Scan(&name, &nonUnique, &column); err != nil {
			return nil, err
		}
		idx, ok := byName[name]
		if !ok {
			idx = &Index{Name: name, Unique: nonUnique == 0}
			byName[name] = idx
			order = append(order, name)
		}
		idx.Columns = append(idx.Columns, column)
	}
	for _, name := range order {
		schema.Indexes = append(schema.Indexes, *byName[name])
	}

	_ = c.db.QueryRowContext(ctx,
		"SELECT COALESCE(table_rows, 0) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?",
		database, table).Scan(&schema.RowEstimate)
	return schema, nil
}

// validColumn confirms a column exists on the table (guards ORDER BY and
// filter columns, which cannot be parameter placeholders).
func columnSet(schema *TableSchema) map[string]bool {
	set := make(map[string]bool, len(schema.Columns))
	for _, col := range schema.Columns {
		set[col.Name] = true
	}
	return set
}

var filterOps = map[string]string{
	"eq": "=", "ne": "<>", "lt": "<", "lte": "<=", "gt": ">", "gte": ">=", "like": "LIKE",
}

// buildWhere renders parameterized predicates, validating columns.
func buildWhere(schema *TableSchema, filters []RowFilter) (string, []any, error) {
	cols := columnSet(schema)
	var clauses []string
	var args []any
	for _, f := range filters {
		if !cols[f.Column] {
			return "", nil, fmt.Errorf("unknown column %q", f.Column)
		}
		id := quoteIdent(f.Column)
		switch f.Operator {
		case "is_null":
			clauses = append(clauses, id+" IS NULL")
		case "not_null":
			clauses = append(clauses, id+" IS NOT NULL")
		default:
			op, ok := filterOps[f.Operator]
			if !ok {
				return "", nil, fmt.Errorf("unsupported operator %q", f.Operator)
			}
			clauses = append(clauses, id+" "+op+" ?")
			args = append(args, f.Value)
		}
	}
	if len(clauses) == 0 {
		return "", nil, nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args, nil
}

// TableData reads a page of rows with optional sort and filters.
func (c *Conn) TableData(ctx context.Context, database, table string, schema *TableSchema,
	limit, offset int, orderBy, dir string, filters []RowFilter) (*RowPage, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	where, args, err := buildWhere(schema, filters)
	if err != nil {
		return nil, err
	}
	qualified := quoteIdent(database) + "." + quoteIdent(table)

	var total int64
	if err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified+where, args...).Scan(&total); err != nil {
		return nil, err
	}

	orderClause := ""
	if orderBy != "" {
		if !columnSet(schema)[orderBy] {
			return nil, fmt.Errorf("unknown sort column %q", orderBy)
		}
		d := "ASC"
		if strings.EqualFold(dir, "desc") {
			d = "DESC"
		}
		orderClause = " ORDER BY " + quoteIdent(orderBy) + " " + d
	}

	start := time.Now()
	query := fmt.Sprintf("SELECT * FROM %s%s%s LIMIT %d OFFSET %d", qualified, where, orderClause, limit, offset)
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	colNames, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	page := &RowPage{Columns: colNames, Rows: [][]*string{}, Total: total, Limit: limit, Offset: offset}
	for rows.Next() {
		raw := make([]sql.RawBytes, len(colNames))
		scan := make([]any, len(colNames))
		for i := range raw {
			scan[i] = &raw[i]
		}
		if err := rows.Scan(scan...); err != nil {
			return nil, err
		}
		row := make([]*string, len(colNames))
		for i, cell := range raw {
			if cell != nil {
				v := string(cell)
				row[i] = &v
			}
		}
		page.Rows = append(page.Rows, row)
	}
	page.DurationMS = float64(time.Since(start).Microseconds()) / 1000
	return page, rows.Err()
}

// identityWhere builds a WHERE targeting exactly one row: by primary key when
// the table has one, else by every supplied original column value. Returns an
// error when identity can't be established safely.
func identityWhere(schema *TableSchema, identity map[string]*string) (string, []any, error) {
	cols := columnSet(schema)
	keyCols := schema.PrimaryKey
	if len(keyCols) == 0 {
		// No primary key: match on all provided columns (Adminer's approach).
		for name := range identity {
			if cols[name] {
				keyCols = append(keyCols, name)
			}
		}
		if len(keyCols) == 0 {
			return "", nil, fmt.Errorf("cannot identify the row: table has no primary key and no columns were provided")
		}
	}
	var clauses []string
	var args []any
	for _, col := range keyCols {
		val, ok := identity[col]
		if !ok {
			return "", nil, fmt.Errorf("missing identity value for column %q", col)
		}
		if val == nil {
			clauses = append(clauses, quoteIdent(col)+" IS NULL")
		} else {
			clauses = append(clauses, quoteIdent(col)+" = ?")
			args = append(args, *val)
		}
	}
	return strings.Join(clauses, " AND "), args, nil
}

// InsertRow inserts one row. Values map column → *string (nil = NULL).
func (c *Conn) InsertRow(ctx context.Context, database, table string, schema *TableSchema, values map[string]*string) error {
	cols := columnSet(schema)
	var names []string
	var placeholders []string
	var args []any
	for name, val := range values {
		if !cols[name] {
			return fmt.Errorf("unknown column %q", name)
		}
		names = append(names, quoteIdent(name))
		placeholders = append(placeholders, "?")
		if val == nil {
			args = append(args, nil)
		} else {
			args = append(args, *val)
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("no values provided")
	}
	qualified := quoteIdent(database) + "." + quoteIdent(table)
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", qualified, strings.Join(names, ", "), strings.Join(placeholders, ", "))
	if _, err := c.db.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	return nil
}

// UpdateRow updates one row identified by `identity`, setting `values`.
func (c *Conn) UpdateRow(ctx context.Context, database, table string, schema *TableSchema,
	identity, values map[string]*string) (int64, error) {
	cols := columnSet(schema)
	var sets []string
	var args []any
	for name, val := range values {
		if !cols[name] {
			return 0, fmt.Errorf("unknown column %q", name)
		}
		if val == nil {
			sets = append(sets, quoteIdent(name)+" = NULL")
		} else {
			sets = append(sets, quoteIdent(name)+" = ?")
			args = append(args, *val)
		}
	}
	if len(sets) == 0 {
		return 0, fmt.Errorf("no values to update")
	}
	where, whereArgs, err := identityWhere(schema, identity)
	if err != nil {
		return 0, err
	}
	args = append(args, whereArgs...)
	qualified := quoteIdent(database) + "." + quoteIdent(table)
	// LIMIT 1 caps blast radius when a keyless table's identity is ambiguous.
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s LIMIT 1", qualified, strings.Join(sets, ", "), where)
	res, err := c.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteRow deletes one row identified by `identity`.
func (c *Conn) DeleteRow(ctx context.Context, database, table string, schema *TableSchema, identity map[string]*string) (int64, error) {
	where, args, err := identityWhere(schema, identity)
	if err != nil {
		return 0, err
	}
	qualified := quoteIdent(database) + "." + quoteIdent(table)
	query := fmt.Sprintf("DELETE FROM %s WHERE %s LIMIT 1", qualified, where)
	res, err := c.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// TruncateTable empties a table.
func (c *Conn) TruncateTable(ctx context.Context, database, table string) error {
	_, err := c.db.ExecContext(ctx, "TRUNCATE TABLE "+quoteIdent(database)+"."+quoteIdent(table))
	return err
}

// DropTable removes a table.
func (c *Conn) DropTable(ctx context.Context, database, table string) error {
	_, err := c.db.ExecContext(ctx, "DROP TABLE "+quoteIdent(database)+"."+quoteIdent(table))
	return err
}
