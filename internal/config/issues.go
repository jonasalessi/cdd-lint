package config

import (
	"fmt"
	"strings"
)

// Issue severities.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Issue is one finding of Validate.
type Issue struct {
	Rule     string
	Severity string
	Message  string
}

func (i Issue) String() string {
	return fmt.Sprintf("%s [%s]: %s", i.Rule, i.Severity, i.Message)
}

// Issues is the aggregated result of Validate.
type Issues []Issue

// HasErrors reports whether at least one issue has error severity.
func (issues Issues) HasErrors() bool {
	for _, i := range issues {
		if i.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Errors returns the issues with error severity.
func (issues Issues) Errors() Issues {
	return issues.bySeverity(SeverityError)
}

// Warnings returns the issues with warning severity.
func (issues Issues) Warnings() Issues {
	return issues.bySeverity(SeverityWarning)
}

func (issues Issues) bySeverity(s string) Issues {
	var out Issues
	for _, i := range issues {
		if i.Severity == s {
			out = append(out, i)
		}
	}
	return out
}

func (issues Issues) String() string {
	lines := make([]string, len(issues))
	for i, issue := range issues {
		lines[i] = issue.String()
	}
	return strings.Join(lines, "\n")
}
