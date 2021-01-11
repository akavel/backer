package dbs

import (
	"fmt"
	"sort"
	"time"

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
		// Using format 1, due to having an issue with 2: https://gitlab.com/cznic/ql/-/issues/227
		FileFormat: 1,
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
	tx := ql.NewRWCtx()
	rs, failed, err := db.q.Run(tx, `
BEGIN TRANSACTION;
	SELECT id() as ID, Hash
		FROM file
		WHERE Hash = $1;
	`, f.Hash)
	if err != nil {
		return 0, fmt.Errorf("ql DB upsert file by hash %q stmt %d: %w", f.Hash, failed, err)
	}
	defer db.rollback(tx)

	row, err := rs[0].FirstRow()
	if err != nil {
		return 0, fmt.Errorf("ql DB upsert file by hash %q: %w", f.Hash, err)
	}
	var fileID int64
	if row == nil { // Insert new
		rs, failed, err = db.q.Run(tx, `
			INSERT INTO file (
				Hash, Date, Thumbnail
			) VALUES (
				$1, $2, $3
			);
			SELECT id() as ID, Hash
				FROM file
				WHERE Hash = $1;`,
			f.Hash, f.Date, f.Thumbnail)
		if err != nil {
			return 0, fmt.Errorf("ql DB upsert new file by hash %q stmt %d: %w", f.Hash, failed, err)
		}
		row, err := rs[0].FirstRow()
		if err != nil {
			return 0, fmt.Errorf("ql DB upsert new file by hash %q: %w", f.Hash, err)
		}
		fileID = row[0].(int64)
	} else { // Update existing
		// FIXME: detect >1 rows returned and log error
		fileID = row[0].(int64)
	}

	err = db.addLocations(tx, fileID, f.Found)
	if err != nil {
		return 0, fmt.Errorf("ql DB upsert new file by hash %q: %w", f.Hash, err)
	}
	_, failed, err = db.q.Run(tx, `COMMIT;`)
	if err != nil {
		return 0, fmt.Errorf("ql DB upsert new file by hash %q stmt %d: %w", f.Hash, failed, err)
	}
	return fileID, nil
}

func (db *qldb) rollback(tx *ql.TCtx) {
	db.q.Run(tx, `ROLLBACK;`)
}

func (db *qldb) addLocations(tx *ql.TCtx, fileID int64, found map[string][]string) error {
	for backend, locations := range found {
		// Do we need to add backend?
		rs, failed, err := db.q.Run(tx, `
			SELECT id() AS ID
				FROM backend WHERE Tag = $1;`, backend)
		if err != nil {
			return fmt.Errorf("checking backend %q stmt %v: %w", backend, failed, err)
		}
		row, err := rs[0].FirstRow()
		if err != nil {
			return fmt.Errorf("checking backend %q: %w", backend, err)
		}
		if row == nil {
			rs, failed, err := db.q.Run(tx, `
				INSERT INTO backend VALUES ($1);
				SELECT id() AS ID
					FROM backend WHERE Tag = $1;`, backend)
			if err != nil {
				return fmt.Errorf("inserting backend %q stmt %v: %w", backend, failed, err)
			}
			row, err = rs[0].FirstRow()
			if err != nil {
				return fmt.Errorf("checking new backend %q: %w", backend, err)
			}
		}
		backendID := row[0].(int64)

		for _, l := range locations {
			// Do we need to add location?
			rs, failed, err := db.q.Run(tx, `
				SELECT id() AS ID
					FROM location
					WHERE BackendID = $1 AND Location = $2;`,
				backendID, l)
			if err != nil {
				return fmt.Errorf("checking location %q / %q stmt %v: %w", backend, l, failed, err)
			}
			row, err = rs[0].FirstRow()
			if err != nil {
				return fmt.Errorf("checking location %q / %q: %w", backend, l, err)
			}
			if row == nil {
				_, failed, err := db.q.Run(tx, `
					INSERT INTO location (
						FileID, BackendID, Location
					) VALUES ($1, $2, $3);`,
					fileID, backendID, l)
				if err != nil {
					return fmt.Errorf("inserting location %q / %q stmt %v: %w", backend, l, failed, err)
				}
			}
		}
	}
	return nil
}

func (db *qldb) FileEach(fn func(int64, *File) error) error {
	return nil // FIXME
	// panic("NIY")
}

func (db *qldb) File(id int64) (*File, error) {
	rs, failed, err := db.q.Run(nil, `
		SELECT Hash, Date, Thumbnail FROM file WHERE id() = $1;
		SELECT b.Tag, l.Location
			FROM (SELECT id() as ID, Tag FROM backend) as b, location as l
			WHERE b.ID = l.BackendID AND l.FileID = $1;
		`, id)
	if err != nil {
		return nil, fmt.Errorf("ql DB loading file %q stmt %v: %w", id, failed, err)
	}

	// Main fields of a File
	row, err := rs[0].FirstRow()
	if err != nil {
		return nil, fmt.Errorf("ql DB loading file %q: %w", id, err)
	}
	f := &File{
		Hash:      row[0].(string),
		Date:      row[1].(time.Time),
		Thumbnail: row[2].([]byte),
		Found:     map[string][]string{},
	}

	// .Found
	err = rs[1].Do(false, func(row []interface{}) (more bool, err error) {
		k, v := row[0].(string), row[1].(string)
		f.Found[k] = append(f.Found[k], v)
		return true, nil
	})
	if err != nil {
		return f, fmt.Errorf("ql DB loading file %q locations: %w", id, err)
	}
	for k := range f.Found {
		sort.Strings(f.Found[k])
	}
	return f, nil
}
