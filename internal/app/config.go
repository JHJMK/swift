package app

import (
	"encoding/json"
	"os"
	"path/filepath"

	"swift/internal/remote"
)

// Config is the app's settings, kept outside the vault so that the vault
// can be moved.
type Config struct {
	Workspace string `json:"workspace"`
	// FileTreeWidth is the width of the knowledge base tree.
	FileTreeWidth float32 `json:"fileTreeWidth,omitempty"`
	// OutlineWidth is the width of the web editor's outline.
	OutlineWidth float32 `json:"outlineWidth,omitempty"`
	// DefaultPageSettings are the page display settings new documents
	// start with, unless a document has settings of its own.
	DefaultPageSettings PageSettings `json:"defaultPageSettings,omitzero"`
	// OpenInPreview shows a document as a reader when it opens, without
	// the editor, as the preview button does.
	OpenInPreview bool `json:"openInPreview,omitempty"`
	// PageSettings are the display settings of documents that override
	// the defaults, by rel.
	PageSettings map[string]PageSettings `json:"pageSettings,omitempty"`
	// PinnedKBs are the knowledge bases shown in the sidebar's 常用 list,
	// in display order. A nil list means every knowledge base is shown,
	// as vaults from before the list existed do.
	PinnedKBs *[]string `json:"pinnedKBs,omitempty"`
	// Sync is the remote the vault is mirrored to, if any.
	Sync remote.Settings `json:"sync,omitempty"`
}

const (
	defaultFileTreeWidth = float32(220)
	minFileTreeWidth     = float32(180)
	maxFileTreeWidth     = float32(360)

	defaultOutlineWidth = float32(240)
	minOutlineWidth     = float32(180)
	maxOutlineWidth     = float32(420)

	defaultPageLineHeight = float32(1.75)
	defaultPageFontSize   = float32(16)
)

// PageSettings are a document page's display settings. They do not
// change its Markdown.
type PageSettings struct {
	LineHeight float32 `json:"lineHeight,omitempty"`
	FontSize   float32 `json:"fontSize,omitempty"`
}

// configDirOverride keeps tests from reading or writing the settings of
// the person running them.
var configDirOverride string

func configPath() (string, error) {
	if configDirOverride != "" {
		return filepath.Join(configDirOverride, "config.json"), nil
	}
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
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(p, b); err != nil {
		return err
	}
	// The file may hold S3 keys, so only this user reads it.
	return os.Chmod(p, 0o600)
}

func (a *app) fileTreeWidth() float32 {
	if a.cfg == nil {
		return defaultFileTreeWidth
	}
	return clampPanelWidth(a.cfg.FileTreeWidth, defaultFileTreeWidth, minFileTreeWidth, maxFileTreeWidth)
}

func (a *app) outlineWidth() float32 {
	if a.cfg == nil {
		return defaultOutlineWidth
	}
	return clampPanelWidth(a.cfg.OutlineWidth, defaultOutlineWidth, minOutlineWidth, maxOutlineWidth)
}

func (a *app) setOutlineWidth(width float32) error {
	if a.cfg == nil {
		return nil
	}
	width = clampPanelWidth(width, defaultOutlineWidth, minOutlineWidth, maxOutlineWidth)
	if a.cfg.OutlineWidth == width {
		return nil
	}
	a.cfg.OutlineWidth = width
	return saveConfig(a.cfg)
}

func clampPanelWidth(width, fallback, minWidth, maxWidth float32) float32 {
	if width <= 0 {
		return fallback
	}
	return min(max(width, minWidth), maxWidth)
}
