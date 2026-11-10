package scrcpy

import (
	"bytes"
	"io"
	"net"
	"testing"
)

func TestLiveBitrateControlPacket(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	driver := &ScrcpyDriver{controlConn: a}
	done := make(chan error, 1)
	go func() { done <- driver.SetVideoBitrate(4000000) }()
	packet := make([]byte, 5)
	if _, e := io.ReadFull(b, packet); e != nil {
		t.Fatal(e)
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(packet, []byte{0x70, 0, 0x3d, 0x09, 0}) {
		t.Fatalf("%x", packet)
	}
	if driver.SetVideoBitrate(1) == nil {
		t.Fatal("unbounded rate")
	}
}
