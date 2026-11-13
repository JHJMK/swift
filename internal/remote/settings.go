// Package remote mirrors a vault to a remote place: an S3 bucket, or an
// NFS or SMB share mounted on the machine.
//
// The mirror is one-way, from the vault to the remote. The engine keeps a
// manifest of what it has sent (size, modification time and content hash
// of each file) in the vault's .swift folder, so it uploads a file when
// it appears or changes, however long ago it was first synced, and
// deletes it from the remote when it goes from the vault. A file moved to
// the trash is kept on the remote until it is deleted from the trash for
// good.
package remote

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Provider is the kind of remote the vault is mirrored to.
type Provider string

const (
	ProviderS3  Provider = "s3"
	ProviderNFS Provider = "nfs"
	ProviderSMB Provider = "smb"
)

// DefaultInterval is how often the engine checks the vault for changes
// when Settings.IntervalSeconds leaves it unset.
const DefaultInterval = 15 * time.Second

// defaultSharePrefix is where shared files sit in the share bucket when
// the settings leave the prefix empty.
const defaultSharePrefix = "share"

// Settings is the remote sync configuration, saved with the app's other
// settings.
type Settings struct {
	Enabled  bool     `json:"enabled,omitempty"`
	Provider Provider `json:"provider,omitempty"`

	// IntervalSeconds is how often the vault is checked for changes.
	IntervalSeconds int `json:"intervalSeconds,omitempty"`
	// Manual turns automatic syncing off: only a press of 立即同步 uploads
	// and deletes. Purging from the trash still removes remote copies at
	// once, as the explicit destructive action it is.
	Manual bool `json:"manual,omitempty"`

	// S3 settings.
	Endpoint     string `json:"endpoint,omitempty"`
	Region       string `json:"region,omitempty"`
	Bucket       string `json:"bucket,omitempty"`
	Prefix       string `json:"prefix,omitempty"`
	AccessKey    string `json:"accessKey,omitempty"`
	SecretKey    string `json:"secretKey,omitempty"`
	SessionToken string `json:"sessionToken,omitempty"`
	// PathStyle asks for https://host/bucket/key rather than
	// https://bucket.host/key. Self-hosted stores such as MinIO usually
	// need it.
	PathStyle bool `json:"pathStyle,omitempty"`

	// ShareBucket is where exported PDFs are published, when the provider
	// is S3. It uses the service and credentials above, so the backup
	// bucket can stay private. Empty turns sharing off.
	ShareBucket string `json:"shareBucket,omitempty"`
	// SharePrefix is the key prefix shared PDFs are stored under.
	SharePrefix string `json:"sharePrefix,omitempty"`

	// MountPath is the mounted folder of an NFS or SMB share.
	MountPath string `json:"mountPath,omitempty"`
}

// WithDefaults returns s with the values an empty setting stands for.
func (s Settings) WithDefaults() Settings {
	if s.IntervalSeconds <= 0 {
		s.IntervalSeconds = int(DefaultInterval / time.Second)
	}
	if strings.TrimSpace(s.Region) == "" {
		s.Region = "us-east-1"
	}
	return s
}

// Normalized returns s trimmed for saving: spaces around the fields go,
// and the mount path becomes absolute.
func (s Settings) Normalized() Settings {
	s = s.WithDefaults()
	s.Endpoint = strings.TrimSpace(s.Endpoint)
	s.Region = strings.TrimSpace(s.Region)
	s.Bucket = strings.TrimSpace(s.Bucket)
	s.AccessKey = strings.TrimSpace(s.AccessKey)
	s.SecretKey = strings.TrimSpace(s.SecretKey)
	s.SessionToken = strings.TrimSpace(s.SessionToken)
	if prefix, err := cleanPrefix(s.Prefix); err == nil {
		s.Prefix = prefix
	}
	s.ShareBucket = strings.TrimSpace(s.ShareBucket)
	if s.ShareBucket == "" {
		s.SharePrefix = ""
	} else if prefix, err := cleanPrefix(s.SharePrefix); err == nil {
		s.SharePrefix = prefix
		if s.SharePrefix == "" {
			s.SharePrefix = defaultSharePrefix
		}
	}
	s.MountPath = strings.TrimSpace(s.MountPath)
	if s.MountPath != "" {
		if abs, err := filepath.Abs(s.MountPath); err == nil {
			s.MountPath = abs
		}
	}
	return s
}

// Interval is how often the vault is checked for changes.
func (s Settings) Interval() time.Duration {
	return time.Duration(s.WithDefaults().IntervalSeconds) * time.Second
}

// Label names the provider as the settings show it.
func (p Provider) Label() string {
	switch p {
	case ProviderS3:
		return "S3"
	case ProviderNFS:
		return "NFS"
	case ProviderSMB:
		return "SMB"
	}
	return "关闭"
}

// Validate reports what keeps s from being used.
func (s Settings) Validate() error {
	if !s.Enabled {
		return nil
	}
	s = s.WithDefaults()
	if !s.Manual && s.IntervalSeconds < 5 {
		return errors.New("自动检查间隔至少 5 秒")
	}
	switch s.Provider {
	case ProviderS3:
		if _, err := s3Endpoint(s); err != nil {
			return err
		}
		if err := validateBucketName(s.Bucket); err != nil {
			return err
		}
		if _, err := cleanPrefix(s.Prefix); err != nil {
			return err
		}
		if s.ShareBucket != "" {
			if err := validateBucketName(s.ShareBucket); err != nil {
				return errors.New("分享桶：" + err.Error())
			}
			if _, err := cleanPrefix(s.SharePrefix); err != nil {
				return errors.New("分享前缀：" + err.Error())
			}
		}
		if strings.TrimSpace(s.AccessKey) == "" || strings.TrimSpace(s.SecretKey) == "" {
			return errors.New("请填写 S3 的 Access Key 和 Secret Key")
		}
	case ProviderNFS, ProviderSMB:
		path := strings.TrimSpace(s.MountPath)
		if path == "" {
			if s.Provider == ProviderNFS {
				return errors.New("请选择 NFS 共享的挂载点")
			}
			return errors.New("请选择 SMB 共享的挂载目录")
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("同步目录无效：%v", err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return fmt.Errorf("同步目录不可用：%v", err)
		}
		if !info.IsDir() {
			return errors.New("同步目录不是文件夹")
		}
	default:
		return errors.New("请选择远端同步的类型")
	}
	return nil
}

// Store builds the store s describes.
func (s Settings) Store() (Store, error) {
	s = s.Normalized()
	if err := s.Validate(); err != nil {
		return nil, err
	}
	switch s.Provider {
	case ProviderS3:
		return NewS3Store(s)
	case ProviderNFS, ProviderSMB:
		return NewDirStore(s.MountPath, s.Provider)
	}
	return nil, errors.New("未知的远端同步类型")
}

// ShareStore builds the store exported PDFs go to. It speaks to the same
// service as the mirror, with the same credentials, but writes to the
// share bucket and prefix.
func (s Settings) ShareStore() (*S3Store, error) {
	s = s.Normalized()
	if s.Provider != ProviderS3 {
		return nil, errors.New("公开分享只支持 S3")
	}
	if s.ShareBucket == "" {
		return nil, errors.New("请先在设置里填写分享桶")
	}
	if err := validateBucketName(s.ShareBucket); err != nil {
		return nil, errors.New("分享桶：" + err.Error())
	}
	prefix, err := cleanPrefix(s.SharePrefix)
	if err != nil {
		return nil, errors.New("分享前缀：" + err.Error())
	}
	shared := s
	shared.Bucket = s.ShareBucket
	shared.Prefix = prefix
	shared.ShareBucket = ""
	shared.SharePrefix = ""
	return NewS3Store(shared)
}

// validateBucketName checks a bucket name as S3 accepts it.
func validateBucketName(bucket string) error {
	if strings.TrimSpace(bucket) == "" {
		return errors.New("请填写 S3 存储桶")
	}
	if strings.ContainsAny(bucket, `/\`) || strings.ContainsAny(bucket, " \t\r\n") {
		return errors.New("S3 存储桶名称不能包含空格或斜杠")
	}
	return nil
}

// Target names the remote a manifest belongs to. A manifest made for
// another target is not reused, so changing the destination uploads the
// vault again instead of trusting what the old remote had.
func (s Settings) Target() string {
	s = s.Normalized()
	switch s.Provider {
	case ProviderS3:
		// The resolved endpoint matters: the same custom address with
		// another region is another place, and so is Amazon's own
		// endpoint in another region.
		endpoint := strings.TrimSpace(s.Endpoint)
		if u, err := s3Endpoint(s); err == nil {
			endpoint = u.Scheme + "://" + u.Host + strings.TrimSuffix(u.EscapedPath(), "/")
		}
		if endpoint == "" {
			endpoint = "amazonaws"
		}
		return "s3:" + endpoint + "/" + s.Bucket + "/" + s.Prefix
	case ProviderNFS, ProviderSMB:
		return string(s.Provider) + ":" + s.MountPath
	}
	return ""
}

// Describe names the remote for people.
func (s Settings) Describe() string {
	s = s.Normalized()
	switch s.Provider {
	case ProviderS3:
		where := s.Bucket
		if s.Prefix != "" {
			where += "/" + s.Prefix
		}
		if s.Endpoint != "" {
			return "S3 · " + where + " · " + s.Endpoint
		}
		return "S3 · " + where
	case ProviderNFS:
		return "NFS · " + s.MountPath
	case ProviderSMB:
		return "SMB · " + s.MountPath
	}
	return "未配置"
}

// s3Endpoint parses the endpoint, filling in Amazon S3's own when empty.
func s3Endpoint(s Settings) (*url.URL, error) {
	raw := strings.TrimSpace(s.Endpoint)
	region := strings.TrimSpace(s.Region)
	if region == "" {
		region = "us-east-1"
	}
	if raw == "" {
		if region == "us-east-1" {
			raw = "https://s3.amazonaws.com"
		} else {
			raw = "https://s3." + region + ".amazonaws.com"
		}
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("S3 服务地址无效：%v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("S3 服务地址必须以 http:// 或 https:// 开头")
	}
	if u.Host == "" {
		return nil, errors.New("S3 服务地址缺少主机名")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("S3 服务地址不能带查询参数或片段")
	}
	if u.User != nil {
		return nil, errors.New("S3 服务地址不能包含账号密码")
	}
	return u, nil
}

// cleanPrefix trims a remote prefix and checks that it does not climb
// out of the bucket.
func cleanPrefix(prefix string) (string, error) {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return "", nil
	}
	for _, part := range strings.Split(prefix, "/") {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("S3 前缀不能包含空段、. 或 ..")
		}
	}
	return prefix, nil
}
