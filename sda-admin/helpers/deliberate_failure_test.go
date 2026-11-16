package helpers

import "testing"

// Throwaway test for #2758: it fails on purpose to show that PR result turns red.
func TestDeliberateFailure(t *testing.T) {
	t.Fatal("deliberate failure for #2758")
}
