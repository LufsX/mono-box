package automation

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"

	"github.com/LufsX/mono-box/internal/box/base"
)

type Runtime struct{ *base.Runtime }

func New(r *base.Runtime) *Runtime { return &Runtime{Runtime: r} }

type Settings struct {
	RecoveryEnabled     bool `json:"recoveryEnabled"`
	UpdateEnabled       bool `json:"updateEnabled"`
	UpdateIntervalHours int  `json:"updateIntervalHours"`
	UpdateWifiOnly      bool `json:"updateWifiOnly"`
}

type Recovery struct {
	State         string `json:"state"`
	Attempts      int    `json:"attempts"`
	NextAttemptAt int64  `json:"nextAttemptAt"`
	LastError     string `json:"lastError"`
}

type Update struct {
	State         string `json:"state"`
	LastAttemptAt int64  `json:"lastAttemptAt"`
	LastSuccessAt int64  `json:"lastSuccessAt"`
	NextRunAt     int64  `json:"nextRunAt"`
	LastError     string `json:"lastError"`
	Updated       int    `json:"updated"`
}

type State struct {
	Settings Settings `json:"settings"`
	Recovery Recovery `json:"recovery"`
	Update   Update   `json:"update"`
}

func defaultSettings() Settings { return Settings{UpdateIntervalHours: 24, UpdateWifiOnly: true} }

func parseSettings(data []byte) (Settings, error) {
	settings := defaultSettings()
	var fields map[string]json.RawMessage
	if len(data) > 4096 || json.Unmarshal(data, &fields) != nil || fields == nil {
		return settings, errors.New("invalid automation settings JSON")
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return settings, errors.New("automation settings cannot be null")
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&settings) != nil || settings.UpdateIntervalHours < 1 || settings.UpdateIntervalHours > 168 {
		return settings, errors.New("invalid automation settings; updateIntervalHours must be 1..168")
	}
	return settings, nil
}

func (r *Runtime) Command(args []string) error {
	var state State
	var err error
	switch {
	case len(args) == 1 && args[0] == "get":
		state, err = r.Get()
	case len(args) == 2 && args[0] == "set":
		if len(args[1]) > 5500 {
			return errors.New("automation settings exceed size limit")
		}
		data, decodeErr := base64.StdEncoding.Strict().DecodeString(args[1])
		if decodeErr != nil {
			return errors.New("invalid automation settings base64")
		}
		settings, parseErr := parseSettings(data)
		if parseErr != nil {
			return parseErr
		}
		state, err = r.set(settings)
	default:
		return errors.New("automation expects get or set <base64 JSON settings>")
	}
	if err != nil {
		return err
	}
	out := r.Out
	if out == nil {
		out = io.Discard
	}
	return json.NewEncoder(out).Encode(state)
}
