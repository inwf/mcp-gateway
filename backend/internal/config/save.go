package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

// configFileMode keeps the file readable only by its owner: it can carry
// API tokens in server headers and secrets in server environments.
const configFileMode os.FileMode = 0o600

// configDirMode matches configFileMode: no reason for the directory
// holding a private file to be world-readable.
const configDirMode os.FileMode = 0o700

// Save writes cfg to path, creating parent directories as needed.
//
// The write is atomic in the sense that matters here: a concurrent
// reader sees either the previous contents or the new contents, never a
// half-written file, and a failed write leaves the previous file intact.
// Durability across a power loss is not guaranteed, since the containing
// directory is not synced.
//
// Save does not validate cfg. Callers persisting user input should call
// [Config.Validate] first.
func Save(path string, cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}

// writeFileAtomic writes data to path by way of a temporary file that is
// renamed into place.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, configDirMode); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	// The temporary file has to be created in the destination directory:
	// a rename is only atomic within a single filesystem, and /tmp is
	// commonly a different one.
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	// Every path out of this function from here must leave no temporary
	// file behind. Both calls are no-ops once the happy path has closed
	// and renamed the file.
	defer func() {
		tmp.Close()
		os.Remove(tmpName)
	}()

	if err := tmp.Chmod(configFileMode); err != nil {
		return fmt.Errorf("set permissions on %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	// Flush before renaming. Without this, a crash shortly after the
	// rename can leave the new name pointing at an empty file, which is
	// worse than either the old or the new contents.
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("flush %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
