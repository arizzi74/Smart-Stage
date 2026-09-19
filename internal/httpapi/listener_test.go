package httpapi

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestConnectionCapReleasesSlotsAndUnblocksShutdown(t *testing.T) {
	raw, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := newBoundedListener(raw, 1)
	t.Cleanup(func() { _ = listener.Close() })
	dial := func() net.Conn {
		t.Helper()
		conn, err := net.DialTimeout("tcp4", listener.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
	type result struct {
		conn net.Conn
		err  error
	}
	accept := func() <-chan result {
		ch := make(chan result, 1)
		go func() {
			conn, err := listener.Accept()
			ch <- result{conn, err}
		}()
		return ch
	}
	receive := func(ch <-chan result) result {
		t.Helper()
		select {
		case got := <-ch:
			return got
		case <-time.After(3 * time.Second):
			t.Fatal("Accept did not finish after capacity became available or listener closed")
			return result{}
		}
	}
	dial()
	first := receive(accept())
	if first.err != nil {
		t.Fatal(first.err)
	}
	t.Cleanup(func() { _ = first.conn.Close() })
	dial()
	pending := accept()
	select {
	case got := <-pending:
		if got.conn != nil {
			_ = got.conn.Close()
		}
		t.Fatalf("second connection passed the cap: %v", got.err)
	case <-time.After(100 * time.Millisecond):
	}
	_ = first.conn.Close()
	// Duplicate close must not release another connection's slot or block.
	_ = first.conn.Close()
	second := receive(pending)
	if second.err != nil {
		t.Fatal(second.err)
	}
	t.Cleanup(func() { _ = second.conn.Close() })
	blocked := accept()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if got := receive(blocked); !errors.Is(got.err, net.ErrClosed) {
		t.Fatalf("shutdown while at capacity = %v; want closed listener", got.err)
	}
	_ = second.conn.Close()
	if got := receive(accept()); !errors.Is(got.err, net.ErrClosed) {
		t.Fatalf("Accept after shutdown = %v; want closed listener", got.err)
	}
}
