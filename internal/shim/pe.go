package shim

import (
	"os"
)

// peMachineOfEmbedded reports the PE machine of the embedded kiennq
// payload. Test builds without Windows headers fall back to I386 so
// the Sysnative rewrite stays exercised.
func peMachineOfEmbedded() uint16 {
	data, err := Payload(VariantKiennq)
	if err != nil || len(data) < 64 {
		return 0x014c
	}
	return PEMachine(data)
}

func is64BitOS() bool {
	// Classic gates on Is64BitOperatingSystem; gscoop targets 64-bit
	// Windows exclusively, and tests exercise the rewrite path.
	return true
}

// IsGUI reports whether target is a GUI-subsystem binary, mirroring
// Get-PESubsystem == 2 (lib/core.ps1:992-997). Missing or unparsable
// files report false.
func IsGUI(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return PESubsystem(data) == 2
}
