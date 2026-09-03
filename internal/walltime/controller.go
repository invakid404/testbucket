package walltime

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// THE INVOCATION CONTROLLER, and why it has to exist.
//
// On cgroup-v2 a process may place a child into a sub-cgroup only if it can
// write the COMMON ANCESTOR's `cgroup.procs`. So whoever creates and admits
// the nested invocation containments must be able to write the script
// containment's membership file — and the measured script must not, because
// that file is the migration control the whole envelope rests on.
//
// Those two facts are irreconcilable in one process, and the previous design
// tried to reconcile them by delegating the script subtree to the measured
// script. The verifier then correctly refused every complete-script row: a
// containment whose membership its own measured process controls proves
// nothing.
//
// So the two parties are two processes. The script-level wrapper — which runs
// under the wrapper credential, is the parent of the measured script, and dies
// with the envelope — stays alive and does that work ON REQUEST. The measured
// script asks; it never creates a containment, never admits a process, never
// writes a ledger and never registers a signer. The script containment is
// therefore left supervisor-owned, which is what makes the level scorable at
// all.
//
// The request channel is a unix socket in the evidence directory, and every
// connection is authenticated by the KERNEL's answer to "who is on the other
// end" rather than by anything the caller says about itself.
type InvocationController struct {
	listener net.Listener
	path     string
	opt      ExecOptions
	parent   ContainmentIdentity
	// allowUID is the credential permitted to ask. It is the declared script
	// account, plus this process's own uid so a single-credential developer
	// run still works.
	allowUID []int
	// peer reads a requester's credential. It is fixed when the controller is
	// constructed and never reassigned, so the serving goroutine and its
	// creator never share a mutable reference to it — a test stands in for the
	// platform facility by building a controller with a different one, not by
	// reaching into a running one.
	peer   func(net.Conn) (int, error)
	mu     sync.Mutex
	done   chan struct{}
	wg     sync.WaitGroup
	served int
}

// controllerSocketName is the request channel's name inside the evidence
// directory. It is a socket rather than a file so that a caller's credential
// can be read from the connection.
const controllerSocketName = ".invocation-controller.sock"

// ControllerSocketPath is where an invocation wrapper looks for the
// controller.
func ControllerSocketPath(dir string) string {
	return filepath.Join(dir, controllerSocketName)
}

// controllerRequest is one invocation the measured script is asking for. It
// names the SPEC the script wrote and nothing else: the argv, the cwd and the
// selector all come from that spec, which the verifier compares against the
// authorised plan, so a script that asks for work the plan does not contain is
// refused by the same rule that has always refused it.
type controllerRequest struct {
	Kind string `json:"kind"`
	Seq  int    `json:"seq"`
	Spec string `json:"spec"`
}

// controllerReply is the measured child's exit status, or the reason there is
// none.
type controllerReply struct {
	Exit  int    `json:"exit"`
	Error string `json:"error,omitempty"`
}

// controllerRequestKind identifies the one request this channel accepts.
const controllerRequestKind = "tb.walltime.invocation-request/v1"

// maxRequestBytes bounds what an unauthenticated peer can make this process
// hold. A request names a sequence number and a path; anything larger is not
// one.
const maxRequestBytes = 64 << 10

// drainRequest reads and discards whatever the caller sent, so the reply can
// be delivered before the connection closes. It is bounded for the same reason
// the decoder is.
func drainRequest(conn net.Conn) {
	_ = conn.SetReadDeadline(time.Now().Add(controllerDrainGrace))
	_, _ = io.Copy(io.Discard, io.LimitReader(conn, maxRequestBytes))
	_ = conn.SetReadDeadline(time.Time{})
}

// controllerDrainGrace bounds that drain. It is short on purpose: a
// well-behaved caller half-closes as soon as it has asked, so the drain ends
// at EOF and never waits: this budget is only ever spent on a caller that
// asked and then held its write side open, and the channel is serial, so
// waiting longer on that caller would delay every other one.
const controllerDrainGrace = 250 * time.Millisecond

// StartInvocationController opens the channel for one script envelope.
//
// It returns nil when there is no second credential to serve: with no declared
// script account the measured script runs as the wrapper itself and can do its
// own nesting, which is the developer path and is unscorable for other reasons
// already.
func StartInvocationController(opt ExecOptions, parent ContainmentIdentity) (*InvocationController, error) {
	return startInvocationController(opt, parent, peerUID)
}

// startInvocationController is StartInvocationController with the peer-credential
// reader supplied, which is how a test stands in for a platform facility the
// way ObserverLauncher lets one stand in for the observer launcher.
func startInvocationController(opt ExecOptions, parent ContainmentIdentity, peer func(net.Conn) (int, error)) (*InvocationController, error) {
	if opt.Level != LevelScript || strings.TrimSpace(os.Getenv(ScriptUserEnv)) == "" {
		return nil, nil
	}
	path := ControllerSocketPath(opt.Dir)
	// A stale socket is refused rather than replaced: something else claiming
	// this channel is not a condition to paper over.
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("walltime: %s already exists; another controller claims this envelope", path)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		// A unix socket path is bounded by the kernel (108 bytes on Linux,
		// 104 on Darwin), and the bind failure for an over-long one is an
		// opaque EINVAL. Saying which limit was hit, and that the records
		// directory is the thing to shorten, is the difference between a
		// diagnosable refusal and a mystery: without this channel the measured
		// script cannot run one invocation, so failing closed here is right
		// and explaining it is the least this can do.
		if len(path) >= 100 {
			return nil, fmt.Errorf(
				"walltime: open the invocation controller: %q is %d bytes, at or past the kernel's unix-socket path limit; the measured script reaches this wrapper through that socket, so choose a shorter records directory: %w",
				path, len(path), err)
		}
		return nil, fmt.Errorf("walltime: open the invocation controller: %w", err)
	}
	// The socket is created with the evidence directory's group (setgid) and
	// is made group-reachable, so the declared script account can connect and
	// nothing wider can. Its OWNER is the wrapper, so the script cannot
	// replace it.
	if err := os.Chmod(path, 0o660); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("walltime: restrict the invocation controller socket: %w", err)
	}
	c := &InvocationController{
		listener: listener, path: path, opt: opt, parent: parent,
		allowUID: allowedRequesterUIDs(), done: make(chan struct{}), peer: peer,
	}
	c.wg.Add(1)
	go c.serve()
	return c, nil
}

// allowedRequesterUIDs is who may ask: the declared script account, and this
// process itself. Nobody else — not the workload account that runs the test
// code, and not another user on the runner.
func allowedRequesterUIDs() []int {
	out := []int{os.Getuid()}
	if uid := resolveWorkloadCredential(strings.TrimSpace(os.Getenv(ScriptUserEnv))).UID; uid >= 0 {
		out = append(out, uid)
	}
	return out
}

func (c *InvocationController) serve() {
	defer c.wg.Done()
	for {
		conn, err := c.listener.Accept()
		if err != nil {
			select {
			case <-c.done:
				return
			default:
				return
			}
		}
		c.handle(conn)
	}
}

// handle serves one request, serially.
//
// Serially because the invocations of one bucket are a SEQUENCE — the cost
// model the planner packs to is the sum of their durations — and because two
// concurrent envelopes would race on the containment names and the ledger
// sequence. A script that asks for two at once gets them one after another.
func (c *InvocationController) handle(conn net.Conn) {
	defer conn.Close()
	c.mu.Lock()
	defer c.mu.Unlock()

	reply := func(r controllerReply) {
		b, err := json.Marshal(r)
		if err != nil {
			return
		}
		_, _ = conn.Write(append(b, '\n'))
	}
	// WHO IS ASKING, ANSWERED BY THE KERNEL.
	//
	// The request says nothing about its own identity and would not be
	// believed if it did. The peer credential comes from the socket layer, so
	// a process that is not the declared script account cannot ask this
	// wrapper to create a containment or spawn a measured child — including
	// the workload account, which runs the test code inside those very
	// invocations.
	uid, err := c.peer(conn)
	if err != nil {
		drainRequest(conn)
		reply(controllerReply{Exit: 1, Error: "the requester's credential could not be read: " + err.Error()})
		return
	}
	var allowed bool
	for _, u := range c.allowUID {
		if u == uid {
			allowed = true
		}
	}
	if !allowed {
		// THE REFUSAL HAS TO REACH THE CALLER.
		//
		// Closing with the request still unread makes the kernel discard the
		// connection abruptly — on Linux the client's own write then fails
		// with EPIPE and it never reads why it was refused, so a decision this
		// controller made deliberately arrives as a broken pipe and is
		// indistinguishable from a crash. Draining first lets the reply be
		// delivered, and the read is bounded because an unauthenticated peer
		// must not be able to make this process hold anything large.
		drainRequest(conn)
		reply(controllerReply{Exit: 1, Error: fmt.Sprintf(
			"uid %d asked for an invocation envelope; only the declared script account may (%v)", uid, c.allowUID)})
		return
	}

	var req controllerRequest
	if err := json.NewDecoder(io.LimitReader(conn, maxRequestBytes)).Decode(&req); err != nil {
		reply(controllerReply{Exit: 1, Error: "unreadable invocation request: " + err.Error()})
		return
	}
	if req.Kind != controllerRequestKind {
		drainRequest(conn)
		reply(controllerReply{Exit: 1, Error: fmt.Sprintf("invocation request names kind %q, want %q", req.Kind, controllerRequestKind)})
		return
	}
	c.served++
	code, err := c.run(req)
	if err != nil {
		reply(controllerReply{Exit: code, Error: err.Error()})
		return
	}
	reply(controllerReply{Exit: code})
}

// run measures one invocation, under the wrapper's own credential.
//
// Everything the envelope needs a capability for happens HERE: the containment
// is created under the script containment and admitted into, the observers are
// started, the ledgers are written, the signer is registered, and the measured
// child is dropped to the workload account. The requester supplied a path and
// nothing more.
func (c *InvocationController) run(req controllerRequest) (int, error) {
	spec, err := LoadInvocationSpec(req.Spec)
	if err != nil {
		return 1, err
	}
	opt := ExecOptions{
		Level: LevelInvocation, Seq: req.Seq, Dir: c.opt.Dir, Run: c.opt.Run,
		Argv: spec.Argv, Cwd: spec.Cwd, Selector: spec.Selector, Desc: spec.Desc,
		UnitDigest: spec.UnitDigest, AtomDigest: spec.AtomDigest,
		Timeout: c.opt.Timeout, Parent: &c.parent, JoinParent: false,
		Stdout: c.opt.Stdout, Stderr: c.opt.Stderr,
	}
	return Exec(opt)
}

// Served is how many invocations were measured through this channel.
func (c *InvocationController) Served() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.served
}

// Close stops accepting and removes the socket. It is called before the script
// envelope's closing reading, so the channel does not outlive the interval it
// served.
func (c *InvocationController) Close() error {
	if c == nil {
		return nil
	}
	close(c.done)
	err := c.listener.Close()
	c.wg.Wait()
	_ = os.Remove(c.path)
	return err
}

// RequestInvocation asks the controller to measure one invocation and returns
// the measured child's exit status.
//
// This is the whole of what a nested invocation wrapper does when a controller
// is present. It creates nothing, admits nothing, writes no record and
// registers no key: it holds none of the capabilities those need, which is the
// point.
func RequestInvocation(dir string, seq int, specPath string) (int, bool, error) {
	path := ControllerSocketPath(dir)
	if _, err := os.Stat(path); err != nil {
		return 0, false, nil
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		return 1, true, fmt.Errorf("walltime: reach the invocation controller: %w", err)
	}
	defer conn.Close()
	b, err := json.Marshal(controllerRequest{Kind: controllerRequestKind, Seq: seq, Spec: specPath})
	if err != nil {
		return 1, true, err
	}
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return 1, true, fmt.Errorf("walltime: send the invocation request: %w", err)
	}
	// HALF-CLOSE, so the controller sees a clean end of request rather than
	// having to guess where it stopped, and so a refusal written back reaches
	// this side instead of racing an abrupt close.
	if half, ok := conn.(*net.UnixConn); ok {
		_ = half.CloseWrite()
	}
	var reply controllerReply
	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		return 1, true, fmt.Errorf("walltime: read the controller's reply: %w", err)
	}
	if reply.Error != "" {
		return reply.Exit, true, fmt.Errorf("walltime: the invocation controller refused: %s", reply.Error)
	}
	return reply.Exit, true, nil
}
