package webservice

import (
	"encoding/binary"
	"github.com/pion/webrtc/v4"
	"sync"
	"testing"
)

func touch(action, id byte) []byte { return []byte{2, action, id, 0, 50, 0, 70, 255, 255, 1} }
func key(action byte, code uint16) []byte {
	p := []byte{0, action, 0, 0}
	binary.BigEndian.PutUint16(p[2:], code)
	return p
}

func TestMarbleLeavingSubscriberPreservesPeers(t *testing.T) {
	m := NewWebRTCManager()
	b := &DeviceBroadcaster{Subscribers: make(map[uint32]*Subscriber)}
	m.broadcasters["phone"] = b
	restored := 0
	m.onLastSubscriber = func() { restored++ }
	for id := uint32(0); id < 4; id++ {
		pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatal(err)
		}
		defer pc.Close()
		b.Subscribers[id] = &Subscriber{PeerConnection: pc, inputSlot: id}
	}
	for id := uint32(0); id < 3; id++ {
		pc := b.Subscribers[id].PeerConnection
		m.removeSubscriber("phone", id, pc)
		m.removeSubscriber("phone", id, pc) // duplicate state callback / deferred cleanup
		if m.broadcasters["phone"] != b || len(b.Subscribers) != 3-int(id) || restored != 0 {
			t.Fatal("departing client ended a shared session")
		}
		if b.Subscribers[3].PeerConnection.ConnectionState() == webrtc.PeerConnectionStateClosed {
			t.Fatal("remaining peer was closed")
		}
	}
	m.removeSubscriber("phone", 3, b.Subscribers[3].PeerConnection)
	if len(m.broadcasters) != 0 || restored != 1 {
		t.Fatal("last departure did not restore once")
	}
}
func TestMarbleSharedControllerInput(t *testing.T) {
	var packets [][]byte
	r := newSharedInput(func(p []byte) error { packets = append(packets, append([]byte(nil), p...)); return nil })
	r.add(10, 0)
	r.add(20, 1)
	_ = r.handle(10, touch(0, 0))
	_ = r.handle(20, touch(0, 0))
	if packets[0][2] == packets[1][2] {
		t.Fatal("two controllers collided on pointer 0")
	}
	_ = r.handle(10, key(0, 29))
	_ = r.handle(20, key(0, 29))
	before := len(packets)
	_ = r.handle(10, key(1, 29))
	if len(packets) != before {
		t.Fatal("first controller released another controller's held key")
	}
	create := []byte{12, 0, 3, 0, 0, 0, 0, 0, 0, 1, 0}
	_ = r.handle(10, create)
	_ = r.handle(20, create)
	if binary.BigEndian.Uint16(packets[len(packets)-2][1:3]) == binary.BigEndian.Uint16(packets[len(packets)-1][1:3]) {
		t.Fatal("UHID ID collision")
	}
	before = len(packets)
	r.remove(10)
	if len(packets) != before+2 || r.touches != 1 {
		t.Fatal("disconnect must release exactly its own touch and UHID device")
	}
	for _, p := range packets[before:] {
		if p[0] == 0 {
			t.Fatal("disconnect released peer key")
		}
	}
	before = len(packets)
	_ = r.handle(10, touch(0, 1))
	if len(packets) != before {
		t.Fatal("departed controller still injected input")
	}
	_ = r.handle(20, touch(2, 0))
	if packets[len(packets)-1][2] != 10 {
		t.Fatal("remaining controller pointer mapping lost")
	}
	r.remove(20)
	if r.touches != 0 || len(r.keys) != 0 || len(r.clients) != 0 {
		t.Fatal("held input leaked after last disconnect")
	}
}
func TestMarbleSharedInputBoundsAndConcurrency(t *testing.T) {
	var count int
	r := newSharedInput(func(p []byte) error { count++; return nil })
	for id := uint32(0); id < 4; id++ {
		r.add(id, id)
	}
	var wg sync.WaitGroup
	for id := uint32(0); id < 4; id++ {
		wg.Add(1)
		go func(id uint32) {
			defer wg.Done()
			for n := 0; n < 100; n++ {
				_ = r.handle(id, touch(0, 0))
				_ = r.handle(id, touch(2, 0))
				_ = r.handle(id, touch(1, 0))
			}
		}(id)
	}
	wg.Wait()
	if count != 1200 || r.touches != 0 {
		t.Fatalf("concurrent input lost: packets=%d touches=%d", count, r.touches)
	}
	for i := byte(0); i < 10; i++ {
		_ = r.handle(0, touch(0, i))
	}
	before := count
	_ = r.handle(1, touch(0, 0))
	if count != before || r.touches != 10 {
		t.Fatal("Android's global ten-contact limit not enforced")
	}
	if r.handle(0, touch(0, 10)) == nil {
		t.Fatal("out of range local pointer accepted")
	}
	if r.handle(0, []byte{12, 0, 1}) == nil {
		t.Fatal("malformed UHID create accepted")
	}
	_ = r.handle(0, []byte{13, 0, 20, 0, 0})
	if count != before {
		t.Fatal("foreign UHID id accepted")
	}
	for id := uint32(0); id < 4; id++ {
		r.remove(id)
	}
	if r.touches != 0 {
		t.Fatal("touch budget not recovered")
	}
}
