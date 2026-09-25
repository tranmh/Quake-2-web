// Package crc ports qcommon/crc.c (16 bit CCITT/XMODEM CRC) and
// COM_BlockSequenceCRCByte from qcommon/common.c.
package crc

const (
	crcInitValue = 0xffff // C: qcommon/crc.c:28 CRC_INIT_VALUE
	crcXorValue  = 0x0000 // C: qcommon/crc.c:29 CRC_XOR_VALUE
)

// CRC_Init sets the initial CRC value.
// C: qcommon/crc.c:67 CRC_Init
func CRC_Init(crcvalue *uint16) {
	*crcvalue = crcInitValue
}

// CRC_ProcessByte feeds one byte.
// C: qcommon/crc.c:72 CRC_ProcessByte
func CRC_ProcessByte(crcvalue *uint16, data byte) {
	*crcvalue = (*crcvalue << 8) ^ crctable[byte(*crcvalue>>8)^data]
}

// CRC_Value returns the final CRC.
// C: qcommon/crc.c:77 CRC_Value
func CRC_Value(crcvalue uint16) uint16 {
	return crcvalue ^ crcXorValue
}

// CRC_Block computes the CRC of a block.
// C: qcommon/crc.c:82 CRC_Block
func CRC_Block(start []byte) uint16 {
	var crc uint16
	CRC_Init(&crc)
	for _, b := range start {
		crc = (crc << 8) ^ crctable[byte(crc>>8)^b]
	}
	return crc
}
