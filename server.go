package main

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

//go:embed web
var webFS embed.FS

type Server struct {
	mu    sync.Mutex
	sess  map[string]time.Time
	fails map[string]int
	jobMu sync.Mutex
	job   *Job
}

type Job struct {
	App     string    `json:"app"`
	Running bool      `json:"running"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended,omitempty"`
	Exit    int       `json:"exit"`
	Log     string    `json:"log,omitempty"`
}

func NewServer() *Server {
	return &Server{sess: map[string]time.Time{}, fails: map[string]int{}}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func jerr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("smc_s"); err == nil {
			s.mu.Lock()
			delete(s.sess, c.Value)
			s.mu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: "smc_s", Value: "", Path: "/", MaxAge: -1})
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("/api/", s.auth(s.handleAPI))
	sub, _ := fs.Sub(webFS, "web")
	static := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'")
		w.Header().Set("X-Frame-Options", "DENY")
		static.ServeHTTP(w, r)
	})
	return mux
}

func (s *Server) loggedIn(r *http.Request) bool {
	c, err := r.Cookie("smc_s")
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.sess[c.Value]
	if !ok || time.Now().After(exp) {
		delete(s.sess, c.Value)
		return false
	}
	return true
}

func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.loggedIn(r) {
			writeJSON(w, 401, map[string]string{"error": "login required"})
			return
		}
		if r.Method != "GET" && r.Header.Get("X-Requested-With") != "smc" {
			writeJSON(w, 403, map[string]string{"error": "missing CSRF header"})
			return
		}
		h(w, r)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}
	ip := clientIP(r)
	s.mu.Lock()
	n := s.fails[ip]
	s.mu.Unlock()
	if n >= 8 {
		jerr(w, 429, errors.New("تلاش زیاد؛ چند دقیقه بعد دوباره امتحان کنید"))
		return
	}
	var in struct{ Username, Password string }
	json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in)
	c, _ := loadConfig()
	if c.PasswordHash == "" || subtle.ConstantTimeCompare([]byte(in.Username), []byte(c.User)) != 1 || !checkPassword(c.PasswordHash, in.Password) {
		s.mu.Lock()
		s.fails[ip]++
		if s.fails[ip] == 8 {
			go func() { time.Sleep(5 * time.Minute); s.mu.Lock(); delete(s.fails, ip); s.mu.Unlock() }()
		}
		s.mu.Unlock()
		time.Sleep(400 * time.Millisecond)
		jerr(w, 401, errors.New("نام کاربری یا رمز اشتباه است"))
		return
	}
	tok := randHex(24)
	s.mu.Lock()
	delete(s.fails, ip)
	s.sess[tok] = time.Now().Add(7 * 24 * time.Hour)
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "smc_s", Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 7 * 24 * 3600})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

var idRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,80}$`)

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/api/")
	c, _ := loadConfig()
	switch {
	case p == "status" && r.Method == "GET":
		writeJSON(w, 200, statusInfo(c, r))
	case p == "sim" && r.Method == "POST":
		var in struct {
			On      bool `json:"on"`
			Minutes *int `json:"minutes"`
		}
		json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in)
		if !in.On {
			writeJSON(w, 200, SimOff())
			return
		}
		m := c.SimMinutes
		if in.Minutes != nil && *in.Minutes >= 0 && *in.Minutes <= 24*60 {
			m = *in.Minutes
		}
		st, err := SimOn(c, m)
		if err != nil {
			jerr(w, 500, err)
			return
		}
		writeJSON(w, 200, st)
	case p == "test" && r.Method == "POST":
		writeJSON(w, 200, map[string]any{"at": time.Now(), "sim": SimStatus().On, "results": RunTests(c)})
	case p == "catalog" && r.Method == "GET":
		cat, err := LoadCatalog()
		if err != nil {
			writeJSON(w, 200, map[string]any{"apps": []any{}, "never": true})
			return
		}
		writeJSON(w, 200, cat)
	case p == "catalog/refresh" && r.Method == "POST":
		cat, err := RefreshCatalog(c)
		if err != nil {
			jerr(w, 502, err)
			return
		}
		writeJSON(w, 200, cat)
	case strings.HasPrefix(p, "readme/") && r.Method == "GET":
		id := strings.TrimPrefix(p, "readme/")
		if !idRe.MatchString(id) {
			jerr(w, 400, errors.New("bad id"))
			return
		}
		b, h, err := hubGet(c, "/catalog/"+url.PathEscape(id)+"/readme", 2<<20)
		if err != nil {
			jerr(w, 502, err)
			return
		}
		writeJSON(w, 200, map[string]string{"name": h.Get("X-Readme-Name"), "markdown": string(b)})
	case strings.HasPrefix(p, "install/") && r.Method == "POST":
		id := strings.TrimPrefix(p, "install/")
		if !idRe.MatchString(id) {
			jerr(w, 400, errors.New("bad id"))
			return
		}
		if err := s.startInstall(c, id); err != nil {
			jerr(w, 409, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	case p == "job" && r.Method == "GET":
		writeJSON(w, 200, s.jobInfo())
	case p == "settings" && r.Method == "PUT":
		var in struct {
			SimMinutes  *int     `json:"sim_minutes"`
			SimAllowDNS *bool    `json:"sim_allow_dns"`
			SimAllow    []string `json:"sim_allow"`
			Targets     []Target `json:"targets"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
			jerr(w, 400, err)
			return
		}
		if in.SimMinutes != nil && *in.SimMinutes >= 0 && *in.SimMinutes <= 24*60 {
			c.SimMinutes = *in.SimMinutes
		}
		if in.SimAllowDNS != nil {
			c.SimAllowDNS = *in.SimAllowDNS
		}
		if in.SimAllow != nil {
			var keep []string
			for _, a := range in.SimAllow {
				a = strings.TrimSpace(a)
				if _, _, err := net.ParseCIDR(a); err == nil || net.ParseIP(a) != nil {
					keep = append(keep, a)
				}
			}
			c.SimAllow = keep
		}
		if in.Targets != nil {
			var keep []Target
			for _, t := range in.Targets {
				u, err := url.Parse(strings.TrimSpace(t.URL))
				if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
					if t.Name == "" {
						t.Name = u.Host
					}
					keep = append(keep, Target{strings.TrimSpace(t.Name), u.String()})
				}
			}
			if len(keep) == 0 {
				keep = defaultTargets()
			}
			c.Targets = keep
		}
		if err := saveConfig(c); err != nil {
			jerr(w, 500, err)
			return
		}
		writeJSON(w, 200, statusInfo(c, r))
	default:
		jerr(w, 404, errors.New("not found"))
	}
}

func statusInfo(c Config, r *http.Request) map[string]any {
	return map[string]any{
		"version": version, "hub": c.Hub, "has_key": c.Key != "", "ui": uiURLs(c),
		"sim": SimStatus(), "sim_minutes": c.SimMinutes, "sim_allow_dns": c.SimAllowDNS,
		"sim_allow": c.SimAllow, "targets": c.Targets, "hostname": hostname(), "now": time.Now(),
	}
}

func hostname() string { h, _ := os.Hostname(); return h }

// localIPs lists global unicast IPv4 addresses of this machine.
func localIPs() []string {
	var out []string
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		if strings.HasPrefix(i.Name, "docker") || strings.HasPrefix(i.Name, "br-") || strings.HasPrefix(i.Name, "veth") {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsGlobalUnicast() {
				out = append(out, n.IP.String())
			}
		}
	}
	return out
}

func uiURLs(c Config) []string {
	port := uiPort(c)
	var out []string
	for _, ip := range localIPs() {
		out = append(out, "http://"+ip+":"+port)
	}
	if len(out) == 0 {
		out = []string{"http://SERVER-IP:" + port}
	}
	return out
}

// ---------- install an app from the hub on this server ----------

func jobLogPath() string { return filepath.Join(dataDir, "install.log") }

func (s *Server) startInstall(c Config, id string) error {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	if s.job != nil && s.job.Running {
		return errors.New("یک نصب دیگر در حال اجراست: " + s.job.App)
	}
	if c.Hub == "" {
		return errors.New("آدرس هاب تنظیم نشده است")
	}
	u := c.Hub + "/i/" + url.PathEscape(id)
	if c.Key != "" {
		u += "?k=" + url.QueryEscape(c.Key)
	}
	os.MkdirAll(dataDir, 0o755)
	f, err := os.Create(jobLogPath())
	if err != nil {
		return err
	}
	script := "set -o pipefail; tmp=$(mktemp); curl -fsSL -k " + shQuote(u) + " -o \"$tmp\" || { echo 'download from hub failed'; exit 90; }; bash \"$tmp\"; rc=$?; rm -f \"$tmp\"; exit $rc"
	cmd := exec.Command("bash", "-c", script)
	cmd.Stdout, cmd.Stderr = f, f
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "TERM=dumb")
	if err := cmd.Start(); err != nil {
		f.Close()
		return err
	}
	j := &Job{App: id, Running: true, Started: time.Now()}
	s.job = j
	go func() {
		err := cmd.Wait()
		f.Close()
		s.jobMu.Lock()
		j.Running, j.Ended = false, time.Now()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				j.Exit = ee.ExitCode()
			} else {
				j.Exit = -1
			}
		}
		s.jobMu.Unlock()
		log.Printf("install %s finished exit=%d", id, j.Exit)
	}()
	return nil
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func (s *Server) jobInfo() any {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	if s.job == nil {
		return map[string]any{"none": true}
	}
	j := *s.job
	if b, err := os.ReadFile(jobLogPath()); err == nil {
		if len(b) > 64<<10 {
			b = b[len(b)-64<<10:]
		}
		j.Log = ansiRe.ReplaceAllString(string(b), "")
	}
	return j
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
