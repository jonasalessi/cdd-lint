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

// emitInto runs Emit with root as the run root and outputFile relative to it.
func emitInto(t *testing.T, root, outputFile string, format string) (string, error) {
	t.Helper()
	res := fullRun()
	res.Root = root
	return Emit(&bytes.Buffer{}, config.Reporter{Format: format, OutputFile: &outputFile}, res, Options{})
}

func TestEmitToFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "cdd-report.json")
	outputFile := "cdd-report.json"
	var out bytes.Buffer
	res := fullRun()
	res.Root = root
	path, err := Emit(&out, config.Reporter{Format: config.FormatJSON, OutputFile: &outputFile}, res, Options{})
	require.NoError(t, err)
	assert.Equal(t, target, path)
	assert.Empty(t, out.String(), "the file takes the report, not stdout")

	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, render(t, config.FormatJSON, res), string(data))

	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(outputFileMode), info.Mode().Perm())
}

func TestEmitResolvesTheOutputFileAgainstTheRunRoot(t *testing.T) {
	root := t.TempDir()
	reports := filepath.Join(root, "reports")
	require.NoError(t, os.Mkdir(reports, 0o755))

	path, err := emitInto(t, root, filepath.Join("reports", "cdd.json"), config.FormatJSON)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(reports, "cdd.json"), path)
	assert.FileExists(t, path)
}

func TestEmitRefusesAnAbsoluteOutputFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "cdd.json")

	_, err := emitInto(t, t.TempDir(), target, config.FormatJSON)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "outputFile")
	assert.NoFileExists(t, target)
}

func TestEmitRefusesAnOutputFileOutsideTheRoot(t *testing.T) {
	outside := t.TempDir()
	root := filepath.Join(outside, "project")
	require.NoError(t, os.Mkdir(root, 0o755))

	_, err := emitInto(t, root, filepath.Join("..", "canary.json"), config.FormatJSON)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "leaves the project directory")
	assert.NoFileExists(t, filepath.Join(outside, "canary.json"))
}

func TestEmitRefusesASymlinkedOutputFile(t *testing.T) {
	root := t.TempDir()
	canary := filepath.Join(t.TempDir(), "canary.json")
	require.NoError(t, os.WriteFile(canary, []byte("untouched"), 0o644))
	require.NoError(t, os.Symlink(canary, filepath.Join(root, "cdd.json")))

	_, err := emitInto(t, root, "cdd.json", config.FormatJSON)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlink")
	data, readErr := os.ReadFile(canary)
	require.NoError(t, readErr)
	assert.Equal(t, "untouched", string(data))
}

func TestEmitRefusesASymlinkedOutputDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "reports")))

	_, err := emitInto(t, root, filepath.Join("reports", "cdd.json"), config.FormatJSON)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlink")
	assert.NoFileExists(t, filepath.Join(outside, "cdd.json"))
}

func TestEmitTruncatesAnExistingFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "cdd-report.txt")
	require.NoError(t, os.WriteFile(target, bytes.Repeat([]byte("stale\n"), 500), 0o644))

	path, err := emitInto(t, root, "cdd-report.txt", config.FormatConsole)

	require.NoError(t, err)
	assert.Equal(t, target, path)
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	res := fullRun()
	res.Root = root
	assert.Equal(t, render(t, config.FormatConsole, res), string(data))
}

func TestEmitMissingParentDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "missing", "cdd-report.md")

	_, err := emitInto(t, root, filepath.Join("missing", "cdd-report.md"), config.FormatMarkdown)

	require.Error(t, err)
	assert.Contains(t, err.Error(), filepath.Dir(target))
	assert.NoFileExists(t, target)
}

func TestEmitParentIsNotADirectory(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "not-a-dir"), []byte("x"), 0o644))

	_, err := emitInto(t, root, filepath.Join("not-a-dir", "cdd-report.xml"), config.FormatXML)

	require.Error(t, err)
}

func TestEmitUnknownFormatWritesNothing(t *testing.T) {
	root := t.TempDir()

	_, err := emitInto(t, root, "cdd-report.out", "yaml")

	require.Error(t, err)
	assert.NoFileExists(t, filepath.Join(root, "cdd-report.out"), "an unknown format never creates the file")
}

func TestEmitUnwritableFile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Chmod(root, 0o500))
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })

	_, err := emitInto(t, root, "cdd-report.txt", config.FormatConsole)

	require.Error(t, err)
}
