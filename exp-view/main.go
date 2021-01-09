package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"

	"github.com/icza/gowut/gwu"
)

type Backend interface {
	Open() error
}

type WinDisk struct {
	Marker string

	id string
}

func (d *WinDisk) Open() error {
	raw, err := ioutil.ReadFile(d.Marker)
	if err != nil {
		return fmt.Errorf("opening WinDisk marker %q: %w", d.Marker, err)
	}
	v := struct {
		Id *string
	}{
		Id: &d.id,
	}
	err = json.Unmarshal(raw, &v)
	if err != nil {
		return fmt.Errorf("opening WinDisk marker %q: %w", d.Marker, err)
	}
	fmt.Printf("MARKER %q\n", d.id)
	return nil
}

// FIXME: move to config (json? ini? ...?) file and/or in DB
var backends = []Backend{
	&WinDisk{Marker: `d:\backer-id.json`},
	&WinDisk{Marker: `c:\fotki\backer-id.json`},
}

func main() {
	win := gwu.NewWindow("main", "Backer viewer")
	win.Style().SetFullWidth()

	// TODO: load data from DB
	// TODO: scan new data into DB
	//  - for now, date from JPEG always [if possible]
	//  - for now, only JPEGs
	//  - for now, calc sha256 hash & store path, incl. "disk ID"

	for _, b := range backends {
		err := b.Open()
		if err != nil {
			debugf("attempt at %s", err)
			continue
		}

	}

	// TODO: show image previews with directory names, sorted by date

	// Create and start a GUI server (omitting error check)
	// TODO: port choice - randomize or take flag
	server := gwu.NewServer("backer-viewer", "localhost:8081")
	server.SetText("Backer viewer app")
	server.AddWin(win)
	server.Start("main")
}

func debugf(format string, args ...interface{}) {
	log.Printf("(debug) "+format, args...)
}
