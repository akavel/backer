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
	Backups map[string]*BackupConfig `gcfg:"Backup"`
}

type BackupConfig struct {
	As string // optional
}

type DstConfig struct {
	Main struct {
		ID string `gcfg:"id"`
	}
}
