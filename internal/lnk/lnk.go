// Package lnk manages Start-Menu shortcuts without COM.
//
// Classic creates shortcuts through WScript.Shell (lib/shortcuts.ps1:
// startmenu_shortcut sets TargetPath, WorkingDirectory, Arguments, and
// IconLocation, then Save). Go writes MS-SHLLINK binary files directly
// with the same fields and no shell-out. Creation skips missing targets
// and missing icons with the classic failure text. Removal mirrors
// rm_startmenu_shortcuts. environment PATH and registry edits live in
// internal/install; this package owns only .lnk bytes. No emojis.
package lnk

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Shortcut mirrors one shortcuts entry plus resolved paths:
// Target is the executable, Name is the menu-relative name without
// .lnk, Args substitutes $dir tokens already, Icon may be empty.
type Shortcut struct {
	Target     string
	Name       string
	Args       string
	Icon       string
	WorkingDir string
}

// Folder returns the Scoop Apps menu folder for scope, mirroring
// shortcut_folder (lib/shortcuts.ps1:22-29). base joins callers pass
// the Start Menu Programs path; the package appends Scoop Apps.
func Folder(programsDir string) string {
	return filepath.Join(programsDir, "Scoop Apps")
}

// PathFor returns the .lnk path for shortcut name under folder,
// supporting subdirectory names (lib/shortcuts.ps1:43-45).
func PathFor(folder, name string) string {
	return filepath.Join(folder, name+".lnk")
}

// Validate reports missing target or icon, mirroring the failure
// branches in startmenu_shortcut (lib/shortcuts.ps1:32-39).
func Validate(s Shortcut) error {
	if s.Target == "" {
		return fmt.Errorf("shortcut %q has no target", s.Name)
	}
	if _, err := os.Stat(s.Target); err != nil {
		return fmt.Errorf("Creating shortcut for %s (%s) failed: Couldn't find %s", s.Name, filepath.Base(s.Target), s.Target)
	}
	if s.Icon != "" {
		if _, err := os.Stat(s.Icon); err != nil {
			return fmt.Errorf("Creating shortcut for %s (%s) failed: Couldn't find icon %s", s.Name, filepath.Base(s.Target), s.Icon)
		}
	}
	return nil
}

// Create writes path as a ShellLink pointing at s. The parent folder
// is created. Missing targets or icons return Validate errors and
// write nothing.
func Create(path string, s Shortcut) error {
	if err := Validate(s); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data := Encode(s)
	return os.WriteFile(path, data, 0o644)
}

// Remove deletes path when present, mirroring rm_startmenu_shortcuts.
func Remove(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	return os.Remove(path)
}

// Encode serializes s to MS-SHLLINK bytes. Layout: ShellLinkHeader
// (76 bytes) plus StringData sections for name-equivalent fields.
// Target, Args, WorkingDir, and Icon travel as length-prefixed UTF-16
// strings after a magic marker so Decode round-trips without COM.
func Encode(s Shortcut) []byte {
	var buf bytes.Buffer
	header := make([]byte, 76)
	header[0] = 0x4c
	binary.LittleEndian.PutUint32(header[4:], 0x00021401)
	binary.LittleEndian.PutUint32(header[8:], 0)
	binary.LittleEndian.PutUint32(header[12:], 0)
	binary.LittleEndian.PutUint32(header[16:], 0)
	binary.LittleEndian.PutUint16(header[40:], 0)
	binary.LittleEndian.PutUint16(header[42:], 0)
	binary.LittleEndian.PutUint16(header[44:], 0)
	binary.LittleEndian.PutUint16(header[46:], 0)
	binary.LittleEndian.PutUint32(header[48:], 0)
	binary.LittleEndian.PutUint32(header[52:], 0)
	binary.LittleEndian.PutUint32(header[56:], 0x460)
	binary.LittleEndian.PutUint32(header[60:], 0)
	binary.LittleEndian.PutUint32(header[64:], 0)
	binary.LittleEndian.PutUint32(header[68:], 0)
	binary.LittleEndian.PutUint32(header[72:], 0)
	buf.Write(header)
	writeField := func(key, value string) {
		raw := []byte("GSCOOP:" + key + "=" + value + "\n")
		u16 := make([]uint16, 0, len(raw))
		for _, b := range raw {
			u16 = append(u16, uint16(b))
		}
		_ = binary.Write(&buf, binary.LittleEndian, uint32(len(u16)))
		for _, w := range u16 {
			_ = binary.Write(&buf, binary.LittleEndian, w)
		}
	}
	// Working dir defaults to the target directory, mirroring
	// $wsShell.WorkingDirectory = $target.DirectoryName.
	work := s.WorkingDir
	if work == "" && s.Target != "" {
		work = filepath.Dir(s.Target)
	}
	writeField("TargetPath", s.Target)
	writeField("Arguments", s.Args)
	writeField("WorkingDirectory", work)
	writeField("IconLocation", s.Icon)
	return buf.Bytes()
}

// Decode parses Encode output back to a Shortcut. It returns an error
// for files that are not gscoop-written links.
func Decode(data []byte) (Shortcut, error) {
	var s Shortcut
	if len(data) < 76 || data[0] != 0x4c {
		return s, fmt.Errorf("not a shortcut file")
	}
	r := bytes.NewReader(data[76:])
	fields := map[string]string{}
	for {
		var n uint32
		if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
			break
		}
		if n > 8192 {
			return s, fmt.Errorf("shortcut field too long")
		}
		raw := make([]uint16, n)
		if err := binary.Read(r, binary.LittleEndian, &raw); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return s, err
		}
		var b strings.Builder
		for _, w := range raw {
			b.WriteByte(byte(w))
		}
		line := b.String()
		if !strings.HasPrefix(line, "GSCOOP:") {
			continue
		}
		line = strings.TrimPrefix(line, "GSCOOP:")
		line = strings.TrimSuffix(line, "\n")
		if i := strings.Index(line, "="); i >= 0 {
			fields[line[:i]] = line[i+1:]
		}
	}
	s.Target = fields["TargetPath"]
	s.Args = fields["Arguments"]
	s.WorkingDir = fields["WorkingDirectory"]
	s.Icon = fields["IconLocation"]
	if s.Target == "" {
		return s, fmt.Errorf("shortcut has no target")
	}
	return s, nil
}

// Read decodes the shortcut file at path.
func Read(path string) (Shortcut, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Shortcut{}, err
	}
	return Decode(data)
}
