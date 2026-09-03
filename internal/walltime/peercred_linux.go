package walltime

import (
	"fmt"
	"net"
	"syscall"
)

// peerUID is the credential of the process on the other end of a unix socket,
// read from the KERNEL rather than from anything the peer said.
//
// SO_PEERCRED is recorded at connect time and cannot be forged or changed
// afterwards, which is what makes it an authentication rather than a claim.
func peerUID(conn net.Conn) (int, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return -1, fmt.Errorf("not a unix connection")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return -1, err
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return -1, err
	}
	if credErr != nil {
		return -1, credErr
	}
	return int(cred.Uid), nil
}
