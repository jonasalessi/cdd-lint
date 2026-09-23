package initcmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// ErrExists reports that the target file is already there and force was not
// given. The caller decides whether to ask the user or to fail.
var ErrExists = errors.New("file already exists")

// Write renders cfg for the languages in specs and writes it to path
// atomically: the document goes to path + ".tmp" in the same directory and
// is renamed over the target, so a crash never leaves a half-written
// configuration.
func Write(cfg *config.Config, specs []config.LanguageSpec, path string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s: %w", path, ErrExists)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	data, err := config.Render(cfg, specs)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	//nolint:gosec // G306: the configuration file is meant to be world-readable
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
