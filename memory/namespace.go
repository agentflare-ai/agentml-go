package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strings"
	"time"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-xmldom"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

// DBTX abstracts *sql.DB and *sql.Tx for Exec/Query operations.
// It matches the subset of methods used by memory executables.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (d *Deps) dbtx() DBTX {
	if d != nil && d.tx != nil {
		return d.tx
	}
	return d.DB
}

// MemoryNamespaceURI is the XML namespace for memory executables.
const MemoryNamespaceURI = "github.com/agentflare-ai/agentml-go/memory"

// Deps holds dependencies for memory executables.
type Deps struct {
	DB          *sql.DB
	Graph       *GraphDB
	Vector      *VectorDB
	DefaultDims int
	// Embed computes the embedding for the provided text using the given model.
	Embed func(ctx context.Context, model, text string) ([]float32, error)
	// internal transaction (single-session convenience). Production code would track tx per store.
	tx *sql.Tx
}

// InitializeMemorySystem creates a fully initialized memory system with DB, Graph, and Vector stores.
// This is a convenience function that sets up everything needed for memory executables.
// dsn can be ":memory:" for in-memory database or a file path for persistent storage.
// vectorDims specifies the dimension for vector embeddings (default: 1536).
func InitializeMemorySystem(ctx context.Context, dsn string, vectorDims int) (*Deps, error) {
	if dsn == "" {
		dsn = ":memory:"
	}
	if vectorDims <= 0 {
		vectorDims = 1536 // Default for OpenAI embeddings
	}

	// Create the database
	db, err := NewDB(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	// Initialize graph store
	graph, err := NewGraphDB(ctx, db, "graph")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create graph store: %w", err)
	}

	// Initialize vector store
	vector, err := NewVectorDB(ctx, db, "vectors", vectorDims)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create vector store: %w", err)
	}

	// Ensure KV table exists
	_, err = db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS kv(key TEXT PRIMARY KEY, value TEXT)")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create KV table: %w", err)
	}

	return &Deps{
		DB:          db,
		Graph:       graph,
		Vector:      vector,
		DefaultDims: vectorDims,
	}, nil
}

// Loader returns a NamespaceLoader for the memory namespace.
func Loader() agentml.NamespaceLoader {
	return func(ctx context.Context, itp agentml.Interpreter, doc xmldom.Document) (agentml.Namespace, error) {
		inst := &ns{
			itp:    itp,
			dbs:    make(map[string]*Deps),
			dbDefs: make(map[string]dbDef),
		}
		// Parse declared memory:db elements (root-level by convention)
		if doc != nil {
			root := doc.DocumentElement()
			if root != nil {
				// Find all memory:db declarations anywhere in the document
				dbs := root.GetElementsByTagNameNS(MemoryNamespaceURI, "db")
				for i := uint(0); i < dbs.Length(); i++ {
					el, ok := dbs.Item(i).(xmldom.Element)
					if !ok || el == nil {
						continue
					}
					id := strings.TrimSpace(string(el.GetAttribute("id")))
					if id == "" {
						return nil, fmt.Errorf("memory:db missing required id attribute")
					}
					if _, exists := inst.dbDefs[id]; exists {
						return nil, fmt.Errorf("memory: duplicate memory:db id '%s'", id)
					}
					def := dbDef{
						dsn:     string(el.GetAttribute("dsn")),
						dsnExpr: string(el.GetAttribute("dsnexpr")),
					}
					inst.dbDefs[id] = def
					if inst.defaultDB == "" {
						inst.defaultDB = id
					}
					// Warn if not under document root
					if p := el.ParentNode(); p != nil {
						if pe, ok := p.(xmldom.Element); ok && pe != nil {
							if pe != root {
								slog.WarnContext(ctx, "memory:db should be declared at the document root", "id", id)
							}
						}
					}
				}
			}
		}
		return inst, nil
	}
}

type dbDef struct {
	dsn     string
	dsnExpr string
}

type ns struct {
	itp       agentml.Interpreter
	deps      *Deps                  // selected deps for current execution
	dbs       map[string]*Deps       // opened databases by id
	dbDefs    map[string]dbDef       // declared database definitions by id
	defaultDB string                 // first declared db id or "default" implicit
}

var _ agentml.Namespace = (*ns)(nil)

func (n *ns) URI() string { return MemoryNamespaceURI }

func (n *ns) Unload(ctx context.Context) error { return nil }

func (n *ns) Handle(ctx context.Context, el xmldom.Element) (bool, error) {
	if el == nil {
		return false, fmt.Errorf("memory: element cannot be nil")
	}
	local := strings.ToLower(string(el.LocalName()))
	switch local {
	case "db":
		// Declaration only; handled during Loader
		return true, nil
	case "close", "put", "get", "delete", "copy", "move", "query",
		"kvtruncate", "exec", "begin", "commit", "rollback", "savepoint", "release",
		"sql", "embed", "upsertvector", "search", "deletevector", "vectorindex",
		"addnode", "addedge", "getnode", "getedge", "deletenode", "deleteedge",
		"neighbors", "getneighbors", "graphpath", "graphtruncate", "graphquery":
		return true, n.execute(ctx, local, el)
case "graph":
		// Legacy element needs DB selection too
		dm := n.itp.DataModel()
		if dm == nil {
			return true, &agentml.PlatformError{EventName: "error.execution", Message: "No data model available for memory", Data: map[string]any{"element": "graph"}, Cause: fmt.Errorf("no datamodel")}
		}
		deps, err := n.selectDeps(ctx, el, dm)
		if err != nil {
			return true, err
		}
		prev := n.deps
		n.deps = deps
		defer func() { n.deps = prev }()
		return true, n.execGraph(ctx, el)
	default:
		return false, nil
	}
}

// execute routes memory operations without allocating per-element executables.
func (n *ns) execute(ctx context.Context, local string, el xmldom.Element) error {
	tr := otel.Tracer("memory")
	ctx, span := tr.Start(ctx, "memory."+local)
	defer span.End()

	dm := n.itp.DataModel()
	if dm == nil {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "No data model available for memory",
			Data:      map[string]any{"element": local},
			Cause:     fmt.Errorf("no datamodel"),
		}
	}

	// Select per-DB dependencies (lazy-open using dsn/dsnexpr)
	if local != "db" { // defensive
		deps, err := n.selectDeps(ctx, el, dm)
		if err != nil {
			return err
		}
		prev := n.deps
		n.deps = deps
		defer func() { n.deps = prev }()
	}

	switch local {
	case "close":
		return n.execClose(ctx, dm)
	case "put":
		return n.execPut(ctx, el, dm)
	case "get":
		return n.execGet(ctx, el, dm)
	case "delete":
		return n.execDelete(ctx, el, dm)
	case "copy":
		return n.execCopy(ctx, el, dm)
	case "move":
		return n.execMove(ctx, el, dm)
	case "query":
		return n.execQuery(ctx, el, dm)
	case "kvtruncate":
		return n.execKVTruncate(ctx)
	case "exec":
		return n.execSQL(ctx, el, dm)
	case "begin":
		return n.execBegin(ctx)
	case "commit":
		return n.execCommit(ctx)
	case "rollback":
		return n.execRollback(ctx)
	case "savepoint":
		return n.execSavepoint(ctx, el, dm)
	case "release":
		return n.execRelease(ctx, el, dm)
	case "sql":
		return n.execSQL(ctx, el, dm)
	case "embed":
		return n.execEmbed(ctx, el, dm)
	case "upsertvector":
		return n.execUpsertVector(ctx, el, dm)
	case "search":
		return n.execSearch(ctx, el, dm)
	case "deletevector":
		return n.execDeleteVector(ctx, el, dm)
	case "vectorindex":
		// auto-initialized on open; treat as success
		return nil
	case "addnode":
		return n.execAddNode(ctx, el, dm)
	case "addedge":
		return n.execAddEdge(ctx, el, dm)
	case "getnode":
		return n.execGetNode(ctx, el, dm)
	case "getedge":
		return n.execGetEdge(ctx, el, dm)
	case "deletenode":
		return n.execDeleteNode(ctx, el, dm)
	case "deleteedge":
		return n.execDeleteEdge(ctx, el, dm)
	case "neighbors", "getneighbors":
		return n.execNeighbors(ctx, el, dm)
	case "graphpath":
		return n.execGraphPath(ctx, el, dm)
	case "graphtruncate":
		return n.execGraphTruncate(ctx)
	case "graphquery":
		return n.execGraphQuery(ctx, el, dm)
	default:
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "unsupported memory element",
			Data:      map[string]any{"element": local},
			Cause:     fmt.Errorf("unsupported"),
		}
	}
}

// ---- Database selection and lazy initialization ----

// selectDeps determines which DB to use for this element and lazily opens it if needed.
func (n *ns) selectDeps(ctx context.Context, el xmldom.Element, dm agentml.DataModel) (*Deps, error) {
	// 1) db attribute on the element
	if el != nil {
		if dbAttr := strings.TrimSpace(string(el.GetAttribute("db"))); dbAttr != "" {
			return n.ensureOpen(ctx, dm, dbAttr)
		}
		// 2) nearest ancestor memory:db (if author nests ops inside db block)
		for p := el.ParentNode(); p != nil; p = p.ParentNode() {
			pe, ok := p.(xmldom.Element)
			if !ok || pe == nil {
				continue
			}
			if strings.ToLower(string(pe.LocalName())) == "db" && string(pe.NamespaceURI()) == MemoryNamespaceURI {
				id := strings.TrimSpace(string(pe.GetAttribute("id")))
				if id != "" {
					return n.ensureOpen(ctx, dm, id)
				}
			}
		}
	}
	// 3) if exactly one declared db, use it
	if len(n.dbDefs) == 1 && n.defaultDB != "" {
		return n.ensureOpen(ctx, dm, n.defaultDB)
	}
	// 4) if none declared, use implicit default
	if len(n.dbDefs) == 0 {
		if n.defaultDB == "" {
			n.defaultDB = "default"
		}
		return n.ensureOpen(ctx, dm, n.defaultDB)
	}
	// 5) ambiguous
	return nil, &agentml.PlatformError{
		EventName: "error.execution",
		Message:   "memory: multiple databases declared; specify db attribute",
		Data:      map[string]any{"element": string(el.LocalName())},
		Cause:     fmt.Errorf("ambiguous database selection"),
	}
}

func (n *ns) ensureOpen(ctx context.Context, dm agentml.DataModel, id string) (*Deps, error) {
	if n.dbs == nil {
		n.dbs = make(map[string]*Deps)
	}
	if deps, ok := n.dbs[id]; ok && deps != nil {
		return deps, nil
	}
	// Resolve DSN lazily
	var dsn string
	if def, ok := n.dbDefs[id]; ok {
		if strings.TrimSpace(def.dsnExpr) != "" {
			// Evaluate expression via data model
			var err error
			dsn, err = evalString(ctx, dm, def.dsnExpr)
			if err != nil {
				return nil, &agentml.PlatformError{
					EventName: "error.execution",
					Message:   "memory: failed to evaluate dsnexpr",
					Data:      map[string]any{"db": id, "dsnexpr": def.dsnExpr},
					Cause:     err,
				}
			}
		}
		if dsn == "" && strings.TrimSpace(def.dsn) != "" {
			dsn = def.dsn
		}
	}
	if dsn == "" {
		dsn = ":memory:?_foreign_keys=on"
	}

	// Open DB and initialize subsystems
	db, err := NewDB(ctx, dsn)
	if err != nil {
		return nil, &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "memory: failed to open database",
			Data:      map[string]any{"db": id, "dsn": dsn},
			Cause:     err,
		}
	}
	graph, err := NewGraphDB(ctx, db, "graph")
	if err != nil {
		_ = db.Close()
		return nil, &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "memory: failed to create graph store",
			Data:      map[string]any{"db": id},
			Cause:     err,
		}
	}
	vector, err := NewVectorDB(ctx, db, "vectors", 1536)
	if err != nil {
		_ = db.Close()
		return nil, &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "memory: failed to create vector store",
			Data:      map[string]any{"db": id},
			Cause:     err,
		}
	}
	// Ensure KV table exists
	if _, err := db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS kv(key TEXT PRIMARY KEY, value TEXT)"); err != nil {
		_ = db.Close()
		return nil, &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "memory: failed to create KV table",
			Data:      map[string]any{"db": id},
			Cause:     err,
		}
	}
	deps := &Deps{DB: db, Graph: graph, Vector: vector, DefaultDims: 1536}
	n.dbs[id] = deps
	slog.InfoContext(ctx, "memory: database opened", "db", id)
	return deps, nil
}

// ---- Core storage helpers (KV) ----

func (n *ns) ensureKV(ctx context.Context) error {
	if n.deps == nil || n.deps.DB == nil {
		return fmt.Errorf("memory DB not configured")
	}
	_, err := n.deps.dbtx().ExecContext(ctx, "CREATE TABLE IF NOT EXISTS kv(key TEXT PRIMARY KEY, value TEXT)")
	return err
}

func (n *ns) execClose(ctx context.Context, dm agentml.DataModel) error {
	if n.deps != nil {
		if n.deps.Graph != nil {
			_ = n.deps.Graph.Close()
			n.deps.Graph = nil
		}
		if n.deps.Vector != nil {
			_ = n.deps.Vector.Close()
			n.deps.Vector = nil
		}
		if n.deps.DB != nil {
			_ = n.deps.DB.Close()
			n.deps.DB = nil
		}
		// Remove this deps from opened map
		if n.dbs != nil {
			for id, d := range n.dbs {
				if d == n.deps {
					slog.InfoContext(ctx, "memory: database closed", "db", id)
					delete(n.dbs, id)
					break
				}
			}
		}
	}
	return nil
}

func (n *ns) execPut(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if err := n.ensureKV(ctx); err != nil {
		return err
	}
	// Support both key and keyexpr
	key, err := getStringOrExpr(ctx, dm, el, "key", "keyexpr")
	if err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "missing key or keyexpr",
			Cause:     fmt.Errorf("missing key"),
		}
	}

	// Handle both value and valueexpr attributes
	var v any
	if valExpr := string(el.GetAttribute("valueexpr")); valExpr != "" {
		v, err = dm.EvaluateValue(ctx, valExpr)
		if err != nil {
			return err
		}
	} else if val := string(el.GetAttribute("value")); val != "" {
		// Use the literal string value
		v = val
	} else {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "missing value or valueexpr",
			Cause:     fmt.Errorf("missing value"),
		}
	}

	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = n.deps.dbtx().ExecContext(ctx, "INSERT INTO kv(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, string(data))
	return err
}

func (n *ns) execGet(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if err := n.ensureKV(ctx); err != nil {
		return err
	}
	// Support both key and keyexpr
	key, err := getStringOrExpr(ctx, dm, el, "key", "keyexpr")
	if err != nil {
		return err
	}
	// Handle both location and dataid attributes
	loc := string(el.GetAttribute("location"))
	if loc == "" {
		loc = string(el.GetAttribute("dataid"))
	}
	row := n.deps.dbtx().QueryRowContext(ctx, "SELECT value FROM kv WHERE key=?", key)
	var s string
	scanErr := row.Scan(&s)
	if scanErr != nil {
		assignIf(ctx, dm, loc, nil)
		return nil
	}
	var out any
	_ = json.Unmarshal([]byte(s), &out)
	assignIf(ctx, dm, loc, out)
	return nil
}

func (n *ns) execDelete(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if err := n.ensureKV(ctx); err != nil {
		return err
	}
	// Support both key and keyexpr
	key, err := getStringOrExpr(ctx, dm, el, "key", "keyexpr")
	if err != nil {
		return err
	}
	_, err = n.deps.dbtx().ExecContext(ctx, "DELETE FROM kv WHERE key=?", key)
	return err
}

func (n *ns) execCopy(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if err := n.ensureKV(ctx); err != nil {
		return err
	}
	// Support both src/srcexpr and srckey/srckeyexpr
	srcKey, err := getStringOrExpr(ctx, dm, el, "src", "srcexpr")
	if err != nil {
		return err
	}
	if srcKey == "" {
		srcKey, err = getStringOrExpr(ctx, dm, el, "srckey", "srckeyexpr")
		if err != nil {
			return err
		}
	}
	// Support both dst/dstexpr and dstkey/dstkeyexpr
	dstKey, err := getStringOrExpr(ctx, dm, el, "dst", "dstexpr")
	if err != nil {
		return err
	}
	if dstKey == "" {
		dstKey, err = getStringOrExpr(ctx, dm, el, "dstkey", "dstkeyexpr")
		if err != nil {
			return err
		}
	}
	if srcKey == "" || dstKey == "" {
		return fmt.Errorf("copy requires src/srckey and dst/dstkey attributes")
	}
	_, err = n.deps.dbtx().ExecContext(ctx,
		"INSERT INTO kv(key,value) SELECT ?, value FROM kv WHERE key=? ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		dstKey, srcKey)
	return err
}

func (n *ns) execMove(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if err := n.ensureKV(ctx); err != nil {
		return err
	}
	// Support both src/srcexpr and srckey/srckeyexpr
	srcKey, err := getStringOrExpr(ctx, dm, el, "src", "srcexpr")
	if err != nil {
		return err
	}
	if srcKey == "" {
		srcKey, err = getStringOrExpr(ctx, dm, el, "srckey", "srckeyexpr")
		if err != nil {
			return err
		}
	}
	// Support both dst/dstexpr and dstkey/dstkeyexpr
	dstKey, err := getStringOrExpr(ctx, dm, el, "dst", "dstexpr")
	if err != nil {
		return err
	}
	if dstKey == "" {
		dstKey, err = getStringOrExpr(ctx, dm, el, "dstkey", "dstkeyexpr")
		if err != nil {
			return err
		}
	}
	if srcKey == "" || dstKey == "" {
		return fmt.Errorf("move requires src/srckey and dst/dstkey attributes")
	}
	// If a transaction is active, use it; otherwise create a short-lived transaction for atomicity
	if n.deps != nil && n.deps.tx != nil {
		// Copy then delete using active tx
		if _, err := n.deps.tx.ExecContext(ctx,
			"INSERT INTO kv(key,value) SELECT ?, value FROM kv WHERE key=? ON CONFLICT(key) DO UPDATE SET value=excluded.value",
			dstKey, srcKey); err != nil {
			return err
		}
		if _, err := n.deps.tx.ExecContext(ctx, "DELETE FROM kv WHERE key=?", srcKey); err != nil {
			return err
		}
		return nil
	}
	// No active tx: use a local transaction on the base DB
	tx, err := n.deps.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO kv(key,value) SELECT ?, value FROM kv WHERE key=? ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		dstKey, srcKey); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM kv WHERE key=?", srcKey); err != nil {
		return err
	}
	return tx.Commit()
}

func (n *ns) execQuery(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	// Query is similar to SQL but for KV operations
	// Support both sql/sqlexpr and query/queryexpr
	sqlStr, err := getStringOrExpr(ctx, dm, el, "sql", "sqlexpr")
	if err != nil {
		return err
	}
	if sqlStr == "" {
		sqlStr, err = getStringOrExpr(ctx, dm, el, "query", "queryexpr")
		if err != nil {
			return err
		}
	}
	loc := string(el.GetAttribute("location"))
	if loc == "" {
		loc = string(el.GetAttribute("dataid"))
	}
	if sqlStr == "" {
		return fmt.Errorf("query requires sql/query attribute")
	}
	rows, err := n.deps.dbtx().QueryContext(ctx, sqlStr)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out []map[string]any
	for rows.Next() {
		scan := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range scan {
			ptrs[i] = &scan[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		m := map[string]any{}
		for i, c := range cols {
			m[c] = scan[i]
		}
		out = append(out, m)
	}
	assignIf(ctx, dm, loc, out)
	return nil
}

func (n *ns) execKVTruncate(ctx context.Context) error {
	if err := n.ensureKV(ctx); err != nil {
		return err
	}
	_, err := n.deps.dbtx().ExecContext(ctx, "DELETE FROM kv")
	return err
}

func (n *ns) execBegin(ctx context.Context) error {
	if n.deps == nil || n.deps.DB == nil {
		return fmt.Errorf("memory DB not configured")
	}
	if n.deps.tx != nil {
		return nil
	}
	tx, err := n.deps.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	n.deps.tx = tx
	return nil
}

func (n *ns) execCommit(ctx context.Context) error {
	if n.deps == nil || n.deps.tx == nil {
		return nil
	}
	err := n.deps.tx.Commit()
	n.deps.tx = nil
	return err
}

func (n *ns) execRollback(ctx context.Context) error {
	if n.deps == nil || n.deps.tx == nil {
		return nil
	}
	err := n.deps.tx.Rollback()
	n.deps.tx = nil
	return err
}

func (n *ns) execSavepoint(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if n.deps == nil || n.deps.DB == nil {
		return fmt.Errorf("memory DB not configured")
	}
	// Support both name and nameexpr
	name, err := getStringOrExpr(ctx, dm, el, "name", "nameexpr")
	if err != nil {
		return err
	}
	if name == "" {
		name = "sp_" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	_, err = n.deps.dbtx().ExecContext(ctx, "SAVEPOINT "+name)
	return err
}

func (n *ns) execRelease(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if n.deps == nil || n.deps.DB == nil {
		return fmt.Errorf("memory DB not configured")
	}
	// Support both name and nameexpr
	name, err := getStringOrExpr(ctx, dm, el, "name", "nameexpr")
	if err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("release requires name or nameexpr attribute")
	}
	_, err = n.deps.dbtx().ExecContext(ctx, "RELEASE SAVEPOINT "+name)
	return err
}

func (n *ns) execSQL(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	// Handle both sql and sqlexpr attributes
	sqlStr := string(el.GetAttribute("sql"))
	if sqlStr == "" {
		sqlexpr := string(el.GetAttribute("sqlexpr"))
		if sqlexpr == "" {
			return fmt.Errorf("exec requires sql or sqlexpr attribute")
		}
		var err error
		sqlStr, err = evalString(ctx, dm, sqlexpr)
		if err != nil {
			return err
		}
	}
	// Handle both location and dataid attributes
	loc := string(el.GetAttribute("location"))
	if loc == "" {
		loc = string(el.GetAttribute("dataid"))
	}
	// If there's no location/dataid specified, it's an exec, not a query
	if loc == "" {
		// Execute the SQL statement (CREATE TABLE, INSERT, etc.)
		_, err := n.deps.dbtx().ExecContext(ctx, sqlStr)
		return err
	} else {
		// Query and store results
		rows, err := n.deps.dbtx().QueryContext(ctx, sqlStr)
		if err != nil {
			return err
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		var out []map[string]any
		for rows.Next() {
			scan := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range scan {
				ptrs[i] = &scan[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				continue
			}
			m := map[string]any{}
			for i, c := range cols {
				m[c] = scan[i]
			}
			out = append(out, m)
		}
		assignIf(ctx, dm, loc, out)
		return nil
	}
}

// ---- Embeddings & Vectors ----

func (n *ns) execEmbed(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if n.deps == nil || n.deps.Embed == nil {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "embedder_unavailable",
			Cause:     fmt.Errorf("no embedder"),
		}
	}
	// Support both model and modelexpr
	model, err := getStringOrExpr(ctx, dm, el, "model", "modelexpr")
	if err != nil {
		return err
	}
	// Support both text and textexpr
	text, err := getStringOrExpr(ctx, dm, el, "text", "textexpr")
	if err != nil {
		return err
	}
	loc := string(el.GetAttribute("location"))
	// Support both key and keyexpr
	key, err := getStringOrExpr(ctx, dm, el, "key", "keyexpr")
	if err != nil {
		return err
	}
	vec, err := n.deps.Embed(ctx, model, text)
	if err != nil {
		return err
	}
	assignIf(ctx, dm, loc, vec)
	// Optional upsert
	if key != "" && n.deps.Vector != nil {
		id := hashKey(key)
		if err := n.deps.Vector.InsertVector(ctx, id, vec); err != nil {
			return err
		}
	}
	return nil
}

func (n *ns) execUpsertVector(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if n.deps == nil || n.deps.Vector == nil {
		return fmt.Errorf("vector store not configured")
	}
	// Support both key and keyexpr
	key, err := getStringOrExpr(ctx, dm, el, "key", "keyexpr")
	if err != nil {
		return err
	}
	// Support both vector and vectorexpr
	var v any
	if vecExpr := string(el.GetAttribute("vectorexpr")); vecExpr != "" {
		v, err = dm.EvaluateValue(ctx, vecExpr)
		if err != nil {
			return err
		}
	} else if vec := string(el.GetAttribute("vector")); vec != "" {
		v, err = dm.EvaluateValue(ctx, vec)
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("upsertvector requires vector or vectorexpr")
	}
	arr, ok := toFloat32Slice(v)
	if !ok {
		return fmt.Errorf("vector must evaluate to []number")
	}
	return n.deps.Vector.InsertVector(ctx, hashKey(key), arr)
}

func (n *ns) execSearch(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if n.deps == nil || n.deps.Vector == nil || n.deps.Embed == nil {
		return fmt.Errorf("vector search not available")
	}
	// Support both model and modelexpr
	model, err := getStringOrExpr(ctx, dm, el, "model", "modelexpr")
	if err != nil {
		return err
	}
	// Support both text and textexpr
	text, err := getStringOrExpr(ctx, dm, el, "text", "textexpr")
	if err != nil {
		return err
	}
	loc := string(el.GetAttribute("location"))
	// Support both topk and topkexpr
	topkStr, err := getStringOrExpr(ctx, dm, el, "topk", "topkexpr")
	if err != nil {
		return err
	}
	topk := 5
	if strings.TrimSpace(topkStr) != "" {
		fmt.Sscan(topkStr, &topk)
	}
	qvec, err := n.deps.Embed(ctx, model, text)
	if err != nil {
		return err
	}
	res, err := n.deps.Vector.SearchSimilarVectors(ctx, qvec, topk)
	if err != nil {
		return err
	}
	// Convert to array of maps
	outs := make([]map[string]any, 0, len(res))
	for _, r := range res {
		outs = append(outs, map[string]any{"id": r.ID, "distance": r.Distance})
	}
	assignIf(ctx, dm, loc, outs)
	return nil
}

func (n *ns) execDeleteVector(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if n.deps == nil || n.deps.Vector == nil {
		return fmt.Errorf("vector store not configured")
	}
	// Support both key and keyexpr
	key, err := getStringOrExpr(ctx, dm, el, "key", "keyexpr")
	if err != nil {
		return err
	}
	_, err = n.deps.dbtx().ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE rowid=?", n.deps.Vector.tableName), int64(hashKey(key)))
	return err
}

// ---- Graph helpers ----

func (n *ns) execAddNode(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if n.deps == nil || n.deps.Graph == nil {
		return fmt.Errorf("graph not configured")
	}
	// Support both labels and labelsexpr
	labelsStr, err := getStringOrExpr(ctx, dm, el, "labels", "labelsexpr")
	if err != nil {
		return err
	}
	labels := parseLabels(labelsStr)
	// Support both props and propsexpr
	var props map[string]any
	if propsExpr := string(el.GetAttribute("propsexpr")); propsExpr != "" {
		props, _ = evalMap(ctx, dm, propsExpr)
	} else if propsVal := string(el.GetAttribute("props")); propsVal != "" {
		props, _ = evalMap(ctx, dm, propsVal)
	}
	node, err := n.deps.Graph.CreateNode(ctx, labels, props)
	if err != nil {
		return err
	}
	assignIf(ctx, dm, string(el.GetAttribute("location")), node)
	return nil
}

func (n *ns) execAddEdge(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if n.deps == nil || n.deps.Graph == nil {
		return fmt.Errorf("graph not configured")
	}
	// Support both src and srcexpr
	src, err := getIntOrExpr(ctx, dm, el, "src", "srcexpr")
	if err != nil {
		return err
	}
	// Support both dst and dstexpr
	dst, err := getIntOrExpr(ctx, dm, el, "dst", "dstexpr")
	if err != nil {
		return err
	}
	// Support both rel and relexpr
	rel, err := getStringOrExpr(ctx, dm, el, "rel", "relexpr")
	if err != nil {
		return err
	}
	// Support both props and propsexpr
	var props map[string]any
	if propsExpr := string(el.GetAttribute("propsexpr")); propsExpr != "" {
		props, _ = evalMap(ctx, dm, propsExpr)
	} else if propsVal := string(el.GetAttribute("props")); propsVal != "" {
		props, _ = evalMap(ctx, dm, propsVal)
	}
	slog.InfoContext(ctx, "memory: adding edge", "src", src, "dst", dst, "rel", rel)
	_, err = n.deps.Graph.CreateRelationship(ctx, src, dst, rel, props)
	if err != nil {
		slog.WarnContext(ctx, "memory: failed to add edge", "error", err)
		return err
	}
	slog.InfoContext(ctx, "memory: edge added")
	return nil
}

func (n *ns) execGetNode(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if n.deps == nil || n.deps.Graph == nil {
		return fmt.Errorf("graph not configured")
	}
	// Support both id and idexpr
	id, err := getIntOrExpr(ctx, dm, el, "id", "idexpr")
	if err != nil {
		return err
	}
	loc := string(el.GetAttribute("location"))
	row := n.deps.dbtx().QueryRowContext(ctx, fmt.Sprintf("SELECT id, labels, properties FROM %s WHERE id=?", n.deps.Graph.nodesTable), id)
	var nid int64
	var labelsJSON, propsJSON string
	if err := row.Scan(&nid, &labelsJSON, &propsJSON); err != nil {
		assignIf(ctx, dm, loc, nil)
		return nil
	}
	var labels []string
	var props map[string]any
	_ = json.Unmarshal([]byte(labelsJSON), &labels)
	_ = json.Unmarshal([]byte(propsJSON), &props)
	assignIf(ctx, dm, loc, &Node{ID: nid, Labels: labels, Properties: props})
	return nil
}

func (n *ns) execDeleteNode(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if n.deps == nil || n.deps.Graph == nil {
		return fmt.Errorf("graph not configured")
	}
	// Support both id and idexpr
	id, err := getIntOrExpr(ctx, dm, el, "id", "idexpr")
	if err != nil {
		return err
	}
	return n.deps.Graph.DeleteNode(ctx, id)
}

func (n *ns) execDeleteEdge(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if n.deps == nil || n.deps.Graph == nil {
		return fmt.Errorf("graph not configured")
	}
	// Support both src and srcexpr
	src, err := getIntOrExpr(ctx, dm, el, "src", "srcexpr")
	if err != nil {
		return err
	}
	// Support both dst and dstexpr
	dst, err := getIntOrExpr(ctx, dm, el, "dst", "dstexpr")
	if err != nil {
		return err
	}
	// Support both rel and relexpr
	rel, err := getStringOrExpr(ctx, dm, el, "rel", "relexpr")
	if err != nil {
		return err
	}
	_, err = n.deps.dbtx().ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE source=? AND target=? AND edge_type=?", n.deps.Graph.edgesTable), src, dst, rel)
	return err
}

func (n *ns) execNeighbors(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if n.deps == nil || n.deps.Graph == nil {
		return fmt.Errorf("graph not configured")
	}
	// Support both id and idexpr
	id, err := getIntOrExpr(ctx, dm, el, "id", "idexpr")
	if err != nil {
		return err
	}
	// Support both direction and directionexpr
	dir, err := getStringOrExpr(ctx, dm, el, "direction", "directionexpr")
	if err != nil {
		return err
	}
	dir = strings.ToLower(dir)
	loc := string(el.GetAttribute("location"))
	var rows *sql.Rows
	if dir == "in" {
		rows, err = n.deps.dbtx().QueryContext(ctx, fmt.Sprintf("SELECT source FROM %s WHERE target=?", n.deps.Graph.edgesTable), id)
	} else {
		rows, err = n.deps.dbtx().QueryContext(ctx, fmt.Sprintf("SELECT target FROM %s WHERE source=?", n.deps.Graph.edgesTable), id)
	}
	if err != nil {
		return err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var nid int64
		if err := rows.Scan(&nid); err == nil {
			out = append(out, nid)
		}
	}
	slog.InfoContext(ctx, "memory: neighbors computed", "count", len(out), "location", loc)
	assignIf(ctx, dm, loc, out)
	return nil
}

func (n *ns) execGetEdge(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if n.deps == nil || n.deps.Graph == nil {
		return fmt.Errorf("graph database not configured")
	}
	// Support both id and idexpr
	id, err := getStringOrExpr(ctx, dm, el, "id", "idexpr")
	if err != nil {
		return err
	}
	loc := string(el.GetAttribute("location"))
	if loc == "" {
		loc = string(el.GetAttribute("dataid"))
	}
	// For now, use a simple query to get edge by ID
	rows, err := n.deps.dbtx().QueryContext(ctx,
		"SELECT id, source, target, edge_type, properties FROM relationships WHERE id = ?", id)
	if err != nil {
		return err
	}
	defer rows.Close()

	if rows.Next() {
		var edge struct {
			ID         string
			Src        int64
			Dst        int64
			Rel        string
			Properties string
		}
		err = rows.Scan(&edge.ID, &edge.Src, &edge.Dst, &edge.Rel, &edge.Properties)
		if err != nil {
			return err
		}
		var props map[string]interface{}
		_ = json.Unmarshal([]byte(edge.Properties), &props)
		assignIf(ctx, dm, loc, map[string]interface{}{
			"id":         edge.ID,
			"src":        edge.Src,
			"dst":        edge.Dst,
			"type":       edge.Rel,
			"properties": props,
		})
	} else {
		assignIf(ctx, dm, loc, nil)
	}
	return nil
}

func (n *ns) execGraphPath(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if n.deps == nil || n.deps.Graph == nil {
		return fmt.Errorf("graph database not configured")
	}
	// Support both src and srcexpr
	src, err := getIntOrExpr(ctx, dm, el, "src", "srcexpr")
	if err != nil {
		return err
	}
	// Support both dst and dstexpr
	dst, err := getIntOrExpr(ctx, dm, el, "dst", "dstexpr")
	if err != nil {
		return err
	}
	loc := string(el.GetAttribute("location"))
	if loc == "" {
		loc = string(el.GetAttribute("dataid"))
	}

	// Simple BFS path finding
	type pathNode struct {
		id   int64
		path []int64
	}

	visited := make(map[int64]bool)
	queue := []pathNode{{id: src, path: []int64{src}}}
	visited[src] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.id == dst {
			assignIf(ctx, dm, loc, current.path)
			return nil
		}

		// Get neighbors
		rows, err := n.deps.dbtx().QueryContext(ctx,
			fmt.Sprintf("SELECT target FROM %s WHERE source=?", n.deps.Graph.edgesTable), current.id)
		if err != nil {
			continue
		}

		for rows.Next() {
			var neighborID int64
			if err := rows.Scan(&neighborID); err == nil && !visited[neighborID] {
				visited[neighborID] = true
				newPath := append([]int64{}, current.path...)
				newPath = append(newPath, neighborID)
				queue = append(queue, pathNode{id: neighborID, path: newPath})
			}
		}
		rows.Close()
	}

	assignIf(ctx, dm, loc, nil) // No path found
	return nil
}

func (n *ns) execGraphTruncate(ctx context.Context) error {
	if n.deps == nil || n.deps.Graph == nil {
		return fmt.Errorf("graph database not configured")
	}
	// Clear both nodes and relationships tables
	_, err := n.deps.DB.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", n.deps.Graph.edgesTable))
	if err != nil {
		return err
	}
	_, err = n.deps.DB.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", n.deps.Graph.nodesTable))
	return err
}

func (n *ns) execGraphQuery(ctx context.Context, el xmldom.Element, dm agentml.DataModel) error {
	if n.deps == nil || n.deps.Graph == nil {
		return fmt.Errorf("graph not configured")
	}
	q := mustEvalString(ctx, dm, string(el.GetAttribute("pathexpr")))
	res, err := n.deps.Graph.Search(ctx, q)
	if err != nil {
		return err
	}
	assignIf(ctx, dm, string(el.GetAttribute("location")), res)
	return nil
}

// ---- Legacy graph: <memory:graph op="..."> ----

func (n *ns) execGraph(ctx context.Context, el xmldom.Element) error {
	tr := otel.Tracer("memory")
	ctx, span := tr.Start(ctx, "memory.graph.execute")
	defer span.End()

	if n.deps == nil || n.deps.Graph == nil {
		err := fmt.Errorf("memory graph dependency not configured")
		span.SetStatus(codes.Error, err.Error())
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "memory graph dependency not configured",
			Data: map[string]any{
				"element": "memory:graph",
			},
			Cause: err,
		}
	}

	dm := n.itp.DataModel()
	if dm == nil {
		err := fmt.Errorf("no data model available")
		span.SetStatus(codes.Error, err.Error())
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "No data model available for memory:graph",
			Data: map[string]any{
				"element": "memory:graph",
			},
			Cause: err,
		}
	}

	// Parse attributes once here for legacy
	op := strings.ToLower(string(el.GetAttribute("op")))
	out := string(el.GetAttribute("out"))
	labels := parseLabels(string(el.GetAttribute("labels")))
	propsExpr := string(el.GetAttribute("properties-expr"))
	startExpr := string(el.GetAttribute("start"))
	endExpr := string(el.GetAttribute("end"))
	relType := string(el.GetAttribute("type"))
	idExpr := string(el.GetAttribute("id"))
	queryExpr := string(el.GetAttribute("query-expr"))
	if op == "" {
		return fmt.Errorf("memory:graph requires op")
	}

	switch op {
	case "create_node", "add_node", "addnode", "create-node":
		props, _ := evalMap(ctx, dm, propsExpr)
		n, err := n.deps.Graph.CreateNode(ctx, labels, props)
		if err != nil {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("Failed to create node: %v", err),
				Data: map[string]any{
					"element": "memory:graph",
					"op":      op,
				},
				Cause: err,
			}
		}
		assignIf(ctx, dm, out, n)
		return nil
	case "create_relationship", "create_edge", "add_relationship", "add_edge", "create-relationship", "create-edge":
		startID, _ := evalInt64(ctx, dm, startExpr)
		endID, _ := evalInt64(ctx, dm, endExpr)
		props, _ := evalMap(ctx, dm, propsExpr)
		_, err := n.deps.Graph.CreateRelationship(ctx, startID, endID, relType, props)
		return err
	case "find_nodes", "find-nodes":
		props, _ := evalMap(ctx, dm, propsExpr)
		nodes, err := n.deps.Graph.FindNodes(ctx, labels, props)
		if err != nil {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("Failed to find nodes: %v", err),
				Data: map[string]any{
					"element": "memory:graph",
					"op":      op,
				},
				Cause: err,
			}
		}
		assignIf(ctx, dm, out, nodes)
		return nil
	case "delete_node", "delete-node":
		id, _ := evalInt64(ctx, dm, idExpr)
		return n.deps.Graph.DeleteNode(ctx, id)
	case "search", "graph-search":
		query, _ := evalString(ctx, dm, queryExpr)
		results, err := n.deps.Graph.Search(ctx, query)
		if err != nil {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("Failed to search graph: %v", err),
				Data: map[string]any{
					"element": "memory:graph",
					"op":      op,
				},
				Cause: err,
			}
		}
		assignIf(ctx, dm, out, results)
		return nil
	default:
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   fmt.Sprintf("unsupported memory:graph op '%s'", op),
			Data: map[string]any{
				"element": "memory:graph",
				"op":      op,
			},
			Cause: fmt.Errorf("unsupported"),
		}
	}
}

// ----------------------- Helpers -----------------------

func parseLabels(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	s = strings.ReplaceAll(s, ",", " ")
	parts := strings.Fields(s)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func evalMap(ctx context.Context, dm agentml.DataModel, expr string) (map[string]any, error) {
	if strings.TrimSpace(expr) == "" {
		return nil, nil
	}
	v, err := dm.EvaluateValue(ctx, expr)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected object for properties-expr, got %T", v)
	}
	return m, nil
}

func evalInt64(ctx context.Context, dm agentml.DataModel, expr string) (int64, error) {
	v, err := dm.EvaluateValue(ctx, expr)
	if err != nil {
		return 0, err
	}
	switch n := v.(type) {
	case int:
		return int64(n), nil
	case int64:
		return n, nil
	case float64:
		return int64(n), nil
	case string:
		var out int64
		_, err := fmt.Sscan(n, &out)
		if err != nil {
			return 0, fmt.Errorf("cannot parse id: %v", err)
		}
		return out, nil
	default:
		return 0, fmt.Errorf("unsupported id type: %T", v)
	}
}

func evalString(ctx context.Context, dm agentml.DataModel, expr string) (string, error) {
	if strings.TrimSpace(expr) == "" {
		return "", nil
	}
	v, err := dm.EvaluateValue(ctx, expr)
	if err != nil {
		return "", err
	}
	if v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("expected string, got %T", v)
	}
	return s, nil
}

func mustEvalString(ctx context.Context, dm agentml.DataModel, expr string) string {
	s, _ := evalString(ctx, dm, expr)
	return s
}

// getStringOrExpr retrieves a string from either a literal attribute or an expression attribute.
// It checks attrExpr first (e.g., "keyexpr"), then falls back to attr (e.g., "key").
func getStringOrExpr(ctx context.Context, dm agentml.DataModel, el xmldom.Element, attr, attrExpr string) (string, error) {
	// Try expression attribute first
	if exprVal := string(el.GetAttribute(xmldom.DOMString(attrExpr))); exprVal != "" {
		return evalString(ctx, dm, exprVal)
	}
	// Fall back to literal attribute
	return string(el.GetAttribute(xmldom.DOMString(attr))), nil
}

// getIntOrExpr retrieves an int64 from either a literal attribute or an expression attribute.
func getIntOrExpr(ctx context.Context, dm agentml.DataModel, el xmldom.Element, attr, attrExpr string) (int64, error) {
	// Try expression attribute first
	if exprVal := string(el.GetAttribute(xmldom.DOMString(attrExpr))); exprVal != "" {
		return evalInt64(ctx, dm, exprVal)
	}
	// Fall back to literal attribute - need to evaluate if it's a number string
	if litVal := string(el.GetAttribute(xmldom.DOMString(attr))); litVal != "" {
		return evalInt64(ctx, dm, litVal)
	}
	return 0, nil
}

func toFloat32Slice(v any) ([]float32, bool) {
	switch arr := v.(type) {
	case []any:
		out := make([]float32, len(arr))
		for i, x := range arr {
			switch n := x.(type) {
			case float64:
				out[i] = float32(n)
			case int:
				out[i] = float32(n)
			default:
				return nil, false
			}
		}
		return out, true
	case []float64:
		out := make([]float32, len(arr))
		for i, n := range arr {
			out[i] = float32(n)
		}
		return out, true
	default:
		return nil, false
	}
}

func assignIf(ctx context.Context, dm agentml.DataModel, loc string, value any) {
	if strings.TrimSpace(loc) == "" {
		return
	}
	// Use SetVariable to avoid JS serialization issues for complex Go values (maps, slices, structs)
	if err := dm.SetVariable(ctx, loc, value); err != nil {
		slog.WarnContext(ctx, "memory: failed to assign result", "location", loc, "error", err)
	}
}

func raise(ctx context.Context, interp agentml.Interpreter, name string, data map[string]any) {
	if name == "" {
		return
	}
	evt := &agentml.Event{
		Name: name,
		Data: data,
		Type: agentml.EventTypeInternal,
	}
	interp.Raise(ctx, evt)
}

func hashKey(key string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return h.Sum64()
}
