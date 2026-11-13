package remote

import (
	"net/url"
	"testing"
	"time"
)

// The presigner reproduces AWS's published example for a signed GET:
// https://docs.aws.amazon.com/AmazonS3/latest/API/sigv4-query-string-auth.html
func TestPresignV4MatchesAWSExample(t *testing.T) {
	u, err := url.Parse("https://examplebucket.s3.amazonaws.com/test.txt")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	presignV4(u, "AKIAIOSFODNN7EXAMPLE",
		"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "", "us-east-1", now, 24*time.Hour)
	want := "aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404"
	if got := u.Query().Get("X-Amz-Signature"); got != want {
		t.Fatalf("signature:\n got %s\nwant %s\nurl %s", got, want, u.String())
	}
	if got := u.Query().Get("X-Amz-Credential"); got != "AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request" {
		t.Fatalf("credential: %s", got)
	}
}

// A bucket nobody may read still yields a link that lives for a while.
func TestPresignGet(t *testing.T) {
	store, err := NewS3Store(Settings{
		Enabled:   true,
		Provider:  ProviderS3,
		Endpoint:  "https://s3.example.com",
		Region:    "cn-east-1",
		Bucket:    "notes",
		Prefix:    "vault",
		AccessKey: "AKIDEXAMPLE",
		SecretKey: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	link, err := store.PresignGet("知识库/文档.pdf", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "notes.s3.example.com" {
		t.Fatalf("host: %s", u.Host)
	}
	if u.EscapedPath() != "/vault/%E7%9F%A5%E8%AF%86%E5%BA%93/%E6%96%87%E6%A1%A3.pdf" {
		t.Fatalf("path: %s", u.EscapedPath())
	}
	q := u.Query()
	if q.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" || q.Get("X-Amz-Expires") != "86400" ||
		q.Get("X-Amz-Signature") == "" || q.Get("X-Amz-SignedHeaders") != "host" {
		t.Fatalf("query: %v", q)
	}
	if _, err := store.PresignGet("a.pdf", 8*24*time.Hour); err == nil {
		t.Fatal("an expiry S3 refuses was accepted")
	}
}

// PublicURL is the plain, unsigned address of a shared file.
func TestPublicURL(t *testing.T) {
	store, err := NewS3Store(Settings{
		Enabled:   true,
		Provider:  ProviderS3,
		Endpoint:  "https://s3.bitiful.net",
		Region:    "cn-east-1",
		Bucket:    "notes",
		AccessKey: "AKIDEXAMPLE",
		SecretKey: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.PublicURL("share/abc.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://notes.s3.bitiful.net/share/abc.pdf"; got != want {
		t.Fatalf("url: %s, want %s", got, want)
	}

	pathStyle, err := NewS3Store(Settings{
		Enabled:   true,
		Provider:  ProviderS3,
		Endpoint:  "https://minio.example.com",
		Bucket:    "notes",
		PathStyle: true,
		AccessKey: "AKIDEXAMPLE",
		SecretKey: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err = pathStyle.PublicURL("a.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://minio.example.com/notes/a.pdf"; got != want {
		t.Fatalf("path style url: %s, want %s", got, want)
	}
}

// The share store writes to the share bucket with the mirror's service
// and credentials.
func TestShareStore(t *testing.T) {
	settings := Settings{
		Enabled:     true,
		Provider:    ProviderS3,
		Endpoint:    "https://s3.bitiful.net",
		Region:      "cn-east-1",
		Bucket:      "backup",
		Prefix:      "notes",
		AccessKey:   "AKIDEXAMPLE",
		SecretKey:   "secret",
		ShareBucket: "public",
		SharePrefix: "shared",
	}
	store, err := settings.ShareStore()
	if err != nil {
		t.Fatal(err)
	}
	if store.bucket != "public" || store.prefix != "shared" {
		t.Fatalf("store %q/%q", store.bucket, store.prefix)
	}
	link, err := store.PublicURL("x.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://public.s3.bitiful.net/shared/x.pdf"; link != want {
		t.Fatalf("link %s, want %s", link, want)
	}

	if _, err := (Settings{Provider: ProviderS3, Enabled: true}).ShareStore(); err == nil {
		t.Fatal("sharing without a share bucket was accepted")
	}
	if _, err := (Settings{Provider: ProviderS3, Enabled: true, ShareBucket: "a/b"}).ShareStore(); err == nil {
		t.Fatal("a share bucket with a slash was accepted")
	}
}
