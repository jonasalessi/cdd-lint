package agenthook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrSymlink reports a settings path that is a symlink; a dotfiles manager
// owns it.
var ErrSymlink = errors.New("is a symlink; a dotfiles manager owns it")

// newSettingsMode is the mode of a settings file the command creates.
const newSettingsMode = 0o644

// settingsFile is a JSON settings document together with the mode it is
// written back with.
type settingsFile struct {
	doc  map[string]any
	mode fs.FileMode
}

// readSettings loads the JSON object at path. A missing file is a nil
// settingsFile. Numbers are kept as written, so a round trip never turns
// 30 into 3e+01.
func readSettings(path string) (*settingsFile, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s %w", path, ErrSymlink)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc, err := parseObject(path, data)
	if err != nil {
		return nil, err
	}
	return &settingsFile{doc: doc, mode: info.Mode().Perm()}, nil
}

// parseObject decodes data as one JSON object.
func parseObject(path string, data []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%s: not a JSON object: %w", path, err)
	}
	if doc == nil || dec.More() {
		return nil, fmt.Errorf("%s: not a JSON object", path)
	}
	return doc, nil
}

// write stores the document atomically through a sibling .tmp and a
// rename, creating the directory when missing. The encoding is what a
// hand-edited settings file looks like: two-space indent, no HTML
// escaping, a final newline.
func (s *settingsFile) write(path string) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s.doc); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), s.mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
