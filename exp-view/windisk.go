package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
)

type WinDisk struct {
	Marker string

	id string
}

func (d *WinDisk) Open() (string, error) {
	raw, err := ioutil.ReadFile(d.Marker)
	if err != nil {
		return "", fmt.Errorf("opening WinDisk marker: %w", err)
	}
	err = json.Unmarshal(raw, &struct {
		Id *string
	}{
		Id: &d.id,
	})
	if err != nil {
		return "", fmt.Errorf("opening WinDisk marker %q: %w", d.Marker, err)
	}
	fmt.Printf("MARKER %q\n", d.id)
	return d.id, nil
}

func (d *WinDisk) Walk(fn func(File)) error {
	root := filepath.Dir(d.Marker)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			warnf("walking %q/%s: %w", d.id, path, err)
			return nil
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			warnf("walking %q/%s: %w", d.id, path, err)
			return nil
		}
		path = rel
		switch strings.ToLower(filepath.Ext(path)) {
		case ".jpg", ".jpeg":
			// TODO[LATER]: streamed processing
			raw, err := ioutil.ReadFile(filepath.Join(root, path))
			if err != nil {
				warnf("reading %q/%s: %w", d.id, path, err)
				return nil
			}
			// TODO[LATER] configurable thumbnail size
			thumb, err := thumbnailImage(bytes.NewReader(raw), 100, 100)
			if err != nil {
				warnf("thumbnailing %q/%s: %w", d.id, path, err)
			}
			// TODO[LATER] try https://godoc.org/github.com/dsoprea/go-exif/v3 to maybe also support PNGs
			var t time.Time
			x, err := exif.Decode(bytes.NewReader(raw))
			if err != nil {
				warnf("timestamping %q/%s: %w", d.id, path, err)
			}
			if x != nil {
				t, err = x.DateTime()
				if err != nil {
					warnf("timestamping %q/%s: %w", d.id, path, err)
				}
			}
			sha := sha256.Sum256(raw)

			fn(File{
				Hash: "sha256-b64:" +
					base64.StdEncoding.EncodeToString(sha[:]),
				Date:      t,
				Found:     map[string]string{d.id: path},
				Thumbnail: string(thumb),
			})
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking WinDisk %q: %w", d.id, err)
	}
	return nil
}
