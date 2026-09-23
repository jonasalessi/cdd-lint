package githook

import (
	"os"
	"slices"
)

// Remove takes the block out of the hook file at path and reports whether
// there was one. A file that held nothing but the block is deleted; any
// other file keeps its remaining content and mode.
func Remove(path string) (bool, error) {
	h, err := readHook(path)
	if err != nil || h == nil {
		return false, err
	}
	begin, end, ok := h.blockBounds()
	if !ok {
		return false, nil
	}
	h.cut(begin, end)
	if h.onlyShebang() {
		return true, os.Remove(path)
	}
	return true, h.write(path)
}

// cut drops the lines from begin to end, plus the blank line on each side
// that Install put there.
func (h *hookFile) cut(begin, end int) {
	if h.isBlank(end + 1) {
		end++
	}
	if h.isBlank(begin - 1) {
		begin--
	}
	h.lines = slices.Concat(h.lines[:begin], h.lines[end+1:])
}

// Installed reports whether the hook file at path holds a block.
func Installed(path string) (bool, error) {
	h, err := readHook(path)
	if err != nil || h == nil {
		return false, err
	}
	_, _, ok := h.blockBounds()
	return ok, nil
}
