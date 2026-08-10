package prop

import "testing"

func TestComputedCachesUntilInvalidated(t *testing.T) {
	src := NewSource(1)
	dbl := NewComputed(func() int { return src.Get() * 2 })

	if got := dbl.Get(); got != 2 {
		t.Fatalf("Get = %d, want 2", got)
	}
	dbl.Get()
	dbl.Get()
	if dbl.Evals() != 1 {
		t.Fatalf("evals = %d, want 1 (cached)", dbl.Evals())
	}

	src.Set(5)
	if got := dbl.Get(); got != 10 {
		t.Fatalf("Get after Set = %d, want 10", got)
	}
	if dbl.Evals() != 2 {
		t.Fatalf("evals = %d, want 2", dbl.Evals())
	}
}

func TestConditionalDependenciesReRecord(t *testing.T) {
	mode := NewSource("a")
	a := NewSource(1)
	b := NewSource(1)
	pick := NewComputed(func() int {
		if mode.Get() == "a" {
			return a.Get()
		}
		return b.Get()
	})

	pick.Get() // records mode, a

	b.Set(99) // not a dependency right now
	if pick.n.dirty {
		t.Fatal("bumping unwatched source dirtied the computed")
	}
	pick.Get()
	if pick.Evals() != 1 {
		t.Fatalf("evals = %d, want 1 — b is not watched", pick.Evals())
	}

	mode.Set("b") // switch branch
	if got := pick.Get(); got != 99 {
		t.Fatalf("Get = %d, want 99", got)
	}

	a.Set(42) // no longer a dependency after re-record
	if pick.n.dirty {
		t.Fatal("bumping a after branch switch dirtied the computed")
	}
	b.Set(7) // watched now
	if !pick.n.dirty {
		t.Fatal("bumping b after branch switch did not dirty the computed")
	}
	if got := pick.Get(); got != 7 {
		t.Fatalf("Get = %d, want 7", got)
	}
}

func TestChainedComputedsAndInvalidateHook(t *testing.T) {
	src := NewSource(2)
	sq := NewComputed(func() int { v := src.Get(); return v * v })
	label := NewComputed(func() int { return sq.Get() + 1 })

	fired := 0
	label.OnInvalidate(func() { fired++ })

	if got := label.Get(); got != 5 {
		t.Fatalf("Get = %d, want 5", got)
	}
	src.Set(3)
	if fired != 1 {
		t.Fatalf("invalidate fired %d times, want 1", fired)
	}
	src.Set(4) // already dirty — hook must not refire until cleaned
	if fired != 1 {
		t.Fatalf("invalidate fired %d times while dirty, want 1", fired)
	}
	if got := label.Get(); got != 17 {
		t.Fatalf("Get = %d, want 17", got)
	}
	src.Set(5)
	if fired != 2 {
		t.Fatalf("invalidate fired %d times after clean, want 2", fired)
	}
}
