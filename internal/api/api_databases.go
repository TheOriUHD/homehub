package api

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"homehub/internal/httpctx"
	"homehub/internal/integrations/dbsql"
	"homehub/internal/integrations/mongodb"
	"homehub/internal/rbac"
	"homehub/internal/store"
)

func isDBType(typ string) bool {
	return typ == dbsql.TypeID || typ == mongodb.TypeID
}

// dbConn resolves an integration id to a live database connection of either
// engine, enforcing instance visibility (404 for invisible and non-DB ids).
func (s *Server) dbConn(ctx context.Context, id *httpctx.Identity, instID uuid.UUID) (any, *store.Integration, string, error) {
	row, err := s.store.GetIntegration(ctx, instID)
	if err != nil || !isDBType(row.Type) {
		return nil, nil, "", store.ErrNotFound
	}
	resource := row.Type + "/" + instID.String()
	if !s.visibleUnder(ctx, id, resource) {
		return nil, nil, "", store.ErrNotFound
	}
	conn, err := s.integrations.Connection(instID)
	if err != nil {
		return nil, row, resource, err
	}
	return conn, row, resource, nil
}

func toAPIDbStats(engine string, conn any, ctx context.Context) *DbStats {
	switch c := conn.(type) {
	case *dbsql.Conn:
		stats, err := c.Stats(ctx)
		if err != nil {
			return nil
		}
		out := &DbStats{Version: stats.Version}
		flavor := stats.Flavor
		out.Flavor = &flavor
		uptime := stats.UptimeSeconds
		out.UptimeSeconds = &uptime
		conns := stats.ThreadsConnected
		out.ConnectionsCurrent = &conns
		maxConns := stats.MaxConnections
		out.ConnectionsMax = &maxConns
		qps := stats.QPS
		out.OpsPerSec = &qps
		slow := stats.SlowQueriesTotal
		out.SlowQueriesTotal = &slow
		return out
	case *mongodb.Conn:
		stats, err := c.Stats(ctx)
		if err != nil {
			return nil
		}
		out := &DbStats{Version: stats.Version}
		uptime := stats.UptimeSeconds
		out.UptimeSeconds = &uptime
		conns := stats.ConnectionsCurrent
		out.ConnectionsCurrent = &conns
		maxConns := stats.ConnectionsCurrent + stats.ConnectionsAvailable
		out.ConnectionsMax = &maxConns
		ops := stats.OpsPerSec
		out.OpsPerSec = &ops
		mem := stats.MemResidentMB
		out.MemResidentMb = &mem
		return out
	}
	return nil
}

func (s *Server) DbListInstances(ctx context.Context, _ DbListInstancesRequestObject) (DbListInstancesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	rows, err := s.store.ListIntegrations(ctx)
	if err != nil {
		return nil, err
	}
	out := []DbInstance{}
	for _, row := range rows {
		if !isDBType(row.Type) || !row.Enabled {
			continue
		}
		resource := row.Type + "/" + row.ID.String()
		if !s.visibleUnder(ctx, id, resource) {
			continue
		}
		instance := DbInstance{
			Id:            row.ID,
			Name:          row.Name,
			Engine:        DbInstanceEngine(row.Type),
			Status:        DbInstanceStatus(row.Status),
			StatusMessage: row.StatusMessage,
		}
		// The connection identity (never the password) — shown beside the
		// console so it is clear whose privileges statements run under.
		cfg := map[string]any{}
		_ = json.Unmarshal(row.Config, &cfg)
		if host, _ := cfg["host"].(string); host != "" {
			instance.Host = &host
		}
		if userRaw, verr := s.vault.Open(ctx, "integration:"+row.ID.String(), "username"); verr == nil {
			user := string(userRaw)
			instance.ConnectionUser = &user
		}
		if conn, cerr := s.integrations.Connection(row.ID); cerr == nil {
			sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			instance.Stats = toAPIDbStats(row.Type, conn, sctx)
			cancel()
		}
		out = append(out, instance)
	}
	return DbListInstances200JSONResponse(out), nil
}

// visibleDbNames filters database names by per-database view grants.
func (s *Server) visibleDbName(ctx context.Context, id *httpctx.Identity, resource, name string) bool {
	return s.can(ctx, id, resource+"/database/"+name, rbac.LevelView)
}

func (s *Server) DbListDatabases(ctx context.Context, req DbListDatabasesRequestObject) (DbListDatabasesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.dbConn(ctx, id, req.Id)
	if err != nil {
		if err == store.ErrNotFound {
			return DbListDatabases404JSONResponse{notFoundErr("instance")}, nil
		}
		return DbListDatabases502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	out := []DbDatabase{}
	switch c := conn.(type) {
	case *dbsql.Conn:
		dbs, derr := c.Databases(ctx)
		if derr != nil {
			return DbListDatabases502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", derr.Error()))}, nil
		}
		for _, d := range dbs {
			if s.visibleDbName(ctx, id, resource, d.Name) {
				out = append(out, DbDatabase{Name: d.Name, SizeBytes: d.SizeBytes, ObjectCount: d.Tables})
			}
		}
	case *mongodb.Conn:
		dbs, derr := c.Databases(ctx)
		if derr != nil {
			return DbListDatabases502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", derr.Error()))}, nil
		}
		for _, d := range dbs {
			if s.visibleDbName(ctx, id, resource, d.Name) {
				out = append(out, DbDatabase{Name: d.Name, SizeBytes: d.SizeBytes, ObjectCount: d.Collections})
			}
		}
	}
	return DbListDatabases200JSONResponse(out), nil
}

func (s *Server) DbListObjects(ctx context.Context, req DbListObjectsRequestObject) (DbListObjectsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.dbConn(ctx, id, req.Id)
	if err != nil {
		if err == store.ErrNotFound {
			return DbListObjects404JSONResponse{notFoundErr("instance")}, nil
		}
		return DbListObjects502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	if !s.visibleDbName(ctx, id, resource, req.Database) {
		return DbListObjects404JSONResponse{notFoundErr("database")}, nil
	}
	out := []DbObject{}
	switch c := conn.(type) {
	case *dbsql.Conn:
		tables, terr := c.Tables(ctx, req.Database)
		if terr != nil {
			return DbListObjects502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", terr.Error()))}, nil
		}
		for _, t := range tables {
			engine := t.Engine
			out = append(out, DbObject{Name: t.Name, Kind: DbObjectKindTable, Engine: &engine, Rows: t.Rows, SizeBytes: t.SizeBytes})
		}
	case *mongodb.Conn:
		colls, cerr := c.Collections(ctx, req.Database)
		if cerr != nil {
			return DbListObjects502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", cerr.Error()))}, nil
		}
		for _, coll := range colls {
			out = append(out, DbObject{Name: coll.Name, Kind: DbObjectKindCollection, Rows: coll.Documents, SizeBytes: coll.SizeBytes})
		}
	}
	return DbListObjects200JSONResponse(out), nil
}

func (s *Server) DbPeek(ctx context.Context, req DbPeekRequestObject) (DbPeekResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.dbConn(ctx, id, req.Id)
	if err != nil {
		if err == store.ErrNotFound {
			return DbPeek404JSONResponse{notFoundErr("instance")}, nil
		}
		return DbPeek502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	mongoConn, ok := conn.(*mongodb.Conn)
	if !ok {
		return DbPeek400JSONResponse{BadRequestJSONResponse(errBody("wrong_engine", "peek is for MongoDB collections — use the SQL console for this instance"))}, nil
	}
	dbRes := resource + "/database/" + req.Database
	if !s.visibleDbName(ctx, id, resource, req.Database) {
		return DbPeek404JSONResponse{notFoundErr("database")}, nil
	}
	if !s.can(ctx, id, dbRes, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "db.peek", dbRes)
		return DbPeek403JSONResponse{forbidden()}, nil
	}
	limit := 25
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	docs, err := mongoConn.Peek(ctx, req.Database, req.Params.Collection, limit)
	if err != nil {
		return DbPeek502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "db.peek", dbRes, "success",
		map[string]any{"collection": req.Params.Collection, "limit": limit})
	return DbPeek200JSONResponse{Documents: docs}, nil
}

func (s *Server) DbQuery(ctx context.Context, req DbQueryRequestObject) (DbQueryResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.dbConn(ctx, id, req.Id)
	if err != nil {
		if err == store.ErrNotFound {
			return DbQuery404JSONResponse{notFoundErr("instance")}, nil
		}
		return DbQuery502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	sqlConn, ok := conn.(*dbsql.Conn)
	if !ok {
		return DbQuery400JSONResponse{BadRequestJSONResponse(errBody("wrong_engine", "the SQL console is for MySQL/MariaDB — MongoDB uses the collection browser"))}, nil
	}
	// The console executes arbitrary SQL as the integration's privileged DB
	// account. A single fully-qualified reference (`SELECT ... FROM otherdb.t`,
	// `mysql.user`) escapes any per-database USE scope, and that CANNOT be
	// safely confined by parsing the statement — `schema.table` is
	// syntactically indistinguishable from `table.column`, so a schema
	// allow-list would either over-block ordinary SQL or be trivially evaded.
	// So the raw console requires INSTANCE-level control: the same grant the UI
	// already requires to show the console tab. A per-database grant still
	// unlocks the structured browser (databases / tables / peek), which is
	// read-scoped server-side, but not the free-form console. This closes the
	// cross-schema/system-schema bypass a one-database grant otherwise had.
	if !s.can(ctx, id, resource, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "db.query", resource)
		return DbQuery403JSONResponse{forbidden()}, nil
	}
	target := resource
	database := ""
	if req.Body.Database != nil && *req.Body.Database != "" {
		database = *req.Body.Database
		target = resource + "/database/" + database
		if !s.visibleDbName(ctx, id, resource, database) {
			return DbQuery404JSONResponse{notFoundErr("database")}, nil
		}
	}
	qctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, err := sqlConn.Query(qctx, database, req.Body.Statement)
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "db.query", target, "error",
			map[string]any{"statement": req.Body.Statement, "error": err.Error()})
		return DbQuery502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "db.query", target, "success",
		map[string]any{"statement": req.Body.Statement, "rows": result.RowsAffected})
	return DbQuery200JSONResponse{
		Columns:      result.Columns,
		Rows:         result.Rows,
		RowsAffected: result.RowsAffected,
		Truncated:    result.Truncated,
		DurationMs:   result.DurationMS,
	}, nil
}

func (s *Server) DbSessions(ctx context.Context, req DbSessionsRequestObject) (DbSessionsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, _, err := s.dbConn(ctx, id, req.Id)
	if err != nil {
		if err == store.ErrNotFound {
			return DbSessions404JSONResponse{notFoundErr("instance")}, nil
		}
		return DbSessions502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	out := []DbSession{}
	switch c := conn.(type) {
	case *dbsql.Conn:
		sessions, serr := c.Sessions(ctx)
		if serr != nil {
			return DbSessions502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", serr.Error()))}, nil
		}
		for _, sess := range sessions {
			entry := DbSession{Id: sess.ID, Client: sess.Host, Command: sess.Command, TimeSec: sess.TimeSec}
			setOpt(&entry.User, sess.User)
			setOpt(&entry.Database, sess.Database)
			setOpt(&entry.State, sess.State)
			setOpt(&entry.Query, sess.Query)
			out = append(out, entry)
		}
	case *mongodb.Conn:
		ops, oerr := c.Operations(ctx)
		if oerr != nil {
			return DbSessions502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", oerr.Error()))}, nil
		}
		for _, op := range ops {
			entry := DbSession{Id: op.ID, Client: op.Client, Command: op.Op, TimeSec: op.RunningSecs}
			setOpt(&entry.Database, op.Namespace)
			setOpt(&entry.State, op.Description)
			out = append(out, entry)
		}
	}
	return DbSessions200JSONResponse(out), nil
}

func setOpt(dst **string, v string) {
	if v != "" {
		*dst = &v
	}
}

func (s *Server) DbKillSession(ctx context.Context, req DbKillSessionRequestObject) (DbKillSessionResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.dbConn(ctx, id, req.Id)
	if err != nil {
		if err == store.ErrNotFound {
			return DbKillSession404JSONResponse{notFoundErr("instance")}, nil
		}
		return DbKillSession502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "db.kill_session", resource)
		return DbKillSession403JSONResponse{forbidden()}, nil
	}
	switch c := conn.(type) {
	case *dbsql.Conn:
		err = c.Kill(ctx, req.SessionId)
	case *mongodb.Conn:
		err = c.KillOp(ctx, req.SessionId)
	}
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "db.kill_session", resource, "error",
			map[string]any{"sessionId": req.SessionId, "error": err.Error()})
		return DbKillSession502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "db.kill_session", resource, "success",
		map[string]any{"sessionId": req.SessionId})
	return DbKillSession204Response{}, nil
}

func (s *Server) DbDump(ctx context.Context, req DbDumpRequestObject) (DbDumpResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	_, row, resource, err := s.dbConn(ctx, id, req.Id)
	if err != nil && err != store.ErrNotFound && row == nil {
		return DbDump404JSONResponse{notFoundErr("instance")}, nil
	}
	if row == nil {
		return DbDump404JSONResponse{notFoundErr("instance")}, nil
	}
	dbRes := resource + "/database/" + req.Body.Database
	if !s.visibleDbName(ctx, id, resource, req.Body.Database) {
		return DbDump404JSONResponse{notFoundErr("database")}, nil
	}
	if !s.can(ctx, id, dbRes, rbac.LevelControl) || !s.can(ctx, id, ResBackups, rbac.LevelView) {
		s.deniedAudit(ctx, id, "db.dump", dbRes)
		return DbDump403JSONResponse{forbidden()}, nil
	}
	if _, err := s.store.GetBackupTarget(ctx, req.Body.TargetId); err != nil {
		return DbDump400JSONResponse{BadRequestJSONResponse(errBody("unknown_target", "backup target not found"))}, nil
	}
	job, err := s.jobs.Enqueue(ctx, "db.dump", map[string]any{
		"integrationId": req.Id,
		"database":      req.Body.Database,
		"targetId":      req.Body.TargetId,
		"userId":        id.User.ID,
	})
	if err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "db.dump", dbRes, "success",
		map[string]any{"target": req.Body.TargetId.String()})
	return DbDump202JSONResponse{JobQueuedJSONResponse{JobId: job.ID}}, nil
}
