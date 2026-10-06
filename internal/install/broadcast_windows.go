//go:build windows

package install

import (
	"syscall"
	"unsafe"
)

var (
	modUser32               = syscall.NewLazyDLL("user32.dll")
	procSendMessageTimeoutW = modUser32.NewProc("SendMessageTimeoutW")
)

// broadcastSettingChange mirrors Publish-EnvVar WM_SETTINGCHANGE
// (lib/system.ps1:5-28).
func broadcastSettingChange() {
	const HWND_BROADCAST = 0xffff
	const WM_SETTINGCHANGE = 0x1a
	env, _ := syscall.UTF16PtrFromString("Environment")
	var result uintptr
	_, _, _ = procSendMessageTimeoutW.Call(
		uintptr(HWND_BROADCAST),
		uintptr(WM_SETTINGCHANGE),
		0,
		uintptr(unsafe.Pointer(env)),
		2,
		5000,
		uintptr(unsafe.Pointer(&result)),
	)
}
