package main

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	cReset  = "\033[0m"
	cBold   = "\033[1m"
	cDim    = "\033[2m"
	cRed    = "\033[31m"
	cGreen  = "\033[32m"
	cYellow = "\033[33m"
	cCyan   = "\033[36m"
)

var in = bufio.NewReader(os.Stdin)

func ask(prompt, def string) string {
	if def != "" {
		fmt.Printf("  %s [%s]: ", prompt, def)
	} else {
		fmt.Printf("  %s: ", prompt)
	}
	s, _ := in.ReadString('\n')
	if s = strings.TrimSpace(s); s == "" {
		return def
	}
	return s
}

func pause() {
	fmt.Print(cDim + "\n  press Enter to go back..." + cReset)
	in.ReadString('\n')
}

func run(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

var ansiStrip = ansiRe

func boxLine(s string, w int) string {
	vis := utf8.RuneCountInString(ansiStrip.ReplaceAllString(s, ""))
	pad := w - vis
	if pad < 0 {
		pad = 0
	}
	return cCyan + "  │ " + cReset + s + strings.Repeat(" ", pad) + cCyan + " │" + cReset
}

func header(c Config) {
	fmt.Print("\033[H\033[2J")
	const w = 62
	top := cCyan + "  ┌" + strings.Repeat("─", w+2) + "┐" + cReset
	bot := cCyan + "  └" + strings.Repeat("─", w+2) + "┘" + cReset
	fmt.Println(top)
	fmt.Println(boxLine(cBold+"Sahand Mirror Companion"+cReset+"  "+cDim+version+cReset, w))
	fmt.Println(boxLine("", w))
	for i, u := range uiURLs(c) {
		label := "UI      "
		if i > 0 {
			label = "        "
		}
		fmt.Println(boxLine(label+cBold+cGreen+u+cReset, w))
	}
	fmt.Println(boxLine("User    "+c.User, w))
	hub := c.Hub
	if hub == "" {
		hub = cRed + "not set" + cReset
	}
	fmt.Println(boxLine("Hub     "+hub, w))
	st := SimStatus()
	sim := cGreen + "● OFF" + cReset + cDim + "  (normal internet)" + cReset
	if st.On {
		sim = cRed + cBold + "● ON" + cReset + "   only the hub is reachable"
		if !st.Until.IsZero() {
			sim += cDim + fmt.Sprintf(" · off in %s", time.Until(st.Until).Round(time.Minute)) + cReset
		}
	}
	fmt.Println(boxLine("Sim     "+sim, w))
	svc := cRed + "stopped" + cReset
	if out, _ := exec.Command("systemctl", "is-active", unitName).Output(); strings.TrimSpace(string(out)) == "active" {
		svc = cGreen + "running" + cReset
	}
	fmt.Println(boxLine("Service "+svc, w))
	fmt.Println(bot)
	fmt.Println()
}

func item(n, text string) { fmt.Printf("   %s%2s%s  %s\n", cBold+cYellow, n, cReset, text) }

func runMenu() {
	if os.Geteuid() != 0 {
		die("run as root")
	}
	for {
		c := mustConfig()
		header(c)
		st := SimStatus()
		simLabel := "Simulate national internet  " + cDim + "(turn ON)" + cReset
		if st.On {
			simLabel = "Simulate national internet  " + cGreen + "(turn OFF, back to normal)" + cReset
		}
		item("1", "Show UI address and login")
		item("2", "Set UI username / password")
		item("3", "Change UI port")
		item("4", simLabel)
		item("5", "Connectivity test (GitHub, Google, …)")
		item("6", "Change hub address / access key")
		item("7", "Restart service")
		item("8", "Update")
		item("9", "Uninstall")
		item("0", "Exit")
		fmt.Println()
		switch ask("choose", "") {
		case "1":
			fmt.Println()
			for _, u := range uiURLs(c) {
				fmt.Println("  UI:   " + cGreen + u + cReset)
			}
			fmt.Println("  user: " + c.User)
			fmt.Println(cDim + "  forgot the password? option 2 sets a new one" + cReset)
			pause()
		case "2":
			user := ask("username", c.User)
			pw := ask("new password (empty = random)", "")
			if pw == "" {
				pw = randPassword()
			}
			if len(pw) < 6 {
				fmt.Println(cRed + "  password must be at least 6 characters" + cReset)
				pause()
				continue
			}
			c.User, c.PasswordHash = user, hashPassword(pw)
			must(saveConfig(c))
			fmt.Printf("\n  user:     %s\n  password: %s%s%s\n", user, cBold, pw, cReset)
			pause()
		case "3":
			p := ask("UI port", uiPort(c))
			n, err := strconv.Atoi(p)
			if err != nil || n < 1 || n > 65535 {
				fmt.Println(cRed + "  invalid port" + cReset)
				pause()
				continue
			}
			c.Listen = "0.0.0.0:" + p
			must(saveConfig(c))
			run("systemctl", "restart", unitName)
			fmt.Println(cGreen + "  saved, service restarted" + cReset)
			pause()
		case "4":
			if st.On {
				printSim(SimOff())
			} else {
				m := ask("auto turn-off after how many minutes (0 = never)", strconv.Itoa(c.SimMinutes))
				n, err := strconv.Atoi(m)
				if err != nil || n < 0 {
					n = c.SimMinutes
				}
				if n == 0 {
					fmt.Println(cYellow + "  without a timer the simulator stays on until you turn it off (a reboot also clears it)." + cReset)
				}
				s2, err := SimOn(c, n)
				if err != nil {
					fmt.Println(cRed + "  " + err.Error() + cReset)
				} else {
					printSim(s2)
					fmt.Println(cDim + "  SSH and the UI keep working. Run option 5 to see what is cut." + cReset)
				}
			}
			pause()
		case "5":
			fmt.Println(cDim + "\n  testing…" + cReset)
			printTests(RunTests(c))
			pause()
		case "6":
			h := ask("hub address (e.g. http://10.0.0.5:8686)", c.Hub)
			nh, err := normalizeHub(h)
			if err != nil {
				fmt.Println(cRed + "  " + err.Error() + cReset)
				pause()
				continue
			}
			if u, _ := url.Parse(h); u != nil && u.Query().Get("k") != "" {
				c.Key = u.Query().Get("k")
			}
			k := ask("hub access key (empty = none, '-' = keep)", "-")
			if k != "-" {
				c.Key = k
			}
			c.Hub = nh
			must(saveConfig(c))
			if SimStatus().On {
				SimOn(c, c.SimMinutes)
			}
			fmt.Println(cGreen + "  saved" + cReset)
			pause()
		case "7":
			run("systemctl", "restart", unitName)
			fmt.Println(cGreen + "  restarted" + cReset)
			pause()
		case "8":
			doUpdate(c)
			pause()
		case "9":
			doUninstall(false)
			return
		case "0", "q", "exit":
			return
		}
	}
}

func printTests(rs []TestResult) {
	fmt.Println()
	for _, r := range rs {
		mark := cGreen + "✔" + cReset
		detail := fmt.Sprintf("%d ms", r.MS)
		if !r.OK {
			mark = cRed + "✘" + cReset
			detail = cRed + asciiOnly(r.Error, "unreachable") + cReset
		}
		name := r.Name
		if r.IsHub {
			name = "sahand-mirror hub"
		}
		fmt.Printf("   %s  %-18s %s%s%s  %s\n", mark, name, cDim, r.URL, cReset, detail)
	}
}

func doUpdate(c Config) {
	if c.Hub == "" {
		fmt.Println(cRed + "  hub address is not set" + cReset)
		return
	}
	u := c.Hub + "/companion"
	if c.Key != "" {
		u += "?k=" + url.QueryEscape(c.Key)
	}
	fmt.Println("  updating from " + c.Hub + " …")
	script := "tmp=$(mktemp); curl -fsSL -k " + shQuote(u) + " -o \"$tmp\" && SMC_NO_MENU=1 bash \"$tmp\"; rc=$?; rm -f \"$tmp\"; exit $rc"
	if err := run("bash", "-c", script); err != nil {
		fmt.Println(cRed + "  update failed: " + err.Error() + cReset)
		fmt.Println(cDim + "  is sahand-mirror-companion mirrored on the hub? (hub panel → apps)" + cReset)
		return
	}
	if out, err := exec.Command(binPath, "version").Output(); err == nil {
		fmt.Println(cGreen + "  now running " + strings.TrimSpace(string(out)) + cReset)
	}
}

func doUninstall(yes bool) {
	if !yes {
		if a := ask("remove sahand-mirror-companion and its settings? type yes", ""); a != "yes" {
			fmt.Println("  cancelled")
			return
		}
	}
	SimOff()
	exec.Command("systemctl", "disable", "--now", unitName).Run()
	os.Remove(unitPath)
	exec.Command("systemctl", "daemon-reload").Run()
	os.RemoveAll(dataDir)
	os.RemoveAll("/etc/sahand-mirror-companion")
	os.Remove(linkPath)
	os.Remove(binPath)
	fmt.Println(cGreen + "  sahand-mirror-companion removed (simulator rules cleared)." + cReset)
}

// asciiOnly keeps SSH output ASCII: many terminals break on Persian text.
func asciiOnly(s, fallback string) string {
	for _, r := range s {
		if r > 127 {
			return fallback
		}
	}
	return s
}
