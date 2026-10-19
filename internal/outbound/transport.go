// Package outbound routes application HTTP connections through administrator
// selected proxies. It never changes destinations, redirects or TLS validation.
package outbound

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"

	"github.com/theo-bnts/aiosources/internal/model"
)

var ErrUnavailable = errors.New("Proxy connection unavailable. Check Network settings")
var ErrSettings = errors.New("Network settings could not be loaded")

type DialFunc func(context.Context, string, string) (net.Conn, error)
type dialKey struct{}
type targetKey struct{}

// Dial preserves caller-owned address checks: protected callers resolve and pin
// their destination before invoking it. The proxy receives that validated IP.
func Dial(ctx context.Context, network, address string, direct DialFunc) (net.Conn, error) {
	if selected, ok := ctx.Value(dialKey{}).(DialFunc); ok {
		return selected(ctx, network, address)
	}
	return direct(ctx, network, address)
}

func WithTarget(ctx context.Context, target string) context.Context {
	return context.WithValue(ctx, targetKey{}, target)
}

func HTTPTransport() *http.Transport {
	base := http.DefaultTransport.(*http.Transport).Clone()
	direct := base.DialContext
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return Dial(ctx, network, address, direct)
	}
	return base
}

type Router struct {
	Settings func(context.Context) (model.NetworkSettings, error)
	Secrets  model.SecretResolver
}

// Transport retains each caller's HTTP limits and destination guards. Callers
// with a custom dialer must use Dial after performing their address validation.
func (r *Router) Transport(target string, base *http.Transport) *Transport {
	return &Transport{router: r, target: target, base: base, transports: make(map[[32]byte]*http.Transport)}
}

type Transport struct {
	router     *Router
	target     string
	base       *http.Transport
	mu         sync.Mutex
	revision   int64
	transports map[[32]byte]*http.Transport
}

func (t *Transport) RoundTrip(request *http.Request) (*http.Response, error) {
	ctx := request.Context()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t.router == nil || t.router.Settings == nil {
		return nil, ErrSettings
	}
	settings, err := t.router.Settings(ctx)
	if err != nil || settings.Validate() != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrSettings
	}
	target := t.target
	if value, ok := ctx.Value(targetKey{}).(string); ok && value != "" {
		target = value
	}
	route := settings.Route(target)
	var selected DialFunc
	identity := route
	if route != model.NetworkSystem && route != model.NetworkDirect {
		var profile *model.NetworkProxy
		for i := range settings.Proxies {
			if settings.Proxies[i].ID == route {
				profile = &settings.Proxies[i]
				break
			}
		}
		if profile == nil || !profile.Enabled {
			return nil, ErrUnavailable
		}
		u, _ := url.Parse(profile.URL) // The complete saved object was validated above.
		username, password := "", ""
		if profile.UsernameRef != "" {
			if t.router.Secrets == nil {
				return nil, ErrUnavailable
			}
			username, err = t.router.Secrets(ctx, profile.UsernameRef)
			if err == nil {
				password, err = t.router.Secrets(ctx, profile.PasswordRef)
			}
			if err != nil || username == "" || password == "" {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, ErrUnavailable
			}
		}
		selected, err = proxyDialer(u, username, password)
		if err != nil {
			return nil, ErrUnavailable
		}
		// Distinct credentials never share authenticated proxy connections.
		identity = fmt.Sprintf("%s\x00%s\x00%s\x00%s", route, profile.URL, username, password)
	}
	key := sha256.Sum256([]byte(identity))
	t.mu.Lock()
	if t.revision != settings.Revision || len(t.transports) > 32 {
		for _, transport := range t.transports {
			transport.CloseIdleConnections()
		}
		clear(t.transports)
		t.revision = settings.Revision
	}
	transport := t.transports[key]
	if transport == nil {
		transport = t.base.Clone()
		if route != model.NetworkSystem {
			transport.Proxy = nil
		}
		t.transports[key] = transport
	}
	t.mu.Unlock()
	if selected != nil {
		request = request.Clone(context.WithValue(ctx, dialKey{}, selected))
	}
	return transport.RoundTrip(request)
}

func (t *Transport) CloseIdleConnections() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, transport := range t.transports {
		transport.CloseIdleConnections()
	}
	t.base.CloseIdleConnections()
}
