package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
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

var (
	debug = flag.Bool("debug", false, "Enable debugging messages")
)

func main() {
	flag.Parse()

	win := gwu.NewWindow("main", "Backer viewer")
	win.Style().SetFullWidth()
	// win.Add(gwu.NewHTML(`<h1>Backer</h1>`))

	type Counter struct {
		name string
		n    *int64
	}
	loaded := []Counter{{"ui", new(int64)}}
	renderLoaded := func() string {
		var b strings.Builder
		fmt.Fprint(&b, "Loaded:")
		for _, c := range loaded {
			n := atomic.LoadInt64(c.n)
			fmt.Fprintf(&b, " %q: %v", c.name, n)
		}
		return b.String()
	}
	loadedPane := gwu.NewLabel(renderLoaded())
	win.Add(loadedPane)

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
		n := new(int64)
		loaded = append(loaded, Counter{id, n})
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
				k, v := f.Found()
				debugf("found %q: %q", k, v)
				files <- f
				atomic.AddInt64(n, 1)
			})
			// close(files)

			if err != nil {
				problemf("loading entries: %w", err)
				return
			} else {
				infof("DONE scanning %q", id)
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
			infof("processing: %q %s", k, v)
			// debugln("processing:", f.Hash(), f.Date(), k, v)
			infof("processing fetch: %s %v %q %s", f.Hash(), f.Date(), k, v)
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
					"thumbnail": base64.StdEncoding.EncodeToString(f.Thumbnail()),
					"found": map[string]map[string]struct{}{
						k: map[string]struct{}{
							v: struct{}{},
						},
					},
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
				for id := range ids {
					debugln("exists:", id)
					infof("update start %v", id)
					err := dbFiles.UpdateBytesFunc(id, func(before []byte) (after []byte, err error) {
						infof("in update %v", id)
						// unmarshal
						var doc struct {
							Hash      string                         `json:"hash"`
							Date      time.Time                      `json:"date"`
							Thumbnail string                         `json:"thumbnail"`
							Found     map[string]map[string]struct{} `json:"found"`
						}
						err = json.Unmarshal(before, &doc)
						if err != nil {
							return before, err
						}
						// validate .hash
						if doc.Hash != f.Hash() {
							return before, fmt.Errorf("hash mismatch, db=%q, update=%q", doc.Hash, f.Hash())
						}
						// update .found with new path
						if doc.Found == nil {
							doc.Found = map[string]map[string]struct{}{}
						}
						if doc.Found[k] == nil {
							doc.Found[k] = map[string]struct{}{}
						}
						doc.Found[k][v] = struct{}{}
						// [TEMPORARY] update .thumbnail, old may be wrongly rotated
						doc.Thumbnail = base64.StdEncoding.EncodeToString(f.Thumbnail())
						// [DEBUG] .found
						tmp, _ := json.Marshal(doc.Found)
						debugf("updating %q: %s", doc.Hash, string(tmp))
						// marshal
						after, err = json.Marshal(doc)
						if err != nil {
							return before, err
						}
						return after, nil
					})
					infof("update end %v", id)
					if err != nil {
						problemf("updating %v in DB: %w", id, err)
					}
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
		infof("DONE scanning DB")
	}()

	photos := gwu.NewNaturalPanel()
	win.Add(photos)
	win.CellFmt(photos).Style().SetFullWidth()

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

	// // TMP
	// uiDates = append(uiDates, UIDate{
	// 	Panel: gwu.NewNaturalPanel(),
	// })
	// date := &uiDates[0]
	// win.Add(date.Panel)
	// win.CellFmt(date.Panel).Style().SetFullWidth()

	// TODO: show image previews with directory names, sorted by date
	// TODO[LATER]: pagination
	refresh := gwu.NewTimer(time.Second)
	refresh.SetRepeat(true)
	refresh.AddEHandlerFunc(func(e gwu.Event) {
		debugln("tick...")
		// date.Panel.Add(gwu.NewHTML(`<p>datetick...</p>`))

		// loadedPane.SetText(fmt.Sprintf("Loaded: %d", loaded))
		loadedPane.SetText(renderLoaded())
		e.MarkDirty(loadedPane)

		for i := 0; i < 100; i++ {
			var f itemForUI
			select {
			case f = <-itemsForUI:
				// OK
			default:
				continue
			}

			// Find date row for specific file, or create if not found
			const fmtDate = "2006-01-02"
			dateStr := f.Date.Format(fmtDate)
			i := sort.Search(len(uiDates), func(i int) bool {
				return dateStr <= uiDates[i].Date
			})
			if i == len(uiDates) {
				uiDates = append(uiDates, UIDate{
					Date:  dateStr,
					Panel: gwu.NewNaturalPanel(),
				})
				uiDates[i].Panel.Add(gwu.NewHTML(`<h3>` + dateStr + `</h3>`))
				photos.Add(uiDates[i].Panel)
				photos.CellFmt(uiDates[i].Panel).Style().SetFullWidth()
			} else if dateStr != uiDates[i].Date {
				uiDates = append(uiDates[:i], append([]UIDate{{
					Date:  dateStr,
					Panel: gwu.NewNaturalPanel(),
				}}, uiDates[i:]...)...)
				uiDates[i].Panel.Add(gwu.NewHTML(`<h3>` + dateStr + `</h3>`))
				photos.Insert(uiDates[i].Panel, i)
				photos.CellFmt(uiDates[i].Panel).Style().SetFullWidth()
			}
			date := &uiDates[i]

			tmp := []string{}
			for _, d := range uiDates {
				tmp = append(tmp, d.Date)
			}
			// debugf("DATES: %v", tmp)

			// Find file with same date (and then skip), or create if not found
			files := date.Files
			i = sort.Search(len(files), func(i int) bool {
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
				// e.MarkDirty(img)
				atomic.AddInt64(loaded[0].n, 1)
			} else if !f.Date.Equal(files[i].Date) {
				files = append(files[:i], append([]UIFile{{
					Date: f.Date,
					DBID: f.DBID,
				}}, files[i:]...)...)
				debugln("showing old", f.Date, f.DBID)
				img := gwu.NewImage("", fmt.Sprint("/thumb/", f.DBID))
				date.Panel.Insert(img, i)
				e.MarkDirty(date.Panel)
				// e.MarkDirty(img)
				atomic.AddInt64(loaded[0].n, 1)
			} else {
				debugln("not showing", f.Date, f.DBID)
			}
			date.Files = files

			e.MarkDirty(photos)
		}

		debugln("ITERATION FIN")
	}, gwu.ETypeStateChange)
	win.Add(refresh) // TODO[LATER]: add to server instead, to make sure it always runs in bg

	// Serve thumbnails over HTTP for <img src="/thumb/...">
	http.Handle("/thumb/", http.StripPrefix("/thumb/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// debugf("HASH QUERY! %s", r.URL.Path)
		id, err := strconv.ParseInt(r.URL.Path, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		doc, err := dbFiles.Read(int(id))
		if err != nil {
			warnf("/thumb/%v DB error: %s", id, err)
			w.WriteHeader(http.StatusNotFound)
		}

		thumbEnc := doc["thumbnail"].(string)
		thumb, _ := base64.StdEncoding.DecodeString(thumbEnc)

		// TODO[LATER]: provide more metadata below maybe?
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader([]byte(thumb)))
	})))

	// Create and start a GUI server (omitting error check)
	// TODO: port choice - randomize or take flag
	server := gwu.NewServer("backer-viewer", "localhost:8081")
	server.SetText("Backer viewer app")
	server.AddWin(win)
	server.Start("main")
}

func debugf(format string, args ...interface{}) {
	if *debug {
		log.Println("(debug)", fmt.Errorf(format, args...))
	}
}
func debugln(args ...interface{}) {
	if *debug {
		log.Println(append([]interface{}{"(debug)"}, args...)...)
	}
}

func infof(format string, args ...interface{}) {
	log.Println("info:", fmt.Errorf(format, args...))
}

func problemf(format string, args ...interface{}) {
	log.Println("ERROR:", fmt.Errorf(format, args...))
}

func warnf(format string, args ...interface{}) {
	log.Println("Warning:", fmt.Errorf(format, args...))
}
