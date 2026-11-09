package scrcpy

import "testing"

func TestMarbleSCIDSocketNames(t *testing.T) {
	for value, want := range map[uint32]string{0: "00000000", 1: "00000001", 0x06cfae55: "06cfae55", 0x7fffffff: "7fffffff", 0xffffffff: "7fffffff"} {
		if got := formatSCID(value); got != want {
			t.Fatalf("SCID %#x: got %q, want scrcpy-compatible %q", value, got, want)
		}
	}
}
