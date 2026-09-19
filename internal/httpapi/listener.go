package httpapi

import (
	"net"
	"sync"
)

// MaxConnections includes ordinary HTTP requests, idle keep-alive connections
// and SSE streams. The smaller ordinary-operation and SSE limits leave capacity
// for STOP. Pending connections beyond this cap remain in the OS listen backlog
// rather than allocating unbounded HTTP goroutines and buffers.
const MaxConnections = 256

func BoundConnections(listener net.Listener) net.Listener {
	return newBoundedListener(listener, MaxConnections)
}

type boundedListener struct {
	net.Listener
	slots chan struct{}
	done  chan struct{}
	once  sync.Once
}

func newBoundedListener(listener net.Listener, limit int) *boundedListener {
	return &boundedListener{Listener: listener, slots: make(chan struct{}, limit), done: make(chan struct{})}
}

func (l *boundedListener) Accept() (net.Conn, error) {
	select {
	case l.slots <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}
	conn, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	return &boundedConn{Conn: conn, release: func() { <-l.slots }}, nil
}

func (l *boundedListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return l.Listener.Close()
}

type boundedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *boundedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}
