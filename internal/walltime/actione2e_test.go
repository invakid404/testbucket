package walltime

// The action end-to-end fixtures lived here.
//
// They built per-invocation stream names for a run assembled from ledger
// fixtures. The action lifecycle is now exercised end to end through the
// production entry points — see the action span in actionspan_test.go and the
// rollback in beginrollback_test.go — so nothing here has a caller.
