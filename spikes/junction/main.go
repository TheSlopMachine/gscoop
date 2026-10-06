//go:build windows

// Command junction-spike verifies NTFS junction create, read, and delete
// using golang.org/x/sys/windows DeviceIoControl with
// FSCTL_SET_REPARSE_POINT, FSCTL_GET_REPARSE_POINT, and
// FSCTL_DELETE_REPARSE_POINT. These calls implement the same Win32
// primitives used for the apps\<app>\current links in the state contract.
//
// Exit code is 0 when every check passes, 1 otherwise.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var failures int

func report(name string, ok bool, detail string) {
	status := "PASS"
	if !ok {
		status = "FAIL"
		failures++
	}
	fmt.Printf("%s %s %s\n", status, name, detail)
}

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

func createJunction(link, target string) error {
	if err := os.MkdirAll(link, 0o755); err != nil {
		return err
	}
	handle, err := openReparseHandle(link, windows.GENERIC_WRITE)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)

	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	substitute := `\??\` + abs
	printName := abs
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
	putU16(0)                  // SubstituteNameOffset
	putU16(uint16(subLen))     // SubstituteNameLength
	putU16(uint16(subLen + 2)) // PrintNameOffset
	putU16(uint16(printLen))   // PrintNameLength
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
	return windows.DeviceIoControl(handle, fsctlSetReparsePoint,
		(*byte)(unsafe.Pointer(&buf)), uint32(8+off),
		nil, 0, &returned, nil)
}

func readJunction(link string) (string, error) {
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
	target := windows.UTF16ToString(w)
	return strings.TrimPrefix(target, `\??\`), nil
}

func deleteJunction(link string) error {
	handle, err := openReparseHandle(link, windows.GENERIC_WRITE)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)

	var buf [8]byte
	*(*uint32)(unsafe.Pointer(&buf[0])) = ioReparseTagMountPoint
	var returned uint32
	return windows.DeviceIoControl(handle, fsctlDeleteReparsePoint,
		&buf[0], uint32(len(buf)), nil, 0, &returned, nil)
}

func isReparsePoint(path string) bool {
	p16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(p16)
	if err != nil {
		return false
	}
	return attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func main() {
	root, err := os.MkdirTemp("", "junction-spike-")
	if err != nil {
		fmt.Printf("FAIL setup temp: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(root)

	target := filepath.Join(root, "version-1.0.0")
	link := filepath.Join(root, "current")
	if err := os.MkdirAll(target, 0o755); err != nil {
		fmt.Printf("FAIL setup target: %v\n", err)
		os.Exit(1)
	}
	sentinel := "junction payload\n"
	if err := os.WriteFile(filepath.Join(target, "sentinel.txt"), []byte(sentinel), 0o644); err != nil {
		fmt.Printf("FAIL setup sentinel: %v\n", err)
		os.Exit(1)
	}

	if err := createJunction(link, target); err != nil {
		report("junction-create", false, err.Error())
		os.Exit(1)
	}
	report("junction-create", true, "link="+link)

	report("junction-attr-reparse", isReparsePoint(link), "GetFileAttributes check")

	got, err := readJunction(link)
	if err != nil {
		report("junction-read", false, err.Error())
	} else {
		want, _ := filepath.Abs(target)
		report("junction-read", filepath.Clean(got) == filepath.Clean(want), fmt.Sprintf("want=%s got=%s", want, got))
	}

	through, err := os.ReadFile(filepath.Join(link, "sentinel.txt"))
	if err != nil {
		report("junction-traverse", false, err.Error())
	} else {
		report("junction-traverse", string(through) == sentinel, "sentinel readable through link")
	}

	if err := deleteJunction(link); err != nil {
		report("junction-delete", false, err.Error())
		os.Exit(1)
	}
	if err := os.Remove(link); err != nil {
		report("junction-delete", false, "reparse removed but dir entry remains: "+err.Error())
		os.Exit(1)
	}
	_, statErr := os.Lstat(link)
	report("junction-delete", os.IsNotExist(statErr), "link entry removed")

	_, targetErr := os.Stat(filepath.Join(target, "sentinel.txt"))
	report("junction-target-preserved", targetErr == nil, "target intact after link removal")

	if failures > 0 {
		os.Exit(1)
	}
}
