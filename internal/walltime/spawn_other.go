package walltime

// THE OBSERVER-PROTOCOL SPAWN HELPERS LIVED HERE.
//
// containmentSysProc built the child's SysProcAttr from a Containment,
// postSpawnAdmit admitted the spawned pid to it, and joinContainment moved a
// later step's process into a containment created by an earlier one. All three
// took or returned the removed abstraction, and none had a caller: the live
// path is execgroup_unix.go's ownProcessGroup and childProcessGroup, which put
// the child in its own group and read back the group it actually landed in.
//
// processGroupOf and processParentOf went with them — the first duplicated
// childProcessGroup, and the second could not be read portably and always
// returned 0.
//
// The file stays because the component map lists it SIMPLIFY, and a SIMPLIFY
// path is reduced rather than deleted. There is nothing left to reduce.
