package config

// languageRules checks that every configured language is known and that
// metrics and icp-limits name the same ones: V3 and V11.
type languageRules struct{ v *validator }

// check runs the group's rules on cfg.
func (r languageRules) check(cfg *Config) {
	r.languages(cfg)
	r.languageSets(cfg)
}

func (r *languageRules) languages(cfg *Config) {
	if len(cfg.Metrics) == 0 {
		r.v.errorf(RuleLanguages, "metrics: at least one language is required")
	}
	for _, lang := range sortedLanguages(cfg.Metrics, r.v.specs) {
		if !r.v.isLanguage(lang) {
			r.v.errorf(RuleLanguages, "metrics: %q is not one of %s", lang, join(LanguageIDs(r.v.specs)))
		}
	}
	for _, lang := range sortedLanguages(cfg.ICPLimits, r.v.specs) {
		if !r.v.isLanguage(lang) {
			r.v.errorf(RuleLanguages, "icp-limits: %q is not one of %s", lang, join(LanguageIDs(r.v.specs)))
		}
	}
}

func (r *languageRules) languageSets(cfg *Config) {
	for _, lang := range sortedLanguages(cfg.Metrics, r.v.specs) {
		if _, ok := cfg.ICPLimits[lang]; !ok {
			r.v.errorf(RuleLanguageSets, "icp-limits: missing entry for %s, which metrics configures", lang)
		}
	}
	for _, lang := range sortedLanguages(cfg.ICPLimits, r.v.specs) {
		if _, ok := cfg.Metrics[lang]; !ok {
			r.v.errorf(RuleLanguageSets, "metrics: missing entry for %s, which icp-limits configures", lang)
		}
	}
}
