// Package prompt renders the interactive cdd init interview with huh. It
// holds no rules of its own: defaults come in through initcmd.Answers, every
// answer goes back out unchanged, and initcmd.Build judges them.
package prompt

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/jonasalessi/cdd-lint/internal/config"
	"github.com/jonasalessi/cdd-lint/internal/detect"
	"github.com/jonasalessi/cdd-lint/internal/initcmd"
)

// ErrAborted is returned when the user cancels the interview with ctrl-c.
var ErrAborted = huh.ErrUserAborted

// Run walks through the interview as one multi-page form and returns the
// answers. defaults pre-fills every question; det adds the file counts and,
// for a cut-short scan, the notice to the language question; specs are the
// languages on offer, in the order they are listed. Pages that do not apply
// to the answers given so far — the enforcement mode of a greenfield
// project, the metrics of an unselected language — stay hidden, and
// shift+tab navigates back.
func Run(defaults initcmd.Answers, det detect.Detected, specs []config.LanguageSpec) (initcmd.Answers, error) {
	a := defaults
	if a.ProjectType == "" {
		a.ProjectType = config.ProjectGreenfield
	}
	if a.LegacyMode == "" {
		a.LegacyMode = config.ModeStrictOnNewOnly
	}
	limitRaw := ""
	if a.Limit != 0 {
		limitRaw = strconv.Itoa(a.Limit)
	}
	customize := false

	groups := []*huh.Group{
		languagesGroup(&a, det, specs),
		projectTypeGroup(&a),
		legacyModeGroup(&a).WithHideFunc(func() bool { return hideLegacyMode(a.ProjectType) }),
		limitGroup(&a, &limitRaw),
	}
	selections := make(map[config.Language]*[]config.MetricID, len(specs))
	for _, spec := range specs {
		sel := initcmd.SeedMetrics(a, spec)
		selections[spec.ID] = &sel
		groups = append(groups, metricsGroup(spec, selections[spec.ID]).
			WithHideFunc(func() bool { return hideLanguagePage(spec.ID, a.Languages) }))
	}
	groups = append(groups, weightsConfirmGroup(&customize))
	pkgRaws := make(map[config.Language]*string, len(specs))
	for _, spec := range specs {
		raw := strings.Join(initcmd.SeedPackages(a, spec.ID), ", ")
		pkgRaws[spec.ID] = &raw
		groups = append(groups, packagesGroup(spec, pkgRaws[spec.ID]).
			WithHideFunc(func() bool { return hideLanguagePage(spec.ID, a.Languages) }))
	}
	groups = append(groups, excludesGroup(&a, specs))
	if err := huh.NewForm(groups...).WithKeyMap(keyMap()).Run(); err != nil {
		return a, err
	}

	a.Limit = 0
	if raw := strings.TrimSpace(limitRaw); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return a, err
		}
		a.Limit = limit
	}
	a.MetricsByLanguage = make(map[config.Language][]config.MetricID, len(a.Languages))
	a.PackagesByLanguage = make(map[config.Language][]string, len(a.Languages))
	for _, lang := range a.Languages {
		a.MetricsByLanguage[lang] = *selections[lang]
		if pkgs := parseCSV(*pkgRaws[lang]); len(pkgs) > 0 {
			a.PackagesByLanguage[lang] = pkgs
		}
	}
	a.Packages = nil
	if customize {
		if err := runWeightsForm(&a, specs); err != nil {
			return a, err
		}
	}
	return a, nil
}

// ConfirmOverwrite asks whether the existing file at path may be replaced.
// The default answer is no.
func ConfirmOverwrite(path string) (bool, error) {
	overwrite := false
	err := huh.NewForm(huh.NewGroup(huh.NewConfirm().
		Title(fmt.Sprintf("%s already exists. Overwrite?", path)).
		Value(&overwrite))).WithKeyMap(keyMap()).Run()
	return overwrite, err
}

// keyMap extends huh's defaults so the arrow keys work the same way on every
// page. Select and multi-select already move their cursor with up and down;
// this adds them to the yes/no toggle and to moving between the weight
// inputs, which otherwise answer only to left/right and tab.
func keyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Confirm.Toggle.SetKeys(append(km.Confirm.Toggle.Keys(), "up", "down")...)
	km.Input.Prev.SetKeys(append(km.Input.Prev.Keys(), "up")...)
	km.Input.Next.SetKeys(append(km.Input.Next.Keys(), "down")...)
	return km
}

// sectionTitle frames a page title in rules so it stands out from the
// options below it.
func sectionTitle(title string) string {
	const rule = "================="
	return rule + " " + title + " " + rule
}

func languagesGroup(a *initcmd.Answers, det detect.Detected, specs []config.LanguageSpec) *huh.Group {
	description := "Detected languages are pre-checked"
	if det.Truncated {
		description += "; " + truncatedNotice(det.Elapsed)
	}
	return huh.NewGroup(huh.NewMultiSelect[config.Language]().
		Title(sectionTitle("Languages")).
		Description(description).
		Options(languageOptions(det, a.Languages, specs)...).
		Validate(validateLanguages).
		Value(&a.Languages))
}

func projectTypeGroup(a *initcmd.Answers) *huh.Group {
	greenfieldLabel := config.ProjectGreenfield + ": strict from day one, limit 7-14 (cdd.md 4A)"
	legacyLabel := config.ProjectLegacy + ": measure existing, enforce new, limit 20-40"
	return huh.NewGroup(huh.NewSelect[string]().
		Title(sectionTitle("Project type")).
		Options(
			huh.NewOption(greenfieldLabel, config.ProjectGreenfield),
			huh.NewOption(legacyLabel, config.ProjectLegacy),
		).
		Value(&a.ProjectType))
}

func legacyModeGroup(a *initcmd.Answers) *huh.Group {
	return huh.NewGroup(huh.NewSelect[string]().
		Title(sectionTitle("Enforcement mode")).
		Options(
			huh.NewOption(config.ModeStrictOnNewOnly+": existing files are measured only, new files must comply",
				config.ModeStrictOnNewOnly),
			huh.NewOption(config.ModeBoyScout+": modified files must not raise their baseline ICP (baseline not yet supported)",
				config.ModeBoyScout),
			huh.NewOption(config.ModeMeasureOnly+": report only, never blocks CI", config.ModeMeasureOnly),
		).
		Value(&a.LegacyMode))
}

func limitGroup(a *initcmd.Answers, raw *string) *huh.Group {
	// The char limit keeps huh's width math non-negative on very narrow
	// terminals, where rendering an empty input's placeholder panics.
	return huh.NewGroup(huh.NewInput().
		Title(sectionTitle("ICP limit")).
		CharLimit(4).
		PlaceholderFunc(func() string { return limitPlaceholder(a.ProjectType) }, &a.ProjectType).
		DescriptionFunc(func() string { return limitDescription(a.ProjectType, *raw) },
			&struct {
				ProjectType *string
				Raw         *string
			}{&a.ProjectType, raw}).
		Validate(validateLimit).
		Value(raw))
}

func metricsGroup(spec config.LanguageSpec, sel *[]config.MetricID) *huh.Group {
	name := languageLabel(spec, 0)
	return huh.NewGroup(huh.NewMultiSelect[config.MetricID]().
		Title(sectionTitle("Metrics — " + name)).
		Description(fmt.Sprintf("Only metrics the %s analyzer can count; pick at least %d", name, config.MinMetrics)).
		Options(metricOptionsFor(spec, *sel)...).
		Validate(validateMetrics).
		Value(sel))
}

func weightsConfirmGroup(customize *bool) *huh.Group {
	return huh.NewGroup(huh.NewConfirm().
		Title(sectionTitle("Customize weights?")).
		Description("Defaults: 1.0, except external_coupling, stdlib_coupling and local_variable at 0.5").
		Value(customize))
}

func packagesGroup(spec config.LanguageSpec, raw *string) *huh.Group {
	return huh.NewGroup(huh.NewInput().
		Title(sectionTitle("Internal packages — " + languageLabel(spec, 0))).
		Description(packagesHintFor(spec)).
		Value(raw))
}

func excludesGroup(a *initcmd.Answers, specs []config.LanguageSpec) *huh.Group {
	return huh.NewGroup(huh.NewConfirm().
		Title(sectionTitle("Exclude tests and generated code?")).
		DescriptionFunc(func() string { return excludesDescription(a.Languages, specs) }, &a.Languages).
		Value(&a.DefaultExcludes))
}

// runWeightsForm asks one weight per metric selected by any language and
// stores the overrides.
func runWeightsForm(a *initcmd.Answers, specs []config.LanguageSpec) error {
	union := metricsUnion(*a)
	values := make([]string, len(union))
	fields := make([]huh.Field, len(union))
	for i, id := range union {
		weight := config.DefaultWeight(id)
		if override, ok := a.Weights[id]; ok {
			weight = override
		}
		values[i] = strconv.FormatFloat(weight, 'f', -1, 64)
		fields[i] = huh.NewInput().
			Title(string(id)).
			Description(weightDescription(*a, id, specs)).
			Validate(validateWeight).
			Value(&values[i])
	}
	// The title goes on the group: this is the one page with several fields,
	// so no single field can carry it.
	group := huh.NewGroup(fields...).
		Title(sectionTitle("Metric weights")).
		Description("One weight per metric selected by any language; must be above 0")
	if err := huh.NewForm(group).WithKeyMap(keyMap()).Run(); err != nil {
		return err
	}
	if a.Weights == nil {
		a.Weights = make(map[config.MetricID]float64, len(union))
	}
	for i, id := range union {
		weight, err := parseWeight(values[i])
		if err != nil {
			return err
		}
		a.Weights[id] = weight
	}
	return nil
}

// hideLegacyMode hides the enforcement-mode page for greenfield projects.
func hideLegacyMode(projectType string) bool {
	return projectType != config.ProjectLegacy
}

// hideLanguagePage hides a per-language page of an unselected language.
func hideLanguagePage(lang config.Language, selected []config.Language) bool {
	return !slices.Contains(selected, lang)
}

// languageOptions lists every spec, labels the detected ones with their
// file count and pre-checks the defaults.
func languageOptions(
	det detect.Detected,
	selected []config.Language,
	specs []config.LanguageSpec,
) []huh.Option[config.Language] {
	out := make([]huh.Option[config.Language], 0, len(specs))
	for _, spec := range specs {
		out = append(out, huh.NewOption(languageLabel(spec, det.Counts[spec.ID]), spec.ID).
			Selected(slices.Contains(selected, spec.ID)))
	}
	return out
}

// languageLabel names the language for a page title or an option, with the
// file count when there is one. A spec without a display name shows its id.
func languageLabel(spec config.LanguageSpec, count int) string {
	label := spec.DisplayName
	if label == "" {
		label = string(spec.ID)
	}
	switch count {
	case 0:
		return label
	case 1:
		return label + " (1 file)"
	default:
		return fmt.Sprintf("%s (%d files)", label, count)
	}
}

func truncatedNotice(elapsed time.Duration) string {
	return fmt.Sprintf("scan stopped after %s, tick anything missing", elapsed.Round(time.Millisecond))
}

// metricOptionsFor lists the metrics the language's analyzer can count, each
// described in the language's own wording, and pre-checks the seed.
func metricOptionsFor(spec config.LanguageSpec, selected []config.MetricID) []huh.Option[config.MetricID] {
	applicable := spec.Applicable()
	out := make([]huh.Option[config.MetricID], 0, len(applicable))
	for _, id := range applicable {
		label := fmt.Sprintf("%s: %s", id, spec.Description(id))
		out = append(out, huh.NewOption(label, id).Selected(slices.Contains(selected, id)))
	}
	return out
}

// metricsUnion joins the per-language selections in canonical metric order.
func metricsUnion(a initcmd.Answers) []config.MetricID {
	present := map[config.MetricID]bool{}
	for _, lang := range a.Languages {
		for _, id := range a.MetricsByLanguage[lang] {
			present[id] = true
		}
	}
	var out []config.MetricID
	for _, id := range config.Metrics() {
		if present[id] {
			out = append(out, id)
		}
	}
	return out
}

// weightDescription describes a weight input: the language's own wording
// when exactly one selected language counts the metric, and a note naming
// the languages it applies to when it is not counted by all of them.
func weightDescription(a initcmd.Answers, id config.MetricID, specs []config.LanguageSpec) string {
	var owners []config.Language
	for _, lang := range a.Languages {
		if slices.Contains(a.MetricsByLanguage[lang], id) {
			owners = append(owners, lang)
		}
	}
	description := config.MetricDescription(id)
	if len(owners) == 1 {
		if spec, ok := config.FindSpec(specs, owners[0]); ok {
			description = spec.Description(id)
		}
	}
	if len(owners) < len(a.Languages) {
		ids := make([]string, len(owners))
		for i, owner := range owners {
			ids[i] = string(owner)
		}
		description += " — applies to: " + strings.Join(ids, ", ")
	}
	return description
}

// packagesHintFor shows the prefix format of the language.
func packagesHintFor(spec config.LanguageSpec) string {
	base := "Comma-separated prefixes counted as internal coupling"
	if spec.PackageExample == "" {
		return base
	}
	return base + ", e.g. " + spec.PackageExample
}

// excludesDescription lists the globs a yes answer writes for the selected
// languages, deduplicated in specs order.
func excludesDescription(langs []config.Language, specs []config.LanguageSpec) string {
	var globs []string
	seen := map[string]bool{}
	for _, spec := range specs {
		if !slices.Contains(langs, spec.ID) {
			continue
		}
		for _, glob := range spec.DefaultExcludes {
			if !seen[glob] {
				seen[glob] = true
				globs = append(globs, glob)
			}
		}
	}
	if len(globs) == 0 {
		return ""
	}
	return "Will exclude: " + strings.Join(globs, ", ")
}

// limitPlaceholder shows the default limit of the chosen project type.
func limitPlaceholder(projectType string) string {
	return strconv.Itoa(config.DefaultLimit(projectType))
}

// limitDescription shows the recommended band and, for a value outside it,
// an inline warning that does not block the input.
func limitDescription(projectType, raw string) string {
	lo, hi := config.LimitBand(projectType)
	base := fmt.Sprintf("Recommended %s band: %d-%d (empty keeps the default %d)",
		projectType, lo, hi, config.DefaultLimit(projectType))
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err == nil && (limit < lo || limit > hi) {
		return fmt.Sprintf("%s. Warning: %d is outside the band, accepted anyway", base, limit)
	}
	return base
}

func validateLanguages(selected []config.Language) error {
	if len(selected) == 0 {
		return errors.New("pick at least one language")
	}
	return nil
}

func validateMetrics(selected []config.MetricID) error {
	if len(selected) < config.MinMetrics {
		return fmt.Errorf("pick at least %d metrics", config.MinMetrics)
	}
	return nil
}

// validateLimit accepts a whole number of at least 1 or an empty input,
// which keeps the default of the project type.
func validateLimit(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return errors.New("enter a whole number")
	}
	if limit < 1 {
		return errors.New("the limit must be at least 1")
	}
	return nil
}

func validateWeight(raw string) error {
	_, err := parseWeight(raw)
	return err
}

// parseWeight reads a strictly positive number.
func parseWeight(raw string) (float64, error) {
	weight, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, errors.New("enter a number")
	}
	if weight <= 0 {
		return 0, errors.New("the weight must be above 0")
	}
	return weight, nil
}

// parseCSV splits a comma-separated input, trimming entries and dropping
// empty ones.
func parseCSV(raw string) []string {
	var out []string
	for part := range strings.SplitSeq(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
