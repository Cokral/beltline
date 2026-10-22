// The HTTP half of the daemon discovery: the authenticated version probe. The discovery checks stay in
// kit/launch, which is free of HTTP; this executable package supplies the probe they call.

package daemon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/kit/launch"
)

// daemonVersionProbeTimeout bounds how long the version request may take, end to end.
const daemonVersionProbeTimeout = 5 * time.Second

// newDaemonDiscovery builds the discovery checker the commands use: the live
// file, the live process table and the authenticated version probe.
// Lock: none.
func newDaemonDiscovery() launch.DaemonDiscovery {
	return launch.NewDaemonDiscovery(nil, nil, daemonVersionProbe)
}

// daemonVersionProbe is GET /api/v1/version with the discovery token as a bearer.
// Redirects, proxies and cookies are refused, and the request has
// daemonVersionProbeTimeout.
// Lock: none. Suspends: the network call, which the caller runs without any lock.
func daemonVersionProbe(ctx context.Context, info launch.DaemonInfo) (launch.DaemonDiscoveryReply, error) {
	ctx, cancel := context.WithTimeout(ctx, daemonVersionProbeTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/v1/version", info.Port), nil)
	if err != nil {
		return launch.DaemonDiscoveryReply{}, daemonVersionProbeError(daemonVersionProbeBadURL)
	}
	request.Header.Set("Authorization", "Bearer "+info.Token)
	// No proxy (the transport has none), no cookie jar, and redirects are answered, not followed.
	client := &http.Client{
		Timeout:       daemonVersionProbeTimeout,
		Transport:     &http.Transport{},
		CheckRedirect: daemonVersionRefuseRedirects,
	}
	response, err := client.Do(request)
	if err != nil {
		return launch.DaemonDiscoveryReply{}, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return launch.DaemonDiscoveryReply{}, err
	}
	return launch.DaemonDiscoveryReply{Status: response.StatusCode, Body: body}, nil
}

// daemonVersionProbeError is a probe that could not build or read its request.
type daemonVersionProbeError string

const (
	// daemonVersionProbeBadURL: the request URL could not be built.
	daemonVersionProbeBadURL daemonVersionProbeError = "badURL"
)

// Error names the probe failure.
func (e daemonVersionProbeError) Error() string { return string(e) }

// daemonVersionRefuseRedirects is the redirect policy of the probe: a redirect is
// answered as it is, never followed.
// Lock: none (pure).
func daemonVersionRefuseRedirects(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}
