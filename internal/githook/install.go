package githook

import (
	"fmt"
	"slices"
	"strings"
)

// Install writes block into the hook file at path: a new file gets a sh
// shebang and the block; an existing shell script gets the block after its
// shebang, or replaced in place when one is already there; an existing
// script of another interpreter is left alone with ErrForeignHook.
func Install(path, block string) error {
	h, err := readHook(path)
	if err != nil {
		return err
	}
	if h == nil {
		h = &hookFile{lines: []string{defaultShebang, "", block, ""}, mode: newHookMode}
		return h.write(path)
	}
	if !h.shellShebang() {
		return fmt.Errorf("%s %w: its first line is %q", path, ErrForeignHook, h.lines[0])
	}
	h.place(block)
	h.mode = executable(h.mode)
	return h.write(path)
}

// place puts block in the file: over the existing one, or right after the
// shebang, or first when there is no shebang. A blank line separates it
// from what surrounds it.
func (h *hookFile) place(block string) {
	blockLines := strings.Split(block, "\n")
	if begin, end, ok := h.blockBounds(); ok {
		h.lines = slices.Concat(h.lines[:begin], blockLines, h.lines[end+1:])
		return
	}
	at := 0
	if h.hasShebang() {
		at = 1
	}
	insert := slices.Concat([]string{""}, blockLines)
	if !h.isBlank(at) {
		insert = append(insert, "")
	}
	if at == 0 {
		insert = insert[1:]
	}
	h.lines = slices.Concat(h.lines[:at], insert, h.lines[at:])
}
