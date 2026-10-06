package shim

import (
	"encoding/binary"
	"os"
)

func patchSubsystem(path string, subsystem int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) < 64 {
		return nil
	}
	peOff := int(int32(binary.LittleEndian.Uint32(data[0x3C:])))
	target := peOff + 0x5C
	if peOff < 0 || target+2 > len(data) {
		return nil
	}
	binary.LittleEndian.PutUint16(data[target:], uint16(subsystem))
	return os.WriteFile(path, data, 0o755)
}
