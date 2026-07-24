package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/imshuai/yaa/internal/config"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStorage 是基于 modernc.org/sqlite（纯 Go，无 CGO）的根 KV 后端。
type SQLiteStorage struct {
	db        *sql.DB
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

// NewSQLite 构造 SQLite 后端。Path 为空、打开失败、目录创建失败、PRAGMA/migration 失败均阻止 Ready。
func NewSQLite(cfg config.StorageConfig) (*SQLiteStorage, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("%w: sqlite path is empty", ErrInvalidPath)
	}
	if err := ensureStorageDir(cfg.Path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("open root storage: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("configure root storage: %w", err)
	}
	s := &SQLiteStorage{db: db, stop: make(chan struct{}), done: make(chan struct{})}
	if err = s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate root storage: %w", err)
	}
	go s.cleanupLoop()
	return s, nil
}

func ensureStorageDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create storage dir %s: %w", dir, err)
	}
	return nil
}

func (s *SQLiteStorage) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS root_kv (
			key        TEXT PRIMARY KEY,
			value      BLOB NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER
		);`,
		`CREATE INDEX IF NOT EXISTS root_kv_expiry
			ON root_kv (expires_at)
			WHERE expires_at IS NOT NULL;`,
		`CREATE TABLE IF NOT EXISTS root_storage_schema_version (
			version INTEGER PRIMARY KEY,
			applied_at INTEGER NOT NULL
		);`,
	}
	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec schema: %w", err)
		}
	}
	// v1 记录；已存在则幂等。
	if _, err := s.db.Exec(
		`INSERT OR IGNORE INTO root_storage_schema_version (version, applied_at) VALUES (1, ?);`,
		time.Now().UTC().UnixNano(),
	); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	// 拒绝未知更高版本：只接受 ≤1。
	var maxVer int
	if err := s.db.QueryRow(`SELECT MAX(version) FROM root_storage_schema_version;`).Scan(&maxVer); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("read schema version: %w", err)
		}
	}
	if maxVer > 1 {
		return fmt.Errorf("storage: unknown schema version %d", maxVer)
	}
	return nil
}

func (s *SQLiteStorage) Get(key string) ([]byte, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	row := s.db.QueryRow(
		`SELECT value FROM root_kv
		 WHERE key = ?
		   AND (expires_at IS NULL OR expires_at > ?);`,
		key, time.Now().UnixNano(),
	)
	var value []byte
	if err := row.Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	out := make([]byte, len(value))
	copy(out, value)
	return out, nil
}

func (s *SQLiteStorage) Set(key string, value []byte, ttl ...time.Duration) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if err := validateValue(value); err != nil {
		return err
	}
	exp, err := expiresAt(time.Now(), ttl)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO root_kv (key, value, created_at, expires_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET
		     value = excluded.value,
		     expires_at = excluded.expires_at;`,
		key, value, time.Now().UTC().UnixNano(), exp,
	)
	return err
}

func (s *SQLiteStorage) Delete(key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM root_kv WHERE key = ?;`, key)
	return err
}

func (s *SQLiteStorage) Has(key string) (bool, error) {
	if err := validateKey(key); err != nil {
		return false, err
	}
	row := s.db.QueryRow(
		`SELECT 1 FROM root_kv
		 WHERE key = ?
		   AND (expires_at IS NULL OR expires_at > ?);`,
		key, time.Now().UnixNano(),
	)
	var one int
	if err := row.Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *SQLiteStorage) Keys(prefix string) ([]string, error) {
	if prefix != "" {
		if err := validateKey(prefix); err != nil {
			return nil, err
		}
	}
	rows, err := s.db.Query(
		`SELECT key FROM root_kv
		 WHERE substr(key, 1, length(?)) = ?
		   AND (expires_at IS NULL OR expires_at > ?)
		 ORDER BY key ASC;`,
		prefix, prefix, time.Now().UnixNano(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := make([]string, 0)
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}

// Close 标记关闭、停止并等待 worker，再关闭 DB。重复调用幂等成功。
func (s *SQLiteStorage) Close() error {
	var firstErr error
	s.closeOnce.Do(func() {
		close(s.stop)
		<-s.done
		if err := s.db.Close(); err != nil {
			firstErr = err
		}
	})
	return firstErr
}

func (s *SQLiteStorage) cleanupLoop() {
	defer close(s.done)
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			for {
				select {
				case <-s.stop:
					return
				default:
				}
				n, err := s.cleanupExpired(1000)
				if err != nil {
					// ponytail: cleanup 失败不关闭 storage；Get/Has/Keys 的 expiry filter 保证不暴露过期值。
					return
				}
				if n < 1000 {
					break
				}
			}
		case <-s.stop:
			return
		}
	}
}

func (s *SQLiteStorage) cleanupExpired(batch int) (int, error) {
	res, err := s.db.Exec(
		`DELETE FROM root_kv
		 WHERE key IN (
		     SELECT key FROM root_kv
		     WHERE expires_at IS NOT NULL AND expires_at <= ?
		     ORDER BY expires_at, key
		     LIMIT ?
		 );`,
		time.Now().UnixNano(), batch,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// 编译期接口实现断言。
var _ Storage = (*SQLiteStorage)(nil)
