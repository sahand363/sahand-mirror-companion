package main

import (
	"strings"
	"testing"
)

func TestNormalizeHub(t *testing.T) {
	for in, want := range map[string]string{
		"10.0.0.5:8686":               "http://10.0.0.5:8686",
		"http://10.0.0.5:8686/":       "http://10.0.0.5:8686",
		"http://10.0.0.5:8686/?k=abc": "http://10.0.0.5:8686",
		"https://hub.example.ir":      "https://hub.example.ir",
	} {
		got, err := normalizeHub(in)
		if err != nil || got != want {
			t.Errorf("%q -> %q %v, want %q", in, got, err, want)
		}
	}
	if _, err := normalizeHub("ftp://x"); err == nil {
		t.Error("ftp accepted")
	}
	h, p, _ := hubHostPort("https://hub.example.ir")
	if h != "hub.example.ir" || p != 443 {
		t.Error(h, p)
	}
}

func TestSimRulesKeepHubAndSSH(t *testing.T) {
	rules := simRules([]string{"-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED"}, []string{"10.0.0.5"}, 8686, true, []string{"10.10.0.0/16"})
	all := []string{}
	for _, r := range rules {
		all = append(all, strings.Join(r, " "))
	}
	s := strings.Join(all, "\n")
	for _, want := range []string{"-o lo -j ACCEPT", "ESTABLISHED,RELATED -j ACCEPT", "-d 10.0.0.5 -p tcp --dport 8686 -j ACCEPT", "-d 10.10.0.0/16 -j ACCEPT", "-p udp --dport 53 -j ACCEPT"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	if !strings.HasSuffix(s, "-j REJECT") {
		t.Error("last rule must reject")
	}
	if strings.Index(s, "ESTABLISHED") > strings.Index(s, "REJECT") {
		t.Error("established must come before reject")
	}
}

func TestSimOnCommands(t *testing.T) {
	dir := t.TempDir()
	dataDir = dir
	var calls []string
	old := cmdRunner
	defer func() { cmdRunner = old }()
	cmdRunner = func(name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return "", nil
	}
	if !have("iptables") {
		t.Skip("iptables binary not present")
	}
	c := defaultConfig()
	c.Hub = "http://10.0.0.5:8686"
	st, err := SimOn(c, 30)
	if err != nil || !st.On || st.Until.IsZero() {
		t.Fatal(st, err)
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "iptables -I OUTPUT 1 -j SMC_SIM") || !strings.Contains(joined, "systemd-run --on-active=30m") {
		t.Fatal(joined)
	}
}

func TestDiffCatalog(t *testing.T) {
	prev := &Catalog{Apps: []CatalogItem{{ID: "a", Latest: "v1"}, {ID: "b", Latest: "v1"}, {ID: "gone", Latest: "v1"}, {ID: "old", Change: "removed"}}}
	cur := &Catalog{Apps: []CatalogItem{{ID: "a", Latest: "v1"}, {ID: "b", Latest: "v2"}, {ID: "c", Latest: "v1"}}}
	diffCatalog(prev, cur)
	if cur.Added != 1 || cur.Updated != 1 || cur.Removed != 1 {
		t.Fatalf("%+v", cur)
	}
	got := map[string]string{}
	for _, it := range cur.Apps {
		got[it.ID] = it.Change
	}
	if got["c"] != "new" || got["b"] != "updated" || got["gone"] != "removed" || got["a"] != "" {
		t.Fatal(got)
	}
	if _, ok := got["old"]; ok {
		t.Fatal("previously removed item must not come back")
	}
	if cur.Apps[len(cur.Apps)-1].ID != "gone" {
		t.Fatal("removed should be last")
	}
	first := &Catalog{Apps: []CatalogItem{{ID: "x"}}}
	diffCatalog(nil, first)
	if !first.First || first.Added != 0 {
		t.Fatal("first check should not mark everything new")
	}
}

func TestPassword(t *testing.T) {
	h := hashPassword("hello123")
	if !checkPassword(h, "hello123") || checkPassword(h, "nope") {
		t.Fatal("password check")
	}
}
