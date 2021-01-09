package main

import (
	"sync"
	// "time"
	// "encoding/json"
	"fmt"
	// "io/ioutil"
	"log"

	tiedot "github.com/HouzuoGuo/tiedot/db"
	"github.com/icza/gowut/gwu"
)

type Backend interface {
	Open() (string, error)
	Walk(func(File)) error
}

// FIXME: move to config (json? ini? ...?) file and/or in DB
var tryBackends = []Backend{
	&WinDisk{Marker: `d:\backer-id.json`},
	&WinDisk{Marker: `c:\fotki\backer-id.json`},
}

// FIXME: move to config and/or flag
const dbPath = "database"

func main() {
	win := gwu.NewWindow("main", "Backer viewer")
	win.Style().SetFullWidth()

	db, err := tiedot.OpenDB(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// TODO: load data from DB

	// TODO: scan new data into DB
	//  - for now, date from JPEG always [if possible]
	//  - for now, only JPEGs
	//  - for now, calc sha256 hash & store path, incl. "disk ID"

	// Initialize & autodetect backends
	backends := map[string]Backend{}
	var wg sync.WaitGroup
	for _, b := range tryBackends {
		id, err := b.Open()
		if err != nil {
			debugf("attempt at %s", err)
			continue
		}
		// TODO(akavel): [LATER] error on duplicate (?)
		backends[id] = b
		// TODO(akavel): add backend ID to DB collection "Backends"

		// Start loading entries
		b := b
		wg.Add(1)
		go func() {
			defer wg.Done()
			// TODO: first, just load .jpg previews and show them + calc hash + save date in DB + save filename in DB
			// TODO[LATER]: handling of errors on files?
			debugf("scanning %s", id)
			files := make(chan File, 1)
			go func() {
				for f := range files {
					k, v := f.Found()
					fmt.Println("found:", f.Hash(), f.Date(), k, v)
				}
			}()
			err := b.Walk(func(f File) {
				files <- f
			})
			close(files)

			if err != nil {
				problemf("loading entries: %w", err)
				return
			}
		}()
	}
	wg.Wait()

	// TODO: show image previews with directory names, sorted by date

	// Create and start a GUI server (omitting error check)
	// TODO: port choice - randomize or take flag
	server := gwu.NewServer("backer-viewer", "localhost:8081")
	server.SetText("Backer viewer app")
	server.AddWin(win)
	// server.Start("main")
}

func debugf(format string, args ...interface{}) {
	log.Println("(debug)", fmt.Errorf(format, args...))
}

func problemf(format string, args ...interface{}) {
	log.Println("ERROR:", fmt.Errorf(format, args...))
}

func warnf(format string, args ...interface{}) {
	log.Println("Warning:", fmt.Errorf(format, args...))
}
