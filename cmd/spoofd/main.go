// spoofd answers Apple positioning queries with a fixed location, on the router that serves
// as Tailscale exit node. TLS connections DNAT'ed to it are inspected for their SNI: traffic
// for gs-loc.apple.com is terminated with a certificate signed by our CA (installed and
// trusted on the phone) and answered locally, everything else is spliced through untouched.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/alcor6502/location-spoofd/spoof"
)

// Version is set at build time: -ldflags "-X main.Version=v1.2.3" (see Makefile).
var Version = "dev"

var (
	lat         = flag.Float64("lat", 0, "spoofed latitude, decimal degrees (required)")
	lon         = flag.Float64("lon", 0, "spoofed longitude, decimal degrees (required)")
	alt         = flag.Int64("alt", 0, "spoofed altitude, metres")
	hAcc        = flag.Int64("hacc", 5, "reported horizontal accuracy, metres (small = trusted over GPS)")
	vAcc        = flag.Int64("vacc", 3, "reported vertical accuracy, metres")
	tlsAddr     = flag.String("listen", ":18443", "where DNAT'ed HTTPS traffic arrives")
	httpAddr    = flag.String("http", ":18080", "status page and CA download (http://<router>:18080/ca.crt)")
	caDir       = flag.String("ca-dir", "/etc/spoofd", "directory holding ca.pem and ca-key.pem (created if missing)")
	verbose     = flag.Bool("v", false, "log every connection, not just spoofed requests")
	showVersion = flag.Bool("version", false, "print version and exit")
	dialLimit   = 10 * time.Second
)

type counters struct {
	conns, spliced, mitm, spoofed, passthrough atomic.Int64
}

var stats counters

func main() {
	flag.Parse()
	log.SetFlags(log.Ltime)
	if *showVersion {
		fmt.Println("spoofd", Version)
		return
	}
	if *lat == 0 && *lon == 0 {
		log.Fatal("spoofd: -lat and -lon are required (decimal degrees, e.g. -lat 48.858370 -lon 2.294481)")
	}
	if *lat < -90 || *lat > 90 || *lon < -180 || *lon > 180 {
		log.Fatalf("spoofd: coordinates out of range: %f, %f", *lat, *lon)
	}

	loc := spoof.Location{Latitude: *lat, Longitude: *lon, Altitude: *alt, HorizontalAccuracy: *hAcc, VerticalAccuracy: *vAcc}

	ca, err := loadOrCreateCA(*caDir)
	if err != nil {
		log.Fatalf("CA: %v", err)
	}
	certs := spoof.NewHostCerts(ca)

	go serveStatus(*httpAddr, ca, loc)

	ln, err := net.Listen("tcp", *tlsAddr)
	if err != nil {
		log.Fatalf("listen %s: %v", *tlsAddr, err)
	}
	log.Printf("spoofd %s: location %.6f, %.6f alt %dm; TLS on %s, status on %s", Version, loc.Latitude, loc.Longitude, loc.Altitude, *tlsAddr, *httpAddr)

	for {
		c, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go handleConn(c, certs, loc)
	}
}

func loadOrCreateCA(dir string) (*tls.Certificate, error) {
	certPath := filepath.Join(dir, "ca.pem")
	keyPath := filepath.Join(dir, "ca-key.pem")
	certPEM, err1 := os.ReadFile(certPath)
	keyPEM, err2 := os.ReadFile(keyPath)
	if err1 != nil || err2 != nil {
		log.Printf("generating new CA in %s", dir)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		certPEM, keyPEM, err1 = spoof.GenerateCA()
		if err1 != nil {
			return nil, err1
		}
		if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
			return nil, err
		}
		if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
			return nil, err
		}
	}
	return spoof.ParseCA(certPEM, keyPEM)
}

// handleConn decides per connection: MITM Apple's location host, splice everything else.
func handleConn(c net.Conn, certs *spoof.HostCerts, loc spoof.Location) {
	defer c.Close()
	stats.conns.Add(1)

	origDst, origErr := originalDst(c)

	_ = c.SetReadDeadline(time.Now().Add(dialLimit))
	sni, replay, err := peekSNI(c)
	_ = c.SetReadDeadline(time.Time{})
	if err != nil {
		if *verbose {
			log.Printf("%s: no ClientHello (%v)", c.RemoteAddr(), err)
		}
		return
	}

	if !spoof.LocationHosts[sni] {
		stats.spliced.Add(1)
		target := origDst
		if origErr != nil {
			target = net.JoinHostPort(sni, "443")
		}
		if *verbose {
			log.Printf("%s: splice %s -> %s", c.RemoteAddr(), sni, target)
		}
		splice(replay, target)
		return
	}

	stats.mitm.Add(1)
	log.Printf("%s: intercepting %s", c.RemoteAddr(), sni)
	tlsConn := tls.Server(replay, certs.TLSConfig())
	if err := tlsConn.Handshake(); err != nil {
		log.Printf("%s: TLS handshake with %s failed (CA not trusted on the phone?): %v", c.RemoteAddr(), sni, err)
		return
	}
	defer tlsConn.Close()

	upstream := origDst
	if origErr != nil {
		upstream = net.JoinHostPort(sni, "443")
	}
	srv := &http.Server{
		Handler:           locationHandler(sni, upstream, loc),
		ReadHeaderTimeout: dialLimit,
	}
	_ = srv.Serve(newOneConnListener(tlsConn))
}

// locationHandler answers /clls/wloc locally and reverse-proxies anything else to the real host.
func locationHandler(host, upstream string, loc spoof.Location) http.Handler {
	proxy := &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			r.URL.Scheme = "https"
			r.URL.Host = host
			r.Host = host
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: dialLimit}).DialContext(ctx, network, upstream)
			},
			TLSClientConfig: &tls.Config{ServerName: host},
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !spoof.IsLocationRequest(r) {
			stats.passthrough.Add(1)
			proxy.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, st, err := spoof.Respond(body, loc)
		if err != nil {
			stats.passthrough.Add(1)
			log.Printf("%s: %v, forwarding to Apple", r.RemoteAddr, err)
			r.Body = io.NopCloser(bytesReader(body))
			r.ContentLength = int64(len(body))
			proxy.ServeHTTP(w, r)
			return
		}
		stats.spoofed.Add(1)
		log.Printf("%s: spoofed %s (%s)", r.RemoteAddr, r.Header.Get("User-Agent"), st)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprint(len(resp)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(resp)
	})
}

// splice copies bytes both ways between the client and the original destination.
func splice(client net.Conn, target string) {
	server, err := net.DialTimeout("tcp", target, dialLimit)
	if err != nil {
		log.Printf("splice to %s: %v", target, err)
		return
	}
	defer server.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(server, client); closeWrite(server); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, server); closeWrite(client); done <- struct{}{} }()
	<-done
	<-done
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
}

// serveStatus exposes the CA for installation on the phone plus a small counters page.
func serveStatus(addr string, ca *tls.Certificate, loc spoof.Location) {
	certPEM := spoof.CertPEM(ca)
	mux := http.NewServeMux()
	mux.HandleFunc("/ca.crt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-x509-ca-cert")
		w.Header().Set("Content-Disposition", `attachment; filename="location-spoofer-ca.crt"`)
		_, _ = w.Write(certPEM)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, statusHTML, Version,
			loc.Latitude, loc.Longitude, loc.Altitude, loc.HorizontalAccuracy,
			ca.Leaf.NotAfter.Format("2006-01-02"),
			stats.conns.Load(), stats.spliced.Load(), stats.mitm.Load(), stats.spoofed.Load(), stats.passthrough.Load())
	})
	if err := http.ListenAndServe(addr, mux); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("status server: %v", err)
	}
}

const statusHTML = `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>spoofd</title><style>body{font-family:-apple-system,system-ui;max-width:32em;margin:2em auto;padding:0 1em;line-height:1.5}
code{background:#eee;padding:0 .3em}ol li{margin:.4em 0}table{border-collapse:collapse}td{padding:.2em .8em .2em 0}</style></head><body>
<h1>spoofd %s</h1>
<p>Location: <code>%.6f, %.6f</code>, altitude %d m, accuracy %d m.<br>CA valid until %s.</p>
<h2>Set up a phone</h2>
<ol>
<li>Connect through this router as Tailscale exit node.</li>
<li>Open <a href="/ca.crt">/ca.crt</a> in Safari and allow the profile download.</li>
<li>Settings &gt; General &gt; VPN &amp; Device Management &gt; install the profile.</li>
<li>Settings &gt; General &gt; About &gt; Certificate Trust Settings &gt; enable <b>Location Spoofer CA</b>.</li>
<li>Toggle Location Services off and on (Settings &gt; Privacy &amp; Security).</li>
</ol>
<h2>Counters</h2>
<table><tr><td>connections</td><td>%d</td></tr><tr><td>spliced (other hosts)</td><td>%d</td></tr>
<tr><td>intercepted</td><td>%d</td></tr><tr><td>spoofed queries</td><td>%d</td></tr><tr><td>forwarded to Apple</td><td>%d</td></tr></table>
</body></html>`
