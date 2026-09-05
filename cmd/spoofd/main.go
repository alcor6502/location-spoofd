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
	"strings"
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
	logFile     = flag.String("log", "", "append the log to this file as well as stderr")
	dumpDir     = flag.String("dump", "", "debug: write each location request/response as raw files into this directory")
	observe     = flag.String("observe", "", "debug: comma-separated hosts to intercept and log (method, path, sizes) while forwarding them unchanged")
	dialLimit   = 10 * time.Second
)

type counters struct {
	conns, spliced, mitm, spoofed, passthrough atomic.Int64
}

var stats counters

// devices is the per-client on/off switch, toggled from the status page.
var devices *deviceSwitch

// bssids is the neighbourhood every reply carries: access points seen in earlier requests.
var bssids *bssidCache

// observed hosts are terminated like location hosts, logged and forwarded (debug).
var observed = map[string]bool{}

func main() {
	flag.Parse()
	log.SetFlags(log.Ldate | log.Ltime)
	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			log.Fatalf("log file: %v", err)
		}
		log.SetOutput(io.MultiWriter(os.Stderr, f))
	}
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
	for _, h := range strings.Split(*observe, ",") {
		if h = strings.TrimSpace(h); h != "" {
			observed[h] = true
		}
	}
	devices = newDeviceSwitch(*caDir)
	bssids = newBSSIDCache(*caDir, 500)

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

	if !(spoof.LocationHosts[sni] || observed[sni]) || !devices.Enabled(c.RemoteAddr()) {
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
			if observed[host] {
				rec := &statusRecorder{ResponseWriter: w}
				proxy.ServeHTTP(rec, r)
				log.Printf("%s: observe %s %s%s req=%dB -> %d %dB ua=%q", r.RemoteAddr, r.Method, host, r.URL.Path, r.ContentLength, rec.status, rec.bytes, r.Header.Get("User-Agent"))
				return
			}
			proxy.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, st, err := spoof.Respond(body, loc, bssids.Neighbours())
		if err != nil {
			stats.passthrough.Add(1)
			log.Printf("%s: %v, forwarding to Apple", r.RemoteAddr, err)
			r.Body = io.NopCloser(bytesReader(body))
			r.ContentLength = int64(len(body))
			proxy.ServeHTTP(w, r)
			return
		}
		bssids.Learn(st.BSSIDs)
		stats.spoofed.Add(1)
		log.Printf("%s: spoofed %s (%s)", r.RemoteAddr, r.Header.Get("User-Agent"), st)
		if *dumpDir != "" {
			dumpExchange(*dumpDir, r, body, resp)
		}
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
	// POST /device?spoof=on|off switches spoofing for the calling device; GET /device reports it.
	// Meant for the button on the status page and for Shortcuts ("Get contents of URL", POST).
	mux.HandleFunc("/device", func(w http.ResponseWriter, r *http.Request) {
		addr := remoteAddr(r)
		if r.Method == http.MethodPost {
			switch r.FormValue("spoof") {
			case "on":
				devices.Set(addr, true)
			case "off":
				devices.Set(addr, false)
			default:
				http.Error(w, "spoof=on|off", http.StatusBadRequest)
				return
			}
			log.Printf("%s: spoofing %s for this device", addr, r.FormValue("spoof"))
			if r.Header.Get("Accept") != "" && strings.Contains(r.Header.Get("Accept"), "text/html") {
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "%s spoof=%s\n", clientIP(addr), onOff(devices.Enabled(addr)))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		addr := remoteAddr(r)
		enabled := devices.Enabled(addr)
		next, label := "off", "Disable spoofing for this device"
		if !enabled {
			next, label = "on", "Enable spoofing for this device"
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, statusHTML, Version,
			loc.Latitude, loc.Longitude, loc.Altitude, loc.HorizontalAccuracy,
			ca.Leaf.NotAfter.Format("2006-01-02"),
			clientIP(addr), strings.ToUpper(onOff(enabled)), next, label,
			stats.conns.Load(), stats.spliced.Load(), stats.mitm.Load(), stats.spoofed.Load(), stats.passthrough.Load())
	})
	if err := http.ListenAndServe(addr, mux); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("status server: %v", err)
	}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func remoteAddr(r *http.Request) net.Addr {
	if a, err := net.ResolveTCPAddr("tcp", r.RemoteAddr); err == nil {
		return a
	}
	return &net.TCPAddr{}
}

const statusHTML = `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>spoofd</title><style>body{font-family:-apple-system,system-ui;max-width:32em;margin:2em auto;padding:0 1em;line-height:1.5}
code{background:#eee;padding:0 .3em}button{font:inherit;padding:.5em 1em}ol li{margin:.4em 0}table{border-collapse:collapse}td{padding:.2em .8em .2em 0}</style></head><body>
<h1>spoofd %s</h1>
<p>Location: <code>%.6f, %.6f</code>, altitude %d m, accuracy %d m.<br>CA valid until %s.</p>
<h2>This device</h2>
<p><code>%s</code> — spoofing <b>%s</b></p>
<form method="post" action="/device"><input type="hidden" name="spoof" value="%s"><button type="submit">%s</button></form>
<p><small>Off = this device keeps the exit node but gets its real position. Toggle Location Services off/on after switching.</small></p>
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

// dumpExchange writes the raw request body, the reply and the request headers for offline
// analysis (decode with `go run ./cmd/wlocdump <file>`).
func dumpExchange(dir string, r *http.Request, reqBody, resp []byte) {
	_ = os.MkdirAll(dir, 0o755)
	base := filepath.Join(dir, time.Now().Format("150405.000")+"-"+clientIP(remoteAddr(r)))
	_ = os.WriteFile(base+".req", reqBody, 0o644)
	_ = os.WriteFile(base+".resp", resp, 0o644)
	var hdr []byte
	for k, v := range r.Header {
		hdr = append(hdr, []byte(k+": "+v[0]+"\n")...)
	}
	_ = os.WriteFile(base+".hdr", hdr, 0o644)
}

// statusRecorder captures status and size of a proxied response for the observe log.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }
func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	s.bytes += len(b)
	return s.ResponseWriter.Write(b)
}
