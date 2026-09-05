package walltime

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shortTempDir is a records directory short enough to hold a unix socket.
//
// A socket path is bounded by the kernel — 108 bytes on Linux, 104 on Darwin —
// and Go's per-test temporary directory embeds the test name, which on this
// package's longer names exceeds that on its own. Production records
// directories are short (`$RUNNER_TEMP/testbucket-wall`); this keeps a test
// from failing for a reason it is not about, and the controller refuses an
// over-long path explicitly rather than silently, which
// TestTheControllerRefusesAnUnbindablePath covers.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// TestTheControllerRefusesAnUnbindablePath: the measured script reaches this
// wrapper through the socket, so a path the kernel will not bind is a refusal
// that has to say what to shorten.
func TestTheControllerRefusesAnUnbindablePath(t *testing.T) {
	t.Setenv(ScriptUserEnv, "tb-script")
	deep := filepath.Join(shortTempDir(t), longName(120))
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Skipf("this platform will not create the long directory: %v", err)
	}
	_, err := StartInvocationController(ExecOptions{Level: LevelScript, Dir: deep}, ContainmentIdentity{})
	if err == nil {
		t.Skip("this platform bound an over-long socket path; nothing to refuse")
	}
	if !contains(err.Error(), "unix-socket path limit") {
		t.Errorf("the refusal does not explain the limit or what to shorten: %v", err)
	}
}

func longName(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'd'
	}
	return string(b)
}

func contains(s, sub string) bool { return len(s) >= len(sub) && stringsIndex(s, sub) >= 0 }

func stringsIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestTheControllerAuthenticatesEveryRequester is the F2 boundary.
//
// The controller does, on request, everything the measured script must not be
// able to do: create a containment under the script's, admit a process into
// it, write ledgers, register signers, and spawn a measured child. Who may ask
// is therefore the whole of the boundary, and the answer comes from the KERNEL
// — SO_PEERCRED is recorded at connect time and cannot be forged — rather than
// from anything the caller says about itself.
func TestTheControllerAuthenticatesEveryRequester(t *testing.T) {
	t.Setenv(ScriptUserEnv, "tb-script")
	// One controller per case, each built with the credential reader the case
	// is about: a running controller's reader is never reassigned, so nothing
	// is shared between the serving goroutine and this one.
	serve := func(t *testing.T, peer func(net.Conn) (int, error)) string {
		t.Helper()
		dir := shortTempDir(t)
		c, err := startInvocationController(ExecOptions{Level: LevelScript, Dir: dir}, ContainmentIdentity{}, peer)
		if err != nil {
			t.Fatal(err)
		}
		if c == nil {
			t.Fatal("no controller was started for a declared script account")
		}
		t.Cleanup(func() {
			if c.Served() != 0 {
				t.Errorf("%d refused request(s) reached the measuring path", c.Served())
			}
			_ = c.Close()
		})
		return dir
	}

	// A REQUESTER THE KERNEL DOES NOT VOUCH FOR IS REFUSED. Every uid that is
	// neither the wrapper's nor the declared script account's — including the
	// workload account, which runs the test code inside these very invocations.
	for _, uid := range []int{os.Getuid() + 4242, os.Getuid() + 4243} {
		dir := serve(t, func(net.Conn) (int, error) { return uid, nil })
		_, served, err := RequestInvocation(dir, 0, filepath.Join(dir, "spec.json"))
		if !served {
			t.Fatal("the controller socket was not reachable")
		}
		if err == nil || !strings.Contains(err.Error(), "only the declared script account may") {
			t.Errorf("uid %d was served: %v", uid, err)
		}
	}

	// AND A CREDENTIAL THAT CANNOT BE READ AT ALL IS REFUSED, which is what
	// every platform without SO_PEERCRED does: serving a caller nobody can
	// attribute would hand the wrapper's capabilities to whoever connected.
	unknown := serve(t, func(net.Conn) (int, error) { return -1, errors.New("no peer credential on this platform") })
	if _, served, err := RequestInvocation(unknown, 0, filepath.Join(unknown, "spec.json")); !served || err == nil ||
		!strings.Contains(err.Error(), "could not be read") {
		t.Errorf("an unattributable requester was served: served=%v err=%v", served, err)
	}

	// A MALFORMED REQUEST from an authorized caller is refused too: the
	// channel accepts exactly one kind of ask.
	dir := serve(t, func(net.Conn) (int, error) { return os.Getuid(), nil })
	conn, err := net.Dial("unix", ControllerSocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("{\"kind\":\"something-else\"}\n")); err != nil {
		t.Fatal(err)
	}
	var reply controllerReply
	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if !strings.Contains(reply.Error, "names kind") {
		t.Errorf("a request of another kind was accepted: %+v", reply)
	}
}

// TestTheControllerIsTheOnlyPartyThatNests: the point of the channel is that
// the asking side holds none of the capabilities.
func TestTheControllerIsTheOnlyPartyThatNests(t *testing.T) {
	// The client path returns the controller's answer and does nothing else:
	// it creates no containment, writes no record and registers no signer.
	b, err := os.ReadFile(filepath.Join("..", "..", "cmd", "testbucket", "wall.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	at := strings.Index(src, "walltime.RequestInvocation(*dir, opt.Seq, *spec)")
	if at < 0 {
		t.Fatal("the invocation wrapper never asks the controller, so the measured script must nest for itself")
	}
	after := src[at:]
	if end := strings.Index(after, "\n\t}\n"); end > 0 {
		after = after[:end]
	}
	if !strings.Contains(after, "return nil") {
		t.Error("the client does not return after the controller answers, so it would also measure the invocation itself")
	}
	// And the controller runs the envelope under its own credential.
	run := productionFunc(t, "controller.go", "func (c *InvocationController) run(")
	for _, want := range []string{"LoadInvocationSpec(req.Spec)", "Level: LevelInvocation", "Parent: &c.parent", "return Exec(opt)"} {
		if !strings.Contains(run, want) {
			t.Errorf("the controller does not %q; the work has to happen on this side of the socket", want)
		}
	}
	// The request carries a spec PATH and nothing else: the argv, the cwd and
	// the selector come from the document the plan authorised, so a script
	// asking for work the plan does not contain is refused by the rule that
	// has always refused it.
	req := productionFunc(t, "controller.go", "type controllerRequest struct {")
	for _, forbidden := range []string{"Argv", "Cwd []", "Env "} {
		if strings.Contains(req, forbidden) {
			t.Errorf("the request carries %s; the measured script would then choose what is measured", forbidden)
		}
	}
}

// TestTheControllerClosesWithItsEnvelope: a request channel that outlives the
// interval it served is a capability left lying around.
func TestTheControllerClosesWithItsEnvelope(t *testing.T) {
	t.Setenv(ScriptUserEnv, "tb-script")
	dir := shortTempDir(t)
	c, err := StartInvocationController(ExecOptions{Level: LevelScript, Dir: dir}, ContainmentIdentity{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ControllerSocketPath(dir)); err != nil {
		t.Fatalf("the controller socket was not created: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if _, err := os.Stat(ControllerSocketPath(dir)); !os.IsNotExist(err) {
		t.Errorf("the socket outlived the envelope: %v", err)
	}
	if _, served, _ := RequestInvocation(dir, 0, "spec.json"); served {
		t.Error("a request was still served after the envelope closed")
	}
	// A second controller for the same directory is refused rather than
	// silently replacing the first.
	c2, err := StartInvocationController(ExecOptions{Level: LevelScript, Dir: dir}, ContainmentIdentity{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c2.Close() })
	if _, err := StartInvocationController(ExecOptions{Level: LevelScript, Dir: dir}, ContainmentIdentity{}); err == nil {
		t.Error("a second controller claimed an envelope another was already serving")
	}
	// With no declared script account there is no second credential to serve.
	t.Setenv(ScriptUserEnv, "")
	if none, err := StartInvocationController(ExecOptions{Level: LevelScript, Dir: shortTempDir(t)}, ContainmentIdentity{}); err != nil || none != nil {
		t.Errorf("a controller was opened with no second credential to serve: %v %v", none, err)
	}
}
