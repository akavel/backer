package dbs

import (
	"time"
)

// type ErrNotFound struct{ error }

type DB interface {
	Close() error

	FileUpsert(f *File) (int64, error)
	FileEach(func(int64, *File) error) error
	File(id int64) (*File, error)
	FileByLocation(backend, location string) (*int64, error)
}

type File struct {
	Hash      string    `json:"hash"`
	Date      time.Time `json:"date"`
	Thumbnail []byte    `json:"thumbnail"`
	// Found maps backend ID to sorted list of file IDs in that backend
	Found map[string][]string `json:"found"`
}
