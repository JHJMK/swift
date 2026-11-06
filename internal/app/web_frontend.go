package app

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
)

// webAssets is the Obsidian-style editor's frontend, built by
// web/editor (npm run build) into internal/app/webassets.
//
//go:embed webassets
var webAssets embed.FS

// webFrontend serves the editor page at mygo://localhost/.
func webFrontend() fs.FS {
	sub, err := fs.Sub(webAssets, "webassets")
	if err != nil {
		log.Printf("swift: embedded editor: %v", err)
		return nil
	}
	return sub
}

// setupWebSupport registers the editor page, the scheme that serves
// pictures out of the vault, and the Go methods the editor calls. It must
// run before the window is created.
func (a *app) setupWebSupport() {
	if f := webFrontend(); f != nil {
		mygo.SetFrontend(f)
	}
	if err := mygo.Protocol.Handle("swiftassets", http.HandlerFunc(a.serveWebAsset)); err != nil {
		log.Printf("swift: asset scheme: %v", err)
	}
	mygo.BindAs("SwiftEditor", &webEditorService{a: a})
}

// serveWebAsset returns a file a Markdown document refers to. The editor
// requests swiftassets://localhost/asset?doc=<rel>&ref=<link>, and the
// paths are resolved inside the vault only.
func (a *app) serveWebAsset(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/asset" {
		http.NotFound(w, r)
		return
	}
	doc := r.URL.Query().Get("doc")
	ref := r.URL.Query().Get("ref")
	abs, ok := a.resolveWebAsset(doc, ref)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=60")
	http.ServeFile(w, r, abs)
}

// resolveWebAsset maps a link to a file inside the vault or one of its
// external knowledge bases.
func (a *app) resolveWebAsset(doc, ref string) (string, bool) {
	if a.vault == nil || ref == "" || strings.Contains(ref, "://") || strings.HasPrefix(ref, "data:") {
		return "", false
	}
	if decoded, err := url.PathUnescape(ref); err == nil {
		ref = decoded
	}
	var abs string
	if doc == "note" {
		ref = strings.TrimPrefix(ref, "./")
		clean := path.Clean(ref)
		if clean == "." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
			return "", false
		}
		// Note pictures are linked by their vault-relative path
		// (.swift/note-assets/...), as in the native editor; a bare name
		// is taken from the note's asset folder too.
		if strings.HasPrefix(clean, ".swift/note-assets/") {
			abs = filepath.Join(a.vault.Root, filepath.FromSlash(clean))
		} else {
			abs = filepath.Join(a.vault.Root, ".swift", "note-assets", filepath.FromSlash(clean))
		}
	} else {
		if doc == "" {
			return "", false
		}
		var ok bool
		abs, ok = a.vault.ResolveAsset(doc, ref)
		if !ok {
			return "", false
		}
	}
	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", false
	}
	if !a.underVault(abs) {
		return "", false
	}
	info, err := os.Stat(abs)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return abs, true
}

// underVault reports whether abs is inside the vault or an external
// knowledge base's folder.
func (a *app) underVault(abs string) bool {
	roots := []string{a.vault.Root}
	for _, root := range a.vault.External {
		roots = append(roots, root)
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		root, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			continue
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return true
		}
	}
	return false
}
