package netconfcm

import "fmt"

const (
	ErrorTagInUse           = "in-use"
	ErrorTagInvalidValue    = "invalid-value"
	ErrorTagLockDenied      = "lock-denied"
	ErrorTagOperationFailed = "operation-failed"
)

type RPCError struct {
	Tag            string `json:"error_tag"`
	Type           string `json:"error_type,omitempty"`
	AppTag         string `json:"error_app_tag,omitempty"`
	Message        string `json:"message"`
	OwnerSessionID uint64 `json:"owner_session_id,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	if e.OwnerSessionID != 0 {
		return fmt.Sprintf("%s (session %d)", e.Message, e.OwnerSessionID)
	}
	return e.Message
}

func NewRPCError(tag, message string) *RPCError {
	if tag == "" {
		tag = ErrorTagOperationFailed
	}
	return &RPCError{Tag: tag, Type: "application", Message: message}
}

func NewLockDenied(message string, ownerSessionID uint64) *RPCError {
	return &RPCError{
		Tag:            ErrorTagLockDenied,
		Type:           "protocol",
		Message:        message,
		OwnerSessionID: ownerSessionID,
	}
}

func NewInUse(message string, ownerSessionID uint64) *RPCError {
	return &RPCError{
		Tag:            ErrorTagInUse,
		Type:           "protocol",
		Message:        message,
		OwnerSessionID: ownerSessionID,
	}
}
