package errors

// Code is a stable error classification used for fault mapping and metrics.
type Code string

const (
	// ErrCodeInternal indicates a programming or unexpected runtime failure.
	ErrCodeInternal Code = "INTERNAL"
	// ErrCodeNetwork indicates transport failures (SSH/NETCONF connectivity, timeouts).
	ErrCodeNetwork Code = "NETWORK"
	// ErrCodeDataCorrupt indicates invalid/unsupported payloads or schema violations.
	ErrCodeDataCorrupt Code = "DATA_CORRUPT"
	// ErrCodeConfig indicates invalid configuration or missing required settings.
	ErrCodeConfig Code = "CONFIG"
	// ErrCodeIO indicates filesystem/socket IO errors.
	ErrCodeIO Code = "IO"
	// ErrCodeProcess indicates failures to start/stop external processes.
	ErrCodeProcess Code = "PROCESS"
)
