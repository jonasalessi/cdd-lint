package prompt

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/jonasalessi/cdd-lint/internal/config"
	"github.com/jonasalessi/cdd-lint/internal/detect"
	"github.com/jonasalessi/cdd-lint/internal/initcmd"
)

func TestValidateLimit(t *testing.T) {
	assert.NoError(t, validateLimit("10"))
	assert.NoError(t, validateLimit(" 1 "))
	assert.Error(t, validateLimit("0"))
	assert.Error(t, validateLimit("-2"))
	assert.Error(t, validateLimit("ten"))
	assert.Error(t, validateLimit("1.5"))
	assert.NoError(t, validateLimit(""), "empty keeps the project-type default")
	assert.NoError(t, validateLimit("  "))
}

func TestParseWeight(t *testing.T) {
	w, err := parseWeight(" 0.5 ")
	assert.NoError(t, err)
	assert.Equal(t, 0.5, w)

	_, err = parseWeight("0")
	assert.Error(t, err)
	_, err = parseWeight("-1")
	assert.Error(t, err)
	_, err = parseWeight("heavy")
	assert.Error(t, err)
	assert.Error(t, validateWeight("0"))
	assert.NoError(t, validateWeight("2"))
}

func TestParseCSV(t *testing.T) {
	assert.Equal(t, []string{"a", "b.c"}, parseCSV(" a , b.c ,, "))
	assert.Nil(t, parseCSV(""))
	assert.Nil(t, parseCSV(" , "))
}

func TestValidateLanguages(t *testing.T) {
	assert.Error(t, validateLanguages(nil))
	assert.NoError(t, validateLanguages([]config.Language{langAlpha}))
}

func TestValidateMetrics(t *testing.T) {
	assert.Error(t, validateMetrics([]config.MetricID{config.MetricCondition}))
	assert.NoError(t, validateMetrics([]config.MetricID{
		config.MetricCondition, config.MetricCodeBranch, config.MetricLambda,
	}))
}

func TestLimitDescription(t *testing.T) {
	inBand := limitDescription(config.ProjectGreenfield, "10")
	assert.Contains(t, inBand, "7-14")
	assert.NotContains(t, inBand, "Warning")

	outOfBand := limitDescription(config.ProjectGreenfield, "50")
	assert.Contains(t, outOfBand, "Warning: 50 is outside the band")

	junk := limitDescription(config.ProjectLegacy, "abc")
	assert.Contains(t, junk, "20-40")
	assert.NotContains(t, junk, "Warning")
}

func TestLanguageLabel(t *testing.T) {
	assert.Equal(t, "Alpha", languageLabel(alphaSpec(), 0))
	assert.Equal(t, "Beta (1 file)", languageLabel(betaSpec(), 1))
	assert.Equal(t, "Gamma (12 files)", languageLabel(gammaSpec(), 12))
	assert.Equal(t, "delta", languageLabel(deltaSpec(), 0), "a spec without a display name shows its id")
}

func TestTruncatedNotice(t *testing.T) {
	notice := truncatedNotice(4 * time.Second)
	assert.Contains(t, notice, "scan stopped after 4s")
}

func TestLanguageOptionsPreselection(t *testing.T) {
	det := detect.Detected{Counts: map[config.Language]int{langAlpha: 2}}
	options := languageOptions(det, []config.Language{langAlpha}, testSpecs())
	assert.Len(t, options, len(testSpecs()))
	assert.Equal(t, "Alpha (2 files)", options[0].Key)
	assert.Equal(t, langAlpha, options[0].Value)
	assert.Equal(t, langDelta, options[3].Value, "specs order")
}

// TestGroupBuildersConstruct exercises every page builder; the forms
// themselves are not driven in tests.
func TestGroupBuildersConstruct(t *testing.T) {
	a := initcmd.Answers{Languages: []config.Language{langAlpha}}
	det := detect.Detected{Truncated: true, Elapsed: time.Second,
		Counts: map[config.Language]int{langAlpha: 2}}
	raw := ""
	sel := []config.MetricID{config.MetricCodeBranch}
	customize := false
	assert.NotNil(t, languagesGroup(&a, det, testSpecs()))
	assert.NotNil(t, projectTypeGroup(&a))
	assert.NotNil(t, legacyModeGroup(&a))
	assert.NotNil(t, limitGroup(&a, &raw))
	assert.NotNil(t, metricsGroup(alphaSpec(), &sel))
	assert.NotNil(t, weightsConfirmGroup(&customize))
	assert.NotNil(t, packagesGroup(alphaSpec(), &raw))
	assert.NotNil(t, excludesGroup(&a, testSpecs()))
}

func TestMetricOptionsOnlyApplicable(t *testing.T) {
	options := metricOptionsFor(alphaSpec(), config.DefaultSelection())
	assert.Len(t, options, len(alphaSpec().Applicable()))
	assert.Equal(t, "code_branch: if/else, switch/select, for", options[0].Key,
		"labels use the language's own wording")
	assert.Equal(t, "condition: "+config.MetricDescription(config.MetricCondition), options[1].Key,
		"and the generic wording otherwise")
	for _, opt := range options {
		assert.NotEqual(t, config.MetricInheritance, opt.Value)
		assert.NotEqual(t, config.MetricExceptionHandling, opt.Value)
	}

	betaOptions := metricOptionsFor(betaSpec(), nil)
	assert.Len(t, betaOptions, len(config.Metrics()))
}

// TestKeyMapArrowKeys pins the arrow keys the interview adds on top of huh's
// defaults, which already move the cursor of a select and a multi-select.
func TestKeyMapArrowKeys(t *testing.T) {
	km := keyMap()
	assert.Contains(t, km.Confirm.Toggle.Keys(), "up")
	assert.Contains(t, km.Confirm.Toggle.Keys(), "down")
	assert.Contains(t, km.Confirm.Toggle.Keys(), "left", "the defaults are kept")
	assert.Contains(t, km.Input.Prev.Keys(), "up")
	assert.Contains(t, km.Input.Prev.Keys(), "shift+tab", "the defaults are kept")
	assert.Contains(t, km.Input.Next.Keys(), "down")
	assert.Contains(t, km.Select.Up.Keys(), "up")
	assert.Contains(t, km.MultiSelect.Down.Keys(), "down")
}

func TestHidePredicates(t *testing.T) {
	assert.True(t, hideLegacyMode(config.ProjectGreenfield))
	assert.False(t, hideLegacyMode(config.ProjectLegacy))

	selected := []config.Language{langAlpha}
	assert.False(t, hideLanguagePage(langAlpha, selected))
	assert.True(t, hideLanguagePage(langBeta, selected))
}

func TestLimitPlaceholder(t *testing.T) {
	assert.Equal(t, "10", limitPlaceholder(config.ProjectGreenfield))
	assert.Equal(t, "25", limitPlaceholder(config.ProjectLegacy))
}

func TestPackagesHintFor(t *testing.T) {
	assert.Contains(t, packagesHintFor(alphaSpec()), "example.com/acme/api")
	assert.Contains(t, packagesHintFor(betaSpec()), "com.acme.app")
	assert.NotContains(t, packagesHintFor(alphaSpec()), "com.acme.app")

	assert.NotContains(t, packagesHintFor(deltaSpec()), "e.g.",
		"a spec without an example gets none")
}

func TestExcludesDescription(t *testing.T) {
	desc := excludesDescription([]config.Language{langBeta, langGamma}, testSpecs())
	assert.Contains(t, desc, "**/src/test/**")
	assert.Equal(t, 1, strings.Count(desc, "**/build/**"), "shared globs listed once")
	assert.NotContains(t, desc, "vendor/**")

	assert.Empty(t, excludesDescription(nil, testSpecs()))
}

func TestMetricsUnion(t *testing.T) {
	a := initcmd.Answers{
		Languages: []config.Language{langAlpha, langBeta},
		MetricsByLanguage: map[config.Language][]config.MetricID{
			langAlpha: {config.MetricCondition, config.MetricCodeBranch},
			langBeta:  {config.MetricInheritance, config.MetricCondition},
		},
	}
	assert.Equal(t, []config.MetricID{
		config.MetricCodeBranch, config.MetricCondition, config.MetricInheritance,
	}, metricsUnion(a), "canonical order, no duplicates")
}

func TestWeightDescription(t *testing.T) {
	a := initcmd.Answers{
		Languages: []config.Language{langAlpha, langBeta},
		MetricsByLanguage: map[config.Language][]config.MetricID{
			langAlpha: {config.MetricCodeBranch, config.MetricCondition},
			langBeta:  {config.MetricCodeBranch, config.MetricInheritance},
		},
	}
	shared := weightDescription(a, config.MetricCodeBranch, testSpecs())
	assert.NotContains(t, shared, "applies to", "metrics counted by every language carry no note")
	assert.Equal(t, config.MetricDescription(config.MetricCodeBranch), shared,
		"two owners get the generic wording, not alpha's")

	partial := weightDescription(a, config.MetricInheritance, testSpecs())
	assert.Contains(t, partial, "applies to: beta")

	alphaOnly := weightDescription(a, config.MetricCondition, testSpecs())
	assert.Equal(t, alphaSpec().Description(config.MetricCondition)+" — applies to: alpha", alphaOnly,
		"a single owner gets its language's wording")

	a.MetricsByLanguage[langAlpha] = []config.MetricID{config.MetricCodeBranch}
	a.MetricsByLanguage[langBeta] = []config.MetricID{config.MetricCodeBranch, config.MetricCondition}
	assert.Equal(t, config.MetricDescription(config.MetricCondition)+" — applies to: beta",
		weightDescription(a, config.MetricCondition, testSpecs()),
		"a single owner without its own wording gets the generic one")
}
