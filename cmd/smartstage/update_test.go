package main

import (
	"errors"
	"reflect"
	"testing"
)

type fakeUpdateHandoff struct {
	calls       []string
	launchError error
}

func (p *fakeUpdateHandoff) Launch() error {
	p.calls = append(p.calls, "launch")
	return p.launchError
}
func (p *fakeUpdateHandoff) Abort() error { p.calls = append(p.calls, "abort"); return nil }

func TestUpdateHandoffRetainsHelperOwnershipAfterSuccessfulLaunch(t *testing.T) {
	prepared := &fakeUpdateHandoff{}
	if err := finishUpdateHandoff(prepared, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prepared.calls, []string{"launch"}) {
		t.Fatalf("a launched helper lost ownership: %v", prepared.calls)
	}
}

func TestUpdateHandoffAbortsUnlaunchedWorkOnFailure(t *testing.T) {
	failure := errors.New("test failure")
	for _, mode := range []string{"shutdown", "helper startup"} {
		t.Run(mode, func(t *testing.T) {
			prepared := &fakeUpdateHandoff{}
			var shutdownErr error
			want := []string{"abort"}
			if mode == "shutdown" {
				shutdownErr = failure
			} else {
				prepared.launchError = failure
				want = []string{"launch", "abort"}
			}
			if err := finishUpdateHandoff(prepared, shutdownErr); !errors.Is(err, failure) {
				t.Fatalf("original failure was lost: %v", err)
			}
			if !reflect.DeepEqual(prepared.calls, want) {
				t.Fatalf("invalid ownership cleanup: %v", prepared.calls)
			}
		})
	}
}

func TestOrdinaryQuitHasNoUpdateHandoff(t *testing.T) {
	if err := finishUpdateHandoff(nil, nil); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("application error")
	if err := finishUpdateHandoff(nil, failure); !errors.Is(err, failure) {
		t.Fatalf("application error changed: %v", err)
	}
}
