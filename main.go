package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

var version = "v0.1.2"

const (
	binPath  = "/usr/local/bin/sahand-mirror-companion"
	linkPath = "/usr/local/bin/smc"
	unitName = "sahand-mirror-companion"
	unitPath = "/etc/systemd/system/sahand-mirror-companion.service"
	selfRepo = "sahand363/sahand-mirror-companion"
)

const usage = `sahand-mirror-companion %s  (short: smc)

  smc                      open the menu
  smc serve                run the web UI (systemd does this)
  smc init --hub URL [--key K] [--port N]   first setup, prints the UI password
  smc hub URL [KEY]        change hub address (and access key)
  smc passwd [USER]        set UI username / password
  smc sim on [MINUTES]     national-internet simulator on (auto-off after MINUTES, 0 = never)
  smc sim off | status
  smc test                 connectivity test
  smc update               reinstall the latest version from the hub
  smc uninstall [-y]
  smc version
`

func main() {
	log.SetFlags(log.LstdFlags)
	args := os.Args[1:]
	if len(args) == 0 {
		runMenu()
		return
	}
	switch args[0] {
	case "serve":
		serve()
	case "menu":
		runMenu()
	case "init":
		cmdInit(args[1:])
	case "hub":
		if len(args) < 2 {
			die("usage: smc hub URL [KEY]")
		}
		c := mustConfig()
		h, err := normalizeHub(args[1])
		if err != nil {
			die(err.Error())
		}
		c.Hub = h
		if len(args) > 2 {
			c.Key = args[2]
		}
		must(saveConfig(c))
		fmt.Println("hub:", c.Hub)
	case "passwd":
		c := mustConfig()
		user := c.User
		if len(args) > 1 {
			user = args[1]
		}
		pw := randPassword()
		c.User, c.PasswordHash = user, hashPassword(pw)
		must(saveConfig(c))
		fmt.Printf("user:     %s\npassword: %s\n", user, pw)
	case "sim":
		cmdSim(args[1:])
	case "test":
		printTests(RunTests(mustConfig()))
	case "update":
		doUpdate(mustConfig())
	case "uninstall":
		yes := len(args) > 1 && (args[1] == "-y" || args[1] == "--yes")
		doUninstall(yes)
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		fmt.Printf(usage, version)
	default:
		fmt.Printf(usage, version)
		os.Exit(2)
	}
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, "error:", msg)
	os.Exit(1)
}

func must(err error) {
	if err != nil {
		die(err.Error())
	}
}

func mustConfig() Config {
	c, err := loadConfig()
	if err != nil {
		die("config not found (" + cfgPath + "); run the installer first: " + err.Error())
	}
	return c
}

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	hub := fs.String("hub", "", "hub URL")
	key := fs.String("key", "", "hub access key")
	port := fs.Int("port", 0, "UI port")
	fs.Parse(args)
	c, err := loadConfig()
	fresh := err != nil
	if *hub != "" {
		h, err := normalizeHub(*hub)
		if err != nil {
			die(err.Error())
		}
		c.Hub = h
	}
	if *key != "" {
		c.Key = *key
	}
	if *port > 0 && *port < 65536 {
		c.Listen = "0.0.0.0:" + strconv.Itoa(*port)
	}
	pw := ""
	if fresh || c.PasswordHash == "" {
		pw = randPassword()
		c.PasswordHash = hashPassword(pw)
	}
	must(saveConfig(c))
	os.MkdirAll(dataDir, 0o755)
	if pw != "" {
		fmt.Println("ui user: " + c.User)
		fmt.Println("ui password: " + pw)
	}
}

func cmdSim(args []string) {
	if len(args) == 0 {
		args = []string{"status"}
	}
	switch args[0] {
	case "on":
		c := mustConfig()
		m := c.SimMinutes
		if len(args) > 1 {
			if n, err := strconv.Atoi(args[1]); err == nil && n >= 0 {
				m = n
			}
		}
		st, err := SimOn(c, m)
		must(err)
		printSim(st)
	case "off":
		printSim(SimOff())
	default:
		printSim(SimStatus())
	}
}

func printSim(st SimState) {
	if !st.On {
		fmt.Println("simulator: OFF (normal internet)")
		return
	}
	fmt.Printf("simulator: ON since %s — only the hub %v is reachable", st.Since.Format("15:04"), st.HubIPs)
	if st.DNS {
		fmt.Print(" (+DNS)")
	}
	fmt.Println()
	if !st.Until.IsZero() {
		fmt.Printf("auto-off at %s (in %s)\n", st.Until.Format("15:04"), time.Until(st.Until).Round(time.Minute))
	}
	if st.Error != "" {
		fmt.Println("warning:", st.Error)
	}
}

func serve() {
	c := mustConfig()
	os.MkdirAll(dataDir, 0o755)
	SimStatus() // reconcile after reboot
	srv := &http.Server{Addr: c.Listen, Handler: NewServer().Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("sahand-mirror-companion %s listening on %s (hub %s)", version, c.Listen, c.Hub)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}
