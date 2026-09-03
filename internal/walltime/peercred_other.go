//go:build !linux

package walltime

import (
	"fmt"
	"net"
)

// peerUID cannot be read portably, so this platform REFUSES every request
// rather than serving one it cannot attribute.
//
// That is the correct answer rather than a gap: the controller exists to do
// work the measured script must not do, and serving a caller whose credential
// is unknown would hand exactly that work to whoever connected first. A host
// without SO_PEERCRED also has no cgroup-v2 containment to nest, so nothing
// scorable is lost.
func peerUID(net.Conn) (int, error) {
	return -1, fmt.Errorf("this platform cannot read a socket peer credential, so no requester can be attributed")
}
