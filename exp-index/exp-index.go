package main

import (
	"filepath"
	"log"
	"strings"

	tiedot "github.com/HouzuoGuo/tiedot/db"

	"github.com/akavel/backer/exp-view/query"
)

func main() {
	log.Printf("up")

	const dbPath = "../exp-view/database"
	backendIDs := []string{
		"bkup-mypassport-foiiwgfi", "sf7-c-fotki",
	}

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
	// dbFiles.Index([]string{"date"})
	// dbFiles.Index([]string{"hash"})
	for _, b := range backendIDs {
		err := dbFiles.Index([]string{"found", b})
		if err != nil && !strings.HasSuffix(err.Error(), "is already indexed") {
			log.Printf("index %q error: %s", b, err)
		}
	}

	const bID = "bkup-mypassport-foiiwgfi"
	const root = `d:\`
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			panic(err)
		}
		path = rel

		// Does such entry already exist?
		ids := map[int]struct{}{}
		err := tiedot.EvalQuery(
			query.Eq(path, query.Path{"found", bID}),
			dbFiles,
			&ids)
	})

	log.Printf("fin")
}
