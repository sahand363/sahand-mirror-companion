package main

// National-internet simulator: blocks every outgoing connection except the
// hub (and loopback, replies of existing/incoming connections, and DNS if
// allowed). Incoming SSH and the companion UI keep working because only the
// OUTPUT chain is touched and replies are ESTABLISHED.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	simChain     = "SMC_SIM"
	rollbackUnit = "smc-sim-rollback"
)

type SimState struct {
	On      bool      `json:"on"`
	Since   time.Time `json:"since,omitempty"`
	Until   time.Time `json:"until,omitempty"` // zero = no auto rollback
	HubIPs  []string  `json:"hub_ips,omitempty"`
	DNS     bool      `json:"dns"`
	Backend string    `json:"backend,omitempty"`
	Error   string    `json:"error,omitempty"`
}

// cmdRunner is swapped in tests.
var cmdRunner = func(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func have(bin string) bool { _, err := exec.LookPath(bin); return err == nil }

func simStatePath() string { return filepath.Join(dataDir, "sim.json") }

func readSimState() SimState {
	var s SimState
	if b, err := os.ReadFile(simStatePath()); err == nil {
		json.Unmarshal(b, &s)
	}
	return s
}

func writeSimState(s SimState) {
	os.MkdirAll(dataDir, 0o755)
	b, _ := json.MarshalIndent(s, "", "  ")
	os.WriteFile(simStatePath(), b, 0o644)
}

// chainActive reports whether our rules are in the kernel right now.
func chainActive() bool {
	_, err := cmdRunner("iptables", "-C", "OUTPUT", "-j", simChain)
	return err == nil
}

// SimStatus reconciles the stored state with the kernel (rules vanish on reboot).
func SimStatus() SimState {
	s := readSimState()
	active := chainActive()
	if s.On != active {
		s.On = active
		if !active {
			s.Until, s.Since = time.Time{}, time.Time{}
		}
		writeSimState(s)
	}
	if s.On && !s.Until.IsZero() && time.Now().After(s.Until.Add(2*time.Minute)) {
		// rollback timer should have fired; do it ourselves
		SimOff()
		return readSimState()
	}
	return s
}

func resolveHub(hub string) (v4, v6 []string, port int, err error) {
	host, port, err := hubHostPort(hub)
	if err != nil {
		return nil, nil, 0, err
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		ips, err = net.LookupIP(host)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("cannot resolve hub %s: %v", host, err)
		}
	}
	for _, ip := range ips {
		if ip.To4() != nil {
			v4 = append(v4, ip.String())
		} else {
			v6 = append(v6, ip.String())
		}
	}
	return v4, v6, port, nil
}

func splitAllow(list []string) (v4, v6 []string) {
	for _, a := range list {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		ip := a
		if i := strings.IndexByte(a, '/'); i >= 0 {
			ip = a[:i]
		}
		p := net.ParseIP(ip)
		if p == nil {
			continue
		}
		if p.To4() != nil {
			v4 = append(v4, a)
		} else {
			v6 = append(v6, a)
		}
	}
	return
}

// simRules builds the rule list for one family.
func simRules(stateMatch []string, hubIPs []string, port int, dns bool, allow []string) [][]string {
	r := [][]string{
		{"-A", simChain, "-o", "lo", "-j", "ACCEPT"},
		append(append([]string{"-A", simChain}, stateMatch...), "-j", "ACCEPT"),
	}
	for _, ip := range hubIPs {
		r = append(r, []string{"-A", simChain, "-d", ip, "-p", "tcp", "--dport", strconv.Itoa(port), "-j", "ACCEPT"})
	}
	for _, a := range allow {
		r = append(r, []string{"-A", simChain, "-d", a, "-j", "ACCEPT"})
	}
	if dns {
		r = append(r, []string{"-A", simChain, "-p", "udp", "--dport", "53", "-j", "ACCEPT"},
			[]string{"-A", simChain, "-p", "tcp", "--dport", "53", "-j", "ACCEPT"})
	}
	r = append(r, []string{"-A", simChain, "-j", "REJECT"})
	return r
}

func applyFamily(bin string, hubIPs []string, port int, dns bool, allow []string) error {
	cleanupFamily(bin)
	if _, err := cmdRunner(bin, "-N", simChain); err != nil {
		return fmt.Errorf("%s -N: %v", bin, err)
	}
	stateMatch := []string{"-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED"}
	if _, err := cmdRunner(bin, "-A", simChain, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"); err != nil {
		stateMatch = []string{"-m", "state", "--state", "ESTABLISHED,RELATED"}
	} else {
		cmdRunner(bin, "-F", simChain)
	}
	for _, rule := range simRules(stateMatch, hubIPs, port, dns, allow) {
		if out, err := cmdRunner(bin, rule...); err != nil {
			cleanupFamily(bin)
			return fmt.Errorf("%s %s: %v %s", bin, strings.Join(rule, " "), err, out)
		}
	}
	if out, err := cmdRunner(bin, "-I", "OUTPUT", "1", "-j", simChain); err != nil {
		cleanupFamily(bin)
		return fmt.Errorf("%s -I OUTPUT: %v %s", bin, err, out)
	}
	return nil
}

func cleanupFamily(bin string) {
	for i := 0; i < 10; i++ {
		if _, err := cmdRunner(bin, "-D", "OUTPUT", "-j", simChain); err != nil {
			break
		}
	}
	cmdRunner(bin, "-F", simChain)
	cmdRunner(bin, "-X", simChain)
}

// SimOn enables the simulator. minutes > 0 schedules an automatic rollback.
func SimOn(c Config, minutes int) (SimState, error) {
	if !have("iptables") {
		return SimStatus(), errors.New("iptables is not installed (apt install iptables)")
	}
	if c.Hub == "" {
		return SimStatus(), errors.New("hub address is not set")
	}
	v4, v6, port, err := resolveHub(c.Hub)
	if err != nil {
		return SimStatus(), err
	}
	a4, a6 := splitAllow(c.SimAllow)
	if err := applyFamily("iptables", v4, port, c.SimAllowDNS, a4); err != nil {
		return SimStatus(), err
	}
	backend := "iptables"
	if have("ip6tables") {
		if err := applyFamily("ip6tables", v6, port, c.SimAllowDNS, a6); err == nil {
			backend += "+ip6tables"
		}
	}
	st := SimState{On: true, Since: time.Now(), HubIPs: append(v4, v6...), DNS: c.SimAllowDNS, Backend: backend}
	cmdRunner("systemctl", "stop", rollbackUnit+".timer", rollbackUnit+".service")
	cmdRunner("systemctl", "reset-failed", rollbackUnit+".timer", rollbackUnit+".service")
	if minutes > 0 {
		self, _ := os.Executable()
		if self == "" {
			self = binPath
		}
		if _, err := cmdRunner("systemd-run", "--on-active="+strconv.Itoa(minutes)+"m", "--unit="+rollbackUnit, self, "sim", "off"); err != nil {
			st.Error = "auto rollback timer could not be created: " + err.Error()
		}
		st.Until = time.Now().Add(time.Duration(minutes) * time.Minute)
	}
	writeSimState(st)
	return st, nil
}

// SimOff removes every rule; safe to call when nothing is active.
func SimOff() SimState {
	cleanupFamily("iptables")
	if have("ip6tables") {
		cleanupFamily("ip6tables")
	}
	cmdRunner("systemctl", "stop", rollbackUnit+".timer")
	st := SimState{On: false}
	writeSimState(st)
	return st
}
