// wlocdump decodes a raw /clls/wloc request (.req) or reply (.resp) captured with `spoofd -dump`
// and prints it as protobuf text, for comparing what different devices send and get.
package main

import (
	"fmt"
	"os"
	"strings"

	pb "github.com/alcor6502/location-spoofd/pb"
	"github.com/alcor6502/location-spoofd/spoof"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: wlocdump <file.req|file.resp> ...")
		os.Exit(2)
	}
	for _, path := range os.Args[1:] {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		fmt.Printf("== %s (%d bytes)\n", path, len(data))
		var payload []byte
		if strings.HasSuffix(path, ".resp") {
			if len(data) < 10 {
				fmt.Println("too short")
				continue
			}
			payload = data[10:]
		} else {
			arpc := spoof.ArpcDeserialize(data)
			if arpc == nil {
				fmt.Println("not an ARPC request")
				continue
			}
			fmt.Printf("locale=%s app=%s os=%s function=%d payload=%dB\n", arpc.Locale, arpc.AppIdentifier, arpc.OsVersion, arpc.FunctionId, len(arpc.Payload))
			payload = arpc.Payload
		}
		msg := &pb.AppleWLoc{}
		if err := proto.Unmarshal(payload, msg); err != nil {
			fmt.Printf("protobuf: %v\nhex: %x\n", err, payload)
			continue
		}
		fmt.Println(prototext.MarshalOptions{Multiline: true}.Format(msg))
	}
}
