package spoof

import (
	"encoding/hex"
	"errors"
	"math"
	"net/http"
	"testing"

	pb "github.com/alcor6502/location-spoofd/pb"
	"google.golang.org/protobuf/proto"
)

// Real request captured from locationd on iOS 26.2. The payload carries no WiFi devices,
// which is why it is only used to validate ARPC header parsing.
const realRequestHex = "0001000a656e2d3030315f3030310013636f6d2e6170706c652e6c6f636174696f6e64000a32362e322e3233433535000000020000000002000000"

func TestDeserializeRealRequest(t *testing.T) {
	requestBytes, _ := hex.DecodeString(realRequestHex)

	arpc := ArpcDeserialize(requestBytes)
	if arpc == nil {
		t.Fatal("Failed to deserialize ARPC")
	}
	if arpc.Version != "1" {
		t.Errorf("Version: got %q, want %q", arpc.Version, "1")
	}
	if arpc.Locale != "en-001_001" {
		t.Errorf("Locale: got %q, want %q", arpc.Locale, "en-001_001")
	}
	if arpc.AppIdentifier != "com.apple.locationd" {
		t.Errorf("AppIdentifier: got %q, want %q", arpc.AppIdentifier, "com.apple.locationd")
	}
	if arpc.OsVersion != "26.2.23C55" {
		t.Errorf("OsVersion: got %q, want %q", arpc.OsVersion, "26.2.23C55")
	}

	wloc := &pb.AppleWLoc{}
	if err := proto.Unmarshal(arpc.Payload, wloc); err != nil {
		t.Fatalf("Failed to unmarshal protobuf: %v", err)
	}
}

func TestArpcSerializeRoundTrip(t *testing.T) {
	in := &ArpcRequest{
		Version:       "1",
		Locale:        "it-IT_IT",
		AppIdentifier: "com.apple.locationd",
		OsVersion:     "26.5.1",
		Payload:       []byte{0x10, 0x01, 0x18, 0x02},
	}
	out := ArpcDeserialize(ArpcSerialize(in))
	if out == nil {
		t.Fatal("round trip failed to deserialize")
	}
	if out.Version != in.Version || out.Locale != in.Locale ||
		out.AppIdentifier != in.AppIdentifier || out.OsVersion != in.OsVersion {
		t.Errorf("header mismatch: got %+v, want %+v", out, in)
	}
	if string(out.Payload) != string(in.Payload) {
		t.Errorf("payload mismatch: got %x, want %x", out.Payload, in.Payload)
	}
}

func TestArpcDeserializeTruncated(t *testing.T) {
	requestBytes, _ := hex.DecodeString(realRequestHex)
	// The real request declares an empty payload, so the message is complete after
	// 55 bytes (header + payload length); anything after that is ignored by the parser.
	const completeLen = 55
	for cut := 0; cut < completeLen; cut++ {
		if arpc := ArpcDeserialize(requestBytes[:cut]); arpc != nil {
			t.Errorf("truncated input of %d bytes should return nil, got %+v", cut, arpc)
		}
	}
}

// TestRewriteAppleWLocCoords is the core test: it builds a realistic request with one WiFi device
// that already carries a Location (with extra fields), one that doesn't, and one cell tower, runs
// the raw wire rewrite, and checks that only the spoofed fields changed while everything else survived.
func TestRewriteAppleWLocCoords(t *testing.T) {
	loc := Location{
		Latitude:           48.858370, // Eiffel Tower
		Longitude:          2.294481,
		HorizontalAccuracy: 5,
		Altitude:           4,
		VerticalAccuracy:   3,
	}
	wantLat, wantLon := IntFromCoord(loc.Latitude), IntFromCoord(loc.Longitude)

	numWifi := int32(2)
	accuracy := int64(39)
	altitude := int64(530)
	motion := int64(7)
	uarfcn := uint32(1234)
	req := &pb.AppleWLoc{
		NumWifiResults: &numWifi,
		WifiDevices: []*pb.WifiDevice{
			{
				Bssid: "aa:bb:cc:dd:ee:ff",
				Location: &pb.Location{
					Latitude:           i64(5151042000),
					Longitude:          i64(-321830600),
					HorizontalAccuracy: &accuracy,
					Altitude:           &altitude,
					MotionActivityType: &motion,
				},
			},
			{Bssid: "11:22:33:44:55:66"}, // no Location, as sent by locationd
		},
		CellTowerResponse: []*pb.CellTower{
			{Mcc: 222, Mnc: 10, CellId: 4242, TacId: 99, Uarfcn: &uarfcn,
				Location: &pb.Location{Latitude: i64(4546420300), Longitude: i64(918998200)}},
		},
		DeviceType: &pb.DeviceType{OperatingSystem: "iPhone OS26.5", Model: "iPhone18,1"},
	}
	payload, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	newPayload, modified := RewriteAppleWLoc(payload, loc)
	if want := 5 * 3; modified != want {
		t.Errorf("modified fields: got %d, want %d (5 fields x 3 locations)", modified, want)
	}

	out := &pb.AppleWLoc{}
	if err := proto.Unmarshal(newPayload, out); err != nil {
		t.Fatalf("rewritten payload does not unmarshal: %v", err)
	}
	if len(out.WifiDevices) != 2 || len(out.CellTowerResponse) != 1 {
		t.Fatalf("got %d wifi devices, %d cell towers; want 2, 1", len(out.WifiDevices), len(out.CellTowerResponse))
	}

	checkLoc := func(name string, got *pb.Location) {
		t.Helper()
		if got == nil {
			t.Fatalf("%s: Location missing after rewrite", name)
		}
		if got.GetLatitude() != wantLat || got.GetLongitude() != wantLon {
			t.Errorf("%s: coords (%d, %d), want (%d, %d)", name, got.GetLatitude(), got.GetLongitude(), wantLat, wantLon)
		}
		if got.GetHorizontalAccuracy() != loc.HorizontalAccuracy || got.GetAltitude() != loc.Altitude || got.GetVerticalAccuracy() != loc.VerticalAccuracy {
			t.Errorf("%s: hAcc/alt/vAcc = %d/%d/%d, want %d/%d/%d", name,
				got.GetHorizontalAccuracy(), got.GetAltitude(), got.GetVerticalAccuracy(), loc.HorizontalAccuracy, loc.Altitude, loc.VerticalAccuracy)
		}
	}
	for i, d := range out.WifiDevices {
		if d.Bssid != req.WifiDevices[i].Bssid {
			t.Errorf("device %d: BSSID changed to %q", i, d.Bssid)
		}
		checkLoc(d.Bssid, d.Location)
	}
	cell := out.CellTowerResponse[0]
	checkLoc("cell", cell.Location)
	if cell.Mcc != 222 || cell.Mnc != 10 || cell.CellId != 4242 || cell.TacId != 99 || cell.GetUarfcn() != uarfcn {
		t.Errorf("cell tower identity not preserved: %+v", cell)
	}

	// Fields we don't spoof must be preserved byte-for-byte.
	if out.WifiDevices[0].Location.GetMotionActivityType() != motion {
		t.Errorf("MotionActivityType not preserved: %d", out.WifiDevices[0].Location.GetMotionActivityType())
	}
	if out.GetNumWifiResults() != numWifi {
		t.Errorf("NumWifiResults not preserved: %d", out.GetNumWifiResults())
	}
	if out.GetDeviceType().GetModel() != "iPhone18,1" {
		t.Errorf("DeviceType not preserved: %+v", out.GetDeviceType())
	}
}

// A cell-only query (no WiFi, only cell_tower_request) must come back with a cell_tower_response
// for the same cell placed at the spoofed location.
func TestRewriteCellQuery(t *testing.T) {
	loc := DefaultAccuracy(48.858370, 2.294481)
	req := &pb.AppleWLoc{
		CellTowerRequest: &pb.CellTower{Mcc: 222, Mnc: 10, CellId: 4242, TacId: 99},
	}
	payload, _ := proto.Marshal(req)
	newPayload, _ := RewriteAppleWLoc(payload, loc)

	out := &pb.AppleWLoc{}
	if err := proto.Unmarshal(newPayload, out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.CellTowerResponse) != 1 {
		t.Fatalf("cell_tower_response entries: %d, want 1", len(out.CellTowerResponse))
	}
	cell := out.CellTowerResponse[0]
	if cell.Mcc != 222 || cell.Mnc != 10 || cell.CellId != 4242 || cell.TacId != 99 {
		t.Errorf("cell identity not copied: %+v", cell)
	}
	if cell.Location == nil || cell.Location.GetLatitude() != IntFromCoord(loc.Latitude) {
		t.Errorf("cell not placed at spoofed location: %+v", cell.Location)
	}
	if out.CellTowerRequest == nil || out.CellTowerRequest.Location == nil {
		t.Errorf("cell_tower_request should be kept (rewritten): %+v", out.CellTowerRequest)
	}
}

func TestRewriteAppleWLocCoordsPassThrough(t *testing.T) {
	garbage := []byte{0x12, 0xff, 0xff, 0xff, 0xff, 0xff} // LEN field claiming an absurd length
	out, modified := RewriteAppleWLoc(garbage, Location{Latitude: 1, Longitude: 2})
	if modified != 0 {
		t.Errorf("modified on garbage: %d", modified)
	}
	if string(out) != string(garbage) {
		t.Errorf("garbage input must be returned unchanged, got %x", out)
	}
}

func TestRespond(t *testing.T) {
	loc := DefaultAccuracy(48.858370, 2.294481)
	req := &pb.AppleWLoc{
		WifiDevices: []*pb.WifiDevice{{Bssid: "aa:bb:cc:dd:ee:ff"}, {Bssid: "11:22:33:44:55:66"}},
	}
	payload, _ := proto.Marshal(req)
	body := ArpcSerialize(&ArpcRequest{Version: "1", Locale: "en_US", AppIdentifier: "com.apple.locationd", OsVersion: "26.5", Payload: payload})

	resp, st, err := Respond(body, loc)
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if st.WifiCount != 2 || st.Modified != 10 {
		t.Errorf("stats: %s", st)
	}
	if string(resp[:8]) != string(arpcResponseMagic) {
		t.Errorf("magic: %x", resp[:8])
	}
	payloadLen := int(resp[8])<<8 | int(resp[9])
	if payloadLen != len(resp)-10 {
		t.Errorf("payload length mismatch: embedded=%d, actual=%d", payloadLen, len(resp)-10)
	}
	out := &pb.AppleWLoc{}
	if err := proto.Unmarshal(resp[10:], out); err != nil {
		t.Fatalf("response payload does not unmarshal: %v", err)
	}
	if got := CoordFromInt(out.WifiDevices[1].Location.GetLatitude()); math.Abs(got-loc.Latitude) > 1e-7 {
		t.Errorf("device 1 latitude %f, want %f", got, loc.Latitude)
	}

	if _, _, err := Respond([]byte("not arpc"), loc); !errors.Is(err, ErrPassThrough) {
		t.Errorf("garbage body: err=%v, want ErrPassThrough", err)
	}
}

func TestIsLocationRequest(t *testing.T) {
	mk := func(method, host, path string) *http.Request {
		r, _ := http.NewRequest(method, "https://"+host+path, nil)
		r.Host = host
		return r
	}
	if !IsLocationRequest(mk("POST", "gs-loc.apple.com", "/clls/wloc")) {
		t.Error("wloc POST should match")
	}
	if IsLocationRequest(mk("GET", "gs-loc.apple.com", "/clls/wloc")) || IsLocationRequest(mk("POST", "www.apple.com", "/clls/wloc")) {
		t.Error("non-location requests matched")
	}
}

func TestCoordinateEncoding(t *testing.T) {
	for _, c := range []float64{51.510420, -3.218306, 45.464203, 9.189982, 0, -179.999999} {
		back := CoordFromInt(IntFromCoord(c))
		if math.Abs(back-c) > 1e-7 {
			t.Errorf("coord %f round-trips to %f", c, back)
		}
	}
}

func i64(i int) *int64 {
	v := int64(i)
	return &v
}
