package errors

import "testing"

func TestSeverityRank(t *testing.T) {
	_ = severityRank(SeverityUnknown)
	_ = severityRank(SeverityInfo)
	_ = severityRank(SeverityWarning)
	_ = severityRank(SeverityMinor)
	_ = severityRank(SeverityMajor)
	_ = severityRank(SeverityCritical)
	_ = severityRank(Severity("x"))
}
