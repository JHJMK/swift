package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The signer reproduces AWS's published S3 example, which checks the
// canonical request, the signing key and the HMAC together.
func TestSignV4MatchesAWSS3Example(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=0-9")
	now := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	signV4(req, emptyPayloadHash, "AKIAIOSFODNN7EXAMPLE",
		"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "", "us-east-1", now)
	want := "AWS4-HMAC-SHA256 " +
		"Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, " +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("authorization:\n got %s\nwant %s", got, want)
	}
}

type fakeS3 struct {
	mu       sync.Mutex
	objects  map[string][]byte
	paths    []string
	status   int
	failures int
	attempts int
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.attempts++
	fail := f.failures
	if fail > 0 {
		f.failures--
	}
	f.mu.Unlock()
	if fail > 0 {
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, "<html><body>502 Bad Gateway</body></html>")
		return
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		io.WriteString(w, "<Error><Code>AccessDenied</Code><Message>denied</Message></Error>")
		return
	}
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=") {
		http.Error(w, "unsigned", http.StatusForbidden)
		return
	}
	sum := sha256.Sum256(body)
	if got := r.Header.Get("x-amz-content-sha256"); got != hex.EncodeToString(sum[:]) {
		http.Error(w, "bad payload hash", http.StatusBadRequest)
		return
	}
	if r.Host == "" {
		http.Error(w, "no host", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		if f.objects == nil {
			f.objects = map[string][]byte{}
		}
		f.objects[r.URL.Path] = body
		f.paths = append(f.paths, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		delete(f.objects, r.URL.Path)
		f.paths = append(f.paths, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
	}
}

func newTestS3Store(t *testing.T, server *httptest.Server) *S3Store {
	t.Helper()
	store, err := NewS3Store(Settings{
		Enabled:   true,
		Provider:  ProviderS3,
		Endpoint:  server.URL,
		Region:    "us-east-1",
		Bucket:    "notes",
		Prefix:    "vault",
		AccessKey: "AKIDEXAMPLE",
		SecretKey: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// PutFile sends the file under the bucket and prefix, with the key
// escaped, and Delete removes it.
func TestS3StorePutAndDelete(t *testing.T) {
	fake := &fakeS3{}
	server := httptest.NewServer(fake)
	defer server.Close()
	store := newTestS3Store(t, server)

	path := filepath.Join(t.TempDir(), "中文 文档.md")
	if err := os.WriteFile(path, []byte("# 标题\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.PutFile(context.Background(), "知识库/中文 文档.md", path); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	got, ok := fake.objects["/notes/vault/知识库/中文 文档.md"]
	fake.mu.Unlock()
	if !ok || string(got) != "# 标题\n" {
		t.Fatalf("object not stored: %q (%v)", got, ok)
	}

	if err := store.Delete(context.Background(), "知识库/中文 文档.md"); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	_, ok = fake.objects["/notes/vault/知识库/中文 文档.md"]
	fake.mu.Unlock()
	if ok {
		t.Fatal("object not deleted")
	}
	if server := fake.paths; len(server) != 2 {
		t.Fatalf("requests: %v", server)
	}
}

// A delete of an object that is already gone is not an error.
func TestS3DeleteMissingIsFine(t *testing.T) {
	fake := &fakeS3{}
	server := httptest.NewServer(fake)
	defer server.Close()
	store := newTestS3Store(t, server)
	// The fake returns 204 for any delete; return a 404 instead.
	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer notFound.Close()
	missing := newTestS3Store(t, notFound)
	if err := missing.Delete(context.Background(), "nope.md"); err != nil {
		t.Fatalf("missing delete: %v", err)
	}
	if err := store.Delete(context.Background(), "nope.md"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

// Ping writes and removes its probe object.
func TestS3Ping(t *testing.T) {
	fake := &fakeS3{}
	server := httptest.NewServer(fake)
	defer server.Close()
	store := newTestS3Store(t, server)
	if err := store.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	left := len(fake.objects)
	fake.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d probe objects left", left)
	}
}

// An error status becomes an error with what the store said.
func TestS3ErrorStatus(t *testing.T) {
	fake := &fakeS3{status: http.StatusForbidden}
	server := httptest.NewServer(fake)
	defer server.Close()
	store := newTestS3Store(t, server)
	path := filepath.Join(t.TempDir(), "a.md")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := store.PutFile(context.Background(), "a.md", path)
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("error is %v", err)
	}
}

// The object URL follows the path style, the virtual-host style and the
// escaping S3 signs.
func TestS3ObjectURL(t *testing.T) {
	pathStore, err := NewS3Store(Settings{
		Enabled: true, Provider: ProviderS3,
		Endpoint: "https://minio.example.com/base", Bucket: "notes",
		Prefix: "vault", AccessKey: "a", SecretKey: "s", PathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := pathStore.objectURL("知识库/中 文.md")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := u.String(), "https://minio.example.com/base/notes/vault/%E7%9F%A5%E8%AF%86%E5%BA%93/%E4%B8%AD%20%E6%96%87.md"; got != want {
		t.Fatalf("path style URL is\n%s\nwant\n%s", got, want)
	}
	u, err = pathStore.objectURL("100% #1?.md")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := u.String(), "https://minio.example.com/base/notes/vault/100%25%20%231%3F.md"; got != want {
		t.Fatalf("escaped key URL is\n%s\nwant\n%s", got, want)
	}

	hostStore, err := NewS3Store(Settings{
		Enabled: true, Provider: ProviderS3,
		Endpoint: "https://s3.example.com", Bucket: "notes",
		Prefix: "vault", AccessKey: "a", SecretKey: "s",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err = hostStore.objectURL("a.md")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := u.String(), "https://notes.s3.example.com/vault/a.md"; got != want {
		t.Fatalf("virtual host URL is\n%s\nwant\n%s", got, want)
	}
}

// A transient 502, as a CDN in front of the bucket returns, is tried
// again; the same bytes and signature are sent each time.
func TestS3RetriesTransientUpload(t *testing.T) {
	fake := &fakeS3{failures: 2}
	server := httptest.NewServer(fake)
	defer server.Close()
	store := newTestS3Store(t, server)
	store.retryDelay = func(int) time.Duration { return 0 }
	path := filepath.Join(t.TempDir(), "a.md")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.PutFile(context.Background(), "a.md", path); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	attempts := fake.attempts
	fake.mu.Unlock()
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

// A persistent 5xx stops after a short number of attempts and says so.
func TestS3StopsRetryingPersistent5xx(t *testing.T) {
	fake := &fakeS3{failures: 10}
	server := httptest.NewServer(fake)
	defer server.Close()
	store := newTestS3Store(t, server)
	store.retryDelay = func(int) time.Duration { return 0 }
	path := filepath.Join(t.TempDir(), "a.md")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := store.PutFile(context.Background(), "a.md", path)
	if err == nil || !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "已尝试") {
		t.Fatalf("error is %v", err)
	}
	fake.mu.Lock()
	attempts := fake.attempts
	fake.mu.Unlock()
	if attempts != s3Attempts {
		t.Fatalf("attempts = %d, want %d", attempts, s3Attempts)
	}
}
