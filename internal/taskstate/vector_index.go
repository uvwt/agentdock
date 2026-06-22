package taskstate

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const templateVectorIndexSchemaVersion = 1

type templateVectorIndex struct {
	path  string
	model string
	db    *sql.DB
}

func newTemplateVectorIndex(path, model string) (*templateVectorIndex, error) {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(model) == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	idx := &templateVectorIndex{path: path, model: model, db: db}
	if err := idx.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return idx, nil
}

func (idx *templateVectorIndex) init(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS template_vectors (
			id TEXT NOT NULL,
			version TEXT NOT NULL,
			hash TEXT NOT NULL,
			model TEXT NOT NULL,
			dim INTEGER NOT NULL,
			search_text TEXT NOT NULL,
			embedding BLOB NOT NULL,
			indexed_at TEXT NOT NULL,
			PRIMARY KEY (id, version, model)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_template_vectors_hash ON template_vectors(hash)`,
	}
	for _, stmt := range stmts {
		if _, err := idx.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	_, err := idx.db.ExecContext(ctx, `INSERT OR REPLACE INTO meta(key, value) VALUES('schema_version', ?)`, fmt.Sprint(templateVectorIndexSchemaVersion))
	return err
}

func (idx *templateVectorIndex) getTemplateVector(ctx context.Context, t Template) ([]float64, bool, error) {
	if idx == nil || idx.db == nil {
		return nil, false, nil
	}
	var storedHash string
	var dim int
	var blob []byte
	err := idx.db.QueryRowContext(ctx, `SELECT hash, dim, embedding FROM template_vectors WHERE id = ? AND version = ? AND model = ?`, t.ID, t.Version, idx.model).Scan(&storedHash, &dim, &blob)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if storedHash != templateVectorCacheKey(t) || dim <= 0 {
		return nil, false, nil
	}
	vector, err := decodeFloat32Vector(blob, dim)
	if err != nil {
		return nil, false, err
	}
	return vector, true, nil
}

func (idx *templateVectorIndex) putTemplateVector(ctx context.Context, t Template, searchText string, vector []float64) error {
	if idx == nil || idx.db == nil || len(vector) == 0 {
		return nil
	}
	blob := encodeFloat32Vector(vector)
	_, err := idx.db.ExecContext(ctx, `INSERT OR REPLACE INTO template_vectors(id, version, hash, model, dim, search_text, embedding, indexed_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Version, templateVectorCacheKey(t), idx.model, len(vector), searchText, blob, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (idx *templateVectorIndex) count(ctx context.Context) int {
	if idx == nil || idx.db == nil {
		return 0
	}
	var count int
	if err := idx.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM template_vectors WHERE model = ?`, idx.model).Scan(&count); err != nil {
		return 0
	}
	return count
}

func encodeFloat32Vector(vector []float64) []byte {
	buf := make([]byte, len(vector)*4)
	for i, value := range vector {
		binary.LittleEndian.PutUint32(buf[i*4:(i+1)*4], math.Float32bits(float32(value)))
	}
	return buf
}

func decodeFloat32Vector(blob []byte, dim int) ([]float64, error) {
	if dim <= 0 || len(blob) != dim*4 {
		return nil, fmt.Errorf("invalid vector blob: bytes=%d dim=%d", len(blob), dim)
	}
	vector := make([]float64, dim)
	for i := 0; i < dim; i++ {
		vector[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4 : (i+1)*4])))
	}
	return vector, nil
}
