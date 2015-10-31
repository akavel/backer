package main

import (
	"flag"
	"fmt"
	"os"

	"gopkg.in/gcfg.v1"
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

	return nil
}
