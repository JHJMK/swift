package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"swift/internal/remote"
)

// shareDuration is how long a shared PDF stays reachable.
type shareDuration int

const (
	shareOneDay shareDuration = iota
	shareThreeDays
	shareSevenDays
	shareForever
)

// shareDurationLabels names the choices in the share dialog, by
// shareDuration.
var shareDurationLabels = []string{"1 天", "3 天", "7 天", "永久"}

// lifetime is how long a link stays reachable; a permanent one has none.
func (d shareDuration) lifetime() time.Duration {
	switch d {
	case shareOneDay:
		return 24 * time.Hour
	case shareThreeDays:
		return 3 * 24 * time.Hour
	case shareSevenDays:
		return 7 * 24 * time.Hour
	}
	return 0
}

// shareRecord is a shared document: its PDF in the share bucket, and when
// the link stops working. A zero ExpiresAt means the link stays public.
type shareRecord struct {
	Rel       string    `json:"rel"`
	Title     string    `json:"title,omitempty"`
	ID        string    `json:"id"`
	Key       string    `json:"key,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

// shareDialog is the document's share popup: the choice of lifetime, and
// the link it produced.
type shareDialog struct {
	open     bool
	duration shareDuration
	busy     bool
	phase    string
	err      string
	link     string
	expires  time.Time
	record   *shareRecord
}

const shareFileName = "shares.json"

func (a *app) sharesPath() string {
	if a.vault == nil {
		return ""
	}
	return filepath.Join(a.vault.Root, ".swift", shareFileName)
}

// loadShares reads the shared documents of this vault. A file that cannot
// be read is treated as no shares, as the other state files do.
func (a *app) loadShares() []shareRecord {
	p := a.sharesPath()
	if p == "" {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var list []shareRecord
	if json.Unmarshal(b, &list) != nil {
		return nil
	}
	return list
}

func (a *app) saveShares(list []shareRecord) error {
	p := a.sharesPath()
	if p == "" {
		return errors.New("没有打开的文档目录")
	}
	if len(list) == 0 {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(p, b)
}

// shareRecordFor returns the record of rel, if it is shared.
func (a *app) shareRecordFor(rel string) *shareRecord {
	for _, r := range a.loadShares() {
		if r.Rel == rel {
			return &r
		}
	}
	return nil
}

// saveShareRecord adds or replaces the record of its document.
func (a *app) saveShareRecord(rec shareRecord) error {
	list := a.loadShares()
	kept := list[:0]
	for _, r := range list {
		if r.Rel != rec.Rel {
			kept = append(kept, r)
		}
	}
	return a.saveShares(append(kept, rec))
}

// removeShareRecord forgets the share of rel.
func (a *app) removeShareRecord(rel string) error {
	list := a.loadShares()
	kept := list[:0]
	for _, r := range list {
		if r.Rel != rel {
			kept = append(kept, r)
		}
	}
	return a.saveShares(kept)
}

// shareKey is where a PDF sits in the share bucket: a random folder, so
// links cannot be guessed, and a file named after the document, so a
// download carries the title the reader knows.
func shareKey(id, title string) string { return id + "/" + sharePDFName(title) }

// sharePDFName is the name a shared PDF downloads as.
func sharePDFName(title string) string { return sanitizeName(title) + ".pdf" }

// shareObjectKey is where the PDF of rec sits; a record made by an older
// version has no key and falls back to the name its title gives.
func shareObjectKey(rec shareRecord) string {
	if rec.Key != "" {
		return rec.Key
	}
	return shareKey(rec.ID, rec.Title)
}

// newShareID names one shared PDF.
func newShareID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// shareConfigured reports whether a document can be shared at all.
func (a *app) shareConfigured() bool {
	return a.cfg != nil && a.cfg.Sync.Provider == remote.ProviderS3 && a.cfg.Sync.ShareBucket != ""
}

// openShareDialog shows the share popup of the open document.
func (a *app) openShareDialog() {
	if a.doc == nil {
		return
	}
	a.flushDoc()
	a.refreshDocFromMarkdown()
	d := &shareDialog{open: true, duration: shareSevenDays}
	if rec := a.shareRecordFor(a.doc.Rel); rec != nil {
		if !rec.ExpiresAt.IsZero() && time.Now().After(rec.ExpiresAt) {
			// The link is dead; drop the leftover PDF without blocking
			// the dialog.
			_ = a.removeShareRecord(rec.Rel)
			a.deleteShareObject(*rec)
		} else {
			d.record = rec
			d.expires = rec.ExpiresAt
			if rec.ExpiresAt.IsZero() {
				d.duration = shareForever
			}
			if link, err := a.shareLink(*rec); err == nil {
				d.link = link
			}
		}
	}
	a.share = d
	a.update()
}

// shareLink builds the link of rec from the current settings.
func (a *app) shareLink(rec shareRecord) (string, error) {
	return shareLinkFor(a.cfg.Sync, rec)
}

// shareLinkFor builds the link of rec: its public address for a permanent
// share, or a signed one that stops working at its expiry.
func shareLinkFor(settings remote.Settings, rec shareRecord) (string, error) {
	settings = settings.Normalized()
	store, err := settings.ShareStore()
	if err != nil {
		return "", err
	}
	key := shareObjectKey(rec)
	if rec.ExpiresAt.IsZero() {
		return store.PublicURL(key)
	}
	left := time.Until(rec.ExpiresAt)
	if left <= 0 {
		return "", errors.New("分享已过期")
	}
	if left > remote.MaxPresignExpiry {
		left = remote.MaxPresignExpiry
	}
	return store.PresignGet(key, left)
}

// createShare exports the open document as a PDF and uploads it. The PDF
// is rendered on the main thread; the upload runs off it.
func (a *app) createShare() {
	d := a.share
	if d == nil || d.busy || a.doc == nil {
		return
	}
	// The settings are read here, on the main thread; the upload below
	// must not touch the config the settings window edits.
	settings := a.cfg.Sync
	store, err := settings.ShareStore()
	if err != nil {
		d.err = err.Error()
		a.update()
		return
	}
	now := time.Now()
	var expires time.Time
	if d.duration != shareForever {
		expires = now.Add(d.duration.lifetime())
	}
	title, rel := a.doc.Title, a.doc.Rel
	old := d.record
	id := newShareID()
	key := shareKey(id, title)
	d.busy = true
	d.phase = "正在生成 PDF…"
	d.err = ""
	d.link = ""
	a.update()

	a.printPDF(func(pdf []byte, err error) {
		if err != nil {
			if a.share == d {
				d.busy = false
				d.phase = ""
				d.err = "分享失败：" + err.Error()
				a.update()
			}
			return
		}
		if a.share == d {
			d.phase = "正在上传…"
			a.update()
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			path, err := writeTempPDF(pdf)
			if err == nil {
				defer os.Remove(path)
				err = store.PutFile(ctx, key, path)
			}
			var link string
			if err == nil {
				link, err = shareLinkFor(settings, shareRecord{ID: id, Key: key, ExpiresAt: expires})
			}
			if err == nil && expires.IsZero() {
				// A permanent link is only useful when the bucket lets
				// anyone read it.
				err = checkSharePublic(ctx, link)
			}
			if err != nil {
				// Do not leave a PDF behind that no record points at.
				_ = store.Delete(context.Background(), key)
				mygo.RunOnMain(func() {
					if a.share == d {
						d.busy = false
						d.phase = ""
						d.err = "分享失败：" + err.Error()
					}
					a.update()
				})
				return
			}
			if old != nil && old.ID != id {
				_ = store.Delete(context.Background(), shareObjectKey(*old))
			}
			mygo.RunOnMain(func() {
				rec := shareRecord{Rel: rel, Title: title, ID: id, Key: key, CreatedAt: now, ExpiresAt: expires}
				if err := a.saveShareRecord(rec); err != nil {
					a.setError(err)
				}
				if a.share == d {
					d.busy = false
					d.phase = ""
					d.link = link
					d.expires = expires
					d.record = &rec
				}
				a.flashToast("分享链接已生成")
				a.update()
			})
		}()
	})
}

// writeTempPDF stores the rendered PDF where the uploader can read it.
func writeTempPDF(pdf []byte) (string, error) {
	f, err := os.CreateTemp("", "swift-share-*.pdf")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if _, err := f.Write(pdf); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

// revokeShare deletes the PDF of the open document and forgets it.
func (a *app) revokeShare() {
	d := a.share
	if d == nil || d.busy || d.record == nil {
		return
	}
	rec := *d.record
	store, err := a.cfg.Sync.ShareStore()
	if err != nil {
		d.err = err.Error()
		a.update()
		return
	}
	d.busy = true
	d.phase = "正在撤销…"
	d.err = ""
	a.update()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := store.Delete(ctx, shareObjectKey(rec))
		mygo.RunOnMain(func() {
			if a.share == d {
				d.busy = false
				d.phase = ""
				if err != nil {
					d.err = "撤销失败：" + err.Error()
				} else {
					d.record = nil
					d.link = ""
					d.expires = time.Time{}
				}
			}
			if err == nil {
				if err := a.removeShareRecord(rec.Rel); err != nil {
					a.setError(err)
				}
				a.flashToast("已撤销分享")
			}
			a.update()
		})
	}()
}

// deleteShareObject removes a PDF in the background, for a share that has
// expired or is being forgotten.
func (a *app) deleteShareObject(rec shareRecord) {
	store, err := a.cfg.Sync.ShareStore()
	if err != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = store.Delete(ctx, shareObjectKey(rec))
	}()
}

// pruneShares removes the PDFs of shares whose link has expired: a signed
// link is unreachable by then, and this only keeps the bucket tidy.
func (a *app) pruneShares() {
	if a.vault == nil {
		return
	}
	now := time.Now()
	var expired []shareRecord
	for _, r := range a.loadShares() {
		if !r.ExpiresAt.IsZero() && now.After(r.ExpiresAt) {
			expired = append(expired, r)
		}
	}
	if len(expired) == 0 {
		return
	}
	store, err := a.cfg.Sync.ShareStore()
	if err != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		for _, r := range expired {
			_ = store.Delete(ctx, shareObjectKey(r))
		}
		mygo.RunOnMain(func() {
			// Drop only what is still expired: a share made while this ran
			// has a new record and must stay.
			now := time.Now()
			list := a.loadShares()
			kept := list[:0]
			for _, r := range list {
				if r.ExpiresAt.IsZero() || now.Before(r.ExpiresAt) {
					kept = append(kept, r)
				}
			}
			_ = a.saveShares(kept)
			a.update()
		})
	}()
}

// checkSharePublic opens a permanent link the way a visitor would, so a
// share bucket that is not public read is reported instead of handing out
// a link that shows 403.
func checkSharePublic(ctx context.Context, link string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return err
	}
	// One byte is enough to tell whether the file is public.
	req.Header.Set("Range", "bytes=0-0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("无法访问分享链接：%v", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusUnauthorized:
		return errors.New("分享桶没有开启公开访问")
	case resp.StatusCode >= 400 && resp.StatusCode != http.StatusRequestedRangeNotSatisfiable:
		return fmt.Errorf("分享链接返回 %d", resp.StatusCode)
	}
	return nil
}

// shareModal is the share popup: how long the link lives, and the link.
func (a *app) shareModal(c *ui.Context) {
	d := a.share
	if d == nil || !d.open {
		return
	}
	t := c.Theme()
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(520).Padding(22).Gap(14).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Width(30).Height(30).Radius(15).Background(t.Accent.Alpha(0.12)).
					Justify(ui.Center).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconShare).FontSize(16).TextColor(t.Accent)
				})
				ui.Text(c, "分享文档").FontSize(17).Bold()
			})

			if !a.shareConfigured() {
				a.shareSetupHint(c)
				return
			}

			ui.Text(c, "把当前文档导出成 PDF，上传到分享桶，生成一个链接；任何拿到链接的人都能查看。").
				FontSize(12).TextColor(t.TextMuted)

			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "链接有效期").FontSize(13)
				label := shareDurationLabels[d.duration]
				sel := ui.Select(c, &label, shareDurationLabels).Label("链接有效期").Width(150)
				if sel.Changed() {
					for i, name := range shareDurationLabels {
						if name == label {
							d.duration = shareDuration(i)
						}
					}
				}
			})

			if d.duration == shareForever {
				ui.Text(c, "永久链接要求分享桶开启公开访问；重新分享会让旧链接失效。").
					FontSize(12).TextColor(t.TextMuted)
			} else {
				ui.Text(c, "到期后链接自动失效，分享的 PDF 会从分享桶里清理掉。").
					FontSize(12).TextColor(t.TextMuted)
			}

			if d.err != "" {
				ui.Text(c, d.err).FontSize(12).TextColor(t.Danger)
			}

			if d.link != "" {
				ui.Column(c).Gap(6).Children(func() {
					ui.Text(c, "分享链接").FontSize(12).TextColor(t.TextMuted)
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Grow(1).MinWidth(0).Padding(8, 10).Radius(6).Background(ui.Hex("#f5f6f7")).
							Border(1, ui.Hex("#e6e7ea")).Children(func() {
							ui.Text(c, d.link).FontSize(12).Selectable()
						})
						if ui.Button(c, "复制").Clicked() {
							mygo.Clipboard.WriteText(d.link)
							a.flashToast("链接已复制")
						}
					})
					if d.expires.IsZero() {
						ui.Text(c, "永久有效").FontSize(12).TextColor(t.TextMuted)
					} else {
						ui.Text(c, "有效至 "+d.expires.Local().Format("2006-01-02 15:04")).
							FontSize(12).TextColor(t.TextMuted)
					}
				})
			}

			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if d.record != nil && !d.busy {
					if ui.Button(c, "撤销分享").Clicked() {
						a.revokeShare()
					}
				}
				ui.Box(c).Grow(1)
				if d.busy {
					phase := d.phase
					if phase == "" {
						phase = "正在处理…"
					}
					ui.Text(c, phase).FontSize(12).TextColor(t.TextMuted)
				}
				if ui.Button(c, "关闭").Clicked() {
					open = false
				}
				label := "生成分享链接"
				if d.record != nil {
					label = "重新分享"
				}
				if ui.PrimaryButton(c, label).Clicked() {
					a.createShare()
				}
			})
		})
	})
	if !open {
		a.share = nil
	}
}

// shareSetupHint tells how to turn sharing on, and opens the settings at
// the remote sync section.
func (a *app) shareSetupHint(c *ui.Context) {
	t := c.Theme()
	ui.Text(c, "还没有设置分享桶。分享需要 S3 远端和一个允许公开读取的桶。").
		FontSize(13).TextColor(t.TextMuted)
	ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
		if ui.Button(c, "关闭").Clicked() {
			a.share = nil
			a.update()
		}
		if ui.PrimaryButton(c, "去设置").Clicked() {
			a.share = nil
			a.settingsOpen = true
			a.settingsSection = settingsSync
			a.update()
		}
	})
}
