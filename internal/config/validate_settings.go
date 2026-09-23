package config

import (
	"regexp"
	"strings"
)

// settingRules checks the scalar settings and the file filters: V1, V2,
// V7, V9, V10 and V12.
type settingRules struct{ v *validator }

// check runs the group's rules on cfg.
func (s settingRules) check(cfg *Config) {
	s.version(cfg)
	s.projectType(cfg)
	s.filters(cfg)
	s.enforcement(cfg)
	s.reporter(cfg)
	s.timeout(cfg)
}

func (s *settingRules) version(cfg *Config) {
	if cfg.Version != SchemaVersion {
		s.v.errorf(RuleVersion, "version: got %d, want %d", cfg.Version, SchemaVersion)
	}
}

func (s *settingRules) projectType(cfg *Config) {
	if !IsProjectType(cfg.ProjectType) {
		s.v.errorf(RuleProjectType, "project_type: %q is not one of %s", cfg.ProjectType, join(ProjectTypes()))
	}
}

func (s *settingRules) reporter(cfg *Config) {
	if !IsReporterFormat(cfg.Reporter.Format) {
		s.v.errorf(RuleReporter, "reporter.format: %q is not one of %s", cfg.Reporter.Format, join(ReporterFormats()))
	}
}

func (s *settingRules) timeout(cfg *Config) {
	if cfg.Timeout < 0 {
		s.v.errorf(RuleTimeout, "timeout: %s must be >= 0", cfg.Timeout)
	}
}

func (s *settingRules) enforcement(cfg *Config) {
	mode := cfg.Enforcement.LegacyMode
	if !IsLegacyMode(mode) {
		s.v.errorf(RuleLegacyMode, "enforcement.legacy_mode: %q is not one of %s", mode, join(LegacyModes()))
		return
	}
	if cfg.ProjectType == ProjectGreenfield && mode != ModeStrictAll {
		s.v.errorf(
			RuleLegacyMode,
			"enforcement.legacy_mode: %s project requires %s, got %s",
			ProjectGreenfield,
			ModeStrictAll,
			mode,
		)
	}
	if mode == ModeMeasureOnly && cfg.Enforcement.BlockOnCI {
		s.v.errorf(RuleLegacyMode, "enforcement.block_on_ci: must be false when legacy_mode is %s", ModeMeasureOnly)
	}
	if mode == ModeBoyScout {
		s.v.warnf(
			RuleLegacyMode,
			"enforcement.legacy_mode: %s needs the baseline store, which is not supported yet",
			ModeBoyScout,
		)
	}
}

func (s *settingRules) filters(cfg *Config) {
	s.filterList("include", cfg.Include)
	s.filterList("exclude", cfg.Exclude)
}

func (s *settingRules) filterList(section string, list []string) {
	for i, entry := range list {
		if strings.TrimSpace(entry) == "" {
			s.v.errorf(RulePatterns, "%s[%d]: empty pattern", section, i)
			continue
		}
		if re, ok := strings.CutPrefix(entry, RegexPrefix); ok {
			if _, err := regexp.Compile(re); err != nil {
				s.v.errorf(RulePatterns, "%s[%d]: invalid regex %q: %v", section, i, re, err)
			}
		}
	}
}
