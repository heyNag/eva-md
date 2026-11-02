package release

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Base is the address of eva-md's repository on GitHub. Its releases are at
// Base/releases: Base/releases/latest redirects to the newest one's tag page,
// and each asset is at Base/releases/download/<tag>/<name>. Tests point it
// at a local test server.
var Base = "https://github.com/heyNag/eva-md"

// LookupTimeout bounds the request for the latest release's tag, and
// DownloadTimeout the download of one asset. Tests raise them before any
// test runs.
var (
	LookupTimeout   = 30 * time.Second
	DownloadTimeout = 5 * time.Minute
)

// The size limits of what a download may be: a binary, and SHA256SUMS.
const (
	MaxBinary = 128 << 20
	MaxSums   = 64 << 10
)

// SumsName is the checksum file of every release.
const SumsName = "SHA256SUMS"

// Asset is the name of the binary for a Mac of the given architecture, as
// Go names it: eva-md-darwin-arm64 or eva-md-darwin-amd64.
func Asset(arch string) string { return "eva-md-darwin-" + arch }

// Latest returns the newest release's tag, read from where Base's
// releases/latest page redirects, without following the redirect.
func Latest(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, LookupTimeout)
	defer cancel()
	base, err := url.Parse(Base)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, Base+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("couldn't reach GitHub to find the latest release")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 300 || resp.StatusCode > 399 {
		return "", fmt.Errorf("GitHub answered %s when asked for the latest release", resp.Status)
	}
	to, err := resp.Location()
	if err != nil {
		return "", errors.New("GitHub's answer for the latest release names no release")
	}
	tag, ok := strings.CutPrefix(to.Path, base.Path+"/releases/tag/")
	if !ok || to.Scheme != base.Scheme || to.Host != base.Host || !ValidTag(tag) {
		return "", errors.New("GitHub's answer for the latest release names no release tag")
	}
	return tag, nil
}

// Download saves the asset name of the release tag at file, a new file,
// reading at most limit bytes. It follows GitHub's redirect to its asset
// host, over HTTPS only (see allowed).
func Download(ctx context.Context, tag, name, file string, limit int64) (err error) {
	ctx, cancel := context.WithTimeout(ctx, DownloadTimeout)
	defer cancel()
	base, err := url.Parse(Base)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, Base+"/releases/download/"+tag+"/"+name, nil)
	if err != nil {
		return err
	}
	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if !allowed(base, r.URL) {
			return fmt.Errorf("a redirect to %s, which isn't GitHub", r.URL.Host)
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't download %s %s: %v", name, tag, plainHTTP(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("couldn't download %s %s: GitHub answered %s", name, tag, resp.Status)
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()
	n, err := io.Copy(f, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return fmt.Errorf("couldn't download %s %s: %v", name, tag, plainHTTP(err))
	}
	if n > limit {
		return fmt.Errorf("%s of %s is larger than eva-md takes", name, tag)
	}
	return nil
}

// allowed reports whether a redirect may go to u: to Base's own host, or,
// over HTTPS, to GitHub's asset host, where github.com sends release
// downloads.
func allowed(base, u *url.URL) bool {
	if u.Scheme == base.Scheme && u.Host == base.Host {
		return true
	}
	return u.Scheme == "https" && u.Port() == "" && strings.HasSuffix(u.Hostname(), ".githubusercontent.com")
}

// plainHTTP shortens a client error to its cause, without the address.
func plainHTTP(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		if errors.Is(ue.Err, context.DeadlineExceeded) {
			return errors.New("it took too long")
		}
		return ue.Err
	}
	return err
}

// Verify checks file against its line in sums, a SHA256SUMS file in the
// format shasum -a 256 writes: "<64 hex digits>  <name>". A missing line,
// or a sum that differs, is an error.
func Verify(sums []byte, name, file string) error {
	want := ""
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		sum, rest, ok := strings.Cut(strings.TrimRight(sc.Text(), "\r"), " ")
		rest = strings.TrimPrefix(rest, " ")
		rest = strings.TrimPrefix(rest, "*")
		if ok && rest == name && len(sum) == 64 {
			if _, err := hex.DecodeString(sum); err == nil {
				want = strings.ToLower(sum)
				break
			}
		}
	}
	if want == "" {
		return fmt.Errorf("%s has no line for %s", SumsName, name)
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		return fmt.Errorf("the checksum of %s doesn't match %s", name, SumsName)
	}
	return nil
}
