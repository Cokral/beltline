// Tests of the artifact link route and its delivery, run against the contract fixture (servertest)
// through the production router. Requests are parsed and served in-process. The repeated-Host cases
// are answered 400 by net/http on the listener, and the listener tests check them.

package server_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
	"github.com/robbertmanders/beltline/daemon/internal/server/servertest"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// artifactHTML is the artifact the contract run carries.
const artifactHTML = "<!doctype html><p>Contract artifact</p>\n"

// encodeSegment percent-encodes a path segment: only ASCII letters, digits
// and "-._~" stay as written.
func encodeSegment(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-._~", c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteString("%" + strings.ToUpper(string("0123456789abcdef"[c>>4])+string("0123456789abcdef"[c&15])))
		}
	}
	return b.String()
}

// issueArtifact posts an artifact link request for the given run and attempt (the action ones when
// empty), with the headers, and returns the reply.
func issueArtifact(t *testing.T, f *servertest.Fixture, name string, header http.Header, runID, attemptID string) servertest.Reply {
	t.Helper()
	if runID == "" {
		runID = f.ActionRunID
	}
	if attemptID == "" {
		attemptID = f.ActionAttemptID
	}
	body, err := json.Marshal(map[string]string{"runId": runID, "attemptId": attemptID, "name": name})
	if err != nil {
		t.Fatal(err)
	}
	return f.Do(servertest.Request{Method: "POST", Path: "/api/v1/artifacts/link", Body: body, Header: header})
}

// bearerHeader is the bearer header of the contract token.
func bearerHeader() http.Header {
	return http.Header{"Authorization": {"Bearer " + servertest.ContractToken}}
}

// cookieHeader is the auth cookie header of the contract token.
func cookieHeader() http.Header {
	return http.Header{"Cookie": {"beltline_auth=" + servertest.ContractToken}}
}

// linkOf decodes an artifact link response.
func linkOf(t *testing.T, reply servertest.Reply) routes.ArtifactLinkResponse {
	t.Helper()
	var link routes.ArtifactLinkResponse
	if err := json.Unmarshal(reply.Body, &link); err != nil {
		t.Fatalf("link: %v (%s)", err, reply.Body)
	}
	return link
}

// artifactFolder is the artifacts folder of the action attempt.
func artifactFolder(t *testing.T, f *servertest.Fixture) string {
	t.Helper()
	run := f.Coordinator.ReadRun(f.ActionRunID)
	if run == nil {
		t.Fatal("no action run")
	}
	for _, attempt := range run.Attempts {
		if attempt.ID == f.ActionAttemptID {
			return f.Coordinator.Paths().ArtifactsFolder(*run, attempt)
		}
	}
	t.Fatal("no action attempt")
	return ""
}

// signer is the artifact link signer the contract fixture's server uses, on the fixture's clock.
func signer(f *servertest.Fixture) routes.ArtifactLinkSigner {
	return routes.ArtifactLinkSigner{Key: servertest.ContractSigningKey, Clock: f.Clock.Now}
}

// splitPath is a link without its query.
func splitPath(link string) string { return strings.SplitN(link, "?", 2)[0] }

// TestArtifactLinksIssueForBearerAndCookieAndDeliverConfinedHTML checks issued bearer and cookie links.
func TestArtifactLinksIssueForBearerAndCookieAndDeliverConfinedHTML(t *testing.T) {
	t.Parallel()
	f := servertest.Start(t, servertest.Options{})
	bearerReply := issueArtifact(t, f, f.ArtifactName(), bearerHeader(), "", "")
	if bearerReply.Status != 200 {
		t.Fatalf("bearer issue: %d", bearerReply.Status)
	}
	if cookie := issueArtifact(t, f, f.ArtifactName(), cookieHeader(), "", ""); cookie.Status != 200 {
		t.Fatalf("cookie issue: %d", cookie.Status)
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(bearerReply.Body, &keys); err != nil || len(keys) != 2 || keys["url"] == nil || keys["expiresAt"] == nil {
		t.Fatalf("link keys: %v %s", err, bearerReply.Body)
	}
	if strings.Contains(bearerReply.Text(), servertest.ContractToken) {
		t.Error("the link exposes the token")
	}
	link := linkOf(t, bearerReply)
	if !link.ExpiresAt.Equal(servertest.ContractNow.Add(60 * time.Second)) {
		t.Errorf("expiresAt %v, want one minute after the contract clock", link.ExpiresAt.Time)
	}
	encodedPath := "/artifacts/" + encodeSegment(f.ActionRunID) + "/" + encodeSegment(f.ActionAttemptID) + "/" + encodeSegment(f.ArtifactName())
	if splitPath(link.URL) != encodedPath {
		t.Errorf("path %s, want %s", splitPath(link.URL), encodedPath)
	}

	delivered := f.Do(servertest.Request{Method: "GET", Path: link.URL})
	if delivered.Status != 200 {
		t.Fatalf("delivery: %d", delivered.Status)
	}
	if string(delivered.Body) != artifactHTML {
		t.Errorf("body %q", delivered.Body)
	}
	want := map[string]string{
		"Content-Type":            "text/html; charset=utf-8",
		"Content-Security-Policy": "sandbox allow-scripts",
		"X-Content-Type-Options":  "nosniff",
		"Cache-Control":           "no-store",
		"Referrer-Policy":         "no-referrer",
	}
	for name, value := range want {
		if got := delivered.Header.Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	for _, name := range []string{"Set-Cookie", "Location"} {
		if got := delivered.Header.Get(name); got != "" {
			t.Errorf("%s = %q, want none", name, got)
		}
	}

	path := splitPath(link.URL)
	if got := f.Do(servertest.Request{Method: "GET", Path: path}).Status; got != 401 {
		t.Errorf("without credentials: %d", got)
	}
	if got := f.Do(servertest.Request{Method: "GET", Path: path, Header: bearerHeader()}).Status; got != 401 {
		t.Errorf("with a bearer but no link signature: %d", got)
	}
	if got := f.Do(servertest.Request{Method: "GET", Path: path, Header: cookieHeader()}).Status; got != 401 {
		t.Errorf("with a cookie but no link signature: %d", got)
	}
	if got := issueArtifact(t, f, f.ArtifactName(), nil, "", "").Status; got != 401 {
		t.Errorf("issue without credentials: %d", got)
	}
}

// TestArtifactSignatureExpiryDuplicatesAndBoundIdentityAreEnforced checks signature expiry and identity.
func TestArtifactSignatureExpiryDuplicatesAndBoundIdentityAreEnforced(t *testing.T) {
	t.Parallel()
	f := servertest.Start(t, servertest.Options{})
	folder := artifactFolder(t, f)
	otherName := "space café ☃.html"
	if err := os.WriteFile(filepath.Join(folder, otherName), []byte("other artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := linkOf(t, issueArtifact(t, f, f.ArtifactName(), bearerHeader(), "", "")).URL
	other := linkOf(t, issueArtifact(t, f, otherName, bearerHeader(), "", "")).URL

	originalPath := splitPath(link)
	query := strings.SplitN(link, "?", 2)[1]
	lastSlash := strings.LastIndex(originalPath, "/")
	changedName := originalPath[:lastSlash+1] + encodeSegment(otherName) + "?" + query
	if got := f.Do(servertest.Request{Method: "GET", Path: changedName}).Status; got != 401 {
		t.Errorf("a link signed for one name served another: %d", got)
	}
	if got := f.Do(servertest.Request{Method: "GET", Path: other}).Status; got != 200 {
		t.Errorf("the other artifact's own link: %d", got)
	}

	// The run segment is bound too: one existing run's link cannot address a same-shaped artifact on another run.
	run := f.Coordinator.ReadRun(f.ActionRunID)
	if run == nil {
		t.Fatal("no action run")
	}
	var attempt core.Attempt
	for _, a := range run.Attempts {
		if a.ID == f.ActionAttemptID {
			attempt = a
		}
	}
	otherRun := *run
	otherRun.ID = "another-existing-run"
	f.Coordinator.AppendRunForContract(otherRun)
	otherFolder := f.Coordinator.Paths().ArtifactsFolder(otherRun, attempt)
	if err := os.MkdirAll(otherFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherFolder, f.ArtifactName()), []byte("other run artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	changedRun := strings.ReplaceAll(link, encodeSegment(run.ID), encodeSegment(otherRun.ID))
	if got := f.Do(servertest.Request{Method: "GET", Path: changedRun}).Status; got != 401 {
		t.Errorf("a link for another run: %d", got)
	}

	if got := f.Do(servertest.Request{Method: "GET", Path: link + "&signature=00"}).Status; got != 401 {
		t.Errorf("a short signature: %d", got)
	}
	if got := f.Do(servertest.Request{Method: "GET", Path: link + "&expires=1"}).Status; got != 401 {
		t.Errorf("a changed expiry: %d", got)
	}
	badSignature := strings.Replace(link, "signature=", "signature=z", 1)
	if got := f.Do(servertest.Request{Method: "GET", Path: badSignature}).Status; got != 401 {
		t.Errorf("a bad signature: %d", got)
	}
	if got := f.Do(servertest.Request{Method: "GET", Path: originalPath + "?expires=no&signature=bad"}).Status; got != 401 {
		t.Errorf("a non-numeric expiry: %d", got)
	}

	f.Clock.Set(servertest.ContractNow.Add(59 * time.Second))
	if got := f.Do(servertest.Request{Method: "GET", Path: link}).Status; got != 200 {
		t.Errorf("a link at 59 seconds: %d", got)
	}
	f.Clock.Set(servertest.ContractNow.Add(60 * time.Second))
	if got := f.Do(servertest.Request{Method: "GET", Path: link}).Status; got != 401 {
		t.Errorf("a link at its expiry: %d", got)
	}
	f.Clock.Set(servertest.ContractNow.Add(61 * time.Second))
	if got := f.Do(servertest.Request{Method: "GET", Path: link}).Status; got != 401 {
		t.Errorf("a link after its expiry: %d", got)
	}
}

// TestArtifactEncodedRunSegmentsAndRecordedAttemptsStayConfined checks encoded run segments and attempts.
func TestArtifactEncodedRunSegmentsAndRecordedAttemptsStayConfined(t *testing.T) {
	t.Parallel()
	f := servertest.Start(t, servertest.Options{})
	original := f.Coordinator.ReadRun(f.ActionRunID)
	if original == nil {
		t.Fatal("no action run")
	}
	var originalAttempt core.Attempt
	for _, a := range original.Attempts {
		if a.ID == f.ActionAttemptID {
			originalAttempt = a
		}
	}
	opaqueRun := *original
	opaqueRun.ID = "opaque/run with #"
	otherAttempt := core.NewAttempt(core.AttemptArgs{StageID: originalAttempt.StageID, Number: 99,
		StartedAt: jsonx.Date{Time: servertest.ContractNow}, Folder: "other-recorded-attempt"})
	opaqueRun.Attempts = append(opaqueRun.Attempts, otherAttempt)
	f.Coordinator.AppendRunForContract(opaqueRun)

	encoded := linkOf(t, issueArtifact(t, f, f.ArtifactName(), bearerHeader(), opaqueRun.ID, originalAttempt.ID))
	expectedPath := "/artifacts/opaque%2Frun%20with%20%23/" + encodeSegment(originalAttempt.ID) + "/" + encodeSegment(f.ArtifactName())
	if splitPath(encoded.URL) != expectedPath {
		t.Errorf("path %s, want %s", splitPath(encoded.URL), expectedPath)
	}
	if got := f.Do(servertest.Request{Method: "GET", Path: encoded.URL}).Text(); got != artifactHTML {
		t.Errorf("body %q", got)
	}

	otherFolder := f.Coordinator.Paths().ArtifactsFolder(opaqueRun, otherAttempt)
	if err := os.MkdirAll(otherFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherFolder, "second.html"), []byte("second attempt only"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := issueArtifact(t, f, "second.html", bearerHeader(), opaqueRun.ID, originalAttempt.ID).Status; got != 404 {
		t.Errorf("a file the first attempt does not hold: %d", got)
	}
	onSecond := issueArtifact(t, f, "second.html", bearerHeader(), opaqueRun.ID, otherAttempt.ID)
	if onSecond.Status != 200 {
		t.Fatalf("the second attempt's file: %d", onSecond.Status)
	}
	secondLink := linkOf(t, onSecond)
	if got := f.Do(servertest.Request{Method: "GET", Path: secondLink.URL}).Text(); got != "second attempt only" {
		t.Errorf("body %q", got)
	}
	if got := issueArtifact(t, f, "second.html", bearerHeader(), opaqueRun.ID, "not-recorded").Status; got != 404 {
		t.Errorf("an unrecorded attempt: %d", got)
	}
	changedAttemptPath := strings.Replace(secondLink.URL, encodeSegment(otherAttempt.ID), encodeSegment(originalAttempt.ID), 1)
	if got := f.Do(servertest.Request{Method: "GET", Path: changedAttemptPath}).Status; got != 401 {
		t.Errorf("a link moved to another attempt: %d", got)
	}
}

// TestArtifactTraversalAndSymlinkReplacementNeverReturnOutsideBytes checks that traversal and symlink replacement never return outside bytes.
func TestArtifactTraversalAndSymlinkReplacementNeverReturnOutsideBytes(t *testing.T) {
	t.Parallel()
	f := servertest.Start(t, servertest.Options{})
	link := linkOf(t, issueArtifact(t, f, f.ArtifactName(), bearerHeader(), "", "")).URL
	run := f.Coordinator.ReadRun(f.ActionRunID)
	var attempt core.Attempt
	for _, a := range run.Attempts {
		if a.ID == f.ActionAttemptID {
			attempt = a
		}
	}
	outside := filepath.Join(f.Root, "outside.html")
	if err := os.WriteFile(outside, []byte("outside secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(artifactFolder(t, f), f.ArtifactName())
	if err := os.Remove(artifact); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, artifact); err != nil {
		t.Fatal(err)
	}
	replacement := f.Do(servertest.Request{Method: "GET", Path: link})
	if replacement.Status != 404 || strings.Contains(replacement.Text(), "outside secret") {
		t.Errorf("a symlinked artifact: %d %q", replacement.Status, replacement.Text())
	}

	traversalName := "../outside.html"
	expires := signer(f).Expiration()
	signature := signer(f).Signature(run.ID, attempt.ID, traversalName, expires)
	rawPath := "/artifacts/" + encodeSegment(run.ID) + "/" + encodeSegment(attempt.ID) + "/%2e%2e%2foutside.html" +
		"?expires=" + itoa(expires) + "&signature=" + signature
	response := f.DoRaw(t, "GET "+rawPath+" HTTP/1.1\r\nHost: 127.0.0.1:"+itoa(servertest.ContractPort)+"\r\nConnection: close\r\n\r\n")
	if response.Status != 404 || strings.Contains(response.Text(), "outside secret") {
		t.Errorf("a signed traversal: %d %q", response.Status, response.Text())
	}
	if got := issueArtifact(t, f, traversalName, bearerHeader(), "", "").Status; got != 404 {
		t.Errorf("a traversal name: %d", got)
	}
}

// TestArtifactFreshServerKeyInvalidatesPreviouslyIssuedLinks checks that a fresh server key invalidates earlier links.
func TestArtifactFreshServerKeyInvalidatesPreviouslyIssuedLinks(t *testing.T) {
	t.Parallel()
	f := servertest.Start(t, servertest.Options{})
	link := linkOf(t, issueArtifact(t, f, f.ArtifactName(), bearerHeader(), "", "")).URL
	replacement := f.RouterWithSigningKey([]byte("a-new-signing-key-for-restart-tests"))
	response := servertest.ServeRequest(replacement, servertest.Request{Method: "GET", Path: link})
	if response.Status != 401 {
		t.Errorf("a link from before the key changed: %d", response.Status)
	}
}

// TestArtifactUnknownNonAgentAndSymlinkedTargetsAre404 checks that unknown, non-agent and symlinked targets answer 404.
func TestArtifactUnknownNonAgentAndSymlinkedTargetsAre404(t *testing.T) {
	t.Parallel()
	f := servertest.Start(t, servertest.Options{})
	run := f.Coordinator.ReadRun(f.ActionRunID)
	var attempt core.Attempt
	for _, a := range run.Attempts {
		if a.ID == f.ActionAttemptID {
			attempt = a
		}
	}
	folder := artifactFolder(t, f)

	if got := issueArtifact(t, f, "missing.html", bearerHeader(), "", "").Status; got != 404 {
		t.Errorf("a missing file: %d", got)
	}
	testkit.WriteFile(t, filepath.Join(folder, "notes.txt"), "notes")
	if got := issueArtifact(t, f, "notes.txt", bearerHeader(), "", "").Status; got != 404 {
		t.Errorf("a file that is not HTML: %d", got)
	}
	if err := os.Mkdir(filepath.Join(folder, "directory.html"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := issueArtifact(t, f, "directory.html", bearerHeader(), "", "").Status; got != 404 {
		t.Errorf("a directory: %d", got)
	}
	if got := issueArtifact(t, f, "sub\\page.html", bearerHeader(), "", "").Status; got != 404 {
		t.Errorf("a backslash name: %d", got)
	}
	if os.Geteuid() != 0 {
		unreadable := filepath.Join(folder, "unreadable.html")
		testkit.WriteFile(t, unreadable, "unreadable")
		if err := os.Chmod(unreadable, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) })
		if got := issueArtifact(t, f, "unreadable.html", bearerHeader(), "", "").Status; got != 404 {
			t.Errorf("an unreadable file: %d", got)
		}
	}

	// An attempt recorded on a different run, and a run nobody recorded.
	foreign := *run
	foreign.ID = "foreign-recorded-run"
	foreign.Attempts = nil
	f.Coordinator.AppendRunForContract(foreign)
	if got := issueArtifact(t, f, f.ArtifactName(), bearerHeader(), foreign.ID, attempt.ID).Status; got != 404 {
		t.Errorf("an attempt recorded on another run: %d", got)
	}
	if got := issueArtifact(t, f, f.ArtifactName(), bearerHeader(), "never-recorded", "").Status; got != 404 {
		t.Errorf("an unrecorded run: %d", got)
	}

	// An attempt whose stage is not one of the run's agent stages.
	nonAgentStage := "approval-only"
	for _, stage := range run.Stages {
		if stage.Kind() != core.StageKindAgent {
			nonAgentStage = stage.ID
			break
		}
	}
	nonAgentAttempt := core.NewAttempt(core.AttemptArgs{StageID: nonAgentStage, Number: 91,
		StartedAt: jsonx.Date{Time: servertest.ContractNow}, Folder: "approval-attempt"})
	if !f.Coordinator.UpdateRunForContract(run.ID, func(r *core.Run) { r.Attempts = append(r.Attempts, nonAgentAttempt) }) {
		t.Fatal("the attempt was not recorded")
	}
	if got := issueArtifact(t, f, "page.html", bearerHeader(), run.ID, nonAgentAttempt.ID).Status; got != 404 {
		t.Errorf("a non-agent attempt: %d", got)
	}

	// A double-encoded traversal never reaches a file, even when signed.
	traversal := "%252e%252e%252foutside.html"
	expires := signer(f).Expiration()
	signature := signer(f).Signature(run.ID, attempt.ID, traversal, expires)
	rawPath := "/artifacts/" + encodeSegment(run.ID) + "/" + encodeSegment(attempt.ID) + "/" + traversal +
		"?expires=" + itoa(expires) + "&signature=" + signature
	doubleEncoded := f.DoRaw(t, "GET "+rawPath+" HTTP/1.1\r\nHost: 127.0.0.1:"+itoa(servertest.ContractPort)+"\r\nConnection: close\r\n\r\n")
	if doubleEncoded.Status != 404 {
		t.Errorf("a double-encoded traversal: %d", doubleEncoded.Status)
	}

	// A symlinked artifacts directory and a symlinked run directory are rejected.
	relocated := filepath.Join(f.Root, "real-artifacts")
	if err := os.Rename(folder, relocated); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relocated, folder); err != nil {
		t.Fatal(err)
	}
	if got := issueArtifact(t, f, f.ArtifactName(), bearerHeader(), "", "").Status; got != 404 {
		t.Errorf("a symlinked artifacts folder: %d", got)
	}

	linkedRun := *run
	linkedRun.ID = "symlinked-run"
	linkedAttempt := core.NewAttempt(core.AttemptArgs{StageID: attempt.StageID, Number: 77,
		StartedAt: jsonx.Date{Time: servertest.ContractNow}, Folder: "agent-attempt"})
	linkedRun.Attempts = append(linkedRun.Attempts, linkedAttempt)
	f.Coordinator.AppendRunForContract(linkedRun)
	linkedArtifacts := f.Coordinator.Paths().ArtifactsFolder(linkedRun, linkedAttempt)
	if err := os.MkdirAll(linkedArtifacts, 0o755); err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, filepath.Join(linkedArtifacts, "page.html"), "linked page")
	runFolder := f.Coordinator.Paths().RunFolder(linkedRun)
	relocatedRun := filepath.Join(f.Root, "relocated-run")
	if err := os.Rename(runFolder, relocatedRun); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relocatedRun, runFolder); err != nil {
		t.Fatal(err)
	}
	if got := issueArtifact(t, f, "page.html", bearerHeader(), linkedRun.ID, linkedAttempt.ID).Status; got != 404 {
		t.Errorf("a symlinked run folder: %d", got)
	}
}

// TestHostAndOriginGuardsPrecedeArtifactDeliveryAndIssuance checks that the Host and Origin guards come before artifact delivery and issuance.
// for the in-process router. The raw duplicate-header cases need the listener: the Host and Origin middleware is the same one
// TestRawRequestsWithBadHostsAreForbidden checks there (a repeated Host is refused by net/http's transport with 400, a known deviation from the 403 the contract gives).
func TestHostAndOriginGuardsPrecedeArtifactDeliveryAndIssuance(t *testing.T) {
	t.Parallel()
	f := servertest.Start(t, servertest.Options{})
	link := linkOf(t, issueArtifact(t, f, f.ArtifactName(), bearerHeader(), "", "")).URL
	hostile := []http.Header{
		{"Host": {"127.0.0.1:" + itoa(servertest.ContractPort+1)}},
		{"Origin": {"http://evil.example"}},
		{"Origin": {"null"}},
	}
	for _, extra := range hostile {
		header := bearerHeader()
		maps.Copy(header, extra)
		delivery := f.Do(servertest.Request{Method: "GET", Path: link, Header: extra})
		if delivery.Status != 403 {
			t.Errorf("delivery with %v: %d", extra, delivery.Status)
		}
		if delivery.Header.Get("Set-Cookie") != "" || strings.Contains(delivery.Text(), "Contract artifact") {
			t.Errorf("delivery with %v leaked", extra)
		}
		issuance := f.Do(servertest.Request{Method: "POST", Path: "/api/v1/artifacts/link",
			Body:   []byte(`{"runId":"` + f.ActionRunID + `","attemptId":"` + f.ActionAttemptID + `","name":"` + f.ArtifactName() + `"}`),
			Header: header})
		if issuance.Status != 403 || strings.Contains(issuance.Text(), "signature") {
			t.Errorf("issuance with %v: %d", extra, issuance.Status)
		}
	}
}

// itoa is the decimal form of n.
func itoa(n int64) string { return strconv.FormatInt(n, 10) }
