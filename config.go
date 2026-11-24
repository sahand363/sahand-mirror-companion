package main

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var (
	cfgPath = "/etc/sahand-mirror-companion/config.json"
	dataDir = "/var/lib/sahand-mirror-companion"
)

type Target struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type Config struct {
	Hub          string   `json:"hub"`           // http://IP:PORT of the sahand-mirror hub
	Key          string   `json:"key,omitempty"` // hub access key (optional)
	Listen       string   `json:"listen"`        // UI listen address
	User         string   `json:"user"`          // UI user
	PasswordHash string   `json:"password_hash"` // pbkdf2
	SimMinutes   int      `json:"sim_minutes"`   // auto rollback, 0 = never
	SimAllowDNS  bool     `json:"sim_allow_dns"` // keep DNS (port 53) open like a real shutdown
	SimAllow     []string `json:"sim_allow"`     // extra IPs/CIDRs that stay reachable
	Targets      []Target `json:"targets"`       // connectivity test list
}

func defaultTargets() []Target {
	return []Target{
		{"GitHub", "https://github.com"},
		{"GitHub raw", "https://raw.githubusercontent.com"},
		{"GitHub API", "https://api.github.com"},
		{"Google", "https://www.google.com"},
		{"Cloudflare", "https://1.1.1.1"},
		{"Docker Hub", "https://registry-1.docker.io/v2/"},
		{"PyPI", "https://pypi.org/simple/"},
		{"Ubuntu archive", "http://archive.ubuntu.com/ubuntu/"},
		{"Let's Encrypt", "https://acme-v02.api.letsencrypt.org/directory"},
		{"Telegram", "https://api.telegram.org"},
	}
}

func defaultConfig() Config {
	return Config{Listen: "0.0.0.0:8687", User: "admin", SimMinutes: 30, SimAllowDNS: true, Targets: defaultTargets()}
}

func normalizeHub(h string) (string, error) {
	h = strings.TrimSpace(h)
	if h == "" {
		return "", errors.New("empty hub address")
	}
	if !strings.Contains(h, "://") {
		h = "http://" + h
	}
	u, err := url.Parse(h)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("bad hub address %q", h)
	}
	if k := u.Query().Get("k"); k != "" {
		u.RawQuery = ""
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery, u.Fragment = "", ""
	return u.String(), nil
}

// hubHostPort returns host and port of the hub URL.
func hubHostPort(hub string) (string, int, error) {
	u, err := url.Parse(hub)
	if err != nil {
		return "", 0, err
	}
	host := u.Hostname()
	p := u.Port()
	if p == "" {
		p = "80"
		if u.Scheme == "https" {
			p = "443"
		}
	}
	n, err := strconv.Atoi(p)
	return host, n, err
}

func uiPort(c Config) string {
	_, p, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return "8687"
	}
	return p
}

func loadConfig() (Config, error) {
	c := defaultConfig()
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if len(c.Targets) == 0 {
		c.Targets = defaultTargets()
	}
	if c.Listen == "" {
		c.Listen = "0.0.0.0:8687"
	}
	if c.User == "" {
		c.User = "admin"
	}
	return c, nil
}

var cfgMu sync.Mutex

func saveConfig(c Config) error {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := cfgPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, cfgPath)
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func randPassword() string {
	const al = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 14)
	rand.Read(b)
	for i := range b {
		b[i] = al[int(b[i])%len(al)]
	}
	return string(b)
}

const pbkdf2Iter = 120000

func hashPassword(pw string) string {
	salt := []byte(randHex(16))
	key, err := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iter, 32)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iter,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

func checkPassword(hash, pw string) bool {
	p := strings.Split(hash, "$")
	if len(p) != 4 || p[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(p[1])
	if err != nil || iter < 1000 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(p[2])
	want, err2 := base64.RawStdEncoding.DecodeString(p[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}
