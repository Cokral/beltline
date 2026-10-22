.PHONY: check build test race quick lint web snapshots run demo install uninstall clean notices

# The version beltlined reports (API version and daemon.json); the LaunchAgent keeps it on install.
BELTLINE_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
export BELTLINE_VERSION

GO_LDFLAGS = -X github.com/robbertmanders/beltline/daemon/internal/daemon.buildVersion=$(BELTLINE_VERSION)

# beltlined with the web app compiled in, in build/bin. The web app is prepared by the
# recipe's own first line rather than as a prerequisite, so a parallel make still
# compiles only after it is ready, and a failed web build stops before anything is compiled.
build:
	scripts/web.sh
	cd daemon && go build -tags embedweb -ldflags "$(GO_LDFLAGS)" -o ../build/bin/beltlined ./cmd/beltlined

# Stages the production web app for the next embedweb build. It does not update an
# existing executable: run make build (or make install) to compile it in.
web:
	scripts/web.sh

snapshots:
	scripts/snapshots.sh

# The contract goldens hold clock labels and day boundaries in the maintainer's time zone, so the Go
# tests run in it wherever they run.
GO_TEST_TZ = TZ=Europe/Amsterdam

# The full suites take a machine-wide lock, so agents testing at the same time run them one after another
# instead of slowing each other into timeouts.
SUITE_LOCK = /tmp/beltline-suite.lock
# macOS has lockf(1); Linux has flock(1) instead.
ifeq ($(shell uname -s),Linux)
LOCKED = flock -n $(SUITE_LOCK) true 2>/dev/null || echo "Waiting for another test suite to finish ($(SUITE_LOCK))"; flock $(SUITE_LOCK) sh -c
else
LOCKED = lockf -t 0 $(SUITE_LOCK) true 2>/dev/null || echo "Waiting for another test suite to finish ($(SUITE_LOCK))"; lockf -k $(SUITE_LOCK) sh -c
endif

# Vet and every Go test except the tmux, PTY and stand-in agent ones, then the web typecheck and tests.
quick:
	@$(LOCKED) 'cd daemon && go vet ./... && $(GO_TEST_TZ) go test ./... && cd ../Web && npm run typecheck && npm test'

# Adds the integration tests (tmux, PTY, the stand-in agent in daemon/testdata/fixtures).
# Each tmux test starts a private server, beltline-test-<pid>-<n>, and removes it.
test:
	@$(LOCKED) 'cd daemon && go vet -tags integration ./... && $(GO_TEST_TZ) go test -tags integration ./... && cd ../Web && npm run typecheck && npm test'

# The pinned golangci-lint release (AGENTS.md says how to install it), and the one `make lint` runs.
GOLANGCI_LINT_VERSION = v2.14.0
GOLANGCI_LINT ?= golangci-lint

# gofmt -s over every tracked Go file (the formatter .golangci.yml names, checked here for the files a build
# tag leaves out of a package), then the daemon's packages and tests with and without the integration tag
# under .golangci.yml, for macOS and for Linux. It reports findings and never rewrites a file.
lint:
	@command -v "$(GOLANGCI_LINT)" >/dev/null 2>&1 || { echo "make lint: $(GOLANGCI_LINT) is not installed; install golangci-lint $(GOLANGCI_LINT_VERSION) (see AGENTS.md)" >&2; exit 1; }
	@found=$$("$(GOLANGCI_LINT)" version --short) || { echo "make lint: $(GOLANGCI_LINT) did not report its version" >&2; exit 1; }; [ "$$found" = "$(GOLANGCI_LINT_VERSION:v%=%)" ] || { echo "make lint: $(GOLANGCI_LINT) is version $$found; this check needs $(GOLANGCI_LINT_VERSION) (see AGENTS.md)" >&2; exit 1; }
	@set -e; files=$$(git ls-files -- 'daemon/*.go'); unformatted=$$(printf '%s\n' "$$files" | tr '\n' '\0' | xargs -0 -r gofmt -s -l); if [ -n "$$unformatted" ]; then echo "make lint: not gofmt -s formatted (run gofmt -s -w on these files):" >&2; echo "$$unformatted" >&2; exit 1; fi
	cd daemon && "$(GOLANGCI_LINT)" run --config ../.golangci.yml ./...
	cd daemon && "$(GOLANGCI_LINT)" run --config ../.golangci.yml --build-tags integration ./...
	cd daemon && GOOS=linux "$(GOLANGCI_LINT)" run --config ../.golangci.yml ./...
	cd daemon && GOOS=linux "$(GOLANGCI_LINT)" run --config ../.golangci.yml --build-tags integration ./...

# The same Go tests under the race detector, about ten times slower; not part of check.
race:
	cd daemon && $(GO_TEST_TZ) go test -race -timeout 30m -tags integration ./...

check: build test
	scripts/test-web-script.sh

# Starts beltlined on your data folder in the foreground and opens the web app; Ctrl-C stops it.
# Its tmux sessions keep running, and the next start picks them up.
run: build
	build/bin/beltlined --open

# The same on a fresh demo folder with fixture belts and runs, without touching GitHub or agents.
demo: build
	build/bin/beltlined --demo --open

# Builds beltlined with the web app compiled in, into build/release, and registers it as your
# LaunchAgent, so it starts at login; keep it there while installed. The web app is prepared
# first and registration follows a successful compile. `beltlined open` then opens the web app.
install:
	scripts/web.sh
	cd daemon && go build -trimpath -tags embedweb -ldflags "$(GO_LDFLAGS)" -o ../build/release/beltlined ./cmd/beltlined
	build/release/beltlined install

uninstall:
	build/release/beltlined uninstall

# Rewrites THIRD_PARTY_NOTICES from the Go modules and npm packages that ship (needs Web/node_modules).
notices:
	scripts/notices.sh

clean:
	rm -rf build daemon/internal/server/webdist
