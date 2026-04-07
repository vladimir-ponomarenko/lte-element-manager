package errors

type Severity string

const (
	SeverityUnknown  Severity = "unknown"
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityMinor    Severity = "minor"
	SeverityMajor    Severity = "major"
	SeverityCritical Severity = "critical"
)

func severityRank(s Severity) int {
	switch s {
	case SeverityInfo:
		return 1
	case SeverityWarning:
		return 2
	case SeverityMinor:
		return 3
	case SeverityMajor:
		return 4
	case SeverityCritical:
		return 5
	default:
		return 0
	}
}

// AtLeast reports whether s is at least min in severity.
func AtLeast(s, min Severity) bool {
	return severityRank(s) >= severityRank(min)
}
