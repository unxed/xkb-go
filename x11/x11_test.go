package x11

import (
	"bytes"
	"testing"
)

// These tests check the raw byte encoding of the XKB request builders
// against the XKB X11 wire protocol specification, without any network
// connection: request encoding needs no reply and so can be fully verified
// offline, the same way x11_test.go in the root xkb-go package verifies
// reply decoding against synthetic buffers.

func TestUseExtensionRequest(t *testing.T) {
	got := useExtensionRequest(0x42, 1, 0)
	want := []byte{
		0x42,     // major opcode
		0,        // minor opcode (UseExtension)
		2, 0,     // length in 4-byte units (8/4)
		1, 0, // wantedMajor
		0, 0, // wantedMinor
	}
	if !bytes.Equal(got, want) {
		t.Errorf("useExtensionRequest(0x42, 1, 0) = % x, want % x", got, want)
	}
}

func TestGetMapRequest(t *testing.T) {
	got := getMapRequest(0x42, 0x0100)
	if len(got) != 28 {
		t.Fatalf("len(getMapRequest(...)) = %d, want 28", len(got))
	}
	want := make([]byte, 28)
	want[0] = 0x42 // major opcode
	want[1] = 8    // minor opcode (GetMap)
	want[2] = 7    // length in 4-byte units (28/4)
	want[4] = 0x00 // deviceSpec low byte
	want[5] = 0x01 // deviceSpec high byte
	want[6] = 0x47 // full: KeyTypes|KeySyms|ModifierMap|VirtualMods
	if !bytes.Equal(got, want) {
		t.Errorf("getMapRequest(0x42, 0x0100) = % x, want % x", got, want)
	}
}

func TestGetNamesRequest(t *testing.T) {
	got := getNamesRequest(0x42, 0x0100)
	want := []byte{
		0x42,       // major opcode
		17,         // minor opcode (GetNames)
		3, 0,       // length in 4-byte units (12/4)
		0x00, 0x01, // deviceSpec
		0, 0, // pad
		0x40, 0x1e, 0, 0, // which: KeyTypeNames|KeyNames|KeyAliases|VirtualModNames|GroupNames = 0x1e40
	}
	if !bytes.Equal(got, want) {
		t.Errorf("getNamesRequest(0x42, 0x0100) = % x, want % x", got, want)
	}
}

func TestGetControlsRequest(t *testing.T) {
	got := getControlsRequest(0x42, 0x0100)
	want := []byte{
		0x42,       // major opcode
		6,          // minor opcode (GetControls)
		2, 0,       // length in 4-byte units (8/4)
		0x00, 0x01, // deviceSpec
		0, 0, // pad
	}
	if !bytes.Equal(got, want) {
		t.Errorf("getControlsRequest(0x42, 0x0100) = % x, want % x", got, want)
	}
}
