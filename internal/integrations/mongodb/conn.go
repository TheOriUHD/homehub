package mongodb

import (
	"context"
	"sort"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"homehub/internal/integration"
	"homehub/internal/metrics"
)

// ---- normalized model ------------------------------------------------------

// ServerStats is the live health snapshot.
type ServerStats struct {
	Version              string  `json:"version"`
	UptimeSeconds        int64   `json:"uptimeSeconds"`
	ConnectionsCurrent   int64   `json:"connectionsCurrent"`
	ConnectionsAvailable int64   `json:"connectionsAvailable"`
	OpsTotal             int64   `json:"opsTotal"` // sum of opcounters
	OpsPerSec            float64 `json:"opsPerSec"`
	MemResidentMB        int64   `json:"memResidentMb"`
}

// Database is one database with size on disk.
type Database struct {
	Name        string `json:"name"`
	SizeBytes   int64  `json:"sizeBytes"`
	Collections int64  `json:"collections"`
}

// Collection is one collection's catalog entry.
type Collection struct {
	Name      string `json:"name"`
	Documents int64  `json:"documents"` // estimated
	SizeBytes int64  `json:"sizeBytes"`
}

// Operation is one entry from currentOp.
type Operation struct {
	ID          int64  `json:"id"`
	Op          string `json:"op"`
	Namespace   string `json:"namespace,omitempty"`
	RunningSecs int64  `json:"runningSecs"`
	Client      string `json:"client,omitempty"`
	Description string `json:"description,omitempty"`
}

// Document is a peeked document rendered as extended JSON.
type Document = string

// systemDatabases are hidden from listings.
var systemDatabases = map[string]bool{"admin": true, "config": true, "local": true}

// IsSystemDatabase reports whether a database is the server's own; admin holds
// system.users with every account's credential hashes.
func IsSystemDatabase(name string) bool { return systemDatabases[name] }

// ---- connection ------------------------------------------------------------

type Conn struct {
	inst   integration.Instance
	client *mongo.Client

	mu         sync.Mutex
	lastOps    int64
	lastSample time.Time
}

// serverStatusDoc is the subset of serverStatus we consume. Nested
// interface{} values decode as bson.D in driver v2, so typed decoding is
// the reliable path.
type serverStatusDoc struct {
	Version     string  `bson:"version"`
	Uptime      float64 `bson:"uptime"`
	Connections struct {
		Current   int64 `bson:"current"`
		Available int64 `bson:"available"`
	} `bson:"connections"`
	Mem struct {
		Resident int64 `bson:"resident"`
	} `bson:"mem"`
	Opcounters struct {
		Insert  int64 `bson:"insert"`
		Query   int64 `bson:"query"`
		Update  int64 `bson:"update"`
		Delete  int64 `bson:"delete"`
		Getmore int64 `bson:"getmore"`
		Command int64 `bson:"command"`
	} `bson:"opcounters"`
}

// Stats runs serverStatus; op rate derives from opcounter deltas.
func (c *Conn) Stats(ctx context.Context) (*ServerStats, error) {
	var status serverStatusDoc
	err := c.client.Database("admin").RunCommand(ctx, bson.D{{Key: "serverStatus", Value: 1}}).Decode(&status)
	if err != nil {
		return nil, err
	}
	stats := &ServerStats{
		Version:              status.Version,
		UptimeSeconds:        int64(status.Uptime),
		ConnectionsCurrent:   status.Connections.Current,
		ConnectionsAvailable: status.Connections.Available,
		MemResidentMB:        status.Mem.Resident,
		OpsTotal: status.Opcounters.Insert + status.Opcounters.Query +
			status.Opcounters.Update + status.Opcounters.Delete +
			status.Opcounters.Getmore + status.Opcounters.Command,
	}
	c.mu.Lock()
	now := time.Now()
	if !c.lastSample.IsZero() && stats.OpsTotal >= c.lastOps {
		elapsed := now.Sub(c.lastSample).Seconds()
		if elapsed > 0 {
			stats.OpsPerSec = float64(stats.OpsTotal-c.lastOps) / elapsed
		}
	}
	c.lastOps = stats.OpsTotal
	c.lastSample = now
	c.mu.Unlock()
	return stats, nil
}

// Databases lists user databases.
func (c *Conn) Databases(ctx context.Context) ([]Database, error) {
	res, err := c.client.ListDatabases(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	out := []Database{}
	for _, db := range res.Databases {
		if systemDatabases[db.Name] {
			continue
		}
		entry := Database{Name: db.Name, SizeBytes: db.SizeOnDisk}
		if names, err := c.client.Database(db.Name).ListCollectionNames(ctx, bson.D{}); err == nil {
			entry.Collections = int64(len(names))
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Collections lists one database's collections with estimated stats.
func (c *Conn) Collections(ctx context.Context, database string) ([]Collection, error) {
	db := c.client.Database(database)
	names, err := db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	out := make([]Collection, 0, len(names))
	for _, name := range names {
		entry := Collection{Name: name}
		if n, err := db.Collection(name).EstimatedDocumentCount(ctx); err == nil {
			entry.Documents = n
		}
		var collStats struct {
			Size int64 `bson:"size"`
		}
		if err := db.RunCommand(ctx, bson.D{{Key: "collStats", Value: name}}).Decode(&collStats); err == nil {
			entry.SizeBytes = collStats.Size
		}
		out = append(out, entry)
	}
	return out, nil
}

// Peek returns the newest documents of a collection as canonical extended
// JSON strings, capped.
func (c *Conn) Peek(ctx context.Context, database, collection string, limit int) ([]Document, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	cursor, err := c.client.Database(database).Collection(collection).
		Find(ctx, bson.D{}, options.Find().
			SetSort(bson.D{{Key: "_id", Value: -1}}).
			SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	out := []Document{}
	for cursor.Next(ctx) {
		raw, err := bson.MarshalExtJSON(cursor.Current, false, false)
		if err != nil {
			continue
		}
		doc := string(raw)
		if len(doc) > 4000 {
			doc = doc[:4000] + "…"
		}
		out = append(out, doc)
	}
	return out, cursor.Err()
}

// Operations lists active client operations (currentOp).
func (c *Conn) Operations(ctx context.Context) ([]Operation, error) {
	var res struct {
		Inprog []struct {
			OpID        int64  `bson:"opid"`
			Op          string `bson:"op"`
			NS          string `bson:"ns"`
			SecsRunning int64  `bson:"secs_running"`
			Client      string `bson:"client"`
			Desc        string `bson:"desc"`
		} `bson:"inprog"`
	}
	err := c.client.Database("admin").RunCommand(ctx, bson.D{
		{Key: "currentOp", Value: 1},
		{Key: "active", Value: true},
	}).Decode(&res)
	if err != nil {
		return nil, err
	}
	out := []Operation{}
	for _, op := range res.Inprog {
		// Internal maintenance threads have no client address; hide them.
		if op.Client == "" {
			continue
		}
		out = append(out, Operation{
			ID:          op.OpID,
			Op:          op.Op,
			Namespace:   op.NS,
			RunningSecs: op.SecsRunning,
			Client:      op.Client,
			Description: op.Desc,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RunningSecs > out[j].RunningSecs })
	return out, nil
}

// KillOp terminates one operation.
func (c *Conn) KillOp(ctx context.Context, opID int64) error {
	return c.client.Database("admin").RunCommand(ctx, bson.D{
		{Key: "killOp", Value: 1},
		{Key: "op", Value: opID},
	}).Err()
}

// ---- integration.Connection ------------------------------------------------

func (c *Conn) Health(ctx context.Context) integration.Health {
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.client.Ping(pctx, nil); err != nil {
		return integration.Health{Status: integration.StatusUnreachable, Message: friendlyError(err)}
	}
	return integration.Health{Status: integration.StatusConnected}
}

// Resources publishes one node per database for per-database grants.
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
		{Resource: res, Metric: "ops_per_sec", Unit: "count", TS: now, Value: stats.OpsPerSec},
		{Resource: res, Metric: "connections_current", Unit: "count", TS: now, Value: float64(stats.ConnectionsCurrent)},
		{Resource: res, Metric: "mem_resident_mb", Unit: "MB", TS: now, Value: float64(stats.MemResidentMB)},
	}, nil
}

func (c *Conn) Close() {
	_ = c.client.Disconnect(context.Background())
}
