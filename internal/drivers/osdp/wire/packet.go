package wire

import "errors"

// maxPacket bounds a plausible packet length so a corrupt length field makes the
// parser resync immediately instead of waiting for bytes that never arrive.
// Reader replies are tiny; this is generous headroom.
const maxPacket = 1024

// Parse errors. ErrNeedMore is the soft "keep reading" signal — the engine
// should accumulate more bytes and retry. The rest are hard framing errors; the
// parser still returns a positive consumed count so the engine can discard the
// offending bytes and resync to the next start-of-message.
var (
	ErrNeedMore          = errors.New("osdp: incomplete packet, need more bytes")
	ErrBadLength         = errors.New("osdp: implausible packet length")
	ErrCRC               = errors.New("osdp: check value mismatch")
	ErrSecureUnsupported = errors.New("osdp: secure-channel packet not supported (clear-text only)")
)

// Reply is a decoded PD→CP packet. Address is the PD address with the reply bit
// stripped; IsReply reports whether that bit was set (a CP ignores packets
// without it — those are stray commands from another panel on the bus).
type Reply struct {
	Address byte
	IsReply bool
	Seq     byte
	Code    Code
	Data    []byte // payload between the code byte and the trailing check value
	CRC     bool   // true if checked with CRC-16, false if 1-byte checksum
}

// BuildCommand frames one CP→PD command: a leading mark byte, the 5-byte header
// (SOM, address, little-endian length, control), the command code, the data, and
// a trailing little-endian CRC-16. The address reply bit is forced clear (this is
// a command); seq is the caller-managed sequence number (0..3); the CP always
// sends CRC. addr may be AddrBroadcast (0x7F) to address every PD.
func BuildCommand(addr, seq byte, code Code, data []byte) []byte {
	return build(addr&AddrMask, seq, code, data)
}

// BuildReply frames one PD→CP reply: identical to BuildCommand but with the
// address reply bit set. It is the symmetric counterpart used by test PD
// simulators and any future bench harness (the runtime CP never sends replies).
func BuildReply(addr, seq byte, code Code, data []byte) []byte {
	return build(addr&AddrMask|ReplyFlag, seq, code, data)
}

func build(addrByte, seq byte, code Code, data []byte) []byte {
	control := (seq & CtrlSeqMask) | CtrlCRC
	total := headerLen + 1 + len(data) + 2 // header + code + data + CRC

	out := make([]byte, 0, 1+total)
	out = append(out, Mark)
	pktStart := len(out)
	out = append(out,
		SOM,
		addrByte,
		byte(total),
		byte(total>>8),
		control,
		byte(code),
	)
	out = append(out, data...)

	crc := crc16(out[pktStart:]) // CRC covers SOM..last data byte, not the mark
	out = append(out, byte(crc), byte(crc>>8))
	return out
}

// ParseReply scans buf for the next complete PD reply. It returns the decoded
// reply, the number of leading bytes the caller should discard from buf, and an
// error. On ErrNeedMore the reply is nil and consumed covers only junk/idle bytes
// ahead of a partial packet (so the partial packet is preserved for the next
// read). On a hard error the reply is nil and consumed advances past the bad
// framing so the caller can resync. On success consumed covers the whole packet.
func ParseReply(buf []byte) (reply *Reply, consumed int, err error) {
	// Find the start-of-message, skipping mark bytes and any line noise.
	som := -1
	for i, b := range buf {
		if b == SOM {
			som = i
			break
		}
	}
	if som < 0 {
		return nil, len(buf), ErrNeedMore // no packet in view; drop the lot
	}

	p := buf[som:]
	if len(p) < headerLen {
		return nil, som, ErrNeedMore // header not fully arrived yet
	}

	total := int(p[2]) | int(p[3])<<8
	control := p[4]
	checkLen := 1
	if control&CtrlCRC != 0 {
		checkLen = 2
	}
	if total < headerLen+1+checkLen || total > maxPacket {
		return nil, som + 1, ErrBadLength // false SOM or corrupt length; step past it
	}
	if len(p) < total {
		return nil, som, ErrNeedMore // body still arriving
	}

	// Verify the trailing check value over the framed packet.
	if control&CtrlCRC != 0 {
		got := uint16(p[total-2]) | uint16(p[total-1])<<8
		if got != crc16(p[:total-2]) {
			return nil, som + 1, ErrCRC
		}
	} else {
		if p[total-1] != checksum(p[:total-1]) {
			return nil, som + 1, ErrCRC
		}
	}

	if control&CtrlSCB != 0 {
		// A secure-channel block sits between the control byte and the code; we
		// don't decrypt in v1, so consume and reject rather than mis-parse.
		return nil, som + total, ErrSecureUnsupported
	}

	data := make([]byte, total-checkLen-(headerLen+1))
	copy(data, p[headerLen+1:total-checkLen])
	reply = &Reply{
		Address: p[1] & AddrMask,
		IsReply: p[1]&ReplyFlag != 0,
		Seq:     control & CtrlSeqMask,
		Code:    Code(p[headerLen]),
		Data:    data,
		CRC:     control&CtrlCRC != 0,
	}
	return reply, som + total, nil
}
