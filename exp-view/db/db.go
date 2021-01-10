package db

import (
	"fmt"
	"strings"

	tiedot "github.com/HouzuoGuo/tiedot/db"

	"github.com/akavel/backer/exp-view/query"
)

type ErrNotFound struct{ error }

type DB interface {
	Close() error

	FileUpsert(f *File) (int64, error)
	FileEach(func(int64, *File) error) error
}

type tdb struct {
	t     *tiedot.DB
	files *tiedot.Col
}

type untyped = map[string]interface{}

func NewTiedot(path string) (DB, error) {
	t, err := tiedot.OpenDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening tiedot DB: %w", err)
	}
	files, err := tiedot.OpenCol(t, "Files")
	if err != nil {
		return nil, fmt.Errorf("initializing tiedot DB %q: %w", path, err)
	}
	indexes := [][]string{
		{"date"},
		{"hash"},
	}
	for _, ind := range indexes {
		err = files.Index(ind)
		if err != nil && !strings.HasSuffix(err.Error(), "is already indexed") {
			return nil, fmt.Errorf("initializing tiedot DB %q index %v: %w",
				path, ind, err)
		}
	}
	return tdb{
		t:     t,
		files: files,
	}, nil
}

func (db *tdb) Close() error { return db.t.Close() }

func (db *tdb) FileUpsert(f *db.File) (int64, error) {
	// validation
	{
		if len(f.Found) != 1 {
			panic(fmt.Errorf("logic error: UpsertFile got .Found >1: %v", *f))
		}
		for _, v := range f.Found {
			if len(v) != 1 {
				panic(fmt.Errorf("logic error: UpsertFile got .Found with >1 path: %v", *f))
			}
		}
	}

	// Does such File already exist?
	ids, err := db.query(db.files, query.Eq(f.Hash, query.Path{"hash"}))
	if err != nil {
		return 0, fmt.Errorf("tiedot DB upsert file by hash %q: %w", f.Hash, err)
	}
	switch len(ids) {
	case 0: // Insert new
		id, err := db.files.Insert(untyped{
			"hash":      f.Hash,
			"date":      f.Date,
			"thumbnail": base64.StdEncoding.EncodeToString(f.Thumbnail),
			"found":     f.Found,
		})
		if err != nil {
			return id, fmt.Errorf("tiedot DB upsert file by hash %q: %w", f.Hash, err)
		}
		return id, nil
	case 1: // Update existing
		err := db.files.UpdateBytesFunc(ids[0], func(before []byte) (after []byte, err error) {
			// unmarshal
			var doc fileDoc
			err = json.Unmarshal(before, &doc)
			if err != nil {
				return before, err
			}
			// validate .hash
			if doc.Hash != f.Hash {
				return before, fmt.Errorf("hash mismatch, db=%q, update=%q", doc.Hash, f.Hash)
			}
			// update .found by adding new path if needed
			found := migratedFound(doc.Found)
			var k string
			for k = range f.Found {
				break
			}
			paths := found[k]
			for _, p := range paths {
				if p == f.Found[k][0] {
					return before, nil // no need to change anything
				}
			}
			paths = append(paths, f.Found[k][0])
			sort.Strings(paths)
			doc.Found[k] = paths
			// marshal
			after, err = json.Marshal(doc)
			if err != nil {
				return before, err
			}
			return after, nil
		})
		if err != nil {
			return ids[0], fmt.Errorf("tiedot DB upsert file by hash %q: %w", f.Hash, err)
		}
		return ids[0], nil
	default:
		return 0, fmt.Errorf("tiedot DB upsert file by hash %q: found %d files: %v", f.Hash, len(ids), ids)
	}
}

func (db *tdb) query(col *tiedot.Col, q interface{}) ([]int64, error) {
	rawIDs := new(map[int]struct{})
	err := tiedot.EvalQuery(q, col, &rawIDs)
	if err != nil {
		return nil, err
	}
	if len(rawIDs) == 0 {
		return nil, nil
	}
	ids := make([]int64, 0, len(rawIDs))
	for id := range rawIDs {
		ids = append(ids, id)
	}
	return ids, nil
}
func (db *tdb) FileEach(fn func(int64, *File) error) error {
	var final error
	dbFiles.ForEachDoc(func(id int, doc []byte) (moveOn bool) {
		var f fileDoc
		err := json.Unmarshal(doc, &f)
		if err != nil {
			final = fmt.Errorf("failed to decode file: %s RAW: %s", err, string(doc))
			return false
		}
		thumb, _ := base64.StdEncoding.DecodeString(f.Thumbnail)
		final = fn(id, &File{
			Hash:      f.Hash,
			Date:      f.Date,
			Thumbnail: thumb,
			Found:     migratedFound(f.Found),
		})
		return final == nil
	})
	return final
}

// [TEMPORARY] backwards compat.
func migratedFound(old interface{}) map[string][]string {
	if fnd, ok := old.(map[string]map[string]struct{}); ok {
		m := map[string][]string{}
		for k, v := range fnd {
			var paths []string
			for p := range v {
				paths = append(p)
			}
			sort.Strings(paths)
			m[k] = paths
		}
		return m
	}
	if old == nil {
		return map[string][]string{}
	}
	return old.(map[string][]string)
}
