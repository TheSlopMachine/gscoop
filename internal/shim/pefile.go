package shim

import (
	"encoding/binary"
)

// PEMachine reads the COFF machine field via the 0x3C PE offset,
// mirroring Get-PEMachine (lib/core.ps1:24-42). Unparsable data
// returns 0.
func PEMachine(data []byte) uint16 {
	if len(data) < 64 {
		return 0
	}
	peOff := int(int32(binary.LittleEndian.Uint32(data[0x3C:])))
	if peOff < 0 || peOff+6 > len(data) {
		return 0
	}
	return binary.LittleEndian.Uint16(data[peOff+4:])
}

// PESubsystem reads the optional-header subsystem field, mirroring
// Get-PESubsystem (lib/core.ps1:1-22). Unparsable data returns -1.
func PESubsystem(data []byte) int {
	if len(data) < 64 {
		return -1
	}
	peOff := int(int32(binary.LittleEndian.Uint32(data[0x3C:])))
	// Classic seeks to peOffset, records fileHeaderOffset, skips 18,
	// then reads Int16 at fileHeaderOffset + 0x5C.
	target := peOff + 0x5C
	if peOff < 0 || target+2 > len(data) {
		return -1
	}
	return int(int16(binary.LittleEndian.Uint16(data[target:])))
}

// SetSubsystem patches the subsystem field in place, mirroring
// Set-PESubsystem (lib/core.ps1:44-68).
func SetSubsystem(path string, subsystem int) error {
	return patchSubsystem(path, subsystem)
}
