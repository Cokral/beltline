// The BeltlineServer
// actor, its start-up errors and its phases.
package server

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// DefaultPort is BeltlineServer.defaultPort: the first port tried.
const DefaultPort = 7331

// listenHost is BeltlineServer.host: the only address the listener binds.
const listenHost = "127.0.0.1"

// StartErrorKind says which start-up error a StartError is.
type StartErrorKind string

// The StartErrorKind values (BeltlineServer.StartError).
const (
	StartErrorEmptyToken     StartErrorKind = "emptyToken"
	StartErrorEmptyVersion   StartErrorKind = "emptyVersion"
	StartErrorInvalidPort    StartErrorKind = "invalidPort"
	StartErrorAlreadyRunning StartErrorKind = "alreadyRunning"
	// StartErrorNoFreePort: every port from the starting one through 65535 was in use.
	StartErrorNoFreePort         StartErrorKind = "noFreePort"
	StartErrorStoppedBeforeReady StartErrorKind = "stoppedBeforeReady"
)

// StartError is a start-up failure. Port is set for invalidPort (the port
// given) and noFreePort (the port the search began at).
type StartError struct {
	Kind StartErrorKind
	Port int
}

// Error implements error for StartError.
func (e StartError) Error() string {
	switch e.Kind {
	case StartErrorEmptyToken:
		return "the server needs a token"
	case StartErrorEmptyVersion:
		return "the server needs a version"
	case StartErrorInvalidPort:
		return "port " + strconv.Itoa(e.Port) + " is outside 1 through 65535"
	case StartErrorAlreadyRunning:
		return "the server is already running"
	case StartErrorNoFreePort:
		return "no free port from " + strconv.Itoa(e.Port) + " through 65535"
	case StartErrorStoppedBeforeReady:
		return "the server stopped before it was ready"
	}
	return string(e.Kind)
}

// phase is the server's phase: idle, starting, running, or stopping.
type phase int

const (
	phaseIdle phase = iota
	phaseStarting
	phaseRunning
	phaseStopping
)

// BeltlineServer is the local HTTP API. It listens on IPv4 loopback only,
// starting at DefaultPort and moving up past ports already in use, and answers
// only requests that carry the token its creator gives it. Stopping it ends the
// listener and nothing else: coordinator work and sessions carry on.
//
// The actor's isolation becomes mu: Start, Stop and the accessors take it.
type BeltlineServer struct {
	mu sync.Mutex
	// Version is the daemon's version string, served by GET api/v1/version.
	Version string
	token   string
	// StartingPort is the first port tried.
	StartingPort int
	bindHost     string
	coordinator  *coordinator.Coordinator
	eventWrite   routes.EventWriteConfiguration
	// configure adds test-only routes to the mux; nil in production.
	configure func(mux *http.ServeMux)
	// WebRoot is the directory override for the web app (BELTLINE_WEB_DIR); empty
	// serves the app compiled into beltlined.
	WebRoot            string
	artifactSigningKey []byte
	artifactClock      func() time.Time
	deps               routes.Dependencies
	phase              phase
	listener           *listener
	// starting is the start attempt in progress while phase is phaseStarting.
	starting *startAttempt
	// stopping is closed when the stop in progress has finished.
	stopping chan struct{}
}

// startAttempt is one Start in progress: its cancel, and the listener it made
// once done is closed (nil when binding failed or was cancelled).
type startAttempt struct {
	cancel   context.CancelFunc
	done     chan struct{}
	listener *listener
}

// NewBeltlineServer builds the beltline server from its arguments:
// the production constructor. A zero startingPort is DefaultPort and an empty
// webRoot is the default root.
func NewBeltlineServer(token, version string, c *coordinator.Coordinator, startingPort int, webRoot string, deps routes.Dependencies) *BeltlineServer {
	if startingPort == 0 {
		startingPort = DefaultPort
	}
	if webRoot == "" {
		webRoot = defaultWebRoot()
	}
	return &BeltlineServer{
		token:         token,
		Version:       version,
		StartingPort:  startingPort,
		bindHost:      listenHost,
		coordinator:   c,
		eventWrite:    routes.EventWriteConfigurationProduction,
		WebRoot:       webRoot,
		artifactClock: time.Now,
		deps:          deps,
	}
}

// defaultWebRoot is StaticWeb.defaultRoot() for the running process: the override
// from BELTLINE_WEB_DIR, or empty for the bundled app.
func defaultWebRoot() string {
	return StaticWebDefaultRoot(os.Getenv)
}

// newBeltlineServerForTest builds the server with a starting port, bind host and web root (test-only).
// used by the lifecycle and listener tests: a bind host, an optional coordinator,
// the event write configuration, a signing key and clock, and a test configure hook.
func newBeltlineServerForTest(token, version string, startingPort int, bindHost, webRoot string,
	c *coordinator.Coordinator, eventWrite routes.EventWriteConfiguration, signingKey []byte,
	clock func() time.Time, deps routes.Dependencies, configure func(mux *http.ServeMux)) *BeltlineServer {
	if webRoot == "" {
		webRoot = defaultWebRoot()
	}
	if clock == nil {
		clock = time.Now
	}
	return &BeltlineServer{
		token:              token,
		Version:            version,
		StartingPort:       startingPort,
		bindHost:           bindHost,
		WebRoot:            webRoot,
		coordinator:        c,
		eventWrite:         eventWrite,
		artifactSigningKey: signingKey,
		artifactClock:      clock,
		deps:               deps,
		configure:          configure,
	}
}

// Port is the port it listens on while running, and
// false when it is not running.
func (s *BeltlineServer) Port() (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase == phaseRunning && s.listener != nil {
		return s.listener.port, true
	}
	return 0, false
}

// BoundAddress is the address the listening
// socket is bound to, while running.
func (s *BeltlineServer) BoundAddress() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase == phaseRunning && s.listener != nil {
		return s.listener.address, true
	}
	return "", false
}

// Start starts listening and returns the bound
// port once requests can be served. It fails with a StartError when already
// running, when the token or version is empty, or when the port is out of range.
func (s *BeltlineServer) Start(ctx context.Context) (int, error) {
	s.mu.Lock()
	if s.phase != phaseIdle {
		s.mu.Unlock()
		return 0, StartError{Kind: StartErrorAlreadyRunning}
	}
	if s.token == "" {
		s.mu.Unlock()
		return 0, StartError{Kind: StartErrorEmptyToken}
	}
	if s.Version == "" {
		s.mu.Unlock()
		return 0, StartError{Kind: StartErrorEmptyVersion}
	}
	if s.StartingPort < 1 || s.StartingPort > 65535 {
		s.mu.Unlock()
		return 0, StartError{Kind: StartErrorInvalidPort, Port: s.StartingPort}
	}
	key := s.artifactSigningKey
	if key == nil {
		key = freshKey()
	}
	cfg := listenConfig{
		token:        s.token,
		version:      s.Version,
		startingPort: s.StartingPort,
		host:         s.bindHost,
		webRoot:      s.WebRoot,
		coordinator:  s.coordinator,
		eventWrite:   s.eventWrite,
		signer:       routes.ArtifactLinkSigner{Key: key, Clock: s.artifactClock},
		deps:         s.deps,
		configure:    s.configure,
	}
	bindCtx, cancel := context.WithCancel(ctx)
	attempt := &startAttempt{cancel: cancel, done: make(chan struct{})}
	s.phase = phaseStarting
	s.starting = attempt
	s.mu.Unlock()

	l, err := listen(bindCtx, cfg)
	attempt.listener = l
	close(attempt.done)

	s.mu.Lock()
	defer s.mu.Unlock()
	cancel()
	if s.starting != attempt {
		// Stop took over while binding and tears the listener down itself.
		return 0, context.Canceled
	}
	s.starting = nil
	if err != nil {
		s.phase = phaseIdle
		return 0, err
	}
	s.listener = l
	s.phase = phaseRunning
	return l.port, nil
}

// Stop stops listening and waits until the
// listener and its goroutines have ended and the port is free. Harmless when not running.
func (s *BeltlineServer) Stop(ctx context.Context) {
	s.mu.Lock()
	switch s.phase {
	case phaseIdle:
		s.mu.Unlock()
	case phaseStopping:
		done := s.stopping
		s.mu.Unlock()
		<-done
	case phaseStarting:
		attempt := s.starting
		s.starting = nil
		done := s.beginStopping()
		s.mu.Unlock()
		attempt.cancel()
		<-attempt.done
		if attempt.listener != nil {
			attempt.listener.shutdown(ctx)
		}
		s.endStopping(done)
	case phaseRunning:
		l := s.listener
		s.listener = nil
		done := s.beginStopping()
		s.mu.Unlock()
		l.shutdown(ctx)
		s.endStopping(done)
	default:
		s.mu.Unlock()
	}
}

// beginStopping moves the server to phaseStopping and returns the channel that
// closes when the stop finishes. It runs with s.mu held.
func (s *BeltlineServer) beginStopping() chan struct{} {
	done := make(chan struct{})
	s.phase = phaseStopping
	s.stopping = done
	return done
}

// endStopping returns the server to idle and releases the stops waiting on done.
func (s *BeltlineServer) endStopping(done chan struct{}) {
	s.mu.Lock()
	s.phase = phaseIdle
	s.stopping = nil
	s.mu.Unlock()
	close(done)
}

// PublishNotification is the publish notification:
// delivers an already-admitted coordinator notification to the current listener's
// clients. The daemon owns the corresponding log append.
func (s *BeltlineServer) PublishNotification(title, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil || s.listener.events == nil {
		return
	}
	s.listener.events.PublishNotification(title, body)
}

// EventClientCount is the number of event clients connected to the running
// listener, zero when it is not running. Tests wait on it.
func (s *BeltlineServer) EventClientCount() int {
	state := s.eventDebugState()
	if state == nil {
		return 0
	}
	return state.Clients
}

// eventDebugState is the event hub's debug
// state while running, nil otherwise.
func (s *BeltlineServer) eventDebugState() *EventHubDebugState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil || s.listener.events == nil {
		return nil
	}
	state := s.listener.events.DebugState()
	return &state
}

// listen tries each port from the starting one
// up, building the routes for it, until a bind succeeds; a port in use moves on.
// It fails with StartErrorNoFreePort when every port is taken.
func listen(ctx context.Context, cfg listenConfig) (*listener, error) {
	hub := NewEventHub(cfg.coordinator)
	for port := cfg.startingPort; port <= 65535; port++ {
		if err := ctx.Err(); err != nil {
			hub.Stop()
			return nil, err
		}
		router := ServerApplicationRouter(port, cfg.token, cfg.version, cfg.webRoot, cfg.coordinator,
			cfg.signer, cfg.deps, cfg.configure)
		rejections := NewEventRejectionRegistry(cfg.eventWrite)
		terminals := newTerminalTracker()
		upgrade := ServerApplicationWebSocket(port, cfg.token, cfg.coordinator, hub, rejections, terminals)
		handler := HTTPProtocolErrorAwareChannel{next: upgradeOrRouter{upgrade: upgrade, router: router}}
		l, err := listenerBind(ctx, handler, cfg.host, port, hub)
		if err == nil {
			l.terminals = terminals
			return l, nil
		}
		if errors.Is(err, syscall.EADDRINUSE) {
			continue
		}
		hub.Stop()
		return nil, err
	}
	hub.Stop()
	return nil, StartError{Kind: StartErrorNoFreePort, Port: cfg.startingPort}
}

// freshKey 32 random bytes for the artifact signer.
func freshKey() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("beltline: no randomness for the artifact signing key: " + err.Error())
	}
	return key
}

// listenConfig is the argument bundle of listen.
type listenConfig struct {
	token        string
	version      string
	startingPort int
	host         string
	webRoot      string
	coordinator  *coordinator.Coordinator
	eventWrite   routes.EventWriteConfiguration
	signer       routes.ArtifactLinkSigner
	deps         routes.Dependencies
	configure    func(mux *http.ServeMux)
}
