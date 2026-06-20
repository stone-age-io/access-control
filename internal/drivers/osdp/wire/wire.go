// Package wire is a minimal, pure-Go OSDP (Open Supervised Device Protocol)
// packet codec for the Control Panel (CP/ACU) side: it builds CP→PD command
// packets and parses PD→CP reply packets on an RS485 bus. It is byte-level only
// — no serial I/O, no state machine, no secure channel — so it is trivially
// unit-testable and the CP engine (internal/drivers/osdp) owns all timing,
// sequencing, retry, and PD lifecycle on top of it.
//
// The clear-text wire format is implemented here; OSDP Secure Channel (the SCB
// block, per-message MAC, and payload encryption) is a deliberate v1 omission —
// a secure reply parses to ErrSecureUnsupported so the engine fails closed
// rather than mis-decoding.
//
// Lineage: the codec is informed by verkada/go-osdp (MIT, pure-Go OSDP codec)
// and the framing details are taken from osdp-dev/libosdp (Apache-2.0, the de
// facto OSDP reference implementation) — specifically the optional mark byte,
// the control-byte bitfield, the address reply-bit convention, the CRC-16
// variant, and the REPLY_RAW payload layout, all of which go-osdp glosses over.
package wire

// Framing bytes.
const (
	// SOM is the OSDP start-of-message marker that opens every packet.
	SOM byte = 0x53
	// Mark is the optional leader byte a CP may send before SOM to help a PD
	// re-frame on an idle bus. It is NOT counted in the length field or the CRC.
	Mark byte = 0xFF
)

// Address-byte conventions. The low 7 bits are the PD address; bit 7 (ReplyFlag)
// is set by a PD on its replies so a CP can tell a reply from a stray command on
// a shared bus. Address 0x7F is the broadcast address (CP→all PDs).
const (
	AddrMask      byte = 0x7F
	ReplyFlag     byte = 0x80
	AddrBroadcast byte = 0x7F
	MaxAddr       byte = 0x7E // 0x00..0x7E are unicast PD addresses
)

// Control-byte bitfield (the 5th byte of every packet).
const (
	CtrlSeqMask byte = 0x03 // bits 0-1: sequence number (0..3)
	CtrlCRC     byte = 0x04 // bit 2: 1 = trailing CRC-16, 0 = 1-byte checksum
	CtrlSCB     byte = 0x08 // bit 3: 1 = a Secure Channel Block follows the control byte
)

// headerLen is SOM + address + len_lsb + len_msb + control.
const headerLen = 5

// Code is an OSDP command or reply code (the byte after the control byte / SCB).
type Code byte

// Command codes (CP→PD). Only the subset the CP engine issues in v1 is enumerated
// here; the full set lives in the OSDP spec.
const (
	CmdPoll Code = 0x60 // CMD_POLL — "anything to report?"; the steady-state heartbeat
	CmdID   Code = 0x61 // CMD_ID — request PD identification (REPLY_PDID)
	CmdCap  Code = 0x62 // CMD_CAP — request PD capabilities (REPLY_PDCAP)
	CmdLED  Code = 0x69 // CMD_LED — drive a reader LED (grant/deny feedback; v2)
	CmdBuz  Code = 0x6A // CMD_BUZ — drive a reader buzzer (v2)
)

// Reply codes (PD→CP).
const (
	ReplyACK    Code = 0x40 // REPLY_ACK — nothing to report / command accepted
	ReplyNAK    Code = 0x41 // REPLY_NAK — command rejected; Data[0] is a NAK code
	ReplyPDID   Code = 0x45 // REPLY_PDID — PD identification (answer to CMD_ID)
	ReplyPDCAP  Code = 0x46 // REPLY_PDCAP — PD capabilities (answer to CMD_CAP)
	ReplyRAW    Code = 0x50 // REPLY_RAW — raw card data (a card was presented)
	ReplyFMT    Code = 0x51 // REPLY_FMT — formatted (ASCII) card data
	ReplyKeypad Code = 0x53 // REPLY_KEYPAD — keypad digits
	ReplyBusy   Code = 0x79 // REPLY_BUSY — PD busy; CP should retry the same command
)

// NAK codes carried in REPLY_NAK Data[0].
const (
	NAKMsgCheck   byte = 0x01 // bad CRC/checksum
	NAKCmdLen     byte = 0x02 // bad command length
	NAKCmdUnknown byte = 0x03 // unknown / unsupported command
	NAKSeqNum     byte = 0x04 // unexpected sequence number — CP must re-init the link
	NAKSecBlock   byte = 0x05 // unsupported security block
	NAKSecCond    byte = 0x06 // security conditions not met
	NAKBioType    byte = 0x07
	NAKBioFmt     byte = 0x08
	NAKUnknown    byte = 0x09
)

// String renders a Code as its OSDP mnemonic where known, else a hex literal, so
// log lines and event reasons stay readable.
func (c Code) String() string {
	switch c {
	case CmdPoll:
		return "CMD_POLL"
	case CmdID:
		return "CMD_ID"
	case CmdCap:
		return "CMD_CAP"
	case CmdLED:
		return "CMD_LED"
	case CmdBuz:
		return "CMD_BUZ"
	case ReplyACK:
		return "REPLY_ACK"
	case ReplyNAK:
		return "REPLY_NAK"
	case ReplyPDID:
		return "REPLY_PDID"
	case ReplyPDCAP:
		return "REPLY_PDCAP"
	case ReplyRAW:
		return "REPLY_RAW"
	case ReplyFMT:
		return "REPLY_FMT"
	case ReplyKeypad:
		return "REPLY_KEYPAD"
	case ReplyBusy:
		return "REPLY_BUSY"
	default:
		const hex = "0123456789abcdef"
		return "0x" + string([]byte{hex[c>>4], hex[c&0x0f]})
	}
}
