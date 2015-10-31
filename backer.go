package main

import (
	"bytes"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"gopkg.in/gcfg.v1" // TODO(akavel): use github.com/akavel/gcfg if pull request not merged
)

var (
	configPath = flag.String("cfg", "c:/backer/backer.cfg", "`path` to configuration file")
	// quality = flag.Int("q", 99, "`quality` of checks to perform")
)

func main() {
	err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	// Parse flags & config.
	flag.Parse()
	var config Config
	err := gcfg.ReadFileInto(&config, *configPath)
	if err != nil {
		return fmt.Errorf("cannot read config: %s", err)
	}

	// Find destination to backup to.
	var dst destination
Detect:
	for i, path := range config.To.CfgPaths {
		fmt.Printf("Detecting destination %d/%d...", i+1, len(config.To.CfgPaths))
		dcfg := DstConfig{}
		err := gcfg.ReadFileInto(&dcfg, path)
		if err != nil {
			fmt.Printf(" no (%s)\n", err)
			continue
		}
		// Does the destination match any known ID?
		for _, id := range config.To.IDs {
			if id != dcfg.Main.ID {
				continue
			}
			dst.id = id
			dst.path = path
			fmt.Printf(" OK: %s\n", dst.path)
			break Detect
		}
		fmt.Printf(" no (ID not matched: %s)\n", dcfg.Main.ID)
	}
	if dst.id == "" {
		return fmt.Errorf("cannot find any backup destination")
	}

	var sources []source
	for root, _ := range config.Backups {
		fmt.Printf("Scanning %s", root)
		sources = append(sources, source{root: root})
		src := &sources[len(sources)-1]
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			// TODO(akavel): handle empty dirs
			if info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return fmt.Errorf("cannot relativize path %q in %q", path, root)
			}
			src.files = append(src.files, rel)
			if total(sources)%100 == 0 {
				fmt.Printf(".")
			}
			return nil
		})
		fmt.Println()
		if err != nil {
			return err
		}
		if len(src.files) == 0 {
			return fmt.Errorf("no files found in source directory %s", root)
		}
	}
	fmt.Printf("Scanned %d files.\n", total)

	// Check which files we're missing in destination.
	var missing, rest []source
	var size int64
	for _, src := range sources {
		droot := dst.Root(src, &config)

		missing, rest = append(missing, source{root: src.root}), append(rest, source{root: src.root})
		m, r := &missing[len(missing)-1], &rest[len(rest)-1]
		for _, path := range src.files {
			if n := total(missing) + total(rest) + 1; n == 1 || n%100 == 100 || n == total(sources) {
				// TODO(akavel): print stats each 1s via goroutine, with MB/s from func 'compare'
				fmt.Printf("Comparing... %d/%d\r", n, total(sources))
			}
			dpath := filepath.Join(droot, path)
			spath := filepath.Join(src.root, path)
			dinfo, sinfo, err := finfos(dpath, spath)
			if err != nil {
				return err
			}
			if dinfo != nil && dinfo.Size() == sinfo.Size() && dinfo.ModTime() == sinfo.ModTime() {
				r.files = append(r.files, path)
			} else {
				m.files = append(m.files, path)
				size += sinfo.Size()
			}
		}
	}

	// Merge old working DB to local DB
	mergeDB(dst)

	// Check which files are absent in the local DB
	// FIXME(akavel): also, check which files differ to what is in the local DB
	// FIXME(akavel): level 2: also store & compare hash sum
	// FIXME(akavel): level 3: if high quality requested: missing, rest = append(missing, rest), nil
	{
		localDB, err := os.Open(filepath.Join(filepath.Dir(*configPath), fmt.Sprintf("db.%s.csv", dst.id)))
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot open CSV DB: %s", err)
		}
		dbInfos := map[string]dbInfo{}
		if err == nil {
			defer localDB.Close()
			r := csv.NewReader(localDB)
			for i := 0; ; i++ {
				rec, err := r.Read()
				if err == io.EOF {
					break
				}
				if err != nil || len(rec) < 3 {
					return fmt.Errorf("cannot parse CSV DB %s:%d: %s", localDB.Name(), i, err)
				}
				dbInfos[rec[0]] = dbInfo{
					size:    atoi(rec[1]), // FIXME(akavel): check error
					modTime: atot(rec[2]), // FIXME(akavel): check error
				}
			}
		}

		for _, src := range rest {
			m := source{root: src.root}
			for _, path := range src.files {
				sinfo, err := os.Stat(filepath.Join(src.root, path))
				if err != nil {
					return fmt.Errorf("cannot scan source file: %s", err)
				}
				dbInfo, found := dbInfos[path]
				if !found || sinfo.Size() != dbInfo.Size() || sinfo.ModTime() != dbInfo.ModTime() {
					m.files = append(m.files, path)
					size += sinfo.Size()
					// TODO(akavel): delete it from rest.files
				}
			}
			if len(m.files) > 0 {
				missing = append(missing, m)
			}
		}
	}

	fmt.Printf("\nMissing: %d/%d (%s)\n", total(missing), total(sources), human(size))

	// Collect information about processed files into a temporary CSV DB.
	workDB, err := os.Create(filepath.Join(filepath.Dir(*configPath), fmt.Sprintf("work.%s.csv", dst.id)))
	if err != nil {
		return fmt.Errorf("cannot create working CSV DB: %s", err)
	}
	defer workDB.Close()
	dst.done = csv.NewWriter(workDB)

	// Run the backup.
	// TODO(akavel): if high quality selected, run on 'rest' too
	// TODO(akavel): show stats: MB/s, ETA, # of files & total, # of bytes & total
	// TODO(akavel): install signal hander: on Ctrl-C, delete currently copied file ("cleanup"), then exit
	var copied struct {
		files int
		bytes int64
	}
	for _, src := range missing {
		droot := dst.Root(src, &config)
		for _, path := range src.files {
			dpath := filepath.Join(droot, path)
			spath := filepath.Join(src.root, path)
			dinfo, sinfo, err := finfos(dpath, spath)
			if err != nil {
				return err
			}

			copied.files++
			fmt.Printf("\rBacking up %d/%d (%s/%s) %s file...       \r",
				copied.files, total(missing),
				human(copied.bytes), human(size), human(sinfo.Size()))

			// Do we need to run the backup?
			if dinfo != nil {
				// If file contents equal, no need to backup.
				if dinfo.Size() == sinfo.Size() && compare(dpath, spath) {
					// TODO(akavel): os.Chtimes(dpath, sinfo.ModTime())?
					dst.Done(path, dinfo)
					copied.bytes += sinfo.Size()
					continue
				}
				moveto, err := mksuffix(dpath)
				if err != nil {
					return err
				}
				err = os.Rename(dpath, moveto)
				if err != nil {
					return fmt.Errorf("cannot rename %s to %s", dpath, filepath.Base(moveto))
				}
				// FIXME(akavel): log the filename to file on dst disk (ideally, make the path configurable)
				fmt.Printf("NOTE: %s exists but differs; renamed to %s\n", dpath, filepath.Base(moveto))
			}

			// Copy the bytes.
			err = backup(dpath, spath, sinfo.ModTime()) // TODO(akavel): return nbytes too, for better calculations
			if err != nil {
				os.Remove(dpath)
				fmt.Printf("\nERROR: cannot backup %s\n", spath)
				return err
			}
			dst.Done(path, dinfo)
			copied.bytes += sinfo.Size()
		}
	}

	fmt.Println("DONE.")
	return nil
}

type source struct {
	root  string
	files []string
}

func total(sources []source) int {
	n := 0
	for _, s := range sources {
		n += len(s.files)
	}
	return n
}

func round(f float64) float64 {
	return float64(int(f + 0.5)) // TODO(akavel): round prettier?
}

func human(size int64) string {
	f, unit := 0.0, "B"
	switch {
	case size > 900*1024*1024*1024:
		f, unit = float64(size)/(1024*1024*1024*1024), "TiB"
	case size > 900*1024*1024:
		f, unit = float64(size)/(1024*1024*1024), "GiB"
	case size > 900*1024:
		f, unit = float64(size)/(1024*1024), "MiB"
	case size > 1024:
		f, unit = float64(size/1024), "KiB"
	}

	switch {
	case f >= 10:
		f = round(f)
	case f >= 0.1:
		f = round(f*10) / 10
	}
	return fmt.Sprintf("%.0f %s", f, unit)
}

type destination struct {
	id   string
	path string
	done *csv.Writer
}

// Root of the destination directory tree where files should get copied.
func (dst destination) Root(src source, config *Config) string {
	home := filepath.Dir(dst.path)
	if as := config.Backups[src.root].As; as != "" {
		return filepath.Join(home, as)
	} else {
		return filepath.Join(home, filepath.Base(src.root))
	}
}

func (dst destination) Done(path string, info os.FileInfo) {
	dst.done.Write([]string{
		path,
		fmt.Sprint(info.Size()),
		info.ModTime().Format(time.RFC3339Nano),
	})
	dst.done.Flush()
	fmt.Print("*")
}

func finfos(dpath, spath string) (os.FileInfo, os.FileInfo, error) {
	dinfo, err := os.Stat(dpath)
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("cannot scan destination file: %s", err)
	}
	sinfo, err := os.Stat(spath)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot scan source file: %s", err)
	}
	return dinfo, sinfo, nil
}

func compare(path1, path2 string) bool {
	f1, err := os.Open(path1)
	if err != nil {
		return false
	}
	defer f1.Close()
	f2, err := os.Open(path2)
	if err != nil {
		return false
	}
	defer f2.Close()

	var buf1, buf2 [1024]byte
	for {
		n1, err1 := io.ReadFull(f1, buf1[:])
		n2, err2 := io.ReadFull(f2, buf2[:])
		if !bytes.Equal(buf1[:n1], buf2[:n2]) {
			return false
		}
		if (err1 == io.EOF || err1 == io.ErrUnexpectedEOF) && err1 == err2 {
			return true
		}
		if err1 != err2 {
			return false
		}
	}
}

func mksuffix(path string) (string, error) {
	const format = "%s.$backer%d"
	for i := 1; i <= 100; i++ {
		fname := fmt.Sprintf(format, path, i)
		_, err := os.Stat(fname)
		if os.IsNotExist(err) {
			return fname, nil
		}
	}
	return "", fmt.Errorf("cannot build filename for rename of %s", path)
}

func backup(dpath, spath string, chtime time.Time) error {
	sfile, err := os.Open(spath)
	if err != nil {
		// TODO(akavel): longer error msg?
		return err
	}
	defer sfile.Close()

	err = os.MkdirAll(filepath.Dir(dpath), 0755)
	if err != nil {
		return err
	}
	dfile, err := os.OpenFile(dpath, os.O_WRONLY|os.O_APPEND|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		// TODO(akavel): longer error msg?
		return err
	}
	_, err = io.Copy(dfile, sfile)
	if err != nil {
		dfile.Close()
		// TODO(akavel): longer error msg?
		return err
	}
	err = dfile.Close()
	if err != nil {
		// TODO(akavel): longer error msg?
		return err
	}
	// FIXME(akavel): are both args below ok?
	err = os.Chtimes(dpath, chtime, chtime)
	if err != nil {
		return err
	}
	return nil
}

type dbInfo struct {
	size    int64
	modTime time.Time
}

func (i dbInfo) Size() int64        { return i.size }
func (i dbInfo) ModTime() time.Time { return i.modTime }

func atoi(s string) int64 {
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		panic(err)
	}
	return i
}

func atot(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t
}

func mergeDB(dst destination) {
	workPath := filepath.Join(filepath.Dir(*configPath), fmt.Sprintf("work.%s.csv", dst.id))
	workDB, err := os.OpenFile(workPath, os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer workDB.Close()
	work, err := csv.NewReader(workDB).ReadAll()
	if err != nil && err != io.EOF {
		return
	}
	_, err = workDB.Seek(0, os.SEEK_END)
	if err != nil {
		fmt.Printf("(note: cannot seek to end in workDB: %s)\n", err)
		return
	}
	sort.Sort(sortedDB(work))

	localPath := filepath.Join(filepath.Dir(*configPath), fmt.Sprintf("db.%s.csv", dst.id))
	localDB, err := os.Open(localPath)
	if err == nil {
		r := csv.NewReader(localDB)
		w := csv.NewWriter(workDB)
		for {
			rec, err := r.Read()
			if err != nil {
				if err != io.EOF {
					fmt.Printf("(note: cannot read localDB record: %s)\n", err)
				}
				break
			}
			if len(rec) < 1 {
				fmt.Printf("(note: cannot read localDB record)\n")
				continue
			}
			i := sort.Search(len(work), func(i int) bool {
				return rec[0] < work[i][0]
			})
			if i >= len(work) || work[i][0] != rec[0] {
				w.Write(rec)
			}
		}
		w.Flush()
		localDB.Close()
		os.Remove(localPath)
	} else {
		fmt.Printf("(note: cannot open localDB: %s)\n", err)
	}
	workDB.Close()
	os.Rename(workPath, localPath)
}

type sortedDB [][]string

func (s sortedDB) Len() int           { return len(s) }
func (s sortedDB) Less(i, j int) bool { return s[i][0] < s[j][0] }
func (s sortedDB) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
