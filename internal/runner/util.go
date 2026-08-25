package runner

import "sort"

// SortedKeys returns a map's string keys in sorted order, so every reduction
// over a map runs in a fixed, value-derived order rather than Go's randomised
// map-iteration order — the difference between a byte-identical plan on
// re-run and one that quietly reshuffles.
func SortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SortedInts is the integer twin of SortedKeys.
func SortedInts(in []int) []int {
	out := append([]int(nil), in...)
	sort.Ints(out)
	return out
}

// SetOfKeys returns the keys of an int-keyed map.
func SetOfKeys[V any](m map[int]V) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Dedupe collapses adjacent duplicates in an already-sorted slice.
func Dedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := in[:1]
	for _, v := range in[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}
