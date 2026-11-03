package app

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is the app's settings, kept outside the vault so that the vault
// can be moved.
type Config struct {
	Workspace string `json:"workspace"`
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Swift", "config.json"), nil
}

func loadConfig() *Config {
	cfg := &Config{}
	p, err := configPath()
	if err != nil {
		return cfg
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(b, cfg)
	return cfg
}

func saveConfig(cfg *Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(p, b)
}
