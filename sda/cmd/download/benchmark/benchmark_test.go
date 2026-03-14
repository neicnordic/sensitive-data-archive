package main

import (
	"math"
	"testing"
)

func TestParseBenchmarkMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    BenchmarkMode
		wantErr bool
	}{
		{name: "endpoint-e2e", input: "endpoint-e2e", want: ModeEndpointE2E},
		{name: "validated-payload", input: "validated-payload", want: ModeValidatedPayload},
		{name: "invalid", input: "nope", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseBenchmarkMode(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.input)
				}

				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPairedIterationTargets(t *testing.T) {
	t.Parallel()

	oldTarget := Target{Name: "old"}
	newTarget := Target{Name: "new"}

	if got := pairedIterationTargets(1, oldTarget, newTarget); got[0].Name != "old" || got[1].Name != "new" {
		t.Fatalf("odd iteration order mismatch: %#v", got)
	}
	if got := pairedIterationTargets(2, oldTarget, newTarget); got[0].Name != "new" || got[1].Name != "old" {
		t.Fatalf("even iteration order mismatch: %#v", got)
	}
}

func TestComparePayloadDigests(t *testing.T) {
	t.Parallel()

	base := payloadDigest{
		EncryptedBytes:  10,
		PlaintextBytes:  8,
		PlaintextSHA256: "abc",
	}

	if err := comparePayloadDigests(base, base); err != nil {
		t.Fatalf("expected equal digests, got error: %v", err)
	}

	if err := comparePayloadDigests(base, payloadDigest{
		EncryptedBytes:  11,
		PlaintextBytes:  8,
		PlaintextSHA256: "abc",
	}); err != nil {
		t.Fatalf("expected encrypted byte mismatch to be tolerated, got error: %v", err)
	}

	mismatch := []payloadDigest{
		{EncryptedBytes: 10, PlaintextBytes: 9, PlaintextSHA256: "abc"},
		{EncryptedBytes: 10, PlaintextBytes: 8, PlaintextSHA256: "def"},
	}

	for _, other := range mismatch {
		if err := comparePayloadDigests(base, other); err == nil {
			t.Fatalf("expected mismatch error for %#v", other)
		}
	}
}

func TestPercentChange(t *testing.T) {
	t.Parallel()

	if got := percentChange(110, 100); got != 10 {
		t.Fatalf("got %v, want 10", got)
	}
	if got := percentChange(0, 0); got != 0 {
		t.Fatalf("got %v, want 0", got)
	}
	if got := percentChange(5, 0); !math.IsInf(got, 1) {
		t.Fatalf("got %v, want +Inf", got)
	}
}
