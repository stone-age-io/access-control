package wire

import (
	"encoding/hex"
	"errors"
)

// ErrShortCardRead reports a REPLY_RAW payload too short to hold its own header.
var ErrShortCardRead = errors.New("osdp: REPLY_RAW payload too short")

// Card-read format codes (REPLY_RAW Data[1]).
const (
	CardFmtRawUnspecified byte = 0x00
	CardFmtRawWiegand     byte = 0x01
	CardFmtASCII          byte = 0x02
)

// CardRead is a decoded REPLY_RAW (raw card data) payload. The OSDP REPLY_RAW
// data block is [reader_no, format, bit_count(2, little-endian), packed-bits…],
// with ceil(bit_count/8) data bytes (libosdp osdp_pd.c REPLY_RAW).
type CardRead struct {
	ReaderNo int    // PD reader index (0 = first/only reader)
	Format   byte   // CardFmt* — how the PD packed the bits
	BitCount int    // number of significant card bits
	Data     []byte // the raw packed card bytes, ceil(BitCount/8) long
}

// DecodeCardRead parses a REPLY_RAW Data block (the bytes after the reply code).
func DecodeCardRead(data []byte) (CardRead, error) {
	if len(data) < 4 {
		return CardRead{}, ErrShortCardRead
	}
	bits := int(data[2]) | int(data[3])<<8
	nbytes := (bits + 7) / 8
	if nbytes > len(data)-4 {
		return CardRead{}, ErrShortCardRead
	}
	out := CardRead{
		ReaderNo: int(data[0]),
		Format:   data[1],
		BitCount: bits,
		Data:     make([]byte, nbytes),
	}
	copy(out.Data, data[4:4+nbytes])
	return out, nil
}

// Credential renders a card read as the opaque credential string that flows into
// policy.Decide and must therefore match what an operator enrolled. v1 uses the
// lowercase hex of the raw card bytes: it is lossless and deterministic for any
// bit length or format, so enrollment can mirror exactly what the bench observes.
//
// Deliberately NOT decimal/facility-code decoding: the OSDP raw bit order
// (LSB-first per byte, with the last byte's high bits padded) versus the Wiegand
// numeric convention is reader-dependent and can only be pinned down against
// physical hardware — see docs/protocol.md. Format-aware decoding is a
// post-bench enhancement layered on top of this raw value.
func (c CardRead) Credential() string {
	return hex.EncodeToString(c.Data)
}
