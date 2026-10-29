package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

type proxyDelayRequest struct {
	Port         int      `json:"port"`
	Secret       string   `json:"secret"`
	Name         string   `json:"name"`
	Aliases      []string `json:"aliases"`
	ProviderName string   `json:"providerName"`
	URL          string   `json:"url"`
	Timeout      *float64 `json:"timeout"`
}

var missingProxyPattern = regexp.MustCompile(`(?i)\b(resource|proxy)\b.*\bnot found\b|\bnot found\b.*\b(resource|proxy)\b`)

func (r *Runtime) ProxyDelay(encoded string) error {
	data, err := base.DecodeText(encoded, 256<<10)
	if err != nil {
		return err
	}
	var request proxyDelayRequest
	if err = json.Unmarshal(data, &request); err != nil {
		return errors.New("invalid proxy delay request")
	}
	if request.Port < 1 || request.Port > 65535 || request.Name == "" {
		return errors.New("proxy delay requires a valid port and name")
	}
	if request.URL == "" {
		request.URL = "http://cp.cloudflare.com/generate_204"
	}
	parsed, err := url.Parse(request.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("probe URL must use HTTP or HTTPS")
	}
	timeout := 5000
	if request.Timeout != nil && !math.IsNaN(*request.Timeout) && !math.IsInf(*request.Timeout, 0) {
		timeout = int(math.Max(1, math.Min(32767, math.Trunc(*request.Timeout))))
	}
	candidates := []string{}
	seen := map[string]bool{}
	for _, name := range append([]string{request.Name}, request.Aliases...) {
		if name != "" && !seen[name] {
			candidates = append(candidates, name)
			seen[name] = true
		}
	}
	last := "Resource not found"
	for _, name := range candidates {
		path := "/proxies/" + url.PathEscape(name) + "/delay"
		if request.ProviderName != "" {
			path = "/providers/proxies/" + url.PathEscape(request.ProviderName) + "/" + url.PathEscape(name) + "/healthcheck"
		}
		query := url.Values{"url": {request.URL}, "timeout": {strconv.Itoa(timeout)}}
		endpoint := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(request.Port)) + path + "?" + query.Encode()
		delay, message, status, err := r.probeDelay(endpoint, request.Secret, time.Duration((timeout+999)/1000+2)*time.Second)
		if err != nil {
			return err
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return fmt.Errorf("Clash API HTTP %d: %s", status, message)
		}
		if status == http.StatusNotFound || missingProxyPattern.MatchString(message) {
			if message != "" {
				last = message
			}
			continue
		}
		probeFailed := strings.Contains(strings.ToLower(message), "error occurred in the delay test") || message == "Timeout"
		if probeFailed && (status == 503 || status == 504 || status >= 200 && status < 300) {
			return r.writeDelay(0)
		}
		if status < 200 || status >= 300 {
			return fmt.Errorf("Clash API HTTP %d: %s", status, message)
		}
		if message != "" {
			return errors.New(message)
		}
		if delay == nil || math.IsNaN(*delay) || math.IsInf(*delay, 0) || *delay <= 0 {
			return errors.New("Clash API did not return a valid delay")
		}
		return r.writeDelay(*delay)
	}
	return fmt.Errorf("%s (tried: %s)", last, strings.Join(candidates, " / "))
}

func (r *Runtime) probeDelay(endpoint, secret string, timeout time.Duration) (*float64, string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", 0, err
	}
	if secret != "" {
		request.Header.Set("Authorization", "Bearer "+secret)
	}
	client := http.Client{}
	if r.HTTP != nil {
		client = *r.HTTP
	} else {
		client = *base.NewHTTPClient()
	}
	client.Timeout = timeout
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if t, ok := transport.(*http.Transport); ok {
		local := t.Clone()
		local.Proxy = nil
		defer local.CloseIdleConnections()
		transport = local
	}
	client.Transport = transport
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return nil, "", 0, fmt.Errorf("proxy delay request: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return nil, "", 0, err
	}
	if len(data) > 1<<20 {
		return nil, "", 0, errors.New("proxy delay response exceeds 1 MiB")
	}
	var result struct {
		Delay   *float64 `json:"delay"`
		Message string   `json:"message"`
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, "", 0, fmt.Errorf("Clash API returned invalid delay JSON (HTTP %d)", response.StatusCode)
	}
	if len(result.Message) > 512 {
		result.Message = result.Message[:512]
	}
	return result.Delay, result.Message, response.StatusCode, nil
}
func (r *Runtime) writeDelay(delay float64) error {
	if r.Out == nil {
		return nil
	}
	return json.NewEncoder(r.Out).Encode(map[string]float64{"delay": delay})
}
