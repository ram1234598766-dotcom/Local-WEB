package crdt

import (
	"testing"
)

// Merge used to append every unknown node at the tail in (Timestamp, Author)
// order and threw away the predecessor each node was inserted after. A node's
// position in an RGA is defined by what it followed, so two replicas that
// applied the same operations in different orders produced different text and
// could never be reconciled. These tests are the convergence property that
// matters, exercised in both directions.
//
// Note on Length: it counts every node in the list including tombstoned ones,
// which is the contract Insert and Delete already follow. String is the live
// text, and these tests compare that.

// live returns the visible characters so a divergence can be reported by index.
func live(t *testing.T, r *RGA) []string {
	t.Helper()
	s := r.String()
	out := make([]string, 0, len(s))
	for _, c := range s {
		out = append(out, string(c))
	}
	return out
}

func requireSameText(t *testing.T, a, b *RGA, context string) {
	t.Helper()
	as, bs := live(t, a), live(t, b)
	if len(as) != len(bs) {
		t.Fatalf("%s: text lengths differ: %q vs %q", context, a.String(), b.String())
	}
	for i := range as {
		if as[i] != bs[i] {
			t.Fatalf("%s: replicas diverged at %d: %q vs %q", context, i, a.String(), b.String())
		}
	}
}

// concurrentSources builds n replicas that each hold one concurrent insert after
// head, authored distinctly so (Timestamp, Author) is a total order over them.
func concurrentSources(vals ...string) []*RGA {
	out := make([]*RGA, 0, len(vals))
	for i, v := range vals {
		r := NewRGA(string(rune('a' + i)))
		r.Insert("head", v)
		out = append(out, r)
	}
	return out
}

// Two replicas learn the same concurrent inserts in opposite orders and must
// still end up with identical text.
func TestRGAMergeConvergesOnConcurrentInsertsAtSamePosition(t *testing.T) {
	sources := concurrentSources("A", "B")

	a, b := NewRGA("reader1"), NewRGA("reader2")
	for _, s := range sources {
		a.Merge(s)
	}
	for i := len(sources) - 1; i >= 0; i-- {
		b.Merge(sources[i])
	}

	requireSameText(t, a, b, "concurrent inserts at head")
	if a.String() != "AB" && a.String() != "BA" {
		t.Errorf("merged text = %q, want both characters present", a.String())
	}
}

// The same operations delivered in reverse order must converge. Tail-append
// merge could never pass this: the late-arriving edit lands in a different
// absolute position depending on when it was learned.
func TestRGAMergeConvergesRegardlessOfArrivalOrder(t *testing.T) {
	sources := concurrentSources("a", "b", "c")

	build := func(order []*RGA) *RGA {
		r := NewRGA("reader")
		for _, s := range order {
			r.Merge(s)
		}
		return r
	}

	forward := build(sources)
	backward := build([]*RGA{sources[2], sources[1], sources[0]})

	requireSameText(t, forward, backward, "same operations, opposite arrival order")
	if forward.Length() != 3 {
		t.Errorf("length = %d, want 3", forward.Length())
	}
}

// Sibling order must come from (Timestamp, Author), not from arrival.
func TestRGASiblingsAreOrderedNotArrivalOrdered(t *testing.T) {
	sources := concurrentSources("a", "b", "c")

	perms := [][]int{
		{0, 1, 2}, {2, 1, 0}, {1, 2, 0}, {1, 0, 2}, {0, 2, 1}, {2, 0, 1},
	}

	var want string
	for i, p := range perms {
		r := NewRGA("reader")
		for _, idx := range p {
			r.Merge(sources[idx])
		}
		got := r.String()
		if i == 0 {
			want = got
			continue
		}
		if got != want {
			t.Errorf("delivery order %v produced %q, want %q; sibling order depends on arrival",
				p, got, want)
		}
	}
	// Deterministic tie-break by author when timestamps are equal.
	if want != "abc" {
		t.Errorf("siblings with equal timestamps resolved to %q, want \"abc\" (author order)", want)
	}
}

// A node that follows an existing node must stay after it, not move to the end.
func TestRGAMergePreservesCausalPosition(t *testing.T) {
	author := NewRGA("alice")
	author.Insert("head", "o")
	one := author.IDs()[0]
	author.Insert(one, "n")
	n := author.IDs()[1]
	author.Insert(n, "e")

	// A replica that already holds "one" learns the whole document.
	other := NewRGA("bob")
	other.Merge(author)

	if other.String() != "one" {
		t.Errorf("merged text = %q, want %q", other.String(), "one")
	}
	if other.Length() != author.Length() {
		t.Errorf("length = %d, want %d", other.Length(), author.Length())
	}
}

// A multi-node causal chain arriving in one merge must be placed as a chain,
// not scattered. The receiver's own node keeps its own sorted position.
func TestRGAMergePlacesACausalChainInOrder(t *testing.T) {
	author := NewRGA("alice")
	author.Insert("head", "o")
	first := author.IDs()[0]
	author.Insert(first, "n")
	second := author.IDs()[1]
	author.Insert(second, "e")

	receiver := NewRGA("receiver")
	// The receiver already has its own character after head, with the same
	// timestamp as the author's first node, so author sorts ahead of it.
	receiver.Insert("head", "z")

	receiver.Merge(author)

	// Author's chain is contiguous and ordered: "one" then the receiver's "z".
	if got := receiver.String(); got != "onez" {
		t.Errorf("text = %q, want \"onez\"; the merged chain lost its internal order", got)
	}
}

// An RGA tombstone is permanent: a delete that is causally newer must not be
// undone by re-learning an older record of the same node.
func TestRGAMergeDoesNotLiftAPermanentTombstone(t *testing.T) {
	older := NewRGA("alice")
	older.Insert("head", "x")

	reader := NewRGA("reader")
	reader.Merge(older)
	reader.Delete(reader.IDs()[0])
	if reader.String() != "" {
		t.Fatalf("setup: text = %q, want the delete to hide the character", reader.String())
	}

	// Re-learning the older, un-deleted record must not resurrect it.
	reader.Merge(older)
	if reader.String() != "" {
		t.Errorf("text = %q, want \"\"; an older record resurrected a deleted node", reader.String())
	}
	if reader.Length() != older.Length() {
		t.Errorf("length = %d, want %d; the tombstoned node must stay in the list",
			reader.Length(), older.Length())
	}
}

// A delete in one replica must propagate so both stop showing erased text.
func TestRGAMergePropagatesDeletes(t *testing.T) {
	author := NewRGA("alice")
	author.Insert("head", "x")
	author.Insert("head", "y")
	author.Delete(author.IDs()[0])

	other := NewRGA("bob")
	other.Merge(author)

	if other.String() != author.String() {
		t.Errorf("merged text = %q, want %q; the delete did not propagate",
			other.String(), author.String())
	}
	if other.String() != "y" {
		t.Errorf("text = %q, want \"y\" after deleting one of two", other.String())
	}
	// Length counts tombstoned nodes too, so it must match the author's.
	if other.Length() != author.Length() {
		t.Errorf("length = %d, want %d", other.Length(), author.Length())
	}
}

// A delete arriving for a node the reader already has must still be applied.
// The old merge only inspected unknown nodes, so it silently dropped these.
func TestRGAMergeAppliesDeleteForAlreadyKnownNode(t *testing.T) {
	reader := NewRGA("reader")
	writer := NewRGA("writer")
	writer.Insert("head", "p")
	writer.Insert("head", "q")
	reader.Merge(writer)

	if reader.String() != "pq" && reader.String() != "qp" {
		t.Fatalf("setup produced %q, want both characters", reader.String())
	}

	// The writer erases a node the reader already holds.
	writer.Delete(writer.IDs()[0])
	reader.Merge(writer)

	if reader.Length() != writer.Length() {
		t.Errorf("reader length = %d, want %d; a delete for a known node was ignored",
			reader.Length(), writer.Length())
	}
	if len(reader.String()) != 1 {
		t.Errorf("reader text = %q, want a single character", reader.String())
	}
	if reader.String() != writer.String() {
		t.Errorf("reader text = %q, want %q", reader.String(), writer.String())
	}
}

// Nested inserts: a node follows a node that itself follows another, and a
// second node is inserted concurrently at the start. Replicas that learn the
// identical set of operations in different orders must agree.
//
// Note the second node is a concurrent child of head, not a sibling of the
// nested node: Insert falls back to head when the named node is absent, so a
// replica that has not seen the root cannot position itself relative to it.
func TestRGAMergeConvergesWithNestedInserts(t *testing.T) {
	base := NewRGA("alice")
	base.Insert("head", "1")
	root := base.IDs()[0]
	base.Insert(root, "3")

	// A concurrent node at the same root, authored by someone else. It knows the
	// root's id but does not hold the base's nodes.
	solo := NewRGA("dave")
	solo.Insert(root, "2")

	// Two receivers learn the same two operations in opposite orders.
	forward := NewRGA("f1")
	forward.Merge(base)
	forward.Merge(solo)

	backward := NewRGA("b1")
	backward.Merge(solo)
	backward.Merge(base)

	requireSameText(t, forward, backward, "nested inserts, opposite order")

	// A receiver that folds them in through an intermediary must also agree.
	middle := NewRGA("m1")
	middle.Merge(forward)
	middle.Merge(solo)
	requireSameText(t, middle, forward, "nested inserts, via intermediary")

	if forward.Length() != 3 {
		t.Errorf("length = %d, want 3", forward.Length())
	}
	// "3" is causally after "1", so it must stay adjacent to it. The sibling
	// cannot reference the root at all, because Insert falls back to head when
	// the named node is absent, so it is a concurrent child of head rather than
	// a sibling of "3". It therefore follows the whole "1,3" subtree, giving
	// "132" and not "123".
	if got, want := forward.String(), "132"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestRGAMergeIsIdempotent(t *testing.T) {
	sources := concurrentSources("1", "2", "3")
	a := NewRGA("a")
	for _, s := range sources {
		a.Merge(s)
	}
	once, lengthOnce := a.String(), a.Length()

	for i := 0; i < 3; i++ {
		for _, s := range sources {
			a.Merge(s)
		}
	}
	if a.String() != once {
		t.Errorf("text changed on re-merge: %q then %q", once, a.String())
	}
	if a.Length() != lengthOnce {
		t.Errorf("length changed on re-merge: %d then %d", lengthOnce, a.Length())
	}
}

func TestRGAMergeIgnoresNilAndSelf(t *testing.T) {
	a := NewRGA("alice")
	a.Insert("head", "x")

	a.Merge(nil)
	a.Merge(a)

	if a.Length() != 1 || a.String() != "x" {
		t.Errorf("length = %d text = %q, want 1 and \"x\"", a.Length(), a.String())
	}
}

// Merging must not corrupt the doubly-linked list: every node's Prev/Next must
// still agree in both directions.
func TestRGAMergeKeepsLinksConsistent(t *testing.T) {
	sources := concurrentSources("a", "b", "c", "d")
	a := NewRGA("reader")
	for _, s := range sources {
		a.Merge(s)
	}

	// Reach into the list directly to check both directions.
	curr := a.head.Next
	prev := a.head
	seen := 0
	for curr != nil && curr != a.tail {
		if curr.Prev != prev {
			t.Fatalf("node %q has Prev %v, want %q; the list is inconsistent", curr.ID, curr.Prev, prev.ID)
		}
		prev = curr
		curr = curr.Next
		seen++
	}
	if a.tail.Prev != prev {
		t.Errorf("tail.Prev = %v, want the last real node", a.tail.Prev)
	}
	if seen != 4 {
		t.Errorf("walked %d nodes, want 4", seen)
	}
}
