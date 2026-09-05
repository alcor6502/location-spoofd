package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"math"
	"net"
	"net/http"
	"testing"

	pb "github.com/alcor6502/location-spoofd/pb"
	"github.com/alcor6502/location-spoofd/spoof"
	"google.golang.org/protobuf/proto"
)

// TestEndToEnd drives the real accept loop: a TLS client with SNI gs-loc.apple.com that trusts
// our CA posts an ARPC query and must get back its own device list placed at the spoofed location.
func TestEndToEnd(t *testing.T) {
	certPEM, keyPEM, err := spoof.GenerateCA()
	if err != nil {
		t.Fatal(err)
	}
	ca, err := spoof.ParseCA(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	certs := spoof.NewHostCerts(ca)
	loc := spoof.DefaultAccuracy(48.858370, 2.294481)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handleConn(c, certs, loc)
		}
	}()

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(_ context.Context, network, _ string) (net.Conn, error) {
			return net.Dial(network, ln.Addr().String())
		},
		TLSClientConfig: &tls.Config{ServerName: "gs-loc.apple.com", RootCAs: pool},
	}}

	payload, _ := proto.Marshal(&pb.AppleWLoc{WifiDevices: []*pb.WifiDevice{{Bssid: "aa:bb:cc:dd:ee:ff"}}})
	body := spoof.ArpcSerialize(&spoof.ArpcRequest{Version: "1", Locale: "en_US", AppIdentifier: "com.apple.locationd", OsVersion: "26.5", Payload: payload})

	resp, err := client.Post("https://gs-loc.apple.com/clls/wloc", "application/octet-stream", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	out := &pb.AppleWLoc{}
	if err := proto.Unmarshal(got[10:], out); err != nil {
		t.Fatalf("response payload: %v", err)
	}
	if l := out.WifiDevices[0].Location; l == nil || math.Abs(spoof.CoordFromInt(l.GetLatitude())-48.858370) > 1e-7 {
		t.Errorf("device not moved: %+v", out.WifiDevices[0])
	}
	if stats.spoofed.Load() != 1 {
		t.Errorf("spoofed counter %d", stats.spoofed.Load())
	}
}
