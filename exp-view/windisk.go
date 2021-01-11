package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
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
	debugf("MARKER %q\n", d.id)
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
			// TODO[LATER]: sensibly handle errors (do loading in background?)
			fn(&lazyDiskFile{
				FoundID: d.id,
				Root:    root,
				Path:    path,
			})
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking WinDisk %q: %w", d.id, err)
	}
	return nil
}
