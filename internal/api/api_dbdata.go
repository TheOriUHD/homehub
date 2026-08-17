package api

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/integrations/dbsql"
	"github.com/TheOriUHD/homehub/internal/integrations/mongodb"
	"github.com/TheOriUHD/homehub/internal/rbac"
	"github.com/TheOriUHD/homehub/internal/store"
)

// dbObjectAccess resolves the connection and enforces database visibility for
// a table/collection operation. level is the minimum grant required on the
// database resource; view for reads, control for writes.
func (s *Server) dbObjectAccess(ctx context.Context, id *httpctx.Identity, instID uuid.UUID, database string, level rbac.Level, action string) (any, string, error) {
	conn, _, resource, err := s.dbConn(ctx, id, instID)
	if err != nil {
		return nil, "", err
	}
	dbRes := resource + "/database/" + database
	if !s.visibleDbName(ctx, id, resource, database) {
		return nil, "", store.ErrNotFound
	}
	if !s.can(ctx, id, dbRes, level) {
		if level != rbac.LevelView {
			s.deniedAudit(ctx, id, action, dbRes)
		}
		return nil, dbRes, errForbidden
	}
	return conn, dbRes, nil
}

var errForbidden = errors.New("forbidden")

// dbErr classifies an access error for the per-op response mappers.
func dbErrKind(err error) string {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return "notfound"
	case errors.Is(err, errForbidden):
		return "forbidden"
	default:
		return "upstream"
	}
}

func dbObjectSchemaErr(err error) DbObjectSchemaResponseObject {
	switch dbErrKind(err) {
	case "notfound":
		return DbObjectSchema404JSONResponse{notFoundErr("database")}
	default:
		return DbObjectSchema502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}
	}
}

func dbObjectDataErr(err error) DbObjectDataResponseObject {
	switch dbErrKind(err) {
	case "notfound":
		return DbObjectData404JSONResponse{notFoundErr("database")}
	default:
		return DbObjectData502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}
	}
}

func dbInsertErr(err error) DbInsertRowResponseObject {
	switch dbErrKind(err) {
	case "notfound":
		return DbInsertRow404JSONResponse{notFoundErr("database")}
	case "forbidden":
		return DbInsertRow403JSONResponse{forbidden()}
	default:
		return DbInsertRow502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}
	}
}

func dbUpdateErr(err error) DbUpdateRowResponseObject {
	switch dbErrKind(err) {
	case "notfound":
		return DbUpdateRow404JSONResponse{notFoundErr("database")}
	case "forbidden":
		return DbUpdateRow403JSONResponse{forbidden()}
	default:
		return DbUpdateRow502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}
	}
}

func dbDeleteErr(err error) DbDeleteRowResponseObject {
	switch dbErrKind(err) {
	case "notfound":
		return DbDeleteRow404JSONResponse{notFoundErr("database")}
	case "forbidden":
		return DbDeleteRow403JSONResponse{forbidden()}
	default:
		return DbDeleteRow502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}
	}
}

func dbTruncateErr(err error) DbTruncateObjectResponseObject {
	switch dbErrKind(err) {
	case "notfound":
		return DbTruncateObject404JSONResponse{notFoundErr("database")}
	case "forbidden":
		return DbTruncateObject403JSONResponse{forbidden()}
	default:
		return DbTruncateObject502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}
	}
}

func dbDropErr(err error) DbDropObjectResponseObject {
	switch dbErrKind(err) {
	case "notfound":
		return DbDropObject404JSONResponse{notFoundErr("database")}
	case "forbidden":
		return DbDropObject403JSONResponse{forbidden()}
	default:
		return DbDropObject502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}
	}
}

func (s *Server) DbObjectSchema(ctx context.Context, req DbObjectSchemaRequestObject) (DbObjectSchemaResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, err := s.dbObjectAccess(ctx, id, req.Id, req.Database, rbac.LevelView, "db.schema")
	if err != nil {
		return dbObjectSchemaErr(err), nil
	}
	switch c := conn.(type) {
	case *dbsql.Conn:
		schema, serr := c.TableSchema(ctx, req.Database, req.Object)
		if serr != nil {
			return DbObjectSchema502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", serr.Error()))}, nil
		}
		out := DbObjectSchema{Kind: DbObjectSchemaKindTable}
		cols := make([]DbColumn, 0, len(schema.Columns))
		for _, col := range schema.Columns {
			cols = append(cols, DbColumn{
				Name: col.Name, Type: col.Type, Nullable: col.Nullable,
				Key: &col.Key, Default: col.Default, Extra: &col.Extra, IsPrimaryKey: col.IsPrimaryKey,
			})
		}
		out.Columns = &cols
		out.PrimaryKey = &schema.PrimaryKey
		idxs := make([]DbIndex, 0, len(schema.Indexes))
		for _, i := range schema.Indexes {
			idxs = append(idxs, DbIndex{Name: i.Name, Unique: i.Unique, Columns: i.Columns})
		}
		out.Indexes = &idxs
		out.RowEstimate = &schema.RowEstimate
		return DbObjectSchema200JSONResponse(out), nil
	case *mongodb.Conn:
		schema, serr := c.CollectionSchema(ctx, req.Database, req.Object)
		if serr != nil {
			return DbObjectSchema502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", serr.Error()))}, nil
		}
		out := DbObjectSchema{Kind: DbObjectSchemaKindCollection}
		idxs := make([]DbIndex, 0, len(schema.Indexes))
		for _, i := range schema.Indexes {
			idxs = append(idxs, DbIndex{Name: i.Name, Unique: i.Unique, Columns: i.Keys})
		}
		out.Indexes = &idxs
		out.SampleKeys = &schema.SampleKeys
		out.RowEstimate = &schema.Documents
		return DbObjectSchema200JSONResponse(out), nil
	}
	return DbObjectSchema404JSONResponse{notFoundErr("object")}, nil
}

func (s *Server) DbObjectData(ctx context.Context, req DbObjectDataRequestObject) (DbObjectDataResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, err := s.dbObjectAccess(ctx, id, req.Id, req.Database, rbac.LevelView, "db.read")
	if err != nil {
		return dbObjectDataErr(err), nil
	}
	limit, offset := 100, 0
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if req.Params.Offset != nil {
		offset = *req.Params.Offset
	}
	switch c := conn.(type) {
	case *dbsql.Conn:
		schema, serr := c.TableSchema(ctx, req.Database, req.Object)
		if serr != nil {
			return DbObjectData502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", serr.Error()))}, nil
		}
		filters, ferr := parseSQLFilters(req.Params.Filter)
		if ferr != nil {
			return DbObjectData400JSONResponse{BadRequestJSONResponse(errBody("bad_filter", ferr.Error()))}, nil
		}
		orderBy, dir := "", ""
		if req.Params.OrderBy != nil {
			orderBy = *req.Params.OrderBy
		}
		if req.Params.Dir != nil {
			dir = string(*req.Params.Dir)
		}
		page, perr := c.TableData(ctx, req.Database, req.Object, schema, limit, offset, orderBy, dir, filters)
		if perr != nil {
			return DbObjectData502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", perr.Error()))}, nil
		}
		rows := make([][]*string, len(page.Rows))
		copy(rows, page.Rows)
		cols := page.Columns
		return DbObjectData200JSONResponse{
			Kind: Rows, Columns: &cols, Rows: &rows,
			Total: page.Total, Limit: page.Limit, Offset: &page.Offset, DurationMs: &page.DurationMS,
		}, nil
	case *mongodb.Conn:
		filter, sortField, sortDir := "", "", ""
		if req.Params.Filter != nil && len(*req.Params.Filter) > 0 {
			filter = (*req.Params.Filter)[0]
		}
		if req.Params.SortField != nil {
			sortField = *req.Params.SortField
		}
		if req.Params.Dir != nil {
			sortDir = string(*req.Params.Dir)
		}
		page, perr := c.Documents(ctx, req.Database, req.Object, limit, offset, filter, sortField, sortDir)
		if perr != nil {
			return DbObjectData400JSONResponse{BadRequestJSONResponse(errBody("bad_query", perr.Error()))}, nil
		}
		docs := page.Documents
		return DbObjectData200JSONResponse{
			Kind: Documents, Documents: &docs,
			Total: page.Total, Limit: page.Limit, Offset: &page.Skip, DurationMs: &page.DurationMS,
		}, nil
	}
	return DbObjectData404JSONResponse{notFoundErr("object")}, nil
}

func (s *Server) DbInsertRow(ctx context.Context, req DbInsertRowRequestObject) (DbInsertRowResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, dbRes, err := s.dbObjectAccess(ctx, id, req.Id, req.Database, rbac.LevelControl, "db.insert")
	if err != nil {
		return dbInsertErr(err), nil
	}
	switch c := conn.(type) {
	case *dbsql.Conn:
		if req.Body.Values == nil {
			return DbInsertRow400JSONResponse{BadRequestJSONResponse(errBody("no_values", "provide column values to insert"))}, nil
		}
		schema, serr := c.TableSchema(ctx, req.Database, req.Object)
		if serr != nil {
			return DbInsertRow502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", serr.Error()))}, nil
		}
		if err := c.InsertRow(ctx, req.Database, req.Object, schema, derefMap(req.Body.Values)); err != nil {
			return DbInsertRow502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
		}
	case *mongodb.Conn:
		if req.Body.Document == nil {
			return DbInsertRow400JSONResponse{BadRequestJSONResponse(errBody("no_document", "provide a document to insert"))}, nil
		}
		if err := c.InsertDocument(ctx, req.Database, req.Object, *req.Body.Document); err != nil {
			return DbInsertRow502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
		}
	}
	s.audit.Log(ctx, actorFor(id), "db.insert", dbRes, "success", map[string]any{"object": req.Object})
	return DbInsertRow204Response{}, nil
}

func (s *Server) DbUpdateRow(ctx context.Context, req DbUpdateRowRequestObject) (DbUpdateRowResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, dbRes, err := s.dbObjectAccess(ctx, id, req.Id, req.Database, rbac.LevelControl, "db.update")
	if err != nil {
		return dbUpdateErr(err), nil
	}
	var affected int64
	switch c := conn.(type) {
	case *dbsql.Conn:
		if req.Body.Values == nil || req.Body.Identity == nil {
			return DbUpdateRow400JSONResponse{BadRequestJSONResponse(errBody("bad_update", "values and identity are required"))}, nil
		}
		schema, serr := c.TableSchema(ctx, req.Database, req.Object)
		if serr != nil {
			return DbUpdateRow502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", serr.Error()))}, nil
		}
		affected, err = c.UpdateRow(ctx, req.Database, req.Object, schema, derefMap(req.Body.Identity), derefMap(req.Body.Values))
	case *mongodb.Conn:
		if req.Body.Id == nil || req.Body.Document == nil {
			return DbUpdateRow400JSONResponse{BadRequestJSONResponse(errBody("bad_update", "id and document are required"))}, nil
		}
		affected, err = c.ReplaceDocument(ctx, req.Database, req.Object, *req.Body.Id, *req.Body.Document)
	}
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "db.update", dbRes, "error", map[string]any{"object": req.Object, "error": err.Error()})
		return DbUpdateRow502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "db.update", dbRes, "success", map[string]any{"object": req.Object, "affected": affected})
	return DbUpdateRow200JSONResponse{Affected: affected}, nil
}

func (s *Server) DbDeleteRow(ctx context.Context, req DbDeleteRowRequestObject) (DbDeleteRowResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, dbRes, err := s.dbObjectAccess(ctx, id, req.Id, req.Database, rbac.LevelControl, "db.delete_row")
	if err != nil {
		return dbDeleteErr(err), nil
	}
	var affected int64
	switch c := conn.(type) {
	case *dbsql.Conn:
		if req.Body.Identity == nil {
			return DbDeleteRow400JSONResponse{BadRequestJSONResponse(errBody("no_identity", "identity is required to delete a row"))}, nil
		}
		schema, serr := c.TableSchema(ctx, req.Database, req.Object)
		if serr != nil {
			return DbDeleteRow502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", serr.Error()))}, nil
		}
		affected, err = c.DeleteRow(ctx, req.Database, req.Object, schema, derefMap(req.Body.Identity))
	case *mongodb.Conn:
		if req.Body.Id == nil {
			return DbDeleteRow400JSONResponse{BadRequestJSONResponse(errBody("no_id", "id is required to delete a document"))}, nil
		}
		affected, err = c.DeleteDocument(ctx, req.Database, req.Object, *req.Body.Id)
	}
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "db.delete_row", dbRes, "error", map[string]any{"object": req.Object, "error": err.Error()})
		return DbDeleteRow502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "db.delete_row", dbRes, "success", map[string]any{"object": req.Object, "affected": affected})
	return DbDeleteRow200JSONResponse{Affected: affected}, nil
}

func (s *Server) DbTruncateObject(ctx context.Context, req DbTruncateObjectRequestObject) (DbTruncateObjectResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if req.Body == nil || !req.Body.Confirm {
		return DbTruncateObject400JSONResponse{BadRequestJSONResponse(errBody("not_confirmed", "confirm is required to empty a table"))}, nil
	}
	conn, dbRes, err := s.dbObjectAccess(ctx, id, req.Id, req.Database, rbac.LevelControl, "db.truncate")
	if err != nil {
		return dbTruncateErr(err), nil
	}
	c, ok := conn.(*dbsql.Conn)
	if !ok {
		return DbTruncateObject400JSONResponse{BadRequestJSONResponse(errBody("wrong_engine", "truncate is for SQL tables; drop the collection instead"))}, nil
	}
	if err := c.TruncateTable(ctx, req.Database, req.Object); err != nil {
		s.audit.Log(ctx, actorFor(id), "db.truncate", dbRes, "error", map[string]any{"object": req.Object, "error": err.Error()})
		return DbTruncateObject502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "db.truncate", dbRes, "success", map[string]any{"object": req.Object})
	return DbTruncateObject204Response{}, nil
}

func (s *Server) DbDropObject(ctx context.Context, req DbDropObjectRequestObject) (DbDropObjectResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !req.Params.Confirm {
		return DbDropObject400JSONResponse{BadRequestJSONResponse(errBody("not_confirmed", "confirm is required to drop"))}, nil
	}
	conn, dbRes, err := s.dbObjectAccess(ctx, id, req.Id, req.Database, rbac.LevelControl, "db.drop")
	if err != nil {
		return dbDropErr(err), nil
	}
	switch c := conn.(type) {
	case *dbsql.Conn:
		err = c.DropTable(ctx, req.Database, req.Object)
	case *mongodb.Conn:
		err = c.DropCollection(ctx, req.Database, req.Object)
	}
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "db.drop", dbRes, "error", map[string]any{"object": req.Object, "error": err.Error()})
		return DbDropObject502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "db.drop", dbRes, "success", map[string]any{"object": req.Object})
	return DbDropObject204Response{}, nil
}

// ---- helpers ---------------------------------------------------------------

func derefMap(m *map[string]*string) map[string]*string {
	if m == nil {
		return nil
	}
	return *m
}

// parseSQLFilters parses repeated column:op:value triples. Values may contain
// colons; only the first two separators are structural.
func parseSQLFilters(raw *[]string) ([]dbsql.RowFilter, error) {
	if raw == nil {
		return nil, nil
	}
	out := make([]dbsql.RowFilter, 0, len(*raw))
	for _, item := range *raw {
		if strings.TrimSpace(item) == "" {
			continue
		}
		parts := strings.SplitN(item, ":", 3)
		if len(parts) < 2 {
			return nil, errBadFilter
		}
		f := dbsql.RowFilter{Column: parts[0], Operator: parts[1]}
		if len(parts) == 3 {
			f.Value = parts[2]
		}
		out = append(out, f)
	}
	return out, nil
}

var errBadFilter = errors.New("filters use column:op[:value], e.g. name:like:%alice% or email:is_null")
