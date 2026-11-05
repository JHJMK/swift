package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DirStore mirrors a vault into a folder: the mount point of an NFS or
// SMB share, which the system keeps mounted for the app.
type DirStore struct {
	root     string
	provider Provider
}

// NewDirStore opens root, the mounted folder of an NFS or SMB share.
func NewDirStore(root string, provider Provider) (*DirStore, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("同步目录不可用：%v", err)
	}
	if !info.IsDir() {
		return nil, errors.New("同步目录不是文件夹")
	}
	return &DirStore{root: abs, provider: provider}, nil
}

// Target names the mounted folder.
func (s *DirStore) Target() string {
	return string(s.provider) + ":" + s.root
}

// Describe names the mounted folder for people.
func (s *DirStore) Describe() string {
	return s.provider.Label() + " · " + s.root
}

func (s *DirStore) path(rel string) (string, error) {
	if err := checkRel(rel); err != nil {
		return "", err
	}
	return filepath.Join(s.root, filepath.FromSlash(rel)), nil
}

// PutFile copies path to rel through a temporary file, so a reader of the
// share never sees a half-written document.
func (s *DirStore) PutFile(ctx context.Context, rel, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	dst, err := s.path(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".swift-sync-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	buf := make([]byte, 256<<10)
	for {
		if err := ctx.Err(); err != nil {
			tmp.Close()
			return err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if _, err := tmp.Write(buf[:n]); err != nil {
				tmp.Close()
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			tmp.Close()
			return readErr
		}
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return err
	}
	// Keep the source's time, so a copy of the vault looks like the vault.
	_ = os.Chtimes(dst, info.ModTime(), info.ModTime())
	return nil
}

// Delete removes rel and the folders that become empty around it, but
// not the share's own folder.
func (s *DirStore) Delete(ctx context.Context, rel string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := s.path(rel)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	// Folders the deleted file left empty go too, as a mirror has no use
	// for them.
	dir := filepath.Dir(p)
	for dir != s.root && strings.HasPrefix(dir, s.root+string(os.PathSeparator)) {
		if err := os.Remove(dir); err != nil {
			break
		}
		dir = filepath.Dir(dir)
	}
	return nil
}

// Ping checks that the share is mounted and writable.
func (s *DirStore) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name := fmt.Sprintf(".swift-sync-probe-%d", time.Now().UnixNano())
	p := filepath.Join(s.root, name)
	if err := os.WriteFile(p, []byte("ok"), 0o644); err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		return err
	}
	return nil
}
