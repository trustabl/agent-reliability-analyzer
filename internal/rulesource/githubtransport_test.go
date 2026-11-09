package rulesource

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestReleaseURLs(t *testing.T) {
	const repo = "https://github.com/trustabl/agent-reliability-rules"
	if got, want := repoWebBase(repo+".git"), repo; got != want {
		t.Errorf("repoWebBase trims .git: got %q want %q", got, want)
	}
	if got, want := repoWebBase(repo+"/"), repo; got != want {
		t.Errorf("repoWebBase trims slash: got %q want %q", got, want)
	}
	if got, want := statementURL(repo, "production"), repo+"/releases/download/channel-production/statement.json"; got != want {
		t.Errorf("statementURL: got %q want %q", got, want)
	}
	if got, want := bundleURL(repo, "abc123"), repo+"/releases/download/bundle-abc123/bundle.tar.gz"; got != want {
		t.Errorf("bundleURL: got %q want %q", got, want)
	}
}

type fakeGetter struct {
	resp   map[string][]byte // url -> 200 body; absent => 404
	status map[string]int    // url -> forced non-200 status
	calls  []string          // "url accept" per request, in order
}

func (f *fakeGetter) Do(req *http.Request) (*http.Response, error) {
	url := req.URL.String()
	f.calls = append(f.calls, strings.TrimSpace(url+" "+req.Header.Get("Accept")))
	if code, ok := f.status[url]; ok {
		return &http.Response{StatusCode: code, Status: strconv.Itoa(code) + " " + http.StatusText(code), Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	if body, ok := f.resp[url]; ok {
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(body))}, nil
	}
	return &http.Response{StatusCode: 404, Status: "404 Not Found", Body: io.NopCloser(strings.NewReader(""))}, nil
}

func gzTar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGitHubTransport_FetchBundle(t *testing.T) {
	const repo = "https://example.com/rules"
	body := gzTar(t, map[string]string{
		"manifest.yaml":        "schema_version: 9\n",
		"claude_sdk/pack.yaml": "rules: []\n",
	})
	tr := &githubTransport{client: &fakeGetter{resp: map[string][]byte{bundleURL(repo, "d1"): body}}}

	fsys, err := tr.FetchBundle(repo, "d1")
	if err != nil {
		t.Fatalf("FetchBundle: %v", err)
	}
	if b, err := fs.ReadFile(fsys, "manifest.yaml"); err != nil || !strings.Contains(string(b), "schema_version") {
		t.Errorf("unpacked bundle missing manifest.yaml: %v", err)
	}
	if _, err := fs.ReadFile(fsys, "claude_sdk/pack.yaml"); err != nil {
		t.Errorf("unpacked bundle missing nested file: %v", err)
	}
}

func TestGitHubTransport_FetchStatement(t *testing.T) {
	const repo = "https://example.com/rules"
	tr := &githubTransport{client: &fakeGetter{resp: map[string][]byte{statementURL(repo, "production"): []byte(`{"hello":"world"}`)}}}
	got, err := tr.FetchStatement(repo, "production")
	if err != nil {
		t.Fatalf("FetchStatement: %v", err)
	}
	if string(got) != `{"hello":"world"}` {
		t.Errorf("FetchStatement body = %q", got)
	}
}

func TestGitHubTransport_Non200(t *testing.T) {
	tr := &githubTransport{client: &fakeGetter{}} // every URL 404s
	if _, err := tr.FetchStatement("https://example.com/rules", "production"); err == nil {
		t.Fatal("want error on 404")
	}
}

// TestGitHubTransport_APIFallback: a 5xx from the web download route retries
// through the REST API asset endpoint; a 404, or a non-GitHub host, does not.
func TestGitHubTransport_APIFallback(t *testing.T) {
	const repo = "https://github.com/o/r"
	web := statementURL(repo, "production")
	rel := "https://api.github.com/repos/o/r/releases/tags/channel-production"
	asset := "https://api.github.com/repos/o/r/releases/assets/7"
	meta := []byte(`{"assets":[{"name":"other","url":"https://api.github.com/x"},{"name":"statement.json","url":"` + asset + `"}]}`)

	f := &fakeGetter{
		status: map[string]int{web: http.StatusGatewayTimeout},
		resp:   map[string][]byte{rel: meta, asset: []byte(`{"ok":1}`)},
	}
	got, err := (&githubTransport{client: f}).FetchStatement(repo, "production")
	if err != nil || string(got) != `{"ok":1}` {
		t.Fatalf("504 should fall back to the API: got %q, %v", got, err)
	}
	want := []string{web, rel + " application/vnd.github+json", asset + " application/octet-stream"}
	if strings.Join(f.calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(f.calls, "\n"), strings.Join(want, "\n"))
	}

	f = &fakeGetter{resp: map[string][]byte{rel: meta, asset: []byte("x")}} // web URL 404s
	if _, err := (&githubTransport{client: f}).FetchStatement(repo, "production"); err == nil || len(f.calls) != 1 {
		t.Errorf("404 must be final, no fallback: err=%v calls=%v", err, f.calls)
	}

	f = &fakeGetter{status: map[string]int{statementURL("https://example.com/r", "production"): 503}}
	if _, err := (&githubTransport{client: f}).FetchStatement("https://example.com/r", "production"); err == nil || len(f.calls) != 1 {
		t.Errorf("non-GitHub host must not fall back: err=%v calls=%v", err, f.calls)
	}

	f = &fakeGetter{status: map[string]int{web: http.StatusGatewayTimeout, rel: http.StatusForbidden}}
	if _, err := (&githubTransport{client: f}).FetchStatement(repo, "production"); err == nil ||
		!strings.Contains(err.Error(), "504") || !strings.Contains(err.Error(), "API fallback") {
		t.Errorf("both routes failing should report both: %v", err)
	}
}

func TestUntarGz_RejectsUnsafePath(t *testing.T) {
	body := gzTar(t, map[string]string{"../escape.yaml": "x"})
	if _, err := untarGz(body); err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("want unsafe-path error, got %v", err)
	}
}

func TestUntarGz_RejectsEmpty(t *testing.T) {
	body := gzTar(t, map[string]string{})
	if _, err := untarGz(body); err == nil {
		t.Fatal("want error for empty bundle")
	}
}
