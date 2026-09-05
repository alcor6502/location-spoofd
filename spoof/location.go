package spoof

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"

	pb "github.com/alcor6502/location-spoofd/pb"
	"google.golang.org/protobuf/proto"
)

// Hosts served by Apple's WiFi/cell positioning service.
var LocationHosts = map[string]bool{
	"gs-loc.apple.com":    true,
	"gs-loc-cn.apple.com": true,
}

// IsLocationRequest reports whether req is a positioning query that should be answered locally.
func IsLocationRequest(req *http.Request) bool {
	return LocationHosts[req.Host] && req.URL.Path == "/clls/wloc" && req.Method == http.MethodPost
}

// Stats describes what Respond did with one request, for diagnostics.
type Stats struct {
	WifiCount int
	CellCount int
	Modified  int // Location fields written
	InBytes   int // ARPC payload size in
	OutBytes  int // ARPC payload size out
}

func (s Stats) String() string {
	return fmt.Sprintf("wifi=%d cell=%d modified=%d in=%dB out=%dB", s.WifiCount, s.CellCount, s.Modified, s.InBytes, s.OutBytes)
}

// ErrPassThrough is returned by Respond when the body isn't a request we understand;
// the caller should forward it to Apple unchanged.
var ErrPassThrough = errors.New("not an ARPC location request")

// arpcResponseMagic is the fixed 8-byte header locationd expects in front of the response payload.
var arpcResponseMagic = []byte{0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00}

// Respond builds the complete ARPC reply for a /clls/wloc request body, with every access point
// and cell tower placed at loc. Apple is never contacted: the request's own device list is turned
// into the answer.
func Respond(body []byte, loc Location) ([]byte, Stats, error) {
	var st Stats

	arpc := ArpcDeserialize(body)
	if arpc == nil {
		return nil, st, fmt.Errorf("%w: ARPC header (gzip or version mismatch?)", ErrPassThrough)
	}
	st.InBytes = len(arpc.Payload)

	// proto.Unmarshal is only used to validate parsing and count devices, not for rewriting.
	wloc := &pb.AppleWLoc{}
	if err := proto.Unmarshal(arpc.Payload, wloc); err != nil {
		return nil, st, fmt.Errorf("%w: protobuf: %v", ErrPassThrough, err)
	}
	st.WifiCount = len(wloc.WifiDevices)
	st.CellCount = len(wloc.CellTowerResponse)

	// Recursive raw wire splice: only touch the Location fields we spoof (lat/lon/accuracy/altitude);
	// every other field (unknown tags/NumCellResults/DeviceType, ...) is preserved byte-for-byte.
	payload, modified := RewriteAppleWLoc(arpc.Payload, loc)
	st.Modified = modified
	st.OutBytes = len(payload)

	// Response framing: 8-byte magic + 2-byte big-endian length + payload.
	out := make([]byte, 0, len(arpcResponseMagic)+2+len(payload))
	out = append(out, arpcResponseMagic...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(payload)))
	out = append(out, payload...)
	return out, st, nil
}
