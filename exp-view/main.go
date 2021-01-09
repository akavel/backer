package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	tiedot "github.com/HouzuoGuo/tiedot/db"
	"github.com/icza/gowut/gwu"

	"github.com/akavel/backer/exp-view/query"
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

	dbFiles, err := tiedot.OpenCol(db, "Files")
	if err != nil {
		log.Fatal(err)
	}
	// TODO[LATER]: detect errors other than "already exists"
	dbFiles.Index([]string{"date"})
	dbFiles.Index([]string{"hash"})

	// TODO: load data from DB

	// TODO: scan new data into DB
	//  - for now, date from JPEG always [if possible]
	//  - for now, only JPEGs
	//  - for now, calc sha256 hash & store path, incl. "disk ID"

	// Initialize & autodetect backends
	backends := map[string]Backend{}
	files := make(chan File, 100)
	// var wg sync.WaitGroup
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
		// wg.Add(1)
		go func() {
			// defer wg.Done()
			// TODO: first, just load .jpg previews and show them + calc hash + save date in DB + save filename in DB
			// TODO[LATER]: handling of errors on files?
			debugf("scanning %s", id)
			err := b.Walk(func(f File) {
				files <- f
			})
			// close(files)

			if err != nil {
				problemf("loading entries: %w", err)
				return
			} else {
				debugf("DONE scanning %q", id)
			}
		}()
	}
	// wg.Wait()

	type itemForUI struct {
		Date time.Time
		DBID int // for showing thumbnail
	}
	itemsForUI := make(chan itemForUI, 100)

	go func() {
		// TODO: start by processing files not existing in DB under specific "Found" ID; only then refresh files already existing in DB
		for f := range files {
			k, v := f.Found()
			debugln("found:", f.Hash(), f.Date(), k, v)
			// Does such entry already exist?
			ids := map[int]struct{}{}
			err := tiedot.EvalQuery(
				query.Eq(f.Hash(), query.Path{"hash"}),
				dbFiles,
				&ids)
			if err != nil {
				log.Fatal("querying DB for hash:", err)
			}
			if len(ids) == 0 {
				id, err := dbFiles.Insert(map[string]interface{}{
					"hash":      f.Hash(),
					"date":      f.Date(),
					"thumbnail": f.Thumbnail(),
				})
				if err != nil {
					log.Fatal("inserting in DB:", err)
				}
				debugln("inserted:", id)
				itemsForUI <- itemForUI{
					Date: f.Date(),
					DBID: id,
				}
			} else {
				// FIXME: update DB
				for k := range ids {
					debugln("exists:", k)
					break
				}
			}
		}
	}()

	// Fetch data into UI from DB
	go func() {
		dbFiles.ForEachDoc(func(id int, doc []byte) (moveOn bool) {
			var f struct {
				Date time.Time `json:"date"`
			}
			err := json.Unmarshal(doc, &f)
			if err != nil {
				panic(fmt.Errorf("failed to decode file: %s\nRAW: %s", err, string(doc)))
			}
			debugln("decoded:", f.Date, id)
			itemsForUI <- itemForUI{
				DBID: id,
				Date: f.Date,
			}
			return true
		})
		debugf("DONE scanning DB")
	}()

	// UI state.
	// (For now, only: date + db_ID for retrieving thumbnail)
	type UIFile struct {
		Date time.Time
		DBID int // TODO[LATER]: some DB with explicit int64 for IDs?
		// gwu.Panel
	}
	type UIDate struct {
		Date string
		gwu.Panel

		Files []UIFile
	}
	var uiDates []UIDate

	// TMP
	uiDates = append(uiDates, UIDate{
		Panel: gwu.NewNaturalPanel(),
	})
	win.Add(uiDates[0].Panel)

	// TODO: show image previews with directory names, sorted by date
	// TODO[LATER]: pagination
	refresh := gwu.NewTimer(time.Second)
	refresh.SetRepeat(true)
	refresh.AddEHandlerFunc(func(e gwu.Event) {
		debugln("tick...")

		limit := 20
		for f := range itemsForUI {
			date := &uiDates[0]
			files := date.Files
			i := sort.Search(len(files), func(i int) bool {
				return !f.Date.After(files[i].Date) // f.date <= files[i].date
			})
			if i == len(files) {
				files = append(files, UIFile{
					Date: f.Date,
					DBID: f.DBID,
				})
				debugln("showing new", f.Date, f.DBID)
				img := gwu.NewImage("", fmt.Sprint("/thumb/", f.DBID))
				date.Panel.Add(img)
				e.MarkDirty(date.Panel)
				e.MarkDirty(img)
			} else if !f.Date.Equal(files[i].Date) {
				files = append(append(files[:i], UIFile{
					Date: f.Date,
					DBID: f.DBID,
				}), files[i:]...)
				debugln("showing old", f.Date, f.DBID)
				img := gwu.NewImage("", fmt.Sprint("/thumb/", f.DBID))
				date.Panel.Insert(img, i)
				e.MarkDirty(date.Panel)
				e.MarkDirty(img)
			} else {
				debugln("not showing", f.Date, f.DBID)
			}
			date.Files = files

			limit--
			if limit == 0 {
				return
			}
		}
	}, gwu.ETypeStateChange)
	win.Add(refresh) // TODO[LATER]: add to server instead, to make sure it always runs in bg

	// Serve thumbnails over HTTP for <img src="/thumb/...">
	http.Handle("/hash/", http.StripPrefix("/thumb/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		debugf("HASH QUERY! %s", r.URL.Path)
		ids := map[int]struct{}{}
		err := tiedot.EvalQuery(
			query.Eq(r.URL.Path, query.Path{"hash"}),
			dbFiles,
			&ids)
		if err != nil || len(ids) == 0 {
			warnf("THUMB querying DB for %q: %s", r.URL.Path, err)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// TODO[LATER]: warn if len(ids) > 1
		var doc map[string]interface{}
		for id := range ids {
			doc, err = dbFiles.Read(id)
			break
		}
		if err != nil {
			panic(err) // TODO[LATER]: don't panic
		}

		thumb := doc["thumbnail"].([]byte)
		// TODO[LATER]: provide more metadata below maybe?
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(thumb))
	})))

	// Create and start a GUI server (omitting error check)
	// TODO: port choice - randomize or take flag
	server := gwu.NewServer("backer-viewer", "localhost:8081")
	server.SetText("Backer viewer app")
	server.AddWin(win)
	server.Start("main")
}

func debugf(format string, args ...interface{}) {
	log.Println("(debug)", fmt.Errorf(format, args...))
}
func debugln(args ...interface{}) {
	log.Println(append([]interface{}{"(debug)"}, args...)...)
}

func problemf(format string, args ...interface{}) {
	log.Println("ERROR:", fmt.Errorf(format, args...))
}

func warnf(format string, args ...interface{}) {
	log.Println("Warning:", fmt.Errorf(format, args...))
}
