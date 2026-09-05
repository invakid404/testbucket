package walltime

import "syscall"

// detachedSysProc gives an observer its own session so it is not killed when
// the step that started it finishes. The action envelope spans several
// Actions steps; its peer and collector have to span them too.
func detachedSysProc() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
