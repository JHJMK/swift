package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"swift/internal/remote"
)

func TestShareDurations(t *testing.T) {
	cases := []struct {
		d    shareDuration
		want time.Duration
	}{
		{shareOneDay, 24 * time.Hour},
		{shareThreeDays, 72 * time.Hour},
		{shareSevenDays, 7 * 24 * time.Hour},
		{shareForever, 0},
	}
	for _, c := range cases {
		if got := c.d.lifetime(); got != c.want {
			t.Fatalf("duration %d: %v, want %v", c.d, got, c.want)
		}
	}
	if len(shareDurationLabels) != 4 {
		t.Fatalf("%d duration labels", len(shareDurationLabels))
	}
}

// Share records live in the vault's state folder, one per document.
func TestShareRecords(t *testing.T) {
	a, _, rel := newTestApp(t)
	rec := shareRecord{Rel: rel, Title: "开始", ID: "abc", CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour)}
	if err := a.saveShareRecord(rec); err != nil {
		t.Fatal(err)
	}
	got := a.shareRecordFor(rel)
	if got == nil || got.ID != "abc" || got.Title != "开始" {
		t.Fatalf("record: %+v", got)
	}

	rec.ID = "def"
	if err := a.saveShareRecord(rec); err != nil {
		t.Fatal(err)
	}
	list := a.loadShares()
	if len(list) != 1 || list[0].ID != "def" {
		t.Fatalf("records: %+v", list)
	}

	if err := a.removeShareRecord(rel); err != nil {
		t.Fatal(err)
	}
	if a.shareRecordFor(rel) != nil {
		t.Fatal("record was not removed")
	}
}

// A permanent share points at the PDF itself; a timed one carries a
// signature that dies with it.
func TestShareLink(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.cfg.Sync = remote.Settings{
		Enabled: true, Provider: remote.ProviderS3,
		Endpoint: "https://s3.bitiful.net", Region: "cn-east-1",
		Bucket: "notes", ShareBucket: "public", SharePrefix: "share",
		AccessKey: "key", SecretKey: "secret",
	}
	link, err := a.shareLink(shareRecord{Rel: rel, ID: "abc", Title: "开始"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://public.s3.bitiful.net/share/abc/%E5%BC%80%E5%A7%8B.pdf"; link != want {
		t.Fatalf("permanent link %s, want %s", link, want)
	}

	signed, err := a.shareLink(shareRecord{Rel: rel, ID: "abc", Title: "开始", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(signed, "X-Amz-Signature=") {
		t.Fatalf("timed link is not signed: %s", signed)
	}

	if _, err := a.shareLink(shareRecord{Rel: rel, ID: "abc", Title: "开始", ExpiresAt: time.Now().Add(-time.Minute)}); err == nil {
		t.Fatal("an expired share still has a link")
	}

	// A record written by an older version has no key; its title still
	// names the file.
	fallback, err := a.shareLink(shareRecord{Rel: rel, ID: "abc", Title: "开始", Key: ""})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(fallback, "/abc/%E5%BC%80%E5%A7%8B.pdf") {
		t.Fatalf("fallback link %s", fallback)
	}
}

// A shared PDF downloads under the document's name, whatever the title
// holds.
func TestSharePDFName(t *testing.T) {
	cases := []struct{ title, want string }{
		{"项目计划", "项目计划.pdf"},
		{"a/b:c*?", "a-b-c--.pdf"},
		{"  ", "未命名.pdf"},
		{"...", "未命名.pdf"},
	}
	for _, c := range cases {
		if got := sharePDFName(c.title); got != c.want {
			t.Fatalf("title %q: %q, want %q", c.title, got, c.want)
		}
	}
}

// A permanent link that visitors cannot open is reported before it is
// shown.
func TestCheckSharePublic(t *testing.T) {
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer public.Close()
	if err := checkSharePublic(context.Background(), public.URL); err != nil {
		t.Fatalf("public file: %v", err)
	}

	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer private.Close()
	if err := checkSharePublic(context.Background(), private.URL); err == nil ||
		!strings.Contains(err.Error(), "公开访问") {
		t.Fatalf("private file: %v", err)
	}
}

// Choosing S3 shows the share bucket settings beside the mirror's.
func TestSettingsShareBucketField(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Sync = remote.Settings{
		Enabled:   true,
		Provider:  remote.ProviderS3,
		Bucket:    "notes",
		AccessKey: "key",
		SecretKey: "secret",
	}
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("远端同步"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"分享桶（可选）", "分享前缀", "分享时把文档导出成 PDF"} {
		if !tt.HasText(want) {
			t.Fatalf("settings lack %q: %q", want, tt.Texts())
		}
	}
	a.settingsOpen = false
	tt.Frame()
}

// The document's header has a share button that explains what is missing
// when no share bucket is set.
func TestShareButtonOpensDialog(t *testing.T) {
	a, _, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.Click("我的知识库"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("开始"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("分享"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("分享文档") {
		t.Fatalf("share dialog not shown: %q", tt.Texts())
	}
	if !tt.HasText("还没有设置分享桶") {
		t.Fatalf("missing setup hint: %q", tt.Texts())
	}
	a.share = nil
}
