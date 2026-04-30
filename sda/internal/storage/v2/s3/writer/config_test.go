package writer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSortBucketsNumerically_OrderingFollowsSuffix pins the regression for
// #2413 — under plain `strings.Compare` ordering, ten buckets named
// `bucket-1`…`bucket-10` sort as `[bucket-1 bucket-10 bucket-2 …
// bucket-9]`. The "latest" entry then becomes `bucket-9`, so the next
// allocation attempts `bucket-10`, which already exists.
// `sortBucketsNumerically` must place numeric suffixes in true numeric
// order so the last element is always the highest-numbered bucket.
func TestSortBucketsNumerically_OrderingFollowsSuffix(t *testing.T) {
	t.Parallel()

	buckets := []string{
		"bucket-1", "bucket-10", "bucket-2", "bucket-3", "bucket-4",
		"bucket-5", "bucket-6", "bucket-7", "bucket-8", "bucket-9",
	}
	sortBucketsNumerically(buckets, "bucket-")

	want := []string{
		"bucket-1", "bucket-2", "bucket-3", "bucket-4", "bucket-5",
		"bucket-6", "bucket-7", "bucket-8", "bucket-9", "bucket-10",
	}
	assert.Equal(t, want, buckets,
		"buckets must be ordered by integer suffix so bucket-10 lands after bucket-9")
	assert.Equal(t, "bucket-10", buckets[len(buckets)-1],
		"highest-numbered bucket must be last so the next allocation increments it")
}

// TestSortBucketsNumerically_NumericFallback pins the behaviour when a
// bucket name does not match the expected `<prefix><integer>` shape —
// the comparator falls back to lexical ordering between that pair so
// the sort stays total and Go's slice-sort contract isn't violated.
func TestSortBucketsNumerically_NumericFallback(t *testing.T) {
	t.Parallel()

	// "bucket-mixed" can't be parsed as an integer suffix; the comparator
	// must not panic or produce a non-deterministic order.
	buckets := []string{"bucket-10", "bucket-mixed", "bucket-2"}
	sortBucketsNumerically(buckets, "bucket-")

	assert.Contains(t, buckets, "bucket-mixed",
		"non-numeric bucket name must still appear in the result")
	assert.Len(t, buckets, 3, "no bucket name should be dropped during sort")
}

// TestSortBucketsNumerically_EmptyAndSingle covers degenerate inputs so
// that we don't accidentally regress the n=0/n=1 paths.
func TestSortBucketsNumerically_EmptyAndSingle(t *testing.T) {
	t.Parallel()

	var empty []string
	sortBucketsNumerically(empty, "bucket-")
	assert.Empty(t, empty)

	single := []string{"bucket-7"}
	sortBucketsNumerically(single, "bucket-")
	assert.Equal(t, []string{"bucket-7"}, single)
}
