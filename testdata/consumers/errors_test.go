package consumers

import "fmt"

func errMultiUnit(n int) error {
	return fmt.Errorf("selected work carries %d units; any surviving structure requires unit-list equality with its unit id, so a multi-unit list is a LABEL and not selected work", n)
}

func errUnitMismatch(unitID, listed string) error {
	return fmt.Errorf("selected work names unit %q but its unit list carries %q; the two must be equal", unitID, listed)
}
