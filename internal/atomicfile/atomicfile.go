// Package atomicfile replaces a file in one step, so a crash never leaves
// it half written and a symlink planted next to it is never written through.
package atomicfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Write stores data at path with mode. The data goes to a temporary file
// beside path that is created exclusively under a fresh name, so it can
// never be a link someone planted, and is then renamed over path. A rename
// replaces a symlink at path instead of writing through it.
func Write(path string, data []byte, mode fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	if err := fill(tmp, data, mode); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// fill writes data into f, gives it mode and closes it.
func fill(f *os.File, data []byte, mode fs.FileMode) error {
	_, writeErr := f.Write(data)
	return errors.Join(writeErr, f.Chmod(mode), f.Close())
}
