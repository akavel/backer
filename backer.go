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

	type source struct {
		root   string
		files  []string
		config *BackupConfig
	}
	var total int64
	var sources []source
	for root, bcfg := range config.Backups {
		fmt.Printf("Scanning %s", root)
		s := source{
			root:   root,
			config: bcfg,
		}
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			// TODO(akavel): handle empty dirs
			if info.IsDir() {
				return nil
			}
			s.files = append(s.files, path)
			total++
			if total%100 == 0 {
				fmt.Printf(".")
			}
			return nil
		})
		fmt.Println()
		if err != nil {
			return err
		}
		if len(s.files) == 0 {
			return fmt.Errorf("no files found in source directory %s", root)
		}
		sources = append(sources, s)
	}

	return nil
}
