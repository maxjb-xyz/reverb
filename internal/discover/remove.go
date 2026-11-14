package discover

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SafeRemover returns a Remove func that deletes one downloaded file, and only
// if it is a regular file inside one of roots (the download and music
// folders). Folders it leaves empty are removed too, up to the root. Anything
// else is refused with an error, so a bad path in a download job can never
// reach the rest of the disk.
func SafeRemover(roots []string) func(path string) error {
	var clean []string
	for _, r := range roots {
		if strings.TrimSpace(r) == "" {
			continue
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		clean = append(clean, abs)
	}
	return func(path string) error {
		if strings.TrimSpace(path) == "" {
			return errors.New("no file path recorded")
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return err
		}
		info, err := os.Lstat(real)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		root := ""
		for _, r := range clean {
			if rel, err := filepath.Rel(r, real); err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
				root = r
				break
			}
		}
		if root == "" {
			return fmt.Errorf("%s is outside the music folders", path)
		}
		if err := os.Remove(real); err != nil {
			return err
		}
		// Tidy now-empty album/artist folders, never the root itself.
		for dir := filepath.Dir(real); dir != root && strings.HasPrefix(dir, root+string(filepath.Separator)); dir = filepath.Dir(dir) {
			if err := os.Remove(dir); err != nil {
				break // not empty (or not ours to remove): stop climbing
			}
		}
		return nil
	}
}
