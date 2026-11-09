package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo"
)

// appVersion is the version mygo build links from mygo.json. A plain
// `go run .` carries no package metadata, so in that case the project's
// mygo.json is read from the working directory (or a parent) instead.
func appVersion() string {
	if v := strings.TrimSpace(mygo.App.Version()); v != "" {
		return v
	}
	fallbackVersionOnce.Do(func() {
		fallbackVersion = versionFromMygoJSON()
	})
	if fallbackVersion != "" {
		return fallbackVersion
	}
	return "开发版"
}

var (
	fallbackVersionOnce sync.Once
	fallbackVersion     string
)

// versionFromMygoJSON reads the version of a mygo.json in the working
// directory, or in one of its parents: `go run .` and tests start in the
// project, while a packaged app has the version in its metadata already.
func versionFromMygoJSON() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for range 4 {
		if v := versionFromFile(filepath.Join(dir, "mygo.json")); v != "" {
			return v
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// versionFromFile reads the version field of a mygo.json, if it has one.
func versionFromFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(b, &cfg) != nil {
		return ""
	}
	return strings.TrimSpace(cfg.Version)
}
