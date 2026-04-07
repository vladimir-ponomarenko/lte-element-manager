package errors

import (
	"fmt"

	"lte-element-manager/internal/ems/domain"
)

// Alarm converts a domain error into an Alarm for Fault Management workflows.
func Alarm(err error) domain.Alarm {
	if err == nil {
		return domain.Alarm{Code: "OK", Message: "no error", Severity: string(SeverityInfo)}
	}

	if e, ok := As(err); ok {
		code := fmt.Sprintf("EMS_%s", e.Code)
		msg := e.Msg
		if msg == "" {
			msg = "error"
		}
		if e.Err != nil {
			msg = fmt.Sprintf("%s: %s", msg, e.Err.Error())
		}
		sev := e.Severity
		if sev == "" {
			sev = SeverityUnknown
		}
		return domain.Alarm{Code: code, Message: msg, Severity: string(sev)}
	}

	return domain.Alarm{Code: "EMS_UNKNOWN", Message: err.Error(), Severity: string(SeverityUnknown)}
}
