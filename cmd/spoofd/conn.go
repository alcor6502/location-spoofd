package main

import (
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync"
)

// peekSNI reads the TLS ClientHello from c without consuming it: the bytes are buffered and
// handed back inside a net.Conn that replays them before the rest of the stream.
func peekSNI(c net.Conn) (sni string, replay net.Conn, err error) {
	var buf bytes.Buffer
	rec := &recordingConn{Conn: c, tee: io.TeeReader(c, &buf)}

	errPeeked := errors.New("peeked")
	cfg := &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			sni = hello.ServerName
			return nil, errPeeked
		},
	}
	herr := tls.Server(rec, cfg).Handshake()
	if !errors.Is(herr, errPeeked) && sni == "" {
		if herr == nil {
			herr = errors.New("handshake completed without SNI")
		}
		return "", nil, herr
	}
	return sni, &prefixConn{Conn: c, prefix: buf.Bytes()}, nil
}

// recordingConn reads through a tee and swallows writes, so a probing tls.Server
// never sends anything to the real client.
type recordingConn struct {
	net.Conn
	tee io.Reader
}

func (r *recordingConn) Read(p []byte) (int, error)  { return r.tee.Read(p) }
func (r *recordingConn) Write(p []byte) (int, error) { return len(p), nil }
func (r *recordingConn) Close() error                { return nil }

// prefixConn replays prefix before reading from the underlying connection.
type prefixConn struct {
	net.Conn
	prefix []byte
	mu     sync.Mutex
}

func (p *prefixConn) Read(b []byte) (int, error) {
	p.mu.Lock()
	if len(p.prefix) > 0 {
		n := copy(b, p.prefix)
		p.prefix = p.prefix[n:]
		p.mu.Unlock()
		return n, nil
	}
	p.mu.Unlock()
	return p.Conn.Read(b)
}

func (p *prefixConn) CloseWrite() error { return closeWriteErr(p.Conn) }

func closeWriteErr(c net.Conn) error {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

// oneConnListener lets http.Server serve a single, already accepted connection.
type oneConnListener struct {
	conn net.Conn
	once sync.Once
	done chan struct{}
}

func newOneConnListener(c net.Conn) *oneConnListener {
	return &oneConnListener{conn: c, done: make(chan struct{})}
}

func (l *oneConnListener) Accept() (net.Conn, error) {
	var c net.Conn
	l.once.Do(func() { c = l.conn })
	if c != nil {
		return c, nil
	}
	<-l.done
	return nil, net.ErrClosed
}

func (l *oneConnListener) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return nil
}

func (l *oneConnListener) Addr() net.Addr { return l.conn.LocalAddr() }

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }
