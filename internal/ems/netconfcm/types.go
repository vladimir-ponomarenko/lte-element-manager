package netconfcm

import "lte-element-manager/internal/ems/configuration"

type IDs struct {
	SubNetwork     string
	ManagedElement string
	ENBFunctionID  string
}

type ArtifactPaths struct {
	Running   string
	Candidate string
}

type SessionMeta struct {
	SessionID uint64 `json:"session_id"`
	Username  string `json:"username"`
}

type EditRequest struct {
	SessionMeta
	Target           string `json:"target"`
	DefaultOperation string `json:"default_operation,omitempty"`
	TestOption       string `json:"test_option,omitempty"`
	ErrorOption      string `json:"error_option,omitempty"`
	Payload          string `json:"payload"`
}

type ValidateRequest struct {
	SessionMeta
	Source  string `json:"source,omitempty"`
	Payload string `json:"payload,omitempty"`
}

type LockRequest struct {
	SessionMeta
	Target string `json:"target"`
}

type CommitRequest struct {
	SessionMeta
}

type KeepAliveRequest struct {
	Sessions []SessionMeta `json:"sessions"`
}

type DataState struct {
	Running   *configuration.EditableConfig `json:"running,omitempty"`
	Candidate *configuration.EditableConfig `json:"candidate,omitempty"`
}
