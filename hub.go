package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------- connectivity test ----------

type TestResult struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	OK    bool   `json:"ok"`
	Code  int    `json:"code,omitempty"`
	MS    int64  `json:"ms"`
	Error string `json:"error,omitempty"`
	IsHub bool   `json:"hub,omitempty"`
}

func probeClient(timeout time.Duration) *http.Client {
	tr := &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: timeout}).DialContext,
		TLSHandshakeTimeout: timeout,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, // reachability only
		DisableKeepAlives:   true,
	}
	return &http.Client{Transport: tr, Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func probe(ctx context.Context, cl *http.Client, t Target, key string) TestResult {
	r := TestResult{Name: t.Name, URL: t.URL}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, "GET", t.URL, nil)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	req.Header.Set("User-Agent", "sahand-mirror-companion")
	if key != "" {
		req.Header.Set("X-Mirror-Key", key)
	}
	resp, err := cl.Do(req)
	r.MS = time.Since(start).Milliseconds()
	if err != nil {
		r.Error = shortErr(err)
		return r
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	r.Code = resp.StatusCode
	r.OK = true // any HTTP answer means the host is reachable
	return r
}

func shortErr(err error) string {
	var ne net.Error
	switch {
	case errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	s := err.Error()
	for _, k := range []string{"connection refused", "no such host", "network is unreachable", "connection reset", "no route to host", "i/o timeout"} {
		if strings.Contains(s, k) {
			return k
		}
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// RunTests checks the hub plus every target concurrently.
func RunTests(c Config) []TestResult {
	cl := probeClient(6 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	targets := append([]Target{}, c.Targets...)
	out := make([]TestResult, len(targets)+1)
	var wg sync.WaitGroup
	if c.Hub != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := probe(ctx, cl, Target{"هاب sahand-mirror", c.Hub + "/myip"}, c.Key)
			r.IsHub, r.URL = true, c.Hub
			if r.OK && r.Code >= 400 {
				r.OK, r.Error = false, fmt.Sprintf("HTTP %d", r.Code)
			}
			out[0] = r
		}()
	} else {
		out[0] = TestResult{Name: "هاب sahand-mirror", Error: "آدرس هاب تنظیم نشده", IsHub: true}
	}
	for i, t := range targets {
		wg.Add(1)
		go func(i int, t Target) { defer wg.Done(); out[i+1] = probe(ctx, cl, t, "") }(i, t)
	}
	wg.Wait()
	return out
}

// ---------- hub catalog ----------

type ReadySummary struct {
	Handled int `json:"handled"`
	Action  int `json:"action"`
	Offline int `json:"offline"`
}

type CatalogItem struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Kind      string       `json:"kind"`
	Repo      string       `json:"repo,omitempty"`
	Command   string       `json:"command,omitempty"`
	Install   string       `json:"install"`
	Latest    string       `json:"latest,omitempty"`
	Published time.Time    `json:"published,omitempty"`
	Releases  int          `json:"releases"`
	Assets    int          `json:"assets"`
	Size      int64        `json:"size"`
	LastSync  time.Time    `json:"last_sync"`
	Error     string       `json:"error,omitempty"`
	Analyzed  bool         `json:"analyzed"`
	Ready     ReadySummary `json:"ready"`
	Readme    bool         `json:"readme"`
	Note      string       `json:"note,omitempty"`
	// filled by the companion
	Change  string `json:"change,omitempty"` // new | updated | removed
	PrevTag string `json:"prev,omitempty"`
}

type Catalog struct {
	Hub       string        `json:"hub"`
	Version   string        `json:"version"`
	Generated time.Time     `json:"generated"`
	LastSync  time.Time     `json:"last_sync"`
	Companion string        `json:"companion"`
	Apps      []CatalogItem `json:"apps"`
	// companion side
	Checked time.Time `json:"checked"`
	Added   int       `json:"added"`
	Removed int       `json:"removed"`
	Updated int       `json:"updated"`
	First   bool      `json:"first,omitempty"`
}

var hubClient = &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil,
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} // hub uses its own internal CA

func hubGet(c Config, path string, max int64) ([]byte, http.Header, error) {
	if c.Hub == "" {
		return nil, nil, errors.New("آدرس هاب تنظیم نشده است")
	}
	req, _ := http.NewRequest("GET", c.Hub+path, nil)
	if c.Key != "" {
		req.Header.Set("X-Mirror-Key", c.Key)
	}
	resp, err := hubClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("هاب در دسترس نیست: %s", shortErr(err))
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, max))
	if err != nil {
		return nil, nil, err
	}
	switch {
	case resp.StatusCode == 401:
		return nil, nil, errors.New("هاب کلید دسترسی می‌خواهد (در منو کلید را وارد کنید)")
	case resp.StatusCode == 404 && path == "/catalog":
		return nil, nil, errors.New("این هاب فهرست ندارد؛ هاب را به نسخهٔ 0.3 یا بالاتر به‌روز کنید")
	case resp.StatusCode >= 300:
		return nil, nil, fmt.Errorf("پاسخ هاب: HTTP %d", resp.StatusCode)
	}
	return b, resp.Header, nil
}

func catalogPath() string { return filepath.Join(dataDir, "catalog.json") }

func LoadCatalog() (*Catalog, error) {
	b, err := os.ReadFile(catalogPath())
	if err != nil {
		return nil, err
	}
	var c Catalog
	return &c, json.Unmarshal(b, &c)
}

// diffCatalog marks new/updated items and appends removed ones.
func diffCatalog(prev, cur *Catalog) {
	cur.Added, cur.Removed, cur.Updated = 0, 0, 0
	if prev == nil {
		cur.First = true
		return
	}
	old := map[string]CatalogItem{}
	for _, it := range prev.Apps {
		if it.Change != "removed" {
			old[it.ID] = it
		}
	}
	seen := map[string]bool{}
	for i := range cur.Apps {
		it := &cur.Apps[i]
		seen[it.ID] = true
		o, ok := old[it.ID]
		switch {
		case !ok:
			it.Change = "new"
			cur.Added++
		case o.Latest != it.Latest && it.Latest != "":
			it.Change, it.PrevTag = "updated", o.Latest
			cur.Updated++
		}
	}
	for id, o := range old {
		if !seen[id] {
			o.Change = "removed"
			cur.Apps = append(cur.Apps, o)
			cur.Removed++
		}
	}
	sort.SliceStable(cur.Apps, func(i, j int) bool {
		ri, rj := cur.Apps[i].Change == "removed", cur.Apps[j].Change == "removed"
		if ri != rj {
			return !ri
		}
		return cur.Apps[i].ID < cur.Apps[j].ID
	})
}

// RefreshCatalog downloads the hub catalog and compares it with the last check.
func RefreshCatalog(c Config) (*Catalog, error) {
	b, _, err := hubGet(c, "/catalog", 8<<20)
	if err != nil {
		return nil, err
	}
	var cur Catalog
	if err := json.Unmarshal(b, &cur); err != nil {
		return nil, fmt.Errorf("پاسخ نامعتبر از هاب: %v", err)
	}
	prev, _ := LoadCatalog()
	diffCatalog(prev, &cur)
	cur.Checked = time.Now()
	os.MkdirAll(dataDir, 0o755)
	nb, _ := json.MarshalIndent(cur, "", " ")
	os.WriteFile(catalogPath(), nb, 0o644)
	return &cur, nil
}
