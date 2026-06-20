package wire

// crc16 computes the OSDP packet check value: CRC-16/AUG-CCITT — polynomial
// 0x1021, initial value 0x1D0F, no input/output reflection, no final XOR. This
// is the same variant libosdp uses (osdp_common.c: crc16_itu_t(0x1D0F, ...)) and
// the catalogue check value for the string "123456789" is 0xE5CC (asserted in
// crc_test.go). It is computed over the whole packet from SOM through the last
// data byte (the mark byte, if present, is excluded), and the result is written
// little-endian as the final two bytes.
func crc16(data []byte) uint16 {
	crc := uint16(0x1D0F)
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// checksum computes the legacy 1-byte OSDP check value: the two's-complement of
// the sum of all bytes, so the bytes plus the checksum sum to zero (mod 256). It
// is used when the control byte's CtrlCRC bit is clear. The CP engine always
// sends CRC, but a PD may reply with a checksum, so the parser must handle both.
func checksum(data []byte) byte {
	var sum byte
	for _, b := range data {
		sum += b
	}
	return -sum // modular two's complement: sum + checksum == 0 (mod 256)
}
