package githook

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrForeignHook reports a hook file whose shebang names something other
// than a POSIX-compatible shell, so the block cannot be inserted.
var ErrForeignHook = errors.New("not a shell script")

// ErrSymlink reports a hook path that is a symlink; a hook manager owns it.
var ErrSymlink = errors.New("is a symlink; a hook manager owns it")

// shebangShells are the interpreters the block may run under.
var shebangShells = map[string]bool{"sh": true, "bash": true, "zsh": true}

const (
	defaultShebang = "#!/bin/sh"
	newHookMode    = 0o755
)

// hookFile is the content of one hook file split into lines.
type hookFile struct {
	lines []string
	mode  fs.FileMode
}

// readHook loads the file at path. A missing file is a nil hookFile.
func readHook(path string) (*hookFile, error) {
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
	return &hookFile{lines: strings.Split(string(data), "\n"), mode: info.Mode().Perm()}, nil
}

// write stores the file atomically through a sibling .tmp and a rename.
func (h *hookFile) write(path string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(h.lines, "\n")), h.mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, h.mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// hasShebang reports whether the first line is an interpreter line.
func (h *hookFile) hasShebang() bool {
	return len(h.lines) > 0 && strings.HasPrefix(h.lines[0], "#!")
}

// shellShebang reports whether the shebang names a shell the block runs
// under. A file without a shebang runs under sh, so it qualifies.
func (h *hookFile) shellShebang() bool {
	if !h.hasShebang() {
		return true
	}
	interpreter := shebangInterpreter(strings.Fields(strings.TrimPrefix(h.lines[0], "#!")))
	return shebangShells[filepath.Base(interpreter)]
}

// shebangInterpreter picks the program a shebang runs, looking through env
// and its options ("#!/usr/bin/env -S bash -e" runs bash).
func shebangInterpreter(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	if filepath.Base(fields[0]) != "env" {
		return fields[0]
	}
	for _, f := range fields[1:] {
		if !strings.HasPrefix(f, "-") {
			return f
		}
	}
	return ""
}

// executable adds the execute bit wherever the read bit is set, so a 0644
// hook becomes 0755 and a private 0600 one becomes 0700.
func executable(mode fs.FileMode) fs.FileMode {
	return mode | (mode&0o444)>>2
}

// blockBounds finds the marker lines; ok is false when there is no block.
func (h *hookFile) blockBounds() (begin, end int, ok bool) {
	begin, end = -1, -1
	for i, line := range h.lines {
		switch strings.TrimSpace(line) {
		case BeginMarker:
			if begin < 0 {
				begin = i
			}
		case EndMarker:
			if begin >= 0 && end < 0 {
				end = i
			}
		}
	}
	return begin, end, begin >= 0 && end > begin
}

// isBlank reports whether line i exists and holds only whitespace.
func (h *hookFile) isBlank(i int) bool {
	return i >= 0 && i < len(h.lines) && strings.TrimSpace(h.lines[i]) == ""
}

// onlyShebang reports whether nothing but the shebang and whitespace is
// left, which is what a file the command created looks like once its block
// is gone.
func (h *hookFile) onlyShebang() bool {
	for i, line := range h.lines {
		if i == 0 && h.hasShebang() {
			continue
		}
		if strings.TrimSpace(line) != "" {
			return false
		}
	}
	return true
}
