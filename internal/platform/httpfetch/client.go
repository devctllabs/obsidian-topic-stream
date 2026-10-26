package httpfetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultTimeout = 30 * time.Second

// NewClient validates every request, including redirects, and connects to the same resolved IPs it
// checked so DNS changes cannot bypass the private-network policy between validation and dialing.
func NewClient(allowPrivateNetwork bool) *http.Client {
	return NewClientWithLimiter(allowPrivateNetwork, nil)
}

// NewClientWithLimiter applies the outbound network policy and optional shared per-host concurrency limit.
func NewClientWithLimiter(allowPrivateNetwork bool, limiter *HostLimiter) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = secureDialContext(allowPrivateNetwork)
	return &http.Client{
		Timeout:   defaultTimeout,
		Transport: guardedTransport{next: transport, limiter: limiter},
	}
}

// HostLimiter shares a concurrency limit per hostname across clients; permits last until response bodies
// close.
type HostLimiter struct {
	limit int
	// mu protects the hostname-to-semaphore map; network I/O never runs under this lock.
	mu    sync.Mutex
	hosts map[string]chan struct{}
}

// NewHostLimiter creates a shared per-host request limit; limit must be positive.
func NewHostLimiter(limit int) (*HostLimiter, error) {
	if limit < 1 {
		return nil, errors.New("per-host concurrency must be positive")
	}
	return &HostLimiter{limit: limit, hosts: make(map[string]chan struct{})}, nil
}

func (l *HostLimiter) acquire(ctx context.Context, host string) (func(), error) {
	l.mu.Lock()
	semaphore, found := l.hosts[host]
	if !found {
		semaphore = make(chan struct{}, l.limit)
		l.hosts[host] = semaphore
	}
	l.mu.Unlock()

	select {
	case semaphore <- struct{}{}:
		return func() { <-semaphore }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// guardedTransport applies URL policy and shares host permits across requests.
type guardedTransport struct {
	next    http.RoundTripper
	limiter *HostLimiter
}

// releaseBody owns a host permit until its response body is closed, releasing the permit at most once.
type releaseBody struct {
	io.ReadCloser
	release func()
	once    sync.Once
}

// Close closes the response body and releases its host permit exactly once.
func (b *releaseBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.release)
	return err
}

// RetryAfterError carries a retryable upstream status and its parsed Retry-After delay.
type RetryAfterError struct {
	statusCode int
	delay      time.Duration
}

// Error returns the caller-facing failure description.
func (e RetryAfterError) Error() string {
	return fmt.Sprintf("upstream returned status %d; retry later", e.statusCode)
}

// Delay returns the parsed Retry-After interval for scheduling another attempt.
func (e RetryAfterError) Delay() time.Duration { return e.delay }

// RoundTrip validates the URL, acquires a host permit, and exposes retryable Retry-After responses as
// errors.
func (t guardedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := validateRequestURL(request.URL); err != nil {
		return nil, err
	}
	release := func() {}
	if t.limiter != nil {
		var err error
		release, err = t.limiter.acquire(request.Context(), strings.ToLower(request.URL.Hostname()))
		if err != nil {
			return nil, err
		}
	}
	response, err := t.next.RoundTrip(request)
	if err != nil {
		release()
		return nil, err
	}
	if retryableStatus(response.StatusCode) {
		if delay, found := parseRetryAfter(response.Header.Get("Retry-After"), time.Now()); found {
			_ = response.Body.Close()
			release()
			return nil, RetryAfterError{statusCode: response.StatusCode, delay: delay}
		}
	}
	response.Body = &releaseBody{ReadCloser: response.Body, release: release}
	return response, nil
}

func retryableStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode == http.StatusServiceUnavailable
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0, false
	}
	return when.Sub(now), true
}

func validateRequestURL(target *url.URL) error {
	if target == nil || target.Hostname() == "" || target.Scheme != "http" && target.Scheme != "https" {
		return errors.New("outbound URL must be absolute HTTP(S)")
	}
	if target.User != nil {
		return errors.New("outbound URL must not contain credentials")
	}
	return nil
}

func secureDialContext(allowPrivateNetwork bool) func(context.Context, string, string) (net.Conn, error) {
	resolver := net.DefaultResolver
	dialer := &net.Dialer{Timeout: defaultTimeout, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("parse outbound address: %w", err)
		}
		addresses, err := resolve(ctx, resolver, host)
		if err != nil {
			return nil, err
		}
		if err := validateResolvedAddresses(host, addresses, allowPrivateNetwork); err != nil {
			return nil, err
		}
		return dialResolved(ctx, dialer, dialRequest{
			network: network, host: host, port: port, addresses: addresses,
		})
	}
}

func validateResolvedAddresses(host string, addresses []net.IP, allowPrivateNetwork bool) error {
	if allowPrivateNetwork {
		return nil
	}
	for _, address := range addresses {
		if prohibited(address) {
			return fmt.Errorf("outbound host %q resolves to a private or special-use address", host)
		}
	}
	return nil
}

type dialRequest struct {
	network   string
	host      string
	port      string
	addresses []net.IP
}

func dialResolved(ctx context.Context, dialer *net.Dialer, request dialRequest) (net.Conn, error) {
	var lastErr error
	for _, ip := range request.addresses {
		connection, err := dialer.DialContext(ctx, request.network, net.JoinHostPort(ip.String(), request.port))
		if err == nil {
			return connection, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("dial outbound host %q: %w", request.host, lastErr)
}

// ipResolver resolves hosts before enforcing the outbound network policy.
type ipResolver interface {
	// LookupIPAddr resolves the host with cancellation through ctx.
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

func resolve(ctx context.Context, resolver ipResolver, host string) ([]net.IP, error) {
	if parsed := net.ParseIP(host); parsed != nil {
		return []net.IP{parsed}, nil
	}
	resolved, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve outbound host %q: %w", host, err)
	}
	if len(resolved) == 0 {
		return nil, fmt.Errorf("outbound host %q has no addresses", host)
	}
	addresses := make([]net.IP, 0, len(resolved))
	for _, address := range resolved {
		addresses = append(addresses, address.IP)
	}
	return addresses, nil
}

func prohibited(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast()
}
