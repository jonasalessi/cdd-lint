package report

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

func TestEmitToStdout(t *testing.T) {
	var out bytes.Buffer
	path, err := Emit(&out, config.Reporter{Format: config.FormatConsole}, fullRun(), Options{})
	require.NoError(t, err)
	assert.Empty(t, path, "stdout has no receipt")
	assert.Equal(t, render(t, config.FormatConsole, fullRun()), out.String())
}

func TestEmitToFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "cdd-report.json")
	var out bytes.Buffer
	path, err := Emit(&out, config.Reporter{Format: config.FormatJSON, OutputFile: &target}, fullRun(), Options{})
	require.NoError(t, err)
	assert.Equal(t, target, path)
	assert.Empty(t, out.String(), "the file takes the report, not stdout")

	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, render(t, config.FormatJSON, fullRun()), string(data))

	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(outputFileMode), info.Mode().Perm())
}

func TestEmitResolvesRelativeOutputFileAgainstTheRunRoot(t *testing.T) {
	root := t.TempDir()
	reports := filepath.Join(root, "reports")
	require.NoError(t, os.Mkdir(reports, 0o755))
	absolute := filepath.Join(t.TempDir(), "cdd.json")

	tests := map[string]struct {
		outputFile string
		want       string
	}{
		"relative": {outputFile: filepath.Join("reports", "cdd.json"), want: filepath.Join(reports, "cdd.json")},
		"absolute": {outputFile: absolute, want: absolute},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			outputFile := tt.outputFile
			res := fullRun()
			res.Root = root

			path, err := Emit(
				&bytes.Buffer{},
				config.Reporter{Format: config.FormatJSON, OutputFile: &outputFile},
				res,
				Options{},
			)

			require.NoError(t, err)
			assert.Equal(t, tt.want, path)
			assert.FileExists(t, tt.want)
		})
	}
}

func TestEmitTruncatesAnExistingFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "cdd-report.txt")
	require.NoError(t, os.WriteFile(target, bytes.Repeat([]byte("stale\n"), 500), 0o644))

	reporter := config.Reporter{Format: config.FormatConsole, OutputFile: &target}
	path, err := Emit(&bytes.Buffer{}, reporter, fullRun(), Options{})
	require.NoError(t, err)
	assert.Equal(t, target, path)

	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, render(t, config.FormatConsole, fullRun()), string(data))
}

func TestEmitMissingParentDirectory(t *testing.T) {
	target := filepath.Join(t.TempDir(), "missing", "cdd-report.md")
	reporter := config.Reporter{Format: config.FormatMarkdown, OutputFile: &target}
	_, err := Emit(&bytes.Buffer{}, reporter, fullRun(), Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), filepath.Dir(target))
	assert.NoFileExists(t, target)
}

func TestEmitParentIsNotADirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	target := filepath.Join(file, "cdd-report.xml")

	reporter := config.Reporter{Format: config.FormatXML, OutputFile: &target}
	_, err := Emit(&bytes.Buffer{}, reporter, fullRun(), Options{})
	require.Error(t, err)
}

func TestEmitUnknownFormatWritesNothing(t *testing.T) {
	target := filepath.Join(t.TempDir(), "cdd-report.out")
	_, err := Emit(&bytes.Buffer{}, config.Reporter{Format: "yaml", OutputFile: &target}, fullRun(), Options{})
	require.Error(t, err)
	assert.NoFileExists(t, target, "an unknown format never creates the file")
}

func TestEmitUnwritableFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	target := filepath.Join(dir, "cdd-report.txt")
	reporter := config.Reporter{Format: config.FormatConsole, OutputFile: &target}
	_, err := Emit(&bytes.Buffer{}, reporter, fullRun(), Options{})
	require.Error(t, err)
}
