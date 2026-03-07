package portalapp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gosuda/portal/v2/sdk"
	"github.com/gosuda/portal/v2/types"
)

type LeaseConfig struct {
	ServerURLs  []string
	Name        string
	Metadata    types.LeaseMetadata
	ReadyTarget int
	LeaseTTL    time.Duration
}

type Lease struct {
	listener   net.Listener
	clients    []*sdk.Client
	relayURLs  []string
	publicURLs []string
	closeOnce  sync.Once
}

func (l *Lease) Listener() net.Listener {
	if l == nil {
		return nil
	}
	return l.listener
}

func (l *Lease) RelayURLs() []string {
	if l == nil {
		return nil
	}
	return append([]string(nil), l.relayURLs...)
}

func (l *Lease) PublicURLs() []string {
	if l == nil {
		return nil
	}
	return append([]string(nil), l.publicURLs...)
}

func (l *Lease) Close() error {
	if l == nil {
		return nil
	}

	var closeErr error
	l.closeOnce.Do(func() {
		if l.listener != nil {
			closeErr = l.listener.Close()
		}
		for _, client := range l.clients {
			if client != nil {
				client.Close()
			}
		}
	})
	return closeErr
}

func ListenAll(ctx context.Context, cfg LeaseConfig) (*Lease, error) {
	relayURLs, err := NormalizeRelayURLs(cfg.ServerURLs)
	if err != nil {
		return nil, err
	}
	if len(relayURLs) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(cfg.Name) == "" {
		return nil, errors.New("lease name is required")
	}

	clients := make([]*sdk.Client, 0, len(relayURLs))
	listeners := make([]net.Listener, 0, len(relayURLs))
	publicURLs := make([]string, 0, len(relayURLs))
	cleanup := func() {
		for _, listener := range listeners {
			if listener != nil {
				_ = listener.Close()
			}
		}
		for _, client := range clients {
			if client != nil {
				client.Close()
			}
		}
	}

	for _, relayURL := range relayURLs {
		client, err := sdk.NewClient(relayURL)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("new client %q: %w", relayURL, err)
		}
		listener, err := client.Listen(ctx, sdk.ListenRequest{
			Name:        cfg.Name,
			Metadata:    cfg.Metadata,
			ReadyTarget: cfg.ReadyTarget,
			LeaseTTL:    cfg.LeaseTTL,
		})
		if err != nil {
			client.Close()
			cleanup()
			return nil, fmt.Errorf("listen %q: %w", relayURL, err)
		}

		clients = append(clients, client)
		listeners = append(listeners, listener)
		publicURLs = append(publicURLs, listener.PublicURLs()...)
	}

	merged, err := mergeRelayListeners(listeners)
	if err != nil {
		cleanup()
		return nil, err
	}

	return &Lease{
		listener:   merged,
		clients:    clients,
		relayURLs:  relayURLs,
		publicURLs: dedupe(publicURLs),
	}, nil
}

func RunHTTP(ctx context.Context, lease *Lease, handler http.Handler, localAddr string) error {
	localAddr = strings.TrimSpace(localAddr)
	if lease != nil && lease.Listener() != nil {
		return sdk.RunHTTP(ctx, lease.Listener(), handler, localAddr)
	}
	if localAddr == "" {
		return errors.New("relay listener or local address is required")
	}

	listener, err := net.Listen("tcp", localAddr)
	if err != nil {
		return fmt.Errorf("listen local http %q: %w", localAddr, err)
	}
	return sdk.RunHTTP(ctx, listener, handler, "")
}

func LocalAddrFromPort(port int) string {
	if port < 0 {
		return ""
	}
	return fmt.Sprintf(":%d", port)
}

func CleanServerURLs(inputs []string) []string {
	out := make([]string, 0, len(inputs))
	for _, input := range inputs {
		for _, part := range strings.Split(input, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func NormalizeRelayURLs(inputs []string) ([]string, error) {
	cleaned := CleanServerURLs(inputs)
	if len(cleaned) == 0 {
		return nil, nil
	}

	out := make([]string, 0, len(cleaned))
	seen := make(map[string]struct{}, len(cleaned))
	for _, raw := range cleaned {
		normalized, err := NormalizeRelayURL(raw)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out, nil
}

func NormalizeRelayURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("relay url is empty")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + strings.TrimPrefix(trimmed, "//")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse relay url %q: %w", raw, err)
	}
	if parsed.Host == "" && parsed.Path != "" && !strings.Contains(parsed.Path, "/") {
		parsed, err = url.Parse("https://" + strings.TrimSpace(parsed.Path))
		if err != nil {
			return "", fmt.Errorf("parse relay url %q: %w", raw, err)
		}
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("relay url host is empty: %q", raw)
	}

	switch strings.ToLower(parsed.Scheme) {
	case "wss":
		parsed.Scheme = "https"
	case "ws":
		parsed.Scheme = "http"
	case "https", "http":
	default:
		return "", fmt.Errorf("unsupported relay url scheme %q in %q", parsed.Scheme, raw)
	}

	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(strings.ToLower(parsed.Path), "/relay") {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/relay")
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.String(), nil
}

func SplitCSV(raw string) []string {
	return sdk.SplitCSV(raw)
}

// mergeRelayListeners delegates multi-relay fan-in to sdk.MergeListeners in
// sdk/helper.go so the SDK owns the accept/close behavior.
func mergeRelayListeners(listeners []net.Listener) (net.Listener, error) {
	if len(listeners) == 0 {
		return nil, errors.New("at least one listener is required")
	}
	if len(listeners) == 1 {
		return listeners[0], nil
	}
	merged, err := sdk.MergeListeners(listeners...)
	if err != nil {
		return nil, fmt.Errorf("merge listeners: %w", err)
	}
	return merged, nil
}

func dedupe(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
