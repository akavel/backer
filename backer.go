package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

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
	var dst struct {
		id   string
		path string
	}
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
		// Destination directory where files will get copied.
		droot := filepath.Dir(dst.path)
		if as := config.Backups[src.root].As; as != "" {
			droot = filepath.Join(droot, as)
		} else {
			droot = filepath.Join(droot, filepath.Base(src.root))
		}

		missing, rest = append(missing, source{root: src.root}), append(rest, source{root: src.root})
		m, r := &missing[len(missing)-1], &rest[len(rest)-1]
		for _, path := range src.files {
			if n := total(missing) + total(rest) + 1; n == 1 || n%100 == 100 || n == total(sources) {
				fmt.Printf("Comparing... %d/%d\r", n, total(sources))
			}
			dpath := filepath.Join(droot, path)
			dinfo, err := os.Stat(dpath)
			switch {
			case os.IsNotExist(err):
				break
			case err != nil:
				return fmt.Errorf("cannot scan destination file: %s", err)
			}
			sinfo, err := os.Stat(filepath.Join(src.root, path))
			switch {
			case err != nil:
				return fmt.Errorf("cannot scan source file: %s", err)
			case dinfo != nil && dinfo.Size() == sinfo.Size() && dinfo.ModTime() == sinfo.ModTime():
				r.files = append(r.files, path)
			default:
				size += sinfo.Size()
				m.files = append(m.files, path)
			}
		}
	}
	fmt.Printf("\nMissing: %d/%d (%s)\n", total(missing), total(sources), human(size))

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
	if f >= 10 {
		f = float64(int(f + 0.5)) // TODO(akavel): round prettier
	}
	return fmt.Sprintf("%.2g %s", f, unit)
}
