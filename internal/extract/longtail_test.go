// Long-tail fixup tests (phase 3B): RAR decode through rardecode with
// runtime-synthesized fixtures, MSI CFB inspection, Inno arg vectors,
// and external-7z failure hints. No binaries are checked in; every
// fixture is generated in memory during the test.
package extract

import (
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// putU32LE appends a little-endian uint32 to b.
func putU32LE(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

// putU16LE appends a little-endian uint16 to b.
func putU16LE(b []byte, v uint16) []byte {
	return append(b, byte(v), byte(v>>8))
}

// rarUVarint encodes a RAR5 variable-length integer.
func rarUVarint(v uint64) []byte {
	var out []byte
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v == 0 {
			return append(out, c)
		}
		out = append(out, c|0x80)
	}
}

// rar15Block builds one RAR4 block. The header CRC is the low 16 bits
// of CRC32-IEEE over the bytes after the CRC field, which is what
// the rardecode v15 reader verifies.
func rar15Block(blockType byte, flags uint16, data []byte) []byte {
	size := 7 + len(data)
	hdr := []byte{blockType, byte(flags), byte(flags >> 8), byte(size), byte(size >> 8)}
	hdr = append(hdr, data...)
	sum := crc32.ChecksumIEEE(hdr)
	out := []byte{byte(sum), byte(sum >> 8)}
	return append(out, hdr...)
}

// rarDosTime packs a fixed DOS timestamp (2024-06-07 08:09:10).
func rarDosTime() uint32 {
	return uint32(10/2) | uint32(9)<<5 | uint32(8)<<11 | uint32(7)<<16 | uint32(6)<<21 | uint32(2024-1980)<<25
}

// rar15FileData builds the data segment of a RAR4 stored file header.
// full is the whole file content (for size and CRC); part is the byte
// range stored in this block (split volumes only).
func rar15FileData(name string, full, part []byte, blockOffset int) []byte {
	var data []byte
	data = putU32LE(data, uint32(len(part)))
	data = putU32LE(data, uint32(len(full)))
	data = append(data, 2) // raw host OS: stored value + 1 = Windows
	data = putU32LE(data, crc32.ChecksumIEEE(full))
	data = putU32LE(data, rarDosTime())
	data = append(data, 20, 0x30) // unpack version 2.0, method stored
	data = putU16LE(data, uint16(len(name)))
	data = putU32LE(data, 0x20)
	data = append(data, name...)
	_ = blockOffset
	return data
}

// buildRar15 synthesizes a single-volume RAR4 archive with stored
// entries, in deterministic name order.
func buildRar15(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	arc := []byte("Rar!\x1a\x07\x00")
	arc = append(arc, rar15Block(0x73, 0, nil)...)
	for _, name := range names {
		content := files[name]
		arc = append(arc, rar15Block(0x74, 0x8000, rar15FileData(name, content, content, 0))...)
		arc = append(arc, content...)
	}
	return append(arc, rar15Block(0x7b, 0, nil)...)
}

// buildRar15Split synthesizes a two-volume RAR4 set holding one stored
// file cut at cut bytes: vol1 carries the split-after flag and a
// not-last end block, vol2 the split-before continuation.
func buildRar15Split(t *testing.T, name string, content []byte, cut int) (vol1, vol2 []byte) {
	t.Helper()
	vol1 = []byte("Rar!\x1a\x07\x00")
	vol1 = append(vol1, rar15Block(0x73, 0x0001|0x0010, nil)...)
	vol1 = append(vol1, rar15Block(0x74, 0x8000|0x0002, rar15FileData(name, content, content[:cut], 0))...)
	vol1 = append(vol1, content[:cut]...)
	vol1 = append(vol1, rar15Block(0x7b, 0x0001, nil)...)
	vol2 = []byte("Rar!\x1a\x07\x00")
	vol2 = append(vol2, rar15Block(0x73, 0x0001|0x0010, nil)...)
	vol2 = append(vol2, rar15Block(0x74, 0x8000|0x0001, rar15FileData(name, content, content[cut:], cut))...)
	vol2 = append(vol2, content[cut:]...)
	return append(vol2, rar15Block(0x7b, 0, nil)...), vol1
}

// rar5Block builds one RAR5 block: CRC32 over everything after the
// CRC field. The header size counts only the bytes after the size
// field itself, which is what the rardecode v50 reader consumes.
func rar5Block(blockType, blockFlags uint64, dataSize uint64, headerData []byte) []byte {
	var body []byte
	body = append(body, rarUVarint(blockType)...)
	body = append(body, rarUVarint(blockFlags)...)
	if blockFlags&0x02 != 0 {
		body = append(body, rarUVarint(dataSize)...)
	}
	body = append(body, headerData...)
	if len(body) >= 128 {
		panic("synthetic RAR5 header too large")
	}
	full := append([]byte{byte(len(body))}, body...)
	sum := crc32.ChecksumIEEE(full)
	out := []byte{byte(sum), byte(sum >> 8), byte(sum >> 16), byte(sum >> 24)}
	return append(out, full...)
}

// buildRar5 synthesizes a single-volume RAR5 archive with stored
// entries, in deterministic name order.
func buildRar5(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	arc := []byte("Rar!\x1a\x07\x01\x00")
	arc = append(arc, rar5Block(1, 0, 0, nil)...)
	for _, name := range names {
		content := files[name]
		var headerData []byte
		headerData = append(headerData, rarUVarint(0)...) // file flags
		headerData = append(headerData, rarUVarint(uint64(len(content)))...)
		headerData = append(headerData, rarUVarint(0x20)...) // attributes
		headerData = append(headerData, rarUVarint(0)...)    // compression info: stored
		headerData = append(headerData, rarUVarint(0)...)    // host OS: Windows
		headerData = append(headerData, rarUVarint(uint64(len(name)))...)
		headerData = append(headerData, name...)
		arc = append(arc, rar5Block(2, 0x02, uint64(len(content)), headerData)...)
		arc = append(arc, content...)
	}
	return append(arc, rar5Block(5, 0, 0, nil)...)
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExtractRarStored15(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{
		"hello.txt":       []byte("rar4 stored fixture\n"),
		"nested/data.txt": []byte("deterministic rar4 content 0123456789\n"),
	}
	archive := filepath.Join(dir, "a.rar")
	writeFile(t, archive, buildRar15(t, files))
	dest := filepath.Join(dir, "out")
	done, err := Extract(archive, dest, Options{})
	if err != nil || !done {
		t.Fatalf("Extract = %v, %v", done, err)
	}
	for name, content := range files {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(name)))
		if err != nil || string(got) != string(content) {
			t.Errorf("%s = %q, %v", name, got, err)
		}
	}
}

func TestExtractRarStored15ExtractDir(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.rar")
	writeFile(t, archive, buildRar15(t, map[string][]byte{"pkg/bin/app.exe": []byte("x")}))
	dest := filepath.Join(dir, "out")
	done, err := Extract(archive, dest, Options{ExtractDir: "pkg"})
	if err != nil || !done {
		t.Fatalf("Extract = %v, %v", done, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "app.exe")); err != nil {
		t.Errorf("scoped file missing: %v", err)
	}
}

func TestExtractRarStored50(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{
		"hello.txt":       []byte("rar5 stored fixture\n"),
		"nested/data.txt": []byte("deterministic rar5 content 0123456789\n"),
	}
	archive := filepath.Join(dir, "a.rar")
	writeFile(t, archive, buildRar5(t, files))
	dest := filepath.Join(dir, "out")
	done, err := Extract(archive, dest, Options{})
	if err != nil || !done {
		t.Fatalf("Extract = %v, %v", done, err)
	}
	for name, content := range files {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(name)))
		if err != nil || string(got) != string(content) {
			t.Errorf("%s = %q, %v", name, got, err)
		}
	}
}

func TestExtractRarSplit15(t *testing.T) {
	dir := t.TempDir()
	content := []byte("split rar4 payload spanning two volumes 0123456789\n")
	vol2, vol1 := buildRar15Split(t, "payload.txt", content, 17)
	writeFile(t, filepath.Join(dir, "t.part1.rar"), vol1)
	writeFile(t, filepath.Join(dir, "t.part2.rar"), vol2)
	dest := filepath.Join(dir, "out")
	done, err := Extract(filepath.Join(dir, "t.part1.rar"), dest, Options{})
	if err != nil || !done {
		t.Fatalf("Extract = %v, %v", done, err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "payload.txt"))
	if err != nil || string(got) != string(content) {
		t.Errorf("payload = %q, %v", got, err)
	}
}

func TestExtractRarRejectsSlip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "evil.rar")
	writeFile(t, archive, buildRar15(t, map[string][]byte{"../evil.txt": []byte("x")}))
	if _, err := Extract(archive, filepath.Join(dir, "out"), Options{}); err == nil {
		t.Error("rar-slip entry must fail")
	}
}

func TestExtractRarNoDependencyError(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.rar")
	writeFile(t, archive, []byte("not a rar archive"))
	_, err := Extract(archive, filepath.Join(dir, "out"), Options{})
	if err == nil {
		t.Fatal("garbage input must fail")
	}
	var need *NeedDependencyError
	if errors.As(err, &need) {
		t.Errorf("RAR must decode natively, not request a module: %v", err)
	}
}

// testDirEntry builds one 128-byte CFB directory entry.
func testDirEntry(name string, entryType byte, child int32, start uint32) []byte {
	e := make([]byte, 128)
	runes := []rune(name)
	for i, c := range runes {
		e[2*i] = byte(c)
		e[2*i+1] = byte(c >> 8)
	}
	n := (len(runes) + 1) * 2
	e[64] = byte(n)
	e[65] = byte(n >> 8)
	e[66] = entryType
	e[67] = 1
	for i, v := range []uint32{0xFFFFFFFF, 0xFFFFFFFF, uint32(child)} {
		off := 68 + 4*i
		e[off] = byte(v)
		e[off+1] = byte(v >> 8)
		e[off+2] = byte(v >> 16)
		e[off+3] = byte(v >> 24)
	}
	off := 116
	e[off] = byte(start)
	e[off+1] = byte(start >> 8)
	e[off+2] = byte(start >> 16)
	e[off+3] = byte(start >> 24)
	return e
}

// buildTestCFB synthesizes a minimal 512-byte-sector compound file:
// header, one FAT sector (sector 0), one directory sector (sector 1)
// holding the root plus zero-size stream entries. Sectors start after
// the header, so sector N lives at file offset (N+1)*512.
func buildTestCFB(streams []string) []byte {
	const sector = 512
	file := make([]byte, 3*sector)
	h := file[:sector]
	copy(h, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
	h[24] = 0x3E
	h[26] = 3
	h[28] = 0xFE
	h[29] = 0xFF
	h[30] = 9
	h[32] = 6
	h[44] = 1 // one FAT sector
	h[48] = 1 // directory chain starts at sector 1
	h[56] = 0x00
	h[57] = 0x10
	h[60] = 0xFE // no MiniFAT
	h[61] = 0xFF
	h[68] = 0xFE // no DIFAT chain
	h[69] = 0xFF
	h[76] = 0 // DIFAT[0] points at the FAT sector (sector 0)
	for i := 1; i < 109; i++ {
		base := 76 + 4*i
		h[base] = 0xFF
		h[base+1] = 0xFF
		h[base+2] = 0xFF
		h[base+3] = 0xFF
	}
	fat := file[sector : 2*sector]
	for i, v := range []uint32{0xFFFFFFFD, 0xFFFFFFFE} {
		base := 4 * i
		fat[base] = byte(v)
		fat[base+1] = byte(v >> 8)
		fat[base+2] = byte(v >> 16)
		fat[base+3] = byte(v >> 24)
	}
	for i := 2; i < 128; i++ {
		base := 4 * i
		fat[base] = 0xFF
		fat[base+1] = 0xFF
		fat[base+2] = 0xFF
		fat[base+3] = 0xFF
	}
	dir := file[2*sector : 3*sector]
	child := int32(-1)
	if len(streams) > 0 {
		child = 1
	}
	copy(dir, testDirEntry("Root Entry", 5, child, 0xFFFFFFFE))
	for i, name := range streams {
		copy(dir[(i+1)*128:], testDirEntry(name, 2, -1, 0xFFFFFFFE))
	}
	return file
}

func TestInspectMsiSynthetic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.msi")
	writeFile(t, path, buildTestCFB(nil))
	info, err := InspectMsi(path)
	if err != nil {
		t.Fatalf("InspectMsi = %v", err)
	}
	if len(info.Streams) != 0 || len(info.Cabinets) != 0 {
		t.Errorf("info = %+v", info)
	}
}

func TestInspectMsiCabinetStream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cab.msi")
	writeFile(t, path, buildTestCFB([]string{"driver.cab"}))
	info, err := InspectMsi(path)
	if err != nil {
		t.Fatalf("InspectMsi = %v", err)
	}
	if len(info.Streams) != 1 || info.Streams[0] != "driver.cab" {
		t.Errorf("streams = %v", info.Streams)
	}
	if len(info.Cabinets) != 1 || info.Cabinets[0] != "driver.cab" {
		t.Errorf("cabinets = %v", info.Cabinets)
	}
}

func TestInspectMsiRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.msi")
	writeFile(t, path, []byte("not a compound file"))
	if _, err := InspectMsi(path); err == nil {
		t.Error("garbage input must fail")
	}
	if _, err := InspectMsi(filepath.Join(dir, "missing.msi")); err == nil {
		t.Error("missing file must fail")
	}
}

func TestIsCabinetStream(t *testing.T) {
	for name, want := range map[string]bool{
		"driver.cab": true,
		"DRIVER.CAB": true,
		"data.cab":   true,
		"setup.exe":  false,
		"stream":     false,
		"cab":        false,
	} {
		if got := IsCabinetStream(name); got != want {
			t.Errorf("IsCabinetStream(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestInnoArgs(t *testing.T) {
	cases := []struct {
		extractDir string
		want       string
	}{
		{"", "-c{app}"},
		{"pkg", `-c{app}\pkg`},
		{`pkg\bin`, `-c{app}\pkg\bin`},
		{"{pf}", "-c{pf}"},
		{"{app}\\sub", "-c{app}\\sub"},
	}
	for _, c := range cases {
		args := InnoArgs("setup.exe", `C:\out`, c.extractDir)
		want := []string{"-x", `-dC:\out`, "setup.exe", "-y", c.want}
		if len(args) != len(want) {
			t.Fatalf("InnoArgs(%q) = %v", c.extractDir, args)
		}
		for i := range want {
			if args[i] != want[i] {
				t.Fatalf("InnoArgs(%q) = %v, want %v", c.extractDir, args, want)
			}
		}
	}
}

func TestInnoHelperOrder(t *testing.T) {
	if len(InnoHelperNames) != 2 || InnoHelperNames[0] != InnoHelperUnicode || InnoHelperNames[1] != InnoHelperName {
		t.Errorf("resolution order = %v", InnoHelperNames)
	}
	if InnoHelperUnicode != "innounp-unicode" || InnoHelperName != "innounp" {
		t.Errorf("helpers = %q, %q", InnoHelperUnicode, InnoHelperName)
	}
	err := InnoError("setup.exe")
	if err == nil || !strings.Contains(err.Error(), "innounp-unicode") || !strings.Contains(err.Error(), "innounp -x") {
		t.Errorf("guidance = %v", err)
	}
}

func TestExternalFailureHint(t *testing.T) {
	if got := externalFailureHint("a.7z.001"); !strings.Contains(got, "multi-volume") {
		t.Errorf("multi-volume hint = %q", got)
	}
	if got := externalFailureHint("setup.exe"); !strings.Contains(got, "NSIS") {
		t.Errorf("nsis hint = %q", got)
	}
	if got := externalFailureHint("a.zip"); got != "" {
		t.Errorf("plain hint = %q", got)
	}
}
