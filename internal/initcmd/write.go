package initcmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/jonasalessi/cdd-lint/internal/atomicfile"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// ErrExists reports that the target file is already there and force was not
// given. The caller decides whether to ask the user or to fail.
var ErrExists = errors.New("file already exists")

// configMode makes the configuration world-readable, like any project file.
const configMode = 0o644

// Write renders cfg for the languages in specs and writes it to path
// atomically, so a crash never leaves a half-written configuration.
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
	return atomicfile.Write(path, data, configMode)
}
