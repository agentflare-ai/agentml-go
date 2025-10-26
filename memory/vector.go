package memory

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"sort"
	"unsafe"

	"github.com/agentflare-ai/go-jsonschema"
)

// Vector operation schemas for reuse
var (
	// AddVectorSchema defines the schema for adding a vector
	AddVectorSchema = &jsonschema.Schema{
		Type:        "object",
		Description: "Add a new vector to the vector store for similarity search",
		Properties: map[string]*jsonschema.Schema{
			"content": {
				Type:        "string",
				Description: "The text content to embed and index",
			},
			"metadata": {
				Type:        "object",
				Description: "Associated metadata for the vector entry",
				Properties: map[string]*jsonschema.Schema{
					"source": {
						Type:        "string",
						Description: "Source of the content (e.g., 'user_input', 'system_output')",
					},
					"timestamp": {
						Type:        "string",
						Description: "ISO 8601 timestamp when the content was created",
					},
					"context_id": {
						Type:        "string",
						Description: "ID linking this vector to a specific context or conversation",
					},
					"tags": {
						Type:        "array",
						Description: "Tags for categorizing the content",
						Items:       &jsonschema.Schema{Type: "string"},
					},
				},
			},
		},
		Required: []string{"content"},
	}

	// SearchVectorSchema defines the schema for vector similarity search
	SearchVectorSchema = &jsonschema.Schema{
		Type:        "object",
		Description: "Search for similar vectors in the vector store",
		Properties: map[string]*jsonschema.Schema{
			"query": {
				Type:        "string",
				Description: "The text query to search for similar content",
			},
			"limit": {
				Type:        "integer",
				Description: "Maximum number of results to return",
				Minimum:     &[]float64{1}[0],
				Maximum:     &[]float64{100}[0],
			},
			"threshold": {
				Type:        "number",
				Description: "Minimum similarity score threshold (0-1)",
				Minimum:     &[]float64{0}[0],
				Maximum:     &[]float64{1}[0],
			},
		},
		Required: []string{"query"},
	}

	// UpdateVectorSchema defines the schema for updating vector metadata
	UpdateVectorSchema = &jsonschema.Schema{
		Type:        "object",
		Description: "Update metadata for an existing vector",
		Properties: map[string]*jsonschema.Schema{
			"id": {
				Type:        "integer",
				Description: "ID of the vector to update",
			},
			"metadata": {
				Type:        "object",
				Description: "New metadata to merge with existing",
				Properties: map[string]*jsonschema.Schema{
					"source": {
						Type:        "string",
						Description: "Updated source information",
					},
					"tags": {
						Type:        "array",
						Description: "Updated tags",
						Items:       &jsonschema.Schema{Type: "string"},
					},
					"notes": {
						Type:        "string",
						Description: "Additional notes or annotations",
					},
				},
			},
		},
		Required: []string{"id", "metadata"},
	}

	// VectorUpdateSchema combines all vector operation schemas
	VectorUpdateSchema = &jsonschema.Schema{
		Type:        "object",
		Description: "A vector store operation",
		AnyOf: []*jsonschema.Schema{
			AddVectorSchema,
			SearchVectorSchema,
			UpdateVectorSchema,
		},
	}
)

// VectorDB represents a vector database for embeddings
type VectorDB struct {
	db          *sql.DB
	tableName   string
	dimensions  int
	vtAvailable bool
}

// VectorResult represents a vector search result
type VectorResult struct {
	ID       int64     `json:"id"`
	Vector   []float32 `json:"vector"`
	Distance float64   `json:"distance"`
}

// NewVectorDB creates a new vector database instance with the given database and table name
func NewVectorDB(ctx context.Context, db *sql.DB, tableName string, dimensions int) (*VectorDB, error) {
	vs := &VectorDB{
		db:         db,
		tableName:  tableName,
		dimensions: dimensions,
	}

	if vs.tableName == "" {
		vs.tableName = "vectors"
	}

	// Try to create the virtual table using the vec extension
	query := fmt.Sprintf("CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(embedding float[%d])", vs.tableName, dimensions)
	if _, err := vs.db.ExecContext(ctx, query); err != nil {
		// Fallback: use a regular table with BLOB storage if vec extension is unavailable
		fallback := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s(rowid INTEGER PRIMARY KEY, embedding BLOB)", vs.tableName)
		if _, err2 := vs.db.ExecContext(ctx, fallback); err2 != nil {
			return nil, fmt.Errorf("failed to create vector table (fallback): %w (original: %v)", err2, err)
		}
		vs.vtAvailable = false
		return vs, nil
	}
	vs.vtAvailable = true
	return vs, nil
}

// InsertVector inserts a vector with the given ID
func (vs *VectorDB) InsertVector(ctx context.Context, id uint64, vector []float32) error {
	if len(vector) != vs.dimensions {
		return fmt.Errorf("vector dimension mismatch: expected %d, got %d", vs.dimensions, len(vector))
	}

	// Convert float32 slice to byte slice
	vectorBytes := make([]byte, len(vector)*4) // 4 bytes per float32
	for i, f := range vector {
		bits := *(*uint32)(unsafe.Pointer(&f))
		vectorBytes[i*4] = byte(bits)
		vectorBytes[i*4+1] = byte(bits >> 8)
		vectorBytes[i*4+2] = byte(bits >> 16)
		vectorBytes[i*4+3] = byte(bits >> 24)
	}

	rowid := int64(id)
	if vs.vtAvailable {
		query := fmt.Sprintf("INSERT INTO %s(rowid, embedding) VALUES (?, ?)", vs.tableName)
		if _, err := vs.db.ExecContext(ctx, query, rowid, vectorBytes); err != nil {
			return fmt.Errorf("failed to insert vector: %w", err)
		}
		return nil
	}
	// Fallback: use INSERT OR REPLACE on regular table
	query := fmt.Sprintf("INSERT OR REPLACE INTO %s(rowid, embedding) VALUES (?, ?)", vs.tableName)
	if _, err := vs.db.ExecContext(ctx, query, rowid, vectorBytes); err != nil {
		return fmt.Errorf("failed to insert vector (fallback): %w", err)
	}
	return nil
}

// SearchSimilarVectors searches for vectors similar to the query vector
func (vs *VectorDB) SearchSimilarVectors(ctx context.Context, queryVector []float32, limit int) ([]VectorResult, error) {
	if len(queryVector) != vs.dimensions {
		return nil, fmt.Errorf("query vector dimension mismatch: expected %d, got %d", vs.dimensions, len(queryVector))
	}

	if vs.vtAvailable {
		// Convert query vector to bytes
		queryBytes := make([]byte, len(queryVector)*4)
		for i, f := range queryVector {
			bits := *(*uint32)(unsafe.Pointer(&f))
			queryBytes[i*4] = byte(bits)
			queryBytes[i*4+1] = byte(bits >> 8)
			queryBytes[i*4+2] = byte(bits >> 16)
			queryBytes[i*4+3] = byte(bits >> 24)
		}

		query := fmt.Sprintf(`
			SELECT rowid, distance 
			FROM %s 
			WHERE embedding MATCH ? 
			ORDER BY distance 
			LIMIT ?
		`, vs.tableName)

		rows, err := vs.db.QueryContext(ctx, query, queryBytes, limit)
		if err != nil {
			return nil, fmt.Errorf("failed to search vectors: %w", err)
		}
		defer rows.Close()

		var results []VectorResult
		for rows.Next() {
			var id int64
			var distance float64
			if err := rows.Scan(&id, &distance); err != nil {
				continue
			}

			results = append(results, VectorResult{
				ID:       id,
				Distance: distance,
			})
		}

		return results, nil
	}

	// Fallback: brute-force scan using Go distance computation
	rows, err := vs.db.QueryContext(ctx, fmt.Sprintf("SELECT rowid, embedding FROM %s", vs.tableName))
	if err != nil {
		return nil, fmt.Errorf("failed to search vectors (fallback): %w", err)
	}
	defer rows.Close()

	tmp := make([]struct{ id int64; dist float64 }, 0, 128)
	for rows.Next() {
		var id int64
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			continue
		}
		vec, err := decodeFloat32Blob(blob)
		if err != nil || len(vec) != vs.dimensions {
			continue
		}
		// Euclidean distance
		var d float64
		for i := 0; i < vs.dimensions; i++ {
			dx := float64(vec[i]) - float64(queryVector[i])
			d += dx * dx
		}
		tmp = append(tmp, struct{ id int64; dist float64 }{id: id, dist: d})
	}

	sort.Slice(tmp, func(i, j int) bool { return tmp[i].dist < tmp[j].dist })
	if limit > len(tmp) {
		limit = len(tmp)
	}
	out := make([]VectorResult, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, VectorResult{ID: tmp[i].id, Distance: tmp[i].dist})
	}
	return out, nil
}

// Close closes the vector store (does not close the underlying database connection)
func (vs *VectorDB) Close() error {
	// Nothing to close for now, as we don't own the database connection
	return nil
}

// decodeFloat32Blob decodes a little-endian byte slice into a float32 slice.
func decodeFloat32Blob(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("invalid vector blob length: %d", len(b))
	}
	n := len(b) / 4
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		bits := binary.LittleEndian.Uint32(b[i*4 : i*4+4])
		f := *(*float32)(unsafe.Pointer(&bits))
		out[i] = f
	}
	return out, nil
}
