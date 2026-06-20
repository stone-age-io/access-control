package wire

import (
	"bytes"
	"errors"
	"testing"
)

// frameReply builds a PD→CP reply packet the way a real PD would: a mark byte,
// the header with the address reply bit set, the code, the data, and the trailing
// CRC-16 or 1-byte checksum. It mirrors what ParseReply expects so the parser is
// exercised against both check variants.
func frameReply(addr, seq byte, code Code, data []byte, useCRC bool) []byte {
	control := seq & CtrlSeqMask
	checkLen := 1
	if useCRC {
		control |= CtrlCRC
		checkLen = 2
	}
	total := headerLen + 1 + len(data) + checkLen

	pkt := []byte{SOM, addr&AddrMask | ReplyFlag, byte(total), byte(total >> 8), control, byte(code)}
	pkt = append(pkt, data...)
	if useCRC {
		crc := crc16(pkt)
		pkt = append(pkt, byte(crc), byte(crc>>8))
	} else {
		pkt = append(pkt, checksum(pkt))
	}
	return append([]byte{Mark}, pkt...)
}

func TestBuildCommandPoll(t *testing.T) {
	got := BuildCommand(0x00, 1, CmdPoll, nil)
	// mark, SOM, addr, len(8) LE, control(seq1|CRC=0x05), code, CRC(2)
	want := []byte{Mark, SOM, 0x00, 0x08, 0x00, 0x05, byte(CmdPoll)}
	if !bytes.Equal(got[:len(want)], want) {
		t.Fatalf("poll prefix = % X, want % X", got[:len(want)], want)
	}
	if len(got) != 1+8 {
		t.Fatalf("poll length = %d, want %d", len(got), 1+8)
	}
}

func TestBuildCommandRoundTrip(t *testing.T) {
	data := []byte{0xAA, 0xBB, 0xCC}
	pkt := BuildCommand(0x05, 2, CmdLED, data)
	r, consumed, err := ParseReply(pkt)
	if err != nil {
		t.Fatalf("ParseReply: %v", err)
	}
	if consumed != len(pkt) {
		t.Fatalf("consumed = %d, want %d", consumed, len(pkt))
	}
	if r.IsReply {
		t.Errorf("IsReply = true for a command packet, want false")
	}
	if r.Address != 0x05 || r.Seq != 2 || r.Code != CmdLED || !r.CRC {
		t.Errorf("fields = {addr:%d seq:%d code:%s crc:%v}", r.Address, r.Seq, r.Code, r.CRC)
	}
	if !bytes.Equal(r.Data, data) {
		t.Errorf("data = % X, want % X", r.Data, data)
	}
}

func TestParseReplyACK(t *testing.T) {
	pkt := frameReply(0x07, 1, ReplyACK, nil, true)
	r, _, err := ParseReply(pkt)
	if err != nil {
		t.Fatalf("ParseReply: %v", err)
	}
	if !r.IsReply || r.Address != 0x07 || r.Code != ReplyACK {
		t.Fatalf("fields = {reply:%v addr:%d code:%s}", r.IsReply, r.Address, r.Code)
	}
}

func TestParseReplyChecksumVariant(t *testing.T) {
	pkt := frameReply(0x02, 3, ReplyACK, nil, false)
	r, _, err := ParseReply(pkt)
	if err != nil {
		t.Fatalf("ParseReply (checksum): %v", err)
	}
	if r.CRC {
		t.Errorf("CRC = true, want checksum variant")
	}
	if r.Code != ReplyACK || r.Seq != 3 {
		t.Errorf("fields = {code:%s seq:%d}", r.Code, r.Seq)
	}
}

func TestParseReplySkipsLeadingJunk(t *testing.T) {
	pkt := frameReply(0x01, 1, ReplyACK, nil, true)
	withJunk := append([]byte{0x11, 0x22, 0xFF, 0xFF}, pkt...)
	r, consumed, err := ParseReply(withJunk)
	if err != nil {
		t.Fatalf("ParseReply: %v", err)
	}
	if consumed != len(withJunk) {
		t.Fatalf("consumed = %d, want %d (all junk + packet)", consumed, len(withJunk))
	}
	if r.Code != ReplyACK {
		t.Errorf("code = %s, want REPLY_ACK", r.Code)
	}
}

func TestParseReplyNeedMore(t *testing.T) {
	pkt := frameReply(0x01, 1, ReplyACK, nil, true)
	// Feed only part of the packet (mark + partial header).
	r, consumed, err := ParseReply(pkt[:4])
	if !errors.Is(err, ErrNeedMore) {
		t.Fatalf("err = %v, want ErrNeedMore", err)
	}
	if r != nil {
		t.Errorf("reply = %+v, want nil", r)
	}
	// The mark byte ahead of SOM may be discarded, but the SOM and what follows
	// must be preserved for the next read.
	if consumed > 1 {
		t.Errorf("consumed = %d, want <= 1 (keep the partial packet)", consumed)
	}
}

func TestParseReplyCRCMismatch(t *testing.T) {
	pkt := frameReply(0x01, 1, ReplyACK, nil, true)
	pkt[len(pkt)-1] ^= 0xFF // corrupt the CRC
	_, consumed, err := ParseReply(pkt)
	if !errors.Is(err, ErrCRC) {
		t.Fatalf("err = %v, want ErrCRC", err)
	}
	if consumed <= 0 {
		t.Errorf("consumed = %d, want > 0 so the caller can resync", consumed)
	}
}

func TestParseReplySecureRejected(t *testing.T) {
	pkt := frameReply(0x01, 1, ReplyACK, nil, true)
	// Set the SCB bit in the control byte (index: mark + 4) and refresh the CRC so
	// the packet is well-framed but secure.
	pkt[1+4] |= CtrlSCB
	body := pkt[1 : len(pkt)-2]
	crc := crc16(body)
	pkt[len(pkt)-2] = byte(crc)
	pkt[len(pkt)-1] = byte(crc >> 8)
	_, _, err := ParseReply(pkt)
	if !errors.Is(err, ErrSecureUnsupported) {
		t.Fatalf("err = %v, want ErrSecureUnsupported", err)
	}
}

func TestDecodeCardReadFromReply(t *testing.T) {
	// REPLY_RAW data: reader 0, Wiegand format, 26 bits, 4 packed bytes.
	cardBytes := []byte{0x12, 0x34, 0x56, 0x01}
	data := append([]byte{0x00, CardFmtRawWiegand, 26, 0x00}, cardBytes...)
	pkt := frameReply(0x01, 1, ReplyRAW, data, true)

	r, _, err := ParseReply(pkt)
	if err != nil {
		t.Fatalf("ParseReply: %v", err)
	}
	if r.Code != ReplyRAW {
		t.Fatalf("code = %s, want REPLY_RAW", r.Code)
	}
	cr, err := DecodeCardRead(r.Data)
	if err != nil {
		t.Fatalf("DecodeCardRead: %v", err)
	}
	if cr.ReaderNo != 0 || cr.Format != CardFmtRawWiegand || cr.BitCount != 26 {
		t.Errorf("cardread = {reader:%d fmt:%d bits:%d}", cr.ReaderNo, cr.Format, cr.BitCount)
	}
	if !bytes.Equal(cr.Data, cardBytes) {
		t.Errorf("card bytes = % X, want % X", cr.Data, cardBytes)
	}
	if cr.Credential() != "12345601" {
		t.Errorf("credential = %q, want %q", cr.Credential(), "12345601")
	}
}
