// monobox is the native Android runtime controller for Mono Box.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/LufsX/mono-box/internal/box"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("monobox", flag.ContinueOnError)
	flags.SetOutput(errOut)
	root := flags.String("root", "/data/adb/box", "runtime data directory")
	module := flags.String("module-dir", "", "Android module directory")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			usage(out)
			return 0
		}
		return 2
	}
	args = flags.Args()
	if len(args) == 0 {
		usage(errOut)
		return 2
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		usage(out)
		return 0
	}
	absRoot, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if *module == "" {
		*module = moduleDir()
	}
	if args[0] == "watch" {
		log, err := openWatchLog(absRoot)
		if err != nil {
			fmt.Fprintln(errOut, "Error: watch log:", err)
			return 1
		}
		defer log.Close()
		out = io.MultiWriter(out, log)
		errOut = io.MultiWriter(errOut, log)
	}
	r, err := box.New(absRoot, *module, out, errOut)
	// Reading or repairing settings and stopping the watcher remain available
	// when a manually edited configuration contains an invalid value.
	if err == nil || args[0] == "config" || args[0] == "module" || args[0] == "watch-stop" || args[0] == "choose-volume" {
		err = dispatch(r, args)
	}
	if errors.Is(err, box.ErrVolumeTimeout) {
		return 1
	}
	if err != nil {
		fmt.Fprintln(errOut, "Error:", err)
		if args[0] == "choose-volume" {
			return 2
		}
		return 1
	}
	return 0
}

func dispatch(r *box.Runtime, args []string) error {
	switch args[0] {
	case "action":
		if len(args) == 1 {
			return r.Action("toggle", nil)
		}
		return r.Action(args[1], args[2:])
	case "service":
		if len(args) != 2 {
			return fmt.Errorf("service requires one action")
		}
		switch args[1] {
		case "logs_size", "log_size":
			return r.LogsSize()
		case "clear_logs", "clean_logs":
			return r.ClearLogs()
		default:
			return r.Service(args[1])
		}
	case "controller":
		return r.Controller(args[1:])
	case "watch":
		if len(args) != 1 {
			return fmt.Errorf("watch takes no arguments")
		}
		return r.Watch()
	case "watch-stop":
		if len(args) != 1 {
			return fmt.Errorf("watch-stop takes no arguments")
		}
		return r.StopWatch()
	case "config", "module", "profiles":
		return r.Storage(args)
	case "logs":
		if len(args) != 3 {
			return fmt.Errorf("usage: logs <read|open> <base64-path>")
		}
		data, err := base64.StdEncoding.DecodeString(args[2])
		if err != nil {
			return fmt.Errorf("invalid base64 path")
		}
		switch args[1] {
		case "read":
			return r.ReadLog(string(data))
		case "open":
			return r.OpenLog(string(data))
		}
		return fmt.Errorf("unknown logs command")
	case "request":
		return r.Request(args[1:], func(command []string) error { return dispatch(r, command) })
	case "choose-volume":
		if len(args) != 2 {
			return fmt.Errorf("choose-volume requires a timeout in seconds")
		}
		seconds, err := strconv.Atoi(args[1])
		if err != nil || seconds < 1 || seconds > 60 {
			return fmt.Errorf("volume key timeout must be 1..60 seconds")
		}
		return r.ChooseVolume(time.Duration(seconds) * time.Second)
	case "proxy-delay":
		if len(args) != 2 {
			return fmt.Errorf("proxy-delay requires a base64 request")
		}
		return r.ProxyDelay(args[1])
	case "download-core":
		if len(args) != 5 {
			return fmt.Errorf("usage: download-core <mihomo|mihomo_smart> <overwrite> <proxy> <abi>")
		}
		overwrite, err := strconv.ParseBool(args[2])
		if err != nil {
			return fmt.Errorf("invalid overwrite flag")
		}
		proxy, err := strconv.ParseBool(args[3])
		if err != nil {
			return fmt.Errorf("invalid proxy flag")
		}
		return r.DownloadCore(args[1], overwrite, proxy, args[4])
	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func moduleDir() string {
	if executable, err := os.Executable(); err == nil {
		if module, ok := executableModuleDir(executable); ok {
			return module
		}
	}
	standard := "/data/adb/modules/mono_box"
	lite := "/data/adb/lite_modules/mono_box"
	if _, err := os.Stat(lite); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		version, _ := exec.CommandContext(ctx, "magisk", "-v").Output()
		if strings.Contains(string(version), "lite") {
			return lite
		}
		if _, err := os.Stat(standard); os.IsNotExist(err) {
			return lite
		}
	}
	return standard
}

// A module-owned executable identifies the exact active module, including Lite
// installations. Explicit --module-dir remains available for installation tools.
func executableModuleDir(executable string) (string, bool) {
	executable = strings.TrimSuffix(executable, " (deleted)")
	bin := filepath.Dir(executable)
	if filepath.Base(bin) != "bin" {
		return "", false
	}
	module := filepath.Dir(bin)
	data, err := os.ReadFile(filepath.Join(module, "module.prop"))
	if err != nil || len(data) > 64*1024 {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "id=mono_box" {
			return module, true
		}
	}
	return "", false
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "Usage: monobox [--root DIR] [--module-dir DIR] <command>")
	fmt.Fprintln(w, "Commands: action [toggle|start|stop|restart|status|logs_size|clear_logs|switch_mode MODE|switch_tun BOOL|get_configs|upgrade_core]")
	fmt.Fprintln(w, "          service <start|stop|restart|status|logs_size|clear_logs>")
	fmt.Fprintln(w, "          controller [boot|EVENT DIR FILE], watch, watch-stop")
	fmt.Fprintln(w, "          config <read|write BASE64|patch BASE64_JSON>, module info")
	fmt.Fprintln(w, "          profiles <list|import NAME CONTENT|download URL NAME|update NAME|switch NAME KIND|delete NAME>")
	fmt.Fprintln(w, "          logs <read|open> BASE64_PATH, download-core KIND OVERWRITE PROXY ABI")
	fmt.Fprintln(w, "          choose-volume SECONDS (prints up or down; timeout exits 1)")
	fmt.Fprintln(w, "Profile NAME, CONTENT and URL arguments use base64 UTF-8.")
}
