package domain

import "testing"

func TestStatusTerminal(t *testing.T) {
	for _, s := range []TaskStatus{StatusPassed, StatusQuarantined, StatusCancelled} {
		if !s.IsTerminal() {
			t.Fatalf("%s should be terminal", s)
		}
	}
	if StatusLoading.IsTerminal() {
		t.Fatal("loading should not be terminal")
	}
}

func TestStatusCanTransition(t *testing.T) {
	if !StatusPendingLock.CanTransition(StatusPendingSample) {
		t.Fatal("pending_lock -> pending_sample should be allowed")
	}
	if StatusPendingLock.CanTransition(StatusLoading) {
		t.Fatal("pending_lock -> loading should be rejected")
	}
}

func TestTerminalCannotTransition(t *testing.T) {
	if StatusPassed.CanTransition(StatusCancelled) {
		t.Fatal("terminal state must not transition")
	}
}

func TestUnknownStatusInvalid(t *testing.T) {
	if TaskStatus("bogus").IsValid() {
		t.Fatal("bogus status must be invalid")
	}
	if TaskStatus("bogus").CanTransition(StatusPassed) {
		t.Fatal("unknown status must not transition")
	}
}
