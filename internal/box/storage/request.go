package storage

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
)

const requestLimit = 16 << 20
const requestChunkLimit = 48 << 10

// Request transports large UI payloads in bounded CLI arguments. The random
// token identifies a private staging file, consumed before executing its JSON.
func (r *Runtime) Request(args []string, execute func([]string) error) error {
	if len(args) == 0 {
		return errors.New("missing request command")
	}
	run, err := openStorageDir(r.RunDir(), true)
	if err != nil {
		return err
	}
	defer run.Close()
	dir, err := run.child("requests")
	if err != nil {
		return err
	}
	defer dir.Close()
	lock, err := dir.open("lock", syscall.O_CREAT|syscall.O_RDWR)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if args[0] == "begin" {
		if len(args) != 1 {
			return errors.New("begin takes no arguments")
		}
		entries, err := os.ReadDir(dir.path("."))
		if err != nil {
			return err
		}
		active := 0
		for _, entry := range entries {
			if !validRequestToken(entry.Name()) {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if time.Since(info.ModTime()) > time.Hour {
				if err = dir.remove(entry.Name()); err != nil {
					return err
				}
			} else {
				active++
			}
		}
		if active >= 8 {
			return errors.New("too many pending requests")
		}
		tokenBytes := make([]byte, 16)
		if _, err = rand.Read(tokenBytes); err != nil {
			return err
		}
		token := hex.EncodeToString(tokenBytes)
		f, err := dir.open(token, syscall.O_CREAT|syscall.O_EXCL|syscall.O_WRONLY)
		if err != nil {
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		if _, err = fmt.Fprintln(r.Out, token); err != nil {
			_ = dir.remove(token)
			return err
		}
		return nil
	}
	if len(args) < 2 || !validRequestToken(args[1]) {
		return errors.New("invalid request token")
	}
	token := args[1]
	switch args[0] {
	case "append":
		if len(args) != 3 || len(args[2]) > requestChunkLimit {
			return errors.New("invalid request chunk")
		}
		data, err := base64.StdEncoding.Strict().DecodeString(args[2])
		if err != nil {
			return errors.New("invalid request base64")
		}
		f, err := dir.open(token, syscall.O_WRONLY|syscall.O_APPEND)
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if info.Size()+int64(len(data)) > requestLimit {
			return errors.New("request exceeds 16 MiB")
		}
		_, err = f.Write(data)
		return err
	case "abort":
		if len(args) != 2 {
			return errors.New("abort takes one token")
		}
		return dir.remove(token)
	case "run":
		if len(args) != 2 {
			return errors.New("run takes one token")
		}
		data, err := dir.read(token, requestLimit)
		if err != nil {
			return err
		}
		if err = dir.remove(token); err != nil {
			return err
		}
		var command []string
		if err = json.Unmarshal(data, &command); err != nil || len(command) == 0 || len(command) > 16 {
			return errors.New("invalid request JSON command")
		}
		// Only payload-bearing storage operations use this transport.
		if command[0] != "config" && command[0] != "profiles" {
			return errors.New("request command must be config or profiles")
		}
		if execute == nil {
			return errors.New("request executor unavailable")
		}
		// Release staging lock before entering service/config locks.
		if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
			return err
		}
		return execute(command)
	default:
		return errors.New("unknown request command")
	}
}
func validRequestToken(token string) bool {
	if len(token) != 32 || strings.ToLower(token) != token {
		return false
	}
	_, err := hex.DecodeString(token)
	return err == nil
}
