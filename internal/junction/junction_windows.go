//go:build windows

package junction

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	fsctlSetReparsePoint    = 0x900A4
	fsctlGetReparsePoint    = 0x900A8
	fsctlDeleteReparsePoint = 0x900AC
	ioReparseTagMountPoint  = 0xA0000003
	maxReparseSize          = 16 * 1024
)

type reparseDataBuffer struct {
	Tag        uint32
	DataLength uint16
	Reserved   uint16
	Data       [maxReparseSize - 8]byte
}

func openReparseHandle(path string, access uint32) (windows.Handle, error) {
	p16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(p16, access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
}

func isReparse(path string) (bool, error) {
	p16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	attrs, err := windows.GetFileAttributes(p16)
	if err != nil {
		return false, err
	}
	return attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0, nil
}

func createReparse(link, absTarget string) error {
	if err := os.MkdirAll(link, 0o755); err != nil {
		return err
	}
	handle, err := openReparseHandle(link, windows.GENERIC_WRITE)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	substitute := `\??\` + absTarget
	printName := absTarget
	subWords := append(utf16.Encode([]rune(substitute)), 0)
	printWords := append(utf16.Encode([]rune(printName)), 0)
	subLen := (len(subWords) - 1) * 2
	printLen := (len(printWords) - 1) * 2
	var buf reparseDataBuffer
	buf.Tag = ioReparseTagMountPoint
	off := 0
	putU16 := func(v uint16) {
		*(*uint16)(unsafe.Pointer(&buf.Data[off])) = v
		off += 2
	}
	putU16(0)
	putU16(uint16(subLen))
	putU16(uint16(subLen + 2))
	putU16(uint16(printLen))
	for _, w := range subWords {
		*(*uint16)(unsafe.Pointer(&buf.Data[off])) = w
		off += 2
	}
	for _, w := range printWords {
		*(*uint16)(unsafe.Pointer(&buf.Data[off])) = w
		off += 2
	}
	buf.DataLength = uint16(off)
	var returned uint32
	if err := windows.DeviceIoControl(handle, fsctlSetReparsePoint,
		(*byte)(unsafe.Pointer(&buf)), uint32(8+off),
		nil, 0, &returned, nil); err != nil {
		return fmt.Errorf("set reparse point %q: %w", link, err)
	}
	// Classic marks the junction read-only (attrib +R /L).
	p16, err := windows.UTF16PtrFromString(link)
	if err == nil {
		attrs, err := windows.GetFileAttributes(p16)
		if err == nil {
			_ = windows.SetFileAttributes(p16, attrs|windows.FILE_ATTRIBUTE_READONLY)
		}
	}
	_ = filepath.Clean
	return nil
}

func readReparse(link string) (string, error) {
	handle, err := openReparseHandle(link, windows.GENERIC_READ)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	var buf [maxReparseSize]byte
	var returned uint32
	if err := windows.DeviceIoControl(handle, fsctlGetReparsePoint,
		nil, 0, &buf[0], uint32(len(buf)), &returned, nil); err != nil {
		return "", err
	}
	tag := *(*uint32)(unsafe.Pointer(&buf[0]))
	if tag != ioReparseTagMountPoint {
		return "", fmt.Errorf("unexpected reparse tag %#x", tag)
	}
	subOff := *(*uint16)(unsafe.Pointer(&buf[8]))
	subLen := *(*uint16)(unsafe.Pointer(&buf[10]))
	base := 16
	raw := buf[base+int(subOff) : base+int(subOff)+int(subLen)]
	w := make([]uint16, len(raw)/2)
	for i := range w {
		w[i] = *(*uint16)(unsafe.Pointer(&raw[i*2]))
	}
	return strings.TrimPrefix(windows.UTF16ToString(w), `\??\`), nil
}

func deleteReparse(link string) error {
	// Classic clears read-only first (attrib -R /L in unlink_current).
	if p16, err := windows.UTF16PtrFromString(link); err == nil {
		if attrs, err := windows.GetFileAttributes(p16); err == nil {
			_ = windows.SetFileAttributes(p16, attrs & ^uint32(windows.FILE_ATTRIBUTE_READONLY))
		}
	}
	handle, err := openReparseHandle(link, windows.GENERIC_WRITE)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	var buf [8]byte
	*(*uint32)(unsafe.Pointer(&buf[0])) = ioReparseTagMountPoint
	var returned uint32
	if err := windows.DeviceIoControl(handle, fsctlDeleteReparsePoint,
		&buf[0], uint32(len(buf)), nil, 0, &returned, nil); err != nil {
		return err
	}
	_ = windows.CloseHandle
	if err := os.Remove(link); err != nil {
		return err
	}
	return nil
}
