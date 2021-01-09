package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"io/ioutil"
	"path/filepath"
	"sync"
	"time"

	"github.com/rwcarlsen/goexif/exif"
)

type File interface {
	// FIXME(akavel): Size() int64
	Hash() string    // TODO[LATER]: kind info, e.g. sha256
	Date() time.Time // TODO[LATER]: source kind info, e.g. jpeg
	// Found maps backend ID to file ID in this backend (e.g. path)
	Found() (string, string)
	Thumbnail() []byte
}

type lazyDiskFile struct {
	Path    string
	Root    string
	FoundID string

	once      sync.Once
	hash      string
	date      time.Time
	thumbnail []byte
}

func (f *lazyDiskFile) Hash() string {
	f.once.Do(f.fetch)
	return f.hash
}

func (f *lazyDiskFile) Date() time.Time {
	f.once.Do(f.fetch)
	return f.date
}

func (f *lazyDiskFile) Found() (string, string) {
	return f.FoundID, f.Path
}

func (f *lazyDiskFile) Thumbnail() []byte {
	f.once.Do(f.fetch)
	return f.thumbnail
}

func (f *lazyDiskFile) fetch() {
	// TODO[LATER]: streamed processing
	path := filepath.Join(f.Root, f.Path)
	raw, err := ioutil.ReadFile(path)
	if err != nil {
		warnf("reading %q/%s: %w", f.FoundID, f.Path, err)
		return
	}

	// TODO[LATER] configurable thumbnail size
	f.thumbnail, err = thumbnailImage(bytes.NewReader(raw), 100, 100)
	if err != nil {
		warnf("thumbnailing %q/%s: %w", f.FoundID, f.Path, err)
	}

	// TODO[LATER] try https://godoc.org/github.com/dsoprea/go-exif/v3 to maybe also support PNGs
	x, err := exif.Decode(bytes.NewReader(raw))
	if err != nil {
		warnf("timestamping %q/%s: %w", f.FoundID, f.Path, err)
	}
	if x != nil {
		f.date, err = x.DateTime()
		if err != nil {
			warnf("timestamping %q/%s: %w", f.FoundID, f.Path, err)
		}
	}

	sha := sha256.Sum256(raw)
	f.hash = "sha256-b64:" +
		base64.StdEncoding.EncodeToString(sha[:])
}
