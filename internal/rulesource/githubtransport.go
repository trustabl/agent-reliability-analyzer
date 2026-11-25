package rulesource

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"testing/fstest"

	"github.com/trustabl/trustabl/internal/rulesign"
)

// Bundle/statement download caps. A statement is a tiny JSON document; a rules
// bundle is a few hundred KiB of YAML. The ceilings bound a hostile or
// misconfigured endpoint — an unbounded body or a decompression bomb — well
// above any real artifact.
// Vars (not consts) so a test can shrink them to exercise the over-cap branches
// without allocating hundreds of MiB. Production values are the real ceilings.
var (
	maxStatementBytes int64 = 1 << 20   // 1 MiB
	maxBundleBytes    int64 = 64 << 20  // 64 MiB compressed
	maxBundleUnpacked int64 = 256 << 20 // 256 MiB unpacked
	maxBundleEntries        = 100_000
)

// githubTransport fetches channel artifacts from GitHub Releases.
//
// Asset layout (the ENG-4 ⇄ RUL-4 contract — RUL-4's publish/promote workflows
// MUST produce exactly this so an engine build can resolve it):
//
//	channel statement: <repo>/releases/download/channel-<name>/statement.json
//	    A release tagged `channel-<name>` whose `statement.json` asset is the
//	    signed pointer. Promote re-points by updating this release's asset; the
//	    download URL stays stable.
//	bundle:            <repo>/releases/download/bundle-<digest>/bundle.tar.gz
//	    An immutable release tagged `bundle-<digest>` carrying the gzipped tar
//	    of the bundle. Content-addressed: the tag encodes the digest the
//	    statement commits to.
type githubTransport struct {
	client httpGetter
}

// httpGetter is the minimal HTTP surface the transport needs, narrowed so a
// test can substitute a fake without a real server. Do (not Get) so the API
// fallback can set its Accept header.
type httpGetter interface {
	Do(req *http.Request) (*http.Response, error)
}

func newGitHubTransport() *githubTransport {
	return &githubTransport{client: &http.Client{Timeout: networkTimeout}}
}

func (t *githubTransport) FetchStatement(repoURL, channel string) ([]byte, error) {
	return t.download(statementURL(repoURL, channel), maxStatementBytes)
}

func (t *githubTransport) FetchBundle(repoURL, digest string) (fs.FS, error) {
	raw, err := t.download(bundleURL(repoURL, digest), maxBundleBytes)
	if err != nil {
		return nil, err
	}
	return untarGz(raw)
}

// repoWebBase normalizes a rules-repo URL to its web base (no trailing slash,
// no ".git"), the prefix GitHub release-asset download URLs hang off.
func repoWebBase(repoURL string) string {
	return strings.TrimSuffix(strings.TrimSuffix(repoURL, "/"), ".git")
}

func statementURL(repoURL, channel string) string {
	return repoWebBase(repoURL) + "/releases/download/channel-" + channel + "/statement.json"
}

func bundleURL(repoURL, digest string) string {
	return repoWebBase(repoURL) + "/releases/download/bundle-" + digest + "/bundle.tar.gz"
}

// download fetches a release asset by its web download URL. When that route
// answers 5xx or fails in transit, it tries once more through the REST API
// asset endpoint, a separate route to the same blob: github.com has served an
// instant, path-keyed 504 for some /releases/download/ paths (our production
// statement and bundle among them) while the API served both. A 404 or any
// other 4xx is final. The route is not a trust input: the caller checks the
// statement's signature and the bundle's digest after download.
func (t *githubTransport) download(webURL string, maxBytes int64) ([]byte, error) {
	data, status, err := t.get(webURL, "", maxBytes)
	if err == nil || (status != 0 && status < 500) {
		return data, err
	}
	releaseURL, name, ok := apiReleaseURL(webURL)
	if !ok {
		return nil, err
	}
	data, ferr := t.apiAsset(releaseURL, name, maxBytes)
	if ferr != nil {
		return nil, fmt.Errorf("%w (API fallback: %v)", err, ferr)
	}
	return data, nil
}

// apiReleaseURL maps https://github.com/<owner>/<repo>/releases/download/<tag>/<name>
// to the REST release-by-tag URL plus the asset name; ok is false for any
// other host or shape.
func apiReleaseURL(webURL string) (releaseURL, name string, ok bool) {
	rest, ok := strings.CutPrefix(webURL, "https://github.com/")
	if !ok {
		return "", "", false
	}
	repo, asset, ok := strings.Cut(rest, "/releases/download/")
	if !ok {
		return "", "", false
	}
	tag, name, ok := strings.Cut(asset, "/")
	if !ok || repo == "" || tag == "" || name == "" {
		return "", "", false
	}
	return "https://api.github.com/repos/" + repo + "/releases/tags/" + tag, name, true
}

// apiAsset looks the release up through the REST API and downloads the named
// asset from its API URL (Accept: application/octet-stream redirects to the blob).
func (t *githubTransport) apiAsset(releaseURL, name string, maxBytes int64) ([]byte, error) {
	meta, _, err := t.get(releaseURL, "application/vnd.github+json", maxStatementBytes)
	if err != nil {
		return nil, err
	}
	var rel struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(meta, &rel); err != nil {
		return nil, fmt.Errorf("decode %s: %w", releaseURL, err)
	}
	for _, a := range rel.Assets {
		if a.Name == name && strings.HasPrefix(a.URL, "https://api.github.com/") {
			data, _, err := t.get(a.URL, "application/octet-stream", maxBytes)
			return data, err
		}
	}
	return nil, fmt.Errorf("%s: no asset %q", releaseURL, name)
}

// get fetches url, enforcing a 200 status and a hard byte ceiling so a hostile
// endpoint cannot stream an unbounded body into memory. status is 0 when no
// response arrived.
func (t *githubTransport) get(url, accept string, maxBytes int64) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("GET %s: %w", url, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, resp.StatusCode, fmt.Errorf("GET %s: response exceeds %d bytes", url, maxBytes)
	}
	return data, resp.StatusCode, nil
}

// untarGz decompresses and unpacks a gzipped tar into an in-memory filesystem,
// rejecting unsafe paths and bounding total size and entry count. The result is
// untrusted until releaseSource matches its CanonicalDigest to the statement.
func untarGz(raw []byte) (fs.FS, error) {
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decompress bundle: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	out := fstest.MapFS{}
	seenLower := map[string]bool{}
	var total int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read bundle tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue // skip dirs, symlinks, devices
		}
		name := path.Clean(hdr.Name)
		if !fs.ValidPath(name) || name == "." {
			return nil, fmt.Errorf("bundle contains unsafe path %q", hdr.Name)
		}
		// Reject names fs.ValidPath accepts but that reinterpret across platforms
		// (backslash, colon/drive-letter, trailing dot/space, reserved device name,
		// control char) — the SAME check the producer applies — so the on-disk
		// install can never diverge from the verified digest.
		if err := rulesign.ValidateBundlePath(name); err != nil {
			return nil, fmt.Errorf("bundle contains non-portable path %q: %w", hdr.Name, err)
		}
		lc := strings.ToLower(name)
		if seenLower[lc] {
			return nil, fmt.Errorf("bundle has case-folding-colliding paths (near %q) that would collapse to one file on a case-insensitive filesystem", hdr.Name)
		}
		seenLower[lc] = true
		if len(out) >= maxBundleEntries {
			return nil, fmt.Errorf("bundle exceeds %d entries", maxBundleEntries)
		}
		remaining := int64(maxBundleUnpacked) - total
		if remaining <= 0 {
			return nil, fmt.Errorf("bundle exceeds %d bytes unpacked", maxBundleUnpacked)
		}
		data, err := io.ReadAll(io.LimitReader(tr, remaining+1))
		if err != nil {
			return nil, fmt.Errorf("read bundle entry %q: %w", name, err)
		}
		if int64(len(data)) > remaining {
			return nil, fmt.Errorf("bundle exceeds %d bytes unpacked", maxBundleUnpacked)
		}
		total += int64(len(data))
		out[name] = &fstest.MapFile{Data: data}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("bundle is empty")
	}
	return out, nil
}
