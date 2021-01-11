package dbs

import (
	"fmt"

	"modernc.org/ql"
)

type qldb struct {
	q *ql.DB
}

var _ DB = (*qldb)(nil)

func OpenQL(path string) (DB, error) {
	q, err := ql.OpenFile(path, &ql.Options{
		CanCreate:      true,
		RemoveEmptyWAL: true,
		FileFormat:     2, // newest, apparently?
	})
	if err != nil {
		return nil, fmt.Errorf("opening ql DB: %w", err)
	}
	_, failed, err := q.Run(ql.NewRWCtx(), qlSchema)
	if err != nil {
		q.Close()
		return nil, fmt.Errorf("initializing ql DB stmt %d: %w", failed, err)
	}
	return &qldb{q}, nil
}

const qlSchema = `
BEGIN TRANSACTION;
	CREATE TABLE IF NOT EXISTS file (
		Hash string  Hash != "",
		Date time,
		Thumbnail blob,
	);
	CREATE        INDEX IF NOT EXISTS file_ID ON file (id());
	CREATE UNIQUE INDEX IF NOT EXISTS file_Hash ON file (Hash);
	CREATE        INDEX IF NOT EXISTS file_Date ON file (Date);
--------
	CREATE TABLE IF NOT EXISTS location (
		FileID int NOT NULL,    -- Foreign Key
		BackendID int NOT NULL, -- Foreign Key
		Location string,
	);
	CREATE UNIQUE INDEX IF NOT EXISTS
		location_perBackend ON location (BackendID, Location);
--------
	CREATE TABLE IF NOT EXISTS backend (
		Tag string  Tag != "",
	);
	CREATE UNIQUE INDEX IF NOT EXISTS
		backend_Tag ON backend (Tag);
COMMIT;
`

func (db *qldb) Close() error { return db.q.Close() }

func (db *qldb) FileUpsert(f *File) (int64, error) {
	panic("NIY")
}

func (db *qldb) FileEach(fn func(int64, *File) error) error {
	panic("NIY")
}

func (db *qldb) File(id int64) (*File, error) {
	panic("NIY")
}
