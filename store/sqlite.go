package store

import (
	"database/sql"

	"github.com/veliborsimonovic/collab/crdt"

	_ "modernc.org/sqlite"
)

type SQLite struct {
	db *sql.DB
}

func OpenSQLite(path string) (*SQLite, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	const schema = `
	CREATE TABLE IF NOT EXISTS ops (
		seq    INTEGER PRIMARY KEY,
		doc    TEXT NOT NULL,
		client INTEGER NOT NULL,
		clock  INTEGER NOT NULL,
		data   BLOB NOT NULL,
		UNIQUE (doc, client, clock)
	);`

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}

	return &SQLite{db: db}, nil
}

func (s *SQLite) Append(doc string, ops []crdt.Op) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO ops (doc, client, clock, data) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, op := range ops {
		data := crdt.EncodeOps([]crdt.Op{op})

		if _, err := stmt.Exec(doc, int64(op.OpID().Client), int64(op.OpID().Clock), data); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLite) Load(doc string) ([]crdt.Op, error) {
	rows, err := s.db.Query(`SELECT data FROM ops WHERE doc = ? ORDER BY seq`, doc)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []crdt.Op
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		decoded, err := crdt.DecodeOps(data)
		if err != nil {
			return nil, err
		}
		out = append(out, decoded...)
	}
	return out, rows.Err()
}

func (s *SQLite) Close() error {
	return s.db.Close()
}
