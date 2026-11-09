package webservice

import (
	"encoding/binary"
	"fmt"
	"sync"
)

// All controllers target the same Android input seat. Namespace client IDs,
// reference-count held keys, and release only the departing controller's input.
type sharedInput struct {
	mu      sync.Mutex
	send    func([]byte) error
	clients map[uint32]*clientInput
	keys    map[uint16]int
	touches int
}
type clientInput struct {
	slot    uint32
	touches map[byte][]byte
	keys    map[uint16]bool
	devices map[uint16]bool
}

func newSharedInput(send func([]byte) error) *sharedInput {
	return &sharedInput{send: send, clients: make(map[uint32]*clientInput), keys: make(map[uint16]int)}
}
func (s *sharedInput) add(id, slot uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[id] = &clientInput{slot: slot, touches: make(map[byte][]byte), keys: make(map[uint16]bool), devices: make(map[uint16]bool)}
}
func (s *sharedInput) handle(id uint32, raw []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.clients[id]
	if c == nil {
		return nil
	}
	if len(raw) == 0 {
		return fmt.Errorf("empty input")
	}
	p := append([]byte(nil), raw...)
	switch p[0] {
	case 2:
		if len(p) != 10 || p[2] >= 10 || p[1] > 2 {
			return fmt.Errorf("invalid touch")
		}
		local := p[2]
		_, held := c.touches[local]
		if p[1] == 0 {
			if held || s.touches >= 10 {
				return nil
			}
			s.touches++
		} else if !held {
			return nil
		}
		p[2] = byte(c.slot*10) + local
		if p[1] == 1 {
			delete(c.touches, local)
			s.touches--
		} else {
			c.touches[local] = append([]byte(nil), p...)
		}
	case 0:
		if len(p) != 4 || p[1] > 1 {
			return fmt.Errorf("invalid key")
		}
		key := binary.BigEndian.Uint16(p[2:])
		if p[1] == 0 {
			if !c.keys[key] {
				c.keys[key] = true
				s.keys[key]++
				if s.keys[key] > 1 {
					return nil
				}
			}
		} else {
			if !c.keys[key] {
				return nil
			}
			delete(c.keys, key)
			s.keys[key]--
			if s.keys[key] > 0 {
				return nil
			}
			delete(s.keys, key)
		}
	case 12, 13, 14:
		if len(p) < 3 {
			return fmt.Errorf("invalid UHID packet")
		}
		local := binary.BigEndian.Uint16(p[1:3])
		if local == 0 || local > 15 {
			return fmt.Errorf("invalid local UHID id")
		}
		if p[0] == 12 {
			if len(p) < 10 {
				return fmt.Errorf("invalid UHID create")
			}
			offset := 8 + int(p[7])
			if len(p) < offset+2 || len(p) != offset+2+int(binary.BigEndian.Uint16(p[offset:offset+2])) {
				return fmt.Errorf("invalid UHID descriptor")
			}
			if c.devices[local] {
				return nil
			}
			c.devices[local] = true
		} else {
			if !c.devices[local] {
				return nil
			}
			if p[0] == 13 && (len(p) < 5 || len(p) != 5+int(binary.BigEndian.Uint16(p[3:5]))) {
				return fmt.Errorf("invalid UHID input")
			}
			if p[0] == 14 {
				if len(p) != 3 {
					return fmt.Errorf("invalid UHID destroy")
				}
				delete(c.devices, local)
			}
		}
		binary.BigEndian.PutUint16(p[1:3], uint16(c.slot*16)+local)
	}
	return s.send(p)
}
func (s *sharedInput) remove(id uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.clients[id]
	if c == nil {
		return
	}
	delete(s.clients, id)
	for _, p := range c.touches {
		p[1] = 1
		p[7] = 0
		p[8] = 0
		p[9] = 0
		_ = s.send(p)
		s.touches--
	}
	for key := range c.keys {
		s.keys[key]--
		if s.keys[key] == 0 {
			delete(s.keys, key)
			p := []byte{0, 1, 0, 0}
			binary.BigEndian.PutUint16(p[2:], key)
			_ = s.send(p)
		}
	}
	for local := range c.devices {
		p := []byte{14, 0, 0}
		binary.BigEndian.PutUint16(p[1:], uint16(c.slot*16)+local)
		_ = s.send(p)
	}
}
