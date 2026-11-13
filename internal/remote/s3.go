package remote

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// emptyPayloadHash is SHA-256 of no body, the payload hash of a GET or a
// DELETE.
const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// S3Store mirrors a vault into an S3 bucket, or a store speaking the S3
// API. It signs requests itself with AWS Signature Version 4, so the app
// carries no AWS SDK.
type S3Store struct {
	endpoint *url.URL
	region   string
	bucket   string
	prefix   string

	accessKey    string
	secretKey    string
	sessionToken string

	pathStyle bool
	client    *http.Client

	target   string
	describe string

	// retryDelay is how long to wait before attempt n + 1; tests make it
	// zero.
	retryDelay func(attempt int) time.Duration
}

// s3Attempts is how many times an upload is tried before it fails: a
// CDN or a load balancer in front of the bucket may return a transient
// 5xx while the object is stored or the next request succeeds.
const s3Attempts = 4

// NewS3Store opens the bucket s names.
func NewS3Store(settings Settings) (*S3Store, error) {
	settings = settings.Normalized()
	// A store is only built for a remote that is turned on, and needs the
	// fields S3 cannot do without.
	settings.Enabled = true
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := s3Endpoint(settings)
	if err != nil {
		return nil, err
	}
	endpoint = trimDefaultPort(endpoint)
	prefix, err := cleanPrefix(settings.Prefix)
	if err != nil {
		return nil, err
	}
	pathStyle := settings.PathStyle
	// A virtual host needs a bucket name in front of a real host name; an
	// IP address or localhost, as a development MinIO has, cannot carry
	// one.
	if isLocalHost(endpoint.Hostname()) {
		pathStyle = true
	}
	s := &S3Store{
		endpoint:     endpoint,
		region:       settings.Region,
		bucket:       settings.Bucket,
		prefix:       prefix,
		accessKey:    settings.AccessKey,
		secretKey:    settings.SecretKey,
		sessionToken: settings.SessionToken,
		pathStyle:    pathStyle,
		client:       &http.Client{Timeout: putTimeout},
		target:       settings.Target(),
		describe:     settings.Describe(),
		retryDelay: func(attempt int) time.Duration {
			return time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
		},
	}
	return s, nil
}

// Target names the bucket and prefix.
func (s *S3Store) Target() string { return s.target }

// Describe names the bucket for people.
func (s *S3Store) Describe() string { return s.describe }

// objectURL returns the URL of rel and the canonical URI that is signed.
func (s *S3Store) objectURL(rel string) (*url.URL, error) {
	if err := checkRel(rel); err != nil {
		return nil, err
	}
	key := rel
	if s.prefix != "" {
		key = s.prefix + "/" + rel
	}
	base := strings.TrimSuffix(s.endpoint.EscapedPath(), "/")
	var raw string
	if s.pathStyle {
		raw = s.endpoint.Scheme + "://" + s.endpoint.Host + base + "/" +
			awsEscape(s.bucket) + "/" + awsEscapePath(key)
	} else {
		raw = s.endpoint.Scheme + "://" + s.bucket + "." + s.endpoint.Host + base + "/" +
			awsEscapePath(key)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	// Sign exactly the escaped path that is sent, whatever the key holds:
	// Chinese titles, spaces, a literal percent sign.
	u.RawPath = u.EscapedPath()
	return u, nil
}

// PutFile uploads path as rel. A 5xx or a network failure is tried
// again: PUT of the same key and bytes is idempotent.
func (s *S3Store) PutFile(ctx context.Context, rel, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	u, err := s.objectURL(rel)
	if err != nil {
		return err
	}
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	payloadHash := hex.EncodeToString(h.Sum(nil))
	var lastErr error
	for attempt := 1; attempt <= s3Attempts; attempt++ {
		if attempt > 1 {
			if err := s.waitRetry(ctx, attempt-1); err != nil {
				return fmt.Errorf("上传 %s 失败：%w", rel, err)
			}
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		// A section reader keeps the file open across attempts: the HTTP
		// client closes a body that is an io.Closer.
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(),
			io.NewSectionReader(f, 0, info.Size()))
		if err != nil {
			return err
		}
		req.ContentLength = info.Size()
		req.Header.Set("Content-Type", contentType)
		signV4(req, payloadHash, s.accessKey, s.secretKey, s.sessionToken, s.region, time.Now())
		lastErr = s.do(req)
		if lastErr == nil {
			return nil
		}
		if !s.retryable(lastErr) {
			break
		}
		log.Printf("swift: s3 upload %s: attempt %d failed: %v", rel, attempt, lastErr)
	}
	if lastErr == nil {
		return nil
	}
	if s.retryable(lastErr) {
		return fmt.Errorf("上传 %s 失败（已尝试 %d 次）：%w", rel, s3Attempts, lastErr)
	}
	return fmt.Errorf("上传 %s 失败：%w", rel, lastErr)
}

// PublicURL is where anyone can read rel, when the bucket allows it.
func (s *S3Store) PublicURL(rel string) (string, error) {
	u, err := s.objectURL(rel)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// MaxPresignExpiry is the longest a signed URL may live; S3 refuses more,
// and so does PresignGet.
const MaxPresignExpiry = 7 * 24 * time.Hour

// PresignGet signs a URL that reads rel until expires from now. It works
// on a bucket nobody may read, which keeps the share bucket private.
func (s *S3Store) PresignGet(rel string, expires time.Duration) (string, error) {
	if expires <= 0 || expires > MaxPresignExpiry {
		return "", errors.New("预签名有效期必须在 0 到 7 天之间")
	}
	u, err := s.objectURL(rel)
	if err != nil {
		return "", err
	}
	presignV4(u, s.accessKey, s.secretKey, s.sessionToken, s.region, time.Now(), expires)
	return u.String(), nil
}

// waitRetry waits before the next attempt, or returns early when the
// sync is cancelled.
func (s *S3Store) waitRetry(ctx context.Context, attempt int) error {
	delay := time.Duration(0)
	if s.retryDelay != nil {
		delay = s.retryDelay(attempt)
	}
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// retryable reports whether an upload may succeed if sent again.
func (s *S3Store) retryable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var httpErr *s3HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.Status {
		case http.StatusTooManyRequests, http.StatusInternalServerError,
			http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
		return false
	}
	// A connection reset or a temporary network failure can be tried
	// again; a missing file or a cancelled context cannot.
	return true
}

// Delete removes rel, trying again when the store is briefly unavailable.
func (s *S3Store) Delete(ctx context.Context, rel string) error {
	u, err := s.objectURL(rel)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 1; attempt <= s3Attempts; attempt++ {
		if attempt > 1 {
			if err := s.waitRetry(ctx, attempt-1); err != nil {
				return fmt.Errorf("删除远端 %s 失败：%w", rel, err)
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
		if err != nil {
			return err
		}
		signV4(req, emptyPayloadHash, s.accessKey, s.secretKey, s.sessionToken, s.region, time.Now())
		lastErr = s.do(req)
		if lastErr == nil {
			return nil
		}
		if !s.retryable(lastErr) {
			break
		}
		log.Printf("swift: s3 delete %s: attempt %d failed: %v", rel, attempt, lastErr)
	}
	if lastErr == nil {
		return nil
	}
	if s.retryable(lastErr) {
		return fmt.Errorf("删除远端 %s 失败（已尝试 %d 次）：%w", rel, s3Attempts, lastErr)
	}
	return fmt.Errorf("删除远端 %s 失败：%w", rel, lastErr)
}

// Ping writes and removes a small object, which checks the address, the
// credentials and the write permission at once.
func (s *S3Store) Ping(ctx context.Context) error {
	rel := ".swift-sync-probe-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	u, err := s.objectURL(rel)
	if err != nil {
		return err
	}
	body := []byte("ok")
	h := sha256.Sum256(body)
	var lastErr error
	for attempt := 1; attempt <= s3Attempts; attempt++ {
		if attempt > 1 {
			if err := s.waitRetry(ctx, attempt-1); err != nil {
				return fmt.Errorf("写入测试对象失败：%w", err)
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.ContentLength = int64(len(body))
		req.Header.Set("Content-Type", "text/plain")
		signV4(req, hex.EncodeToString(h[:]), s.accessKey, s.secretKey, s.sessionToken, s.region, time.Now())
		lastErr = s.do(req)
		if lastErr == nil {
			break
		}
		if !s.retryable(lastErr) {
			break
		}
		log.Printf("swift: s3 probe: attempt %d failed: %v", attempt, lastErr)
	}
	if lastErr != nil {
		return fmt.Errorf("写入测试对象失败：%w", lastErr)
	}
	if err := s.Delete(ctx, rel); err != nil {
		return fmt.Errorf("删除测试对象失败：%w", err)
	}
	return nil
}

// do sends a signed request and turns an error status into an error with
// what S3 said about it.
func (s *S3Store) do(req *http.Request) error {
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusNotFound && req.Method == http.MethodDelete:
		// Already gone is what a mirror wants.
		return nil
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	return &s3HTTPError{Status: resp.StatusCode, Message: truncate(message, 400)}
}

// s3HTTPError is an error status S3 or something in front of it returned.
type s3HTTPError struct {
	Status  int
	Message string
}

func (e *s3HTTPError) Error() string {
	message := fmt.Sprintf("S3 返回 %d：%s", e.Status, e.Message)
	if (e.Status == http.StatusBadGateway || e.Status == http.StatusServiceUnavailable ||
		e.Status == http.StatusGatewayTimeout) && strings.Contains(strings.ToLower(e.Message), "<html") {
		message += "（可能是 CDN 或反向代理的临时错误；若持续出现，请确认服务地址指向对象存储源站）"
	}
	return message
}

// signV4 adds AWS Signature Version 4 headers to req, signing host, the
// payload hash and the headers req carries.
func signV4(req *http.Request, payloadHash, accessKey, secretKey, sessionToken, region string, now time.Time) {
	amzDate := now.UTC().Format("20060102T150405Z")
	date := now.UTC().Format("20060102")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	if sessionToken != "" {
		req.Header.Set("x-amz-security-token", sessionToken)
	}

	type header struct{ name, value string }
	headers := []header{{"host", req.URL.Host}}
	for name := range req.Header {
		lower := strings.ToLower(name)
		if !signedHeader(lower) {
			continue
		}
		values := req.Header.Values(name)
		for i := range values {
			values[i] = normalizeHeaderValue(values[i])
		}
		headers = append(headers, header{lower, strings.Join(values, ",")})
	}
	sort.Slice(headers, func(i, j int) bool { return headers[i].name < headers[j].name })
	var canonicalHeaders strings.Builder
	names := make([]string, 0, len(headers))
	for _, h := range headers {
		canonicalHeaders.WriteString(h.name)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(h.value)
		canonicalHeaders.WriteByte('\n')
		names = append(names, h.name)
	}
	signedHeaders := strings.Join(names, ";")

	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	// The app never signs a request with a query.
	canonicalRequest := req.Method + "\n" + canonicalURI + "\n" + req.URL.RawQuery + "\n" +
		canonicalHeaders.String() + "\n" + signedHeaders + "\n" + payloadHash
	requestHash := sha256.Sum256([]byte(canonicalRequest))

	scope := date + "/" + region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" +
		hex.EncodeToString(requestHash[:])
	signature := hex.EncodeToString(hmacSHA256(signingKey(secretKey, date, region), stringToSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+accessKey+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
}

// presignV4 turns u into a URL that reads the object until expires from
// now. It signs the query, as AWS documents for a presigned GET, with an
// unsigned payload.
func presignV4(u *url.URL, accessKey, secretKey, sessionToken, region string, now time.Time, expires time.Duration) {
	amzDate := now.UTC().Format("20060102T150405Z")
	date := now.UTC().Format("20060102")
	scope := date + "/" + region + "/s3/aws4_request"
	q := url.Values{}
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", accessKey+"/"+scope)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", strconv.Itoa(int(expires/time.Second)))
	q.Set("X-Amz-SignedHeaders", "host")
	if sessionToken != "" {
		q.Set("X-Amz-Security-Token", sessionToken)
	}
	// Values are sorted by name and escaped as AWS canonicalizes them; no
	// value holds a space, which Go would write as a plus.
	query := q.Encode()
	canonicalURI := u.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalRequest := "GET\n" + canonicalURI + "\n" + query + "\n" +
		"host:" + u.Host + "\n\nhost\nUNSIGNED-PAYLOAD"
	requestHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" +
		hex.EncodeToString(requestHash[:])
	signature := hex.EncodeToString(hmacSHA256(signingKey(secretKey, date, region), stringToSign))
	u.RawQuery = query + "&X-Amz-Signature=" + signature
}

// signedHeader reports whether a header is part of the signature. The
// transport adds a few of its own after signing, and the host header is
// signed from the URL.
func signedHeader(name string) bool {
	switch name {
	case "authorization", "user-agent", "accept-encoding", "connection",
		"content-length", "expect", "transfer-encoding", "host":
		return false
	}
	return true
}

func signingKey(secret, date, region string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), date)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, "s3")
	return hmacSHA256(kService, "aws4_request")
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// normalizeHeaderValue trims a header value and collapses runs of spaces,
// as AWS canonicalizes it.
func normalizeHeaderValue(v string) string {
	return strings.Join(strings.Fields(v), " ")
}

// awsEscapePath escapes each segment of a slash-separated path, keeping
// the slashes.
func awsEscapePath(p string) string {
	parts := strings.Split(p, "/")
	for i := range parts {
		parts[i] = awsEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

// awsEscape percent-encodes everything but the RFC 3986 unreserved
// characters. S3 signs this form, not the plus-for-space form.
func awsEscape(s string) string {
	const digits = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(digits[c>>4])
			b.WriteByte(digits[c&15])
		}
	}
	return b.String()
}

// trimDefaultPort drops :443 from https and :80 from http endpoints, so
// the host that is signed is the host the server sees.
func trimDefaultPort(u *url.URL) *url.URL {
	host, port := u.Hostname(), u.Port()
	if port == "" {
		return u
	}
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		if strings.Contains(host, ":") {
			u.Host = "[" + host + "]"
		} else {
			u.Host = host
		}
	}
	return u
}

// isLocalHost reports whether host is an IP address or localhost, where a
// bucket cannot sit in front of the host name.
func isLocalHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	lower := strings.ToLower(host)
	return lower == "localhost" || strings.HasSuffix(lower, ".localhost")
}

// truncate shortens s to n runes for an error message.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
