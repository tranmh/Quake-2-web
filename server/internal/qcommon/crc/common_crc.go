package crc

import (
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// COM_BlockSequenceCRCByte is the proxy-protection checksum byte of a
// packet: CRC of up to 60 bytes of base plus 4 table bytes selected by
// sequence, xored with their byte sum.
// C: qcommon/common.c:1329 COM_BlockSequenceCRCByte
func COM_BlockSequenceCRCByte(base []byte, sequence int32) byte {
	var chkb [60 + 4]byte

	if sequence < 0 {
		shared.Error(q2const.ERR_FATAL, "sequence < 0, this shouldn't happen\n")
	}

	p := chktbl[sequence%(int32(len(chktbl))-4):]

	length := len(base)
	if length > 60 {
		length = 60
	}
	copy(chkb[:], base[:length])

	chkb[length] = p[0]
	chkb[length+1] = p[1]
	chkb[length+2] = p[2]
	chkb[length+3] = p[3]

	length += 4

	crc := CRC_Block(chkb[:length])

	x := 0
	for n := 0; n < length; n++ {
		x += int(chkb[n])
	}

	crc = uint16((int(crc) ^ x) & 0xff)
	return byte(crc)
}
