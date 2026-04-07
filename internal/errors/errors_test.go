package errors

import (
	stderrors "errors"
	"testing"
	"time"
)

func TestNewAndWrap(t *testing.T) {
	e := New(ErrCodeInternal, "boom", WithOp("unit"), WithSeverity(SeverityCritical))
	if e.Code != ErrCodeInternal {
		t.Fatalf("code mismatch")
	}
	if e.Op != "unit" {
		t.Fatalf("op mismatch")
	}
	if e.Severity != SeverityCritical {
		t.Fatalf("severity mismatch")
	}
	if e.Error() == "" {
		t.Fatalf("empty error string")
	}
	if len(e.Stack) == 0 {
		t.Fatalf("stack not captured")
	}
	_ = e.StackString()

	base := stderrors.New("root")
	w := Wrap(base, ErrCodeNetwork, "net down", WithOp("nbi"), WithSeverity(SeverityMajor))
	if w == nil {
		t.Fatalf("expected wrapped error")
	}
	if CodeOf(w) != ErrCodeNetwork {
		t.Fatalf("code mismatch: %s", CodeOf(w))
	}
	if SeverityOf(w) != SeverityMajor {
		t.Fatalf("severity mismatch: %s", SeverityOf(w))
	}
	if _, ok := As(w); !ok {
		t.Fatalf("expected As(*Error)")
	}
	if !stderrors.Is(w, base) {
		t.Fatalf("expected unwrap to base")
	}
}

func TestAlarmMapping(t *testing.T) {
	a := Alarm(nil)
	if a.Code != "OK" {
		t.Fatalf("unexpected code: %s", a.Code)
	}

	e := New(ErrCodeIO, "disk", WithSeverity(SeverityWarning))
	a = Alarm(e)
	if a.Code != "EMS_IO" {
		t.Fatalf("unexpected code: %s", a.Code)
	}
	if a.Severity != string(SeverityWarning) {
		t.Fatalf("unexpected severity: %s", a.Severity)
	}
}

func TestAtLeast(t *testing.T) {
	if AtLeast(SeverityMajor, SeverityCritical) {
		t.Fatalf("expected false")
	}
	if !AtLeast(SeverityCritical, SeverityMajor) {
		t.Fatalf("expected true")
	}
}

func TestAlarmMapping_Unknown(t *testing.T) {
	a := Alarm(stderrors.New("x"))
	if a.Code != "EMS_UNKNOWN" {
		t.Fatalf("unexpected: %s", a.Code)
	}
}

func TestAlarmMapping_EmptyMsg(t *testing.T) {
	e := New(ErrCodeInternal, "", WithSeverity(""))
	a := Alarm(e)
	if a.Message == "" {
		t.Fatalf("expected message")
	}
}

func TestCodeOfAndSeverityOf_Defaults(t *testing.T) {
	if CodeOf(nil) != "" {
		t.Fatalf("expected empty code")
	}
	if SeverityOf(nil) != SeverityUnknown {
		t.Fatalf("expected unknown severity")
	}
	if CodeOf(stderrors.New("x")) != "" {
		t.Fatalf("expected empty code")
	}
	if SeverityOf(stderrors.New("x")) != SeverityUnknown {
		t.Fatalf("expected unknown severity")
	}

	e := New(ErrCodeInternal, "x", WithSeverity(""))
	if SeverityOf(e) != SeverityUnknown {
		t.Fatalf("expected unknown severity")
	}
}

func TestWithTimeAndEmptyStackString(t *testing.T) {
	ts := time.Unix(1, 0)
	e := New(ErrCodeInternal, "x", WithTime(ts))
	if !e.Time.Equal(ts) {
		t.Fatalf("time mismatch")
	}
	if (&Error{}).StackString() != "" {
		t.Fatalf("expected empty stack string")
	}
}
