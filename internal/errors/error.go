package errors

import (
	stderrors "errors"
	"fmt"
	"runtime"
	"strings"
	"time"
)

type Error struct {
	Code     Code
	Severity Severity
	Op       string
	Msg      string
	Err      error
	Time     time.Time
	Stack    []uintptr
}

func (e *Error) Error() string {
	var b strings.Builder
	if e.Op != "" {
		b.WriteString(e.Op)
		b.WriteString(": ")
	}
	if e.Code != "" {
		b.WriteString(string(e.Code))
		b.WriteString(": ")
	}
	if e.Msg != "" {
		b.WriteString(e.Msg)
	}
	if e.Err != nil {
		if e.Msg != "" {
			b.WriteString(": ")
		}
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

func (e *Error) Unwrap() error { return e.Err }

// StackString returns a best-effort.
func (e *Error) StackString() string {
	if len(e.Stack) == 0 {
		return ""
	}
	frames := runtime.CallersFrames(e.Stack)
	var out strings.Builder
	for {
		f, more := frames.Next()
		if !strings.Contains(f.File, "/runtime/") {
			out.WriteString(fmt.Sprintf("%s:%d %s\n", f.File, f.Line, f.Function))
		}
		if !more {
			break
		}
	}
	return out.String()
}

// Option configures Error creation.
type Option func(*Error)

func WithOp(op string) Option {
	return func(e *Error) { e.Op = op }
}

func WithSeverity(s Severity) Option {
	return func(e *Error) { e.Severity = s }
}

func WithTime(t time.Time) Option {
	return func(e *Error) { e.Time = t }
}

// New creates a typed error and captures the stack at the call site.
func New(code Code, msg string, opts ...Option) *Error {
	e := &Error{
		Code:     code,
		Severity: SeverityUnknown,
		Msg:      msg,
		Time:     time.Now(),
		Stack:    callers(3),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Wrap wraps err as a typed Error. If err is already an *Error with the same code/op,
// Wrap still captures a new stack at this boundary to preserve call context.
func Wrap(err error, code Code, msg string, opts ...Option) error {
	if err == nil {
		return nil
	}
	e := &Error{
		Code:     code,
		Severity: SeverityUnknown,
		Msg:      msg,
		Err:      err,
		Time:     time.Now(),
		Stack:    callers(3),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func callers(skip int) []uintptr {
	pcs := make([]uintptr, 32)
	n := runtime.Callers(skip, pcs)
	return pcs[:n]
}

// As returns the first *Error in the chain.
func As(err error) (*Error, bool) {
	var e *Error
	ok := stderrors.As(err, &e)
	return e, ok
}

// CodeOf returns the first error code found in the chain.
func CodeOf(err error) Code {
	if e, ok := As(err); ok {
		return e.Code
	}
	return ""
}

// SeverityOf returns the first severity found in the chain.
func SeverityOf(err error) Severity {
	if e, ok := As(err); ok && e.Severity != "" {
		return e.Severity
	}
	return SeverityUnknown
}
