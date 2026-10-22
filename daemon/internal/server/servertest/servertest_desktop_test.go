// The desktop commands' targets, their exact open arguments, their refusals and the guards that run
// before any launch. The fixture's Commands recorder stands for /usr/bin/open; its calls are the launches.
package servertest

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/kit/launch"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// desktopOpen is the open command the desktop routes run.
const desktopOpen = "/usr/bin/open"

// desktopPost sends one desktop command with a JSON object body of string members, under the contract token.
func desktopPost(t testing.TB, f *Fixture, path string, payload map[string]string) Reply {
	t.Helper()
	body, err := jsonx.Marshal(payload)
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	return f.Do(Request{Method: "POST", Path: path, Token: ContractToken, Body: body})
}

// desktopCallsFrom is the launches recorded from call index n on.
func desktopCallsFrom(f *Fixture, n int) []shell.Call {
	calls := f.Commands.Calls()
	if n > len(calls) {
		return nil
	}
	return calls[n:]
}

// wantOpen is the launch the open command makes with args.
func wantOpen(args ...string) shell.Call {
	return shell.Call{Executable: desktopOpen, Args: args}
}

// sameCall reports whether a recorded launch is the wanted one (the working directory and input are not part of a launch's identity).
func sameCall(got, want shell.Call) bool {
	return got.Executable == want.Executable && slices.Equal(got.Args, want.Args)
}

// failOpen makes every launch answer with a failed status and the given stderr.
func failOpen(f *Fixture, stderr string) {
	f.Commands.Reply = func(shell.Call) shell.Result { return shell.Result{Status: 1, Stderr: stderr} }
}

// actionRun is the fixture's action run and its action attempt.
func actionRun(t testing.TB, f *Fixture) (core.Run, core.Attempt) {
	t.Helper()
	run := f.Coordinator.ReadRun(f.ActionRunID)
	if run == nil {
		t.Fatalf("no action run %s", f.ActionRunID)
	}
	for _, attempt := range run.Attempts {
		if attempt.ID == f.ActionAttemptID {
			return *run, attempt
		}
	}
	t.Fatalf("no action attempt %s", f.ActionAttemptID)
	return core.Run{}, core.Attempt{}
}

// stageOf is the stage an attempt ran on.
func stageOf(t testing.TB, run core.Run, attempt core.Attempt) core.Stage {
	t.Helper()
	for _, stage := range run.Stages {
		if stage.ID == attempt.StageID {
			return stage
		}
	}
	t.Fatalf("no stage %s on run %s", attempt.StageID, run.ID)
	return core.Stage{}
}

// dataInventory is every regular file under root with its bytes, keyed by the path below root.
func dataInventory(t testing.TB, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		files[rel] = string(data)
		return err
	})
	if err != nil {
		t.Fatalf("inventory %s: %v", root, err)
	}
	return files
}

// desktopExpectError fails unless reply has the status and the error message.
func desktopExpectError(t testing.TB, reply Reply, status int, message string) {
	t.Helper()
	requireStatus(t, reply, status)
	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(reply.Body, &body); err != nil {
		t.Fatalf("error body %s: %v", reply.Body, err)
	}
	if body.Message != message {
		t.Fatalf("message %q, want %q", body.Message, message)
	}
}

// desktopWorktreeByID is the worktree target with id.
func desktopWorktreeByID(targets []runner.WorktreeTarget, id string) (runner.WorktreeTarget, bool) {
	for _, target := range targets {
		if target.ID == id {
			return target, true
		}
	}
	return runner.WorktreeTarget{}, false
}

// desktopLinkByID is the run's link target with id, failing the test when there is none.
func desktopLinkByID(t testing.TB, f *Fixture, run core.Run, id string) core.RunLinksTarget {
	t.Helper()
	targets := core.RunLinksTargets(run.Subject, run.PullRequests, core.RepoLabelAmbiguousNames(f.Coordinator.ReadRuns()))
	for _, target := range targets {
		if target.ID == id {
			return target
		}
	}
	t.Fatalf("no link target %s", id)
	return core.RunLinksTarget{}
}

// desktopStoreLinkURL stores url as the action run's link target: the pull request the target names, or the subject.
func desktopStoreLinkURL(t testing.TB, f *Fixture, url string) {
	t.Helper()
	if !f.Coordinator.UpdateRunForContract(f.ActionRunID, func(r *core.Run) {
		for i := range r.PullRequests {
			if "pr:"+r.PullRequests[i].Repo+"#"+strconv.Itoa(r.PullRequests[i].Number) == f.LinkTargetID {
				r.PullRequests[i].URL = url
				return
			}
		}
		r.Subject.URL = url
	}) {
		t.Fatal("could not update the isolated fixture run")
	}
}

// headerOf is a request header set from name/value pairs.
func headerOf(pairs map[string]string) http.Header {
	h := http.Header{}
	for name, value := range pairs {
		h.Set(name, value)
	}
	return h
}

// desktopPayloads is one valid payload per desktop command, for the fixture's action run.
func desktopPayloads(f *Fixture) []struct {
	path    string
	payload map[string]string
} {
	return []struct {
		path    string
		payload map[string]string
	}{
		{"/api/v1/desktop/reveal", map[string]string{"runId": f.ActionRunID, "kind": "run"}},
		{"/api/v1/desktop/reveal-data", map[string]string{}},
		{"/api/v1/desktop/vscode", map[string]string{"runId": f.ActionRunID, "targetId": f.VSCodeTargetID}},
		{"/api/v1/desktop/ghostty", map[string]string{"runId": f.ActionRunID, "attemptId": f.ActionAttemptID}},
		{"/api/v1/desktop/open-file", map[string]string{"runId": f.ActionRunID, "attemptId": f.ActionAttemptID}},
		{"/api/v1/desktop/open-url", map[string]string{"runId": f.ActionRunID, "targetId": f.LinkTargetID}},
	}
}

// TestDesktopCommandsLaunchTheDaemonResolvedTargetsWithExactArguments checks every command's launch: the
// target the daemon resolves, the open arguments, and the tmux attach for a continue session.
func TestDesktopCommandsLaunchTheDaemonResolvedTargetsWithExactArguments(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	run, attempt := actionRun(t, f)
	stage := stageOf(t, run, attempt)
	paths := f.Coordinator.Paths()
	output := filepath.Join(paths.AttemptFolder(run, attempt), stage.Writes())
	artifact := filepath.Join(paths.ArtifactsFolder(run, attempt), f.ArtifactName())
	worktree, ok := desktopWorktreeByID(runner.WorktreeTargetsFor(run, paths.WorkFolder(run)), f.VSCodeTargetID)
	if !ok {
		t.Fatalf("no worktree target %s", f.VSCodeTargetID)
	}
	link := desktopLinkByID(t, f, run, f.LinkTargetID)

	reveals := []struct {
		payload map[string]string
		want    string
	}{
		{map[string]string{"runId": run.ID, "kind": "run"}, paths.RunFolder(run)},
		{map[string]string{"runId": run.ID, "kind": "attempt", "attemptId": attempt.ID}, paths.AttemptFolder(run, attempt)},
		{map[string]string{"runId": run.ID, "kind": "output", "attemptId": attempt.ID}, output},
		{map[string]string{"runId": run.ID, "kind": "artifact", "attemptId": attempt.ID, "name": f.ArtifactName()}, artifact},
		{map[string]string{"runId": run.ID, "kind": "worktree", "targetId": f.VSCodeTargetID}, worktree.Folder},
	}
	for _, tc := range reveals {
		n := len(f.Commands.Calls())
		reply := desktopPost(t, f, "/api/v1/desktop/reveal", tc.payload)
		if reply.Status != 200 || reply.Text() != `{"ok":true}` {
			t.Fatalf("reveal %v: status %d body %s", tc.payload, reply.Status, reply.Body)
		}
		if got := desktopCallsFrom(f, n); len(got) != 1 || !sameCall(got[0], wantOpen("-R", filepath.Clean(tc.want))) {
			t.Fatalf("reveal %v launched %v, want open -R %s", tc.payload, got, tc.want)
		}
	}

	// The data-root reveal takes no selectors: the daemon owns the target.
	n := len(f.Commands.Calls())
	if reply := desktopPost(t, f, "/api/v1/desktop/reveal-data", map[string]string{}); reply.Status != 200 {
		t.Fatalf("reveal-data: status %d", reply.Status)
	}
	if got := desktopCallsFrom(f, n); len(got) != 1 || !sameCall(got[0], wantOpen("-R", filepath.Clean(paths.Root))) {
		t.Fatalf("reveal-data launched %v", got)
	}

	// The last launch of each command below is its own.
	last := func(path string, payload map[string]string, want shell.Call) {
		t.Helper()
		n := len(f.Commands.Calls())
		if reply := desktopPost(t, f, path, payload); reply.Status != 200 {
			t.Fatalf("%s: status %d", path, reply.Status)
		}
		if got := desktopCallsFrom(f, n); len(got) == 0 || !sameCall(got[len(got)-1], want) {
			t.Fatalf("%s launched %v, want %v", path, got, want)
		}
	}
	last("/api/v1/desktop/open-file", map[string]string{"runId": run.ID, "attemptId": attempt.ID}, wantOpen(filepath.Clean(output)))
	last("/api/v1/desktop/vscode", map[string]string{"runId": run.ID, "targetId": f.VSCodeTargetID},
		wantOpen("-b", "com.microsoft.VSCode", filepath.Clean(worktree.Folder)))
	last("/api/v1/desktop/open-url", map[string]string{"runId": run.ID, "targetId": link.ID}, wantOpen(link.URL))

	// Ghostty attaches to the continue session when one is listed, otherwise to the attempt's session.
	session := *attempt.Session
	continueSession := runner.TmuxContinueName(session)
	ghostty := func(name string) []string {
		return []string{"-na", "Ghostty", "--args", "-e", "/bin/zsh", "-lc", launch.DesktopActionsGhosttyCommand(runner.TmuxSocket(), name)}
	}
	f.DesktopSessions.Set(session, continueSession)
	n = len(f.Commands.Calls())
	if reply := desktopPost(t, f, "/api/v1/desktop/ghostty", map[string]string{"runId": run.ID, "attemptId": attempt.ID}); reply.Status != 200 {
		t.Fatalf("ghostty: status %d", reply.Status)
	}
	if got := desktopCallsFrom(f, n); len(got) == 0 || !slices.Equal(got[len(got)-1].Args, ghostty(continueSession)) {
		t.Fatalf("ghostty (continue) launched %v", got)
	}
	f.DesktopSessions.Set(session)
	n = len(f.Commands.Calls())
	if reply := desktopPost(t, f, "/api/v1/desktop/ghostty", map[string]string{"runId": run.ID, "attemptId": attempt.ID}); reply.Status != 200 {
		t.Fatalf("ghostty: status %d", reply.Status)
	}
	if got := desktopCallsFrom(f, n); len(got) == 0 || !slices.Equal(got[len(got)-1].Args, ghostty(session)) {
		t.Fatalf("ghostty (attempt) launched %v", got)
	}

	// A worktree whose folder has spaces opens as one argument, and a hostile session name is quoted.
	spaced := filepath.Join(paths.WorkFolder(run), "my route", "worktree")
	for _, repo := range run.Repos {
		if err := os.MkdirAll(runner.WorkspaceSpecCheckout(repo, run.Repos, spaced), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	target, ok := desktopWorktreeByID(runner.WorktreeTargetsFor(run, paths.WorkFolder(run)), "r:my route")
	if !ok {
		t.Fatal("no worktree target r:my route")
	}
	last("/api/v1/desktop/vscode", map[string]string{"runId": run.ID, "targetId": target.ID},
		wantOpen("-b", "com.microsoft.VSCode", filepath.Clean(target.Folder)))

	const hostile = "bl-1'; touch pwned; echo '"
	if !f.Coordinator.UpdateRunForContract(run.ID, func(r *core.Run) {
		for i := range r.Attempts {
			if r.Attempts[i].ID == attempt.ID {
				session := hostile
				r.Attempts[i].Session = &session
			}
		}
	}) {
		t.Fatal("could not set the hostile session")
	}
	f.DesktopSessions.Set(hostile)
	n = len(f.Commands.Calls())
	if reply := desktopPost(t, f, "/api/v1/desktop/ghostty", map[string]string{"runId": run.ID, "attemptId": attempt.ID}); reply.Status != 200 {
		t.Fatalf("ghostty: status %d", reply.Status)
	}
	if got := desktopCallsFrom(f, n); len(got) == 0 || !slices.Equal(got[len(got)-1].Args, ghostty(hostile)) {
		t.Fatalf("ghostty launched %v", got)
	}
}

// TestDesktopRefusalsLaunchNothingAndNameTheirReason checks every refusal: malformed bodies, unknown or foreign
// targets, unavailable sessions, applications and worktrees, and stored links with a disallowed scheme.
func TestDesktopRefusalsLaunchNothingAndNameTheirReason(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	run, attempt := actionRun(t, f)

	// GET is never a command, and a malformed body or a selector a command does not take is a 422.
	if reply := f.Do(Request{Method: "GET", Path: "/api/v1/desktop/reveal", Token: ContractToken}); reply.Status != 404 {
		t.Fatalf("GET reveal: status %d, want 404", reply.Status)
	}
	desktopExpectError(t, f.Do(Request{Method: "POST", Path: "/api/v1/desktop/reveal", Token: ContractToken, Body: []byte("{")}),
		422, "Malformed request body or unsupported selectors.")
	desktopExpectError(t, desktopPost(t, f, "/api/v1/desktop/reveal", map[string]string{"kind": "run"}),
		422, "Expected runId and kind.")
	requireStatus(t, desktopPost(t, f, "/api/v1/desktop/reveal", map[string]string{"runId": run.ID, "kind": "run", "path": "/tmp/x"}), 422)
	requireStatus(t, desktopPost(t, f, "/api/v1/desktop/reveal", map[string]string{"runId": run.ID, "kind": "arbitrary"}), 422)
	for _, payload := range []map[string]string{
		{"runId": run.ID, "attemptId": attempt.ID, "session": "evil", "command": "rm -rf /"},
		{"runId": run.ID, "targetId": f.VSCodeTargetID, "app": "Terminal", "path": "/Applications/Terminal.app"},
		{"runId": run.ID, "targetId": f.LinkTargetID, "url": "https://evil.example/"},
	} {
		for _, path := range []string{"/api/v1/desktop/reveal", "/api/v1/desktop/vscode", "/api/v1/desktop/ghostty",
			"/api/v1/desktop/open-file", "/api/v1/desktop/open-url"} {
			requireStatus(t, desktopPost(t, f, path, payload), 422)
		}
	}
	requireStatus(t, desktopPost(t, f, "/api/v1/desktop/open-url", map[string]string{"runId": run.ID, "targetId": "link", "url": "https://example.test"}), 422)

	// Unknown and foreign runs and attempts are 404s, even with a healthy session lookup.
	requireStatus(t, desktopPost(t, f, "/api/v1/desktop/open-file", map[string]string{"runId": "foreign-run", "attemptId": attempt.ID}), 404)
	requireStatus(t, desktopPost(t, f, "/api/v1/desktop/open-file", map[string]string{"runId": run.ID, "attemptId": "foreign-attempt"}), 404)
	requireStatus(t, desktopPost(t, f, "/api/v1/desktop/ghostty", map[string]string{"runId": "foreign-run", "attemptId": attempt.ID}), 404)
	foreignAttempt := core.NewAttempt(core.AttemptArgs{StageID: attempt.StageID, Number: 98, StartedAt: jsonxDate(ContractNow), Folder: "foreign-attempt"})
	foreignRun := run
	foreignRun.ID = "foreign-recorded-run"
	foreignRun.Attempts = []core.Attempt{foreignAttempt}
	f.Coordinator.AppendRunForContract(foreignRun)
	requireStatus(t, desktopPost(t, f, "/api/v1/desktop/ghostty", map[string]string{"runId": run.ID, "attemptId": foreignAttempt.ID}), 404)

	// A recorded attempt with no session, or no listed session, is the 422 session failure.
	sessionless := core.NewAttempt(core.AttemptArgs{StageID: attempt.StageID, Number: 97, StartedAt: jsonxDate(ContractNow), Folder: "sessionless-attempt"})
	if !f.Coordinator.UpdateRunForContract(run.ID, func(r *core.Run) { r.Attempts = append(r.Attempts, sessionless) }) {
		t.Fatal("could not add the sessionless attempt")
	}
	f.DesktopSessions.Reset(true)
	desktopExpectError(t, desktopPost(t, f, "/api/v1/desktop/ghostty", map[string]string{"runId": run.ID, "attemptId": sessionless.ID}),
		422, "This attempt has no available session.")
	f.DesktopSessions.Set()
	desktopExpectError(t, desktopPost(t, f, "/api/v1/desktop/ghostty", map[string]string{"runId": run.ID, "attemptId": attempt.ID}),
		422, "This attempt has no available session.")

	// Ghostty and Visual Studio Code must be installed, and a worktree deleted since discovery is gone.
	f.DesktopSessions.Set(*attempt.Session)
	f.DesktopApps.Set()
	desktopExpectError(t, desktopPost(t, f, "/api/v1/desktop/ghostty", map[string]string{"runId": run.ID, "attemptId": attempt.ID}),
		422, "Ghostty is not installed. Install it and try again.")
	desktopExpectError(t, desktopPost(t, f, "/api/v1/desktop/vscode", map[string]string{"runId": run.ID, "targetId": f.VSCodeTargetID}),
		422, "Visual Studio Code is not installed. Install it and try again.")
	f.DesktopApps.Reset(true)
	if err := os.RemoveAll(filepath.Join(f.Coordinator.Paths().WorkFolder(run), "worktree")); err != nil {
		t.Fatalf("remove worktree: %v", err)
	}
	desktopExpectError(t, desktopPost(t, f, "/api/v1/desktop/vscode", map[string]string{"runId": run.ID, "targetId": f.VSCodeTargetID}),
		422, "This worktree is no longer available.")

	// A stored link with a disallowed or malformed scheme is never opened.
	for _, value := range []string{"file:///tmp/private", "javascript:alert(1)", "JaVaScRiPt:alert(1)", "https:"} {
		desktopStoreLinkURL(t, f, value)
		desktopExpectError(t, desktopPost(t, f, "/api/v1/desktop/open-url", map[string]string{"runId": f.ActionRunID, "targetId": f.LinkTargetID}),
			422, "Only absolute HTTP and HTTPS links can be opened.")
	}
	if got := f.Commands.Calls(); len(got) != 0 {
		t.Fatalf("launches after refusals: %v", got)
	}
}

// TestStoredWebLinksOpenExactlyAsStored checks that an uppercase web scheme opens as it is stored.
func TestStoredWebLinksOpenExactlyAsStored(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	const stored = "HTTP://Example.Test/Path?q=1"
	desktopStoreLinkURL(t, f, stored)
	if reply := desktopPost(t, f, "/api/v1/desktop/open-url", map[string]string{"runId": f.ActionRunID, "targetId": f.LinkTargetID}); reply.Status != 200 {
		t.Fatalf("open-url: status %d", reply.Status)
	}
	if got := f.Commands.Calls(); len(got) == 0 || !sameCall(got[len(got)-1], wantOpen(stored)) {
		t.Fatalf("launched %v, want open %s", got, stored)
	}
}

// TestDesktopGuardsRunBeforeAnyLaunchAndAcceptBearerOrCookie checks Host and Origin before any launch, that
// a request without credentials is 401, and that the cookie is an accepted credential.
func TestDesktopGuardsRunBeforeAnyLaunchAndAcceptBearerOrCookie(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	hostile := []Request{}
	for _, header := range []map[string]string{
		{"Host": "127.0.0.1:" + strconv.Itoa(ContractPort+1)},
		{"Origin": "http://evil.example"},
		{"Origin": "null"},
		{"Origin": "http://localhost:" + strconv.Itoa(ContractPort)},
	} {
		hostile = append(hostile, Request{Token: ContractToken, Header: headerOf(header)})
	}
	for _, tc := range desktopPayloads(f) {
		body, err := jsonx.Marshal(tc.payload)
		if err != nil {
			t.Fatal(err)
		}
		for _, req := range hostile {
			req.Method, req.Path, req.Body = "POST", tc.path, body
			if reply := f.Do(req); reply.Status != 403 {
				t.Fatalf("%s %v: status %d, want 403", tc.path, req.Header, reply.Status)
			}
		}
		// A repeated Origin with a good Host is refused before any launch (net/http refuses a repeated Host itself, a recorded deviation).
		raw := "POST " + tc.path + " HTTP/1.1\r\nHost: 127.0.0.1:" + strconv.Itoa(ContractPort) +
			"\r\nOrigin: http://127.0.0.1:" + strconv.Itoa(ContractPort) + "\r\nOrigin: http://127.0.0.1:" + strconv.Itoa(ContractPort) +
			"\r\nAuthorization: Bearer " + ContractToken + "\r\nContent-Type: application/json\r\nContent-Length: " +
			strconv.Itoa(len(body)) + "\r\nConnection: close\r\n\r\n" + string(body)
		if reply := f.DoRaw(t, raw); reply.Status != 403 {
			t.Fatalf("%s duplicate origin: status %d, want 403", tc.path, reply.Status)
		}

		if missing := f.Do(Request{Method: "POST", Path: tc.path, Body: body}); missing.Status != 401 {
			t.Fatalf("%s without credentials: status %d, want 401", tc.path, missing.Status)
		}
		cookie := f.Do(Request{Method: "POST", Path: tc.path, Body: body, Header: map[string][]string{"Cookie": {"beltline_auth=" + ContractToken}}})
		if cookie.Status != 200 {
			t.Fatalf("%s with the cookie: status %d, want 200", tc.path, cookie.Status)
		}
	}
	for _, path := range []string{"/api/v1/desktop/reveal", "/api/v1/desktop/vscode", "/api/v1/desktop/ghostty",
		"/api/v1/desktop/open-file", "/api/v1/desktop/open-url"} {
		if reply := f.Do(Request{Method: "GET", Path: path, Token: ContractToken}); reply.Status != 404 {
			t.Fatalf("GET %s: status %d, want 404", path, reply.Status)
		}
	}
	if got := len(f.Commands.Calls()); got != 6 {
		t.Fatalf("launches = %d, want 6 (one per accepted command)", got)
	}
}

// TestLaunchFailuresAreMappedToTheirMessageAndChangeNoRunsOrSettings checks the wording of a failed launch for
// each command, and that the data folder, the runs and the settings are unchanged by any command.
func TestLaunchFailuresAreMappedToTheirMessageAndChangeNoRunsOrSettings(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	disk := dataInventory(t, f.Root)
	runs := f.Coordinator.ReadRuns()
	settings := f.Coordinator.ReadSettings()
	failOpen(f, "open rejected")
	messages := map[string]string{
		"/api/v1/desktop/reveal":      "open rejected",
		"/api/v1/desktop/reveal-data": "open rejected",
		"/api/v1/desktop/open-file":   "open rejected",
		"/api/v1/desktop/vscode":      "open rejected Try opening the worktree again.",
		"/api/v1/desktop/ghostty":     "open rejected",
		"/api/v1/desktop/open-url":    "open rejected",
	}
	for _, tc := range desktopPayloads(f) {
		before := len(f.Commands.Calls())
		desktopExpectError(t, desktopPost(t, f, tc.path, tc.payload), 422, messages[tc.path])
		if got := len(f.Commands.Calls()) - before; got != 1 {
			t.Fatalf("%s: launches = %d, want 1", tc.path, got)
		}
	}
	if got := dataInventory(t, f.Root); !reflect.DeepEqual(got, disk) {
		t.Fatal("the data folder changed after failed desktop commands")
	}
	if !reflect.DeepEqual(f.Coordinator.ReadRuns(), runs) || !reflect.DeepEqual(f.Coordinator.ReadSettings(), settings) {
		t.Fatal("a failed launch changed the runs or settings")
	}
}

// jsonxDate is a jsonx date for t.
func jsonxDate(t time.Time) jsonx.Date { return jsonx.Date{Time: t} }
