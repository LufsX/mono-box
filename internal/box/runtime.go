package box

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Runtime contains the device paths and I/O used by the native controller.
type Runtime struct {
	Root      string
	ModuleDir string
	Config    Config
	Out       io.Writer
	Err       io.Writer
	HTTP      *http.Client
}

func New(root, moduleDir string, out, errOut io.Writer) (*Runtime, error) {
	c, err := LoadConfig(filepath.Join(root, "scripts", "box.config"))
	if err != nil {
		c = DefaultConfig()
	}
	client := newHTTPClient()
	client.Timeout = 60 * time.Second
	return &Runtime{Root: root, ModuleDir: moduleDir, Config: c, Out: out, Err: errOut,
		HTTP: client}, err
}
func (r *Runtime) RunDir() string     { return filepath.Join(r.Root, "run") }
func (r *Runtime) CoreDir() string    { return filepath.Join(r.Root, r.Config.BinName) }
func (r *Runtime) CoreBinary() string { return r.serviceBinary(r.Root) }
func (r *Runtime) PIDFile() string    { return filepath.Join(r.RunDir(), r.Config.BinName+".pid") }
func (r *Runtime) ActiveConfig() string {
	data, _ := os.ReadFile(filepath.Join(r.CoreDir(), ".mono-box", "active-profile"))
	name := strings.TrimRight(string(data), "\r\n")
	if ValidProfileName(name) {
		p := filepath.Join(r.CoreDir(), ".mono-box", "profiles", name)
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			return p
		}
	}
	return filepath.Join(r.CoreDir(), "config.yaml")
}
func exists(path string) bool { _, err := os.Stat(path); return err == nil }

// AtomicWrite replaces a file only after the complete contents reach disk.
func AtomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".monobox-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
