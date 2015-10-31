package main

type Config struct {
	Main struct {
		ID    string `gcfg:"id"`
		DBDir string `gcfg:"db-dir"`
	}
	To struct {
		IDs      []string `gcfg:"id"`
		CfgPaths []string `gcfg:"cfg-path"`
	}
	Backup map[string]*struct {
		As string // optional
	}
}

type DstConfig struct {
	Main struct {
		ID string `gcfg:"id"`
	}
}
