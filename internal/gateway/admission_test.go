package gateway

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"
)

func admissionRequest(value string) http.Header {
	return http.Header{"Cookie": {commandCookie + "=" + value}}
}

func admissionResponse(value string, age int, expires time.Time) http.Header {
	c := &http.Cookie{Name: commandCookie, Value: value, Path: "/smartstage/e/endpoint/", MaxAge: age, Expires: expires}
	return http.Header{"Set-Cookie": {c.String()}}
}

func TestAdmissionOnlyLearnsSuccessfulPairing(t *testing.T) {
	var a sessionAdmission
	now := time.Now()
	h := admissionRequest("real-session")
	response := admissionResponse("real-session", 3600, time.Time{})
	for _, trial := range []struct {
		path   string
		status int
	}{{"/api/pair", 401}, {"/api/pair", 429}, {"/api/state", 200}, {"/command", 200}} {
		if !a.observe(trial.path, trial.status, h, response, now) || a.known(h, now) {
			t.Fatalf("learned session from %s status %d", trial.path, trial.status)
		}
	}
	if !a.observe("/api/pair", 200, nil, response, now) || !a.known(h, now) {
		t.Fatal("successful pairing did not admit session")
	}
	for _, bogus := range []http.Header{nil, admissionRequest("forged"), admissionRequest(""), admissionRequest(strings.Repeat("x", 257)), {"Cookie": {commandCookie + "=real-session; " + commandCookie + "=forged"}}} {
		if a.known(bogus, now) {
			t.Fatal("unknown or ambiguous session admitted")
		}
	}
	var other sessionAdmission
	if other.known(h, now) {
		t.Fatal("session crossed endpoint boundary")
	}
	// An attacker cannot invalidate a real session through a bad CSRF token.
	a.observe("/api/stop", 403, h, nil, now)
	a.observe("/api/logout", 403, h, nil, now)
	a.observe("/api/pair", 401, h, nil, now)
	a.observe("/command", 401, h, nil, now)
	if !a.known(h, now) {
		t.Fatal("denied command or failed re-pair invalidated a paired session")
	}
	a.observe("/api/logout", 200, h, nil, now)
	if a.known(h, now) {
		t.Fatal("logged-out session remained admitted")
	}
	a.observe("/api/pair", 200, nil, response, now)
	a.observe("/api/state", 401, h, nil, now)
	if a.known(h, now) {
		t.Fatal("host-invalidated session remained admitted")
	}
}

func TestAdmissionExpiryIsBoundedAndNotExtendedByUse(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	for _, trial := range []struct {
		name    string
		age     int
		expires time.Time
		limit   time.Duration
	}{
		{"session cookie", 0, time.Time{}, maxAdmissionAge},
		{"long cookie", 1 << 30, time.Time{}, maxAdmissionAge},
		{"max age", 10, time.Time{}, 10 * time.Second},
		{"expires", 20, now.Add(5 * time.Second), 5 * time.Second},
	} {
		t.Run(trial.name, func(t *testing.T) {
			var a sessionAdmission
			h := admissionRequest("session")
			if !a.observe("/api/pair", 200, nil, admissionResponse("session", trial.age, trial.expires), now) {
				t.Fatal("pairing rejected")
			}
			if !a.known(h, now.Add(trial.limit-time.Nanosecond)) || a.known(h, now.Add(trial.limit)) || len(a.sessions) != 0 {
				t.Fatal("admission expiry was extended or expired record retained")
			}
		})
	}
	for _, response := range []http.Header{admissionResponse("session", -1, time.Time{}), admissionResponse("session", 10, now.Add(-time.Second)), admissionResponse("", 10, time.Time{}), admissionResponse(strings.Repeat("x", 257), 10, time.Time{})} {
		var a sessionAdmission
		if a.observe("/api/pair", 200, nil, response, now) || len(a.sessions) != 0 {
			t.Fatal("invalid pairing cookie admitted")
		}
	}
}

func TestAdmissionCapacityNeverEvictsLiveSessions(t *testing.T) {
	var a sessionAdmission
	now := time.Now()
	for n := range maxAdmittedSessions {
		if !a.observe("/api/pair", 200, nil, admissionResponse(fmt.Sprint(n), 60, time.Time{}), now) {
			t.Fatalf("pair %d rejected before capacity", n)
		}
	}
	for n := range 1000 {
		bogus := fmt.Sprintf("bogus-%d", n)
		a.known(admissionRequest(bogus), now)
		a.observe("/api/pair", 401, nil, admissionResponse(bogus, 60, time.Time{}), now)
	}
	if a.observe("/api/pair", 200, nil, admissionResponse("overflow", 60, time.Time{}), now) {
		t.Fatal("new pairing exceeded admission capacity")
	}
	if len(a.sessions) != maxAdmittedSessions {
		t.Fatal("admission storage grew beyond its bound")
	}
	for n := range maxAdmittedSessions {
		if !a.known(admissionRequest(fmt.Sprint(n)), now) {
			t.Fatalf("existing session %d was evicted", n)
		}
	}
	// A successful re-pair replaces only the same browser's previous session.
	if !a.observe("/api/pair", 200, admissionRequest("0"), admissionResponse("replacement", 60, time.Time{}), now) || !a.known(admissionRequest("replacement"), now) || a.known(admissionRequest("0"), now) {
		t.Fatal("re-pair failed to replace the previous admission")
	}
	if !a.observe("/api/pair", 200, nil, admissionResponse("after-expiry", 60, time.Time{}), now.Add(time.Minute)) || len(a.sessions) != 1 {
		t.Fatal("expired entries did not free capacity")
	}
}

func TestAdmissionConcurrentPairAndLookup(t *testing.T) {
	var a sessionAdmission
	now := time.Now()
	var wg sync.WaitGroup
	for n := range 32 {
		wg.Go(func() {
			value := fmt.Sprint(n)
			h := admissionRequest(value)
			for range 20 {
				a.observe("/api/pair", 200, nil, admissionResponse(value, 60, time.Time{}), now)
				a.known(h, now)
				a.observe("/api/logout", 200, h, nil, now)
			}
		})
	}
	wg.Wait()
}

func TestBodyReadLimitsDoNotConsumeRelayCapacity(t *testing.T) {
	e := &endpoint{capacity: newCapacity(), reading: newCapacity()}
	// Saturate ordinary body reading independently of actual relay dispatch.
	for range cap(e.reading.ordinary) {
		release, ok := e.reading.acquire("/api/pair")
		if !ok {
			t.Fatal("could not fill body-reading capacity")
		}
		defer release()
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/pair", strings.NewReader(`{}`))
	if _, ok := e.readBody(w, r, "/api/pair"); ok || w.Code != http.StatusTooManyRequests {
		t.Fatal("ordinary body-reading bound not enforced")
	}
	for _, path := range []string{"/api/stop", "/api/emergency-stop"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		body, ok := e.readBody(w, r, path)
		if !ok || string(body) != `{}` {
			t.Fatalf("%s body blocked by ordinary read pressure", path)
		}
	}
	if len(e.capacity.ordinary) != 0 || len(e.capacity.stops) != 0 {
		t.Fatal("body reading consumed relay capacity")
	}
}

type countedRequestBody struct {
	io.Reader
	bytes int
}

func (r *countedRequestBody) Read(b []byte) (int, error) {
	n, err := r.Reader.Read(b)
	r.bytes += n
	return n, err
}

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (w *deadlineRecorder) SetReadDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

func TestBodyReadByteDeadlineAndReleaseBounds(t *testing.T) {
	for _, trial := range []struct {
		name          string
		source        io.Reader
		contentLength int64
		status, bytes int
		ok            bool
	}{
		{"complete", strings.NewReader(`{}`), -1, 200, 2, true},
		{"declared oversize", strings.NewReader("not read"), maxBody + 1, 413, 0, false},
		{"chunked oversize", strings.NewReader(strings.Repeat("x", maxBody+100)), -1, 413, maxBody + 1, false},
		{"incomplete", iotest.ErrReader(io.ErrUnexpectedEOF), -1, 413, 0, false},
	} {
		t.Run(trial.name, func(t *testing.T) {
			e := &endpoint{capacity: newCapacity(), reading: newCapacity()}
			body := &countedRequestBody{Reader: trial.source}
			r := httptest.NewRequest("POST", "/api/stop", body)
			r.ContentLength = trial.contentLength
			w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
			before := time.Now()
			_, ok := e.readBody(w, r, "/api/stop")
			if ok != trial.ok || w.Code != trial.status || body.bytes != trial.bytes {
				t.Fatalf("read result ok=%v status=%d bytes=%d", ok, w.Code, body.bytes)
			}
			if len(e.reading.stops) != 0 || len(e.capacity.stops) != 0 {
				t.Fatal("completed or rejected body retained read/relay capacity")
			}
			if trial.contentLength > maxBody {
				if len(w.deadlines) != 0 {
					t.Fatal("declared oversize body reached the read phase")
				}
				return
			}
			if len(w.deadlines) == 0 || w.deadlines[0].Before(before.Add(bodyReadTimeout)) || w.deadlines[0].After(time.Now().Add(bodyReadTimeout)) {
				t.Fatal("body read did not install the bounded deadline")
			}
			if ok && (len(w.deadlines) != 2 || !w.deadlines[1].IsZero()) {
				t.Fatal("successful body read did not clear its deadline")
			}
			if !ok && (len(w.deadlines) != 1 || !r.Close) {
				t.Fatal("failed read reset the deadline or allowed body draining")
			}
		})
	}
}
