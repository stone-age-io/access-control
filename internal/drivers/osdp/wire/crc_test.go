package wire

import "testing"

func TestCRC16CatalogueVector(t *testing.T) {
	// CRC-16/AUG-CCITT has a published check value of 0xE5CC for "123456789".
	// Asserting it pins our implementation to the standard variant libosdp uses.
	if got := crc16([]byte("123456789")); got != 0xE5CC {
		t.Fatalf("crc16(\"123456789\") = 0x%04X, want 0xE5CC", got)
	}
}

func TestChecksumSumsToZero(t *testing.T) {
	data := []byte{0x53, 0x80, 0x07, 0x00, 0x01, 0x40}
	cs := checksum(data)
	var total byte
	for _, b := range data {
		total += b
	}
	total += cs
	if total != 0 {
		t.Fatalf("data+checksum = %d (mod 256), want 0", total)
	}
}
