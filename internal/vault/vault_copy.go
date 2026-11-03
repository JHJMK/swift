package vault

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// Copy copies a document or a folder under newParentRel and returns the
// new path. When deep is false a document goes alone and a folder is made
// empty; when deep is true its children go with it.
func (v *Vault) Copy(rel, newParentRel string, deep bool) (string, error) {
	_, oldName, err := splitRel(rel)
	if err != nil {
		return "", err
	}
	if newParentRel != "" && !v.isDir(newParentRel) {
		return "", errors.New("目标不是文件夹")
	}
	if rel == newParentRel || strings.HasPrefix(newParentRel, rel+"/") {
		return "", errors.New("不能复制到自身内部")
	}
	srcAbs, err := v.abs(rel)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(srcAbs)
	if err != nil {
		return "", err
	}
	dstParentAbs, err := v.abs(newParentRel)
	if err != nil {
		return "", err
	}

	ext := ""
	base := oldName
	if !info.IsDir() {
		ext = filepath.Ext(oldName)
		base = strings.TrimSuffix(oldName, ext)
	}
	newBase := v.uniqueCopyBase(dstParentAbs, base, ext, !info.IsDir())
	newAbs := filepath.Join(dstParentAbs, newBase+ext)
	newRel := path.Join(newParentRel, newBase+ext)

	if info.IsDir() {
		if deep {
			if err := copyTree(srcAbs, newAbs); err != nil {
				os.RemoveAll(newAbs)
				return "", err
			}
			v.copyOrder(rel, newRel)
		} else if err := os.Mkdir(newAbs, 0o755); err != nil {
			return "", err
		}
		v.insertOrder(newParentRel, newBase, "")
		return newRel, v.saveOrder()
	}

	if err := copyFile(srcAbs, newAbs); err != nil {
		return "", err
	}
	if !deep {
		v.insertOrder(newParentRel, newBase+ext, "")
		return newRel, v.saveOrder()
	}

	// A document's children live in the folder beside it; copy that too.
	if oldDirRel, ok := v.docDir(rel); ok {
		oldDirAbs := filepath.Join(v.Root, filepath.FromSlash(oldDirRel))
		newDirRel := path.Join(newParentRel, newBase)
		newDirAbs := filepath.Join(v.Root, filepath.FromSlash(newDirRel))
		if err := copyTree(oldDirAbs, newDirAbs); err != nil {
			os.Remove(newAbs)
			os.RemoveAll(newDirAbs)
			return "", err
		}
		v.copyOrder(oldDirRel, newDirRel)
		v.insertOrder(newParentRel, newBase, "")
	}
	v.insertOrder(newParentRel, newBase+ext, "")
	return newRel, v.saveOrder()
}

// uniqueCopyBase finds a name whose file and, for a document, whose
// children folder are free in parentAbs.
func (v *Vault) uniqueCopyBase(parentAbs, base, ext string, withDir bool) string {
	base = sanitizeName(base)
	name := base
	for i := 2; ; i++ {
		free := true
		if _, err := os.Lstat(filepath.Join(parentAbs, name+ext)); err == nil {
			free = false
		}
		if free && withDir {
			if _, err := os.Lstat(filepath.Join(parentAbs, name)); err == nil {
				free = false
			}
		}
		if free {
			return name
		}
		name = base + " " + strconv.Itoa(i)
	}
}

// copyOrder copies the order keys below a folder, leaving the source's
// keys in place.
func (v *Vault) copyOrder(oldRel, newRel string) {
	if oldRel == newRel {
		return
	}
	copied := map[string][]string{}
	for key, names := range v.Order {
		if key == oldRel || strings.HasPrefix(key, oldRel+"/") {
			copied[newRel+strings.TrimPrefix(key, oldRel)] = append([]string(nil), names...)
		}
	}
	for key, names := range copied {
		v.Order[key] = names
	}
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFile(p, target)
	})
}
