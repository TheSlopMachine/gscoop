// Fetch engine: cache download with Range resume and segmented fetch.
//
// Classic restarts every download from zero into $cached.download and
// renames it into place (lib/download.ps1:57-60). Go resumes a partial
// .download file with Range and optionally splits the file across
// SPLIT_DOWNLOADS connections; the cache contract is unchanged.
// Progress reports through the ui.Progress line on TTY only.
package download

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gscoop/internal/ui"
)

// progressInterval throttles live progress lines, mirroring the 100ms
// stopwatch gate (lib/download.ps1:177-180).
const progressInterval = 100 * time.Millisecond

// fetchToCache downloads resolvedURL into the cache path through a
// .download staging file, then renames it into place.
func (d *Downloader) fetchToCache(ctx context.Context, cached, resolvedURL string, req Request, index, total int) (int64, bool, error) {
	staging := cached + ".download"
	split := d.opts.SplitDownloads
	if split > 1 {
		if bytes, ok, err := d.fetchSegmented(ctx, staging, resolvedURL, req, split, index, total); err == nil && ok {
			if err := renameOrCopy(staging, cached); err != nil {
				return 0, false, err
			}
			return bytes, false, nil
		} else if err != nil {
			return 0, false, err
		}
	}
	bytes, resumed, err := d.fetchSingle(ctx, staging, resolvedURL, req, index, total)
	if err != nil {
		return 0, false, err
	}
	if err := renameOrCopy(staging, cached); err != nil {
		return 0, false, err
	}
	return bytes, resumed, nil
}

// fetchSingle streams one connection, resuming a partial staging file.
func (d *Downloader) fetchSingle(ctx context.Context, staging, rawurl string, req Request, index, total int) (int64, bool, error) {
	var start int64
	if info, err := os.Stat(staging); err == nil && !info.IsDir() && info.Size() > 0 {
		start = info.Size()
	}
	decorate := func(r *http.Request) {
		if start > 0 {
			r.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
		}
	}
	resp, _, err := d.doManual(ctx, rawurl, decorate, req.Cookies)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		d.warn("Token might be misconfigured.")
	}
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		os.Remove(staging)
		start = 0
		return d.fetchSingle(ctx, staging, rawurl, req, index, total)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return 0, false, fmt.Errorf("download of %s failed: %s", WireURL(rawurl), resp.Status)
	}
	resumed := start > 0 && resp.StatusCode == http.StatusPartialContent
	flags := os.O_CREATE | os.O_WRONLY
	if resumed {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
		start = 0
	}
	out, err := os.OpenFile(staging, flags, 0o644)
	if err != nil {
		return 0, false, err
	}
	length := resp.ContentLength
	remote := URLRemoteFilename(req.URL)
	d.announce(remote, length, start)
	written, err := d.streamBody(out, resp.Body, remote, start, length, index, total)
	closeErr := out.Close()
	if err != nil {
		return 0, false, err
	}
	if closeErr != nil {
		return 0, false, closeErr
	}
	return written, resumed, nil
}

// fetchSegmented probes Range support, then fans the file out across n
// connections into part files and assembles them. It reports false when
// the server ignores ranges so the caller falls back to a single fetch.
func (d *Downloader) fetchSegmented(ctx context.Context, staging, rawurl string, req Request, n, index, total int) (int64, bool, error) {
	probe, _, err := d.doManual(ctx, rawurl, func(r *http.Request) {
		r.Header.Set("Range", "bytes=0-0")
	}, req.Cookies)
	if err != nil {
		return 0, false, err
	}
	defer probe.Body.Close()
	if probe.StatusCode != http.StatusPartialContent {
		io.Copy(io.Discard, io.LimitReader(probe.Body, 1<<20))
		return 0, false, nil
	}
	size, ok := parseContentRangeTotal(probe.Header.Get("Content-Range"))
	if !ok || size <= 0 {
		io.Copy(io.Discard, io.LimitReader(probe.Body, 1<<20))
		return 0, false, nil
	}
	io.Copy(io.Discard, io.LimitReader(probe.Body, 1<<20))
	remote := URLRemoteFilename(req.URL)
	d.announce(remote, size, 0)
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf("%s.part%d", staging, i)
	}
	stride := size / int64(n)
	var received atomic.Int64
	var failed atomic.Value
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		from := int64(i) * stride
		to := size - 1
		if i+1 < n {
			to = int64(i+1)*stride - 1
		}
		wg.Add(1)
		go func(part int, from, to int64) {
			defer wg.Done()
			if failed.Load() != nil {
				return
			}
			if err := d.fetchRange(ctx, rawurl, req, parts[part], from, to, &received, remote, size, index, total); err != nil {
				if failed.CompareAndSwap(nil, err) {
					_ = err
				}
			}
		}(i, from, to)
	}
	wg.Wait()
	if v := failed.Load(); v != nil {
		for _, p := range parts {
			os.Remove(p)
		}
		return 0, false, v.(error)
	}
	out, err := os.OpenFile(staging, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, false, err
	}
	for _, p := range parts {
		if err := appendFile(out, p); err != nil {
			out.Close()
			return 0, false, err
		}
		os.Remove(p)
	}
	if err := out.Close(); err != nil {
		return 0, false, err
	}
	d.progressDone()
	return size, false, nil
}

// fetchRange downloads one byte range into a part file.
func (d *Downloader) fetchRange(ctx context.Context, rawurl string, req Request, part string, from, to int64, received *atomic.Int64, remote string, size int64, index, total int) error {
	resp, _, err := d.doManual(ctx, rawurl, func(r *http.Request) {
		r.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", from, to))
	}, req.Cookies)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("segment download of %s failed: %s", WireURL(rawurl), resp.Status)
	}
	out, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	buf := make([]byte, 32*1024)
	last := time.Now()
	for {
		nr, er := resp.Body.Read(buf)
		if nr > 0 {
			if _, ew := out.Write(buf[:nr]); ew != nil {
				out.Close()
				return ew
			}
			now := time.Now()
			done := received.Add(int64(nr))
			if now.Sub(last) >= progressInterval {
				last = now
				d.updateProgress(index, total, remote, done, size)
			}
		}
		if er != nil {
			out.Close()
			if er == io.EOF {
				return nil
			}
			return er
		}
	}
}

// streamBody copies a response body with throttled progress.
func (d *Downloader) streamBody(out *os.File, body io.Reader, remote string, start, length int64, index, total int) (int64, error) {
	buf := make([]byte, 32*1024)
	written := start
	last := time.Now()
	d.updateProgress(index, total, remote, written, length)
	for {
		nr, er := body.Read(buf)
		if nr > 0 {
			if _, ew := out.Write(buf[:nr]); ew != nil {
				return written, ew
			}
			written += int64(nr)
			now := time.Now()
			if now.Sub(last) >= progressInterval {
				last = now
				d.updateProgress(index, total, remote, written, length)
			}
		}
		if er != nil {
			d.updateProgress(index, total, remote, written, length)
			d.progressDone()
			if er == io.EOF {
				return written, nil
			}
			return written, er
		}
	}
}

// announce prints the download start line for non-TTY output
// (lib/download.ps1:160).
func (d *Downloader) announce(remote string, length, start int64) {
	if d.opts.Progress != nil && d.opts.Progress.Enabled {
		return
	}
	if length >= 0 {
		d.infof("Downloading %s (%s)...", remote, ui.FormatSize(length))
	} else {
		d.infof("Downloading %s...", remote)
	}
	_ = start
}

// updateProgress renders one live line on TTY only.
func (d *Downloader) updateProgress(index, total int, remote string, done, size int64) {
	prog := d.opts.Progress
	if prog == nil || !prog.Enabled {
		return
	}
	var line string
	if size >= 0 {
		line = fmt.Sprintf("DL %d/%d  %s  %s/%s", index+1, total, remote, ui.FormatSize(done), ui.FormatSize(size))
	} else {
		line = fmt.Sprintf("DL %d/%d  %s  %s", index+1, total, remote, ui.FormatSize(done))
	}
	prog.Update(line)
}

func (d *Downloader) progressDone() {
	if d.opts.Progress != nil {
		d.opts.Progress.Done()
	}
}

// parseContentRangeTotal reads the total from "bytes 0-0/1234".
func parseContentRangeTotal(header string) (int64, bool) {
	slash := strings.LastIndex(header, "/")
	if slash < 0 {
		return 0, false
	}
	total, err := strconv.ParseInt(strings.TrimSpace(header[slash+1:]), 10, 64)
	if err != nil || total < 0 {
		return 0, false
	}
	return total, true
}

// appendFile copies a part file into the assembly stream.
func appendFile(out *os.File, part string) error {
	in, err := os.Open(part)
	if err != nil {
		return err
	}
	defer in.Close()
	_, err = io.Copy(out, in)
	return err
}

// copyFile copies src to dst, creating parents.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// renameOrCopy renames src to dst, falling back to copy+remove across
// volumes (Move-Item -Force semantics, lib/download.ps1:60).
func renameOrCopy(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
