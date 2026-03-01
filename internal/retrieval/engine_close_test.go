package retrieval

import (
	"slices"
	"testing"
)

// verify that Engine.Close invokes registered cleanup functions in LIFO order.
// the existing implementation iterated the slice in insertion order which
// could result in dependent resources being closed before the things they
// relied on. reversing the iteration ensures the last-created resource is
// torn down first.
func TestEngineClose_LIFOOrder(t *testing.T) {
	var calls []int
	e := &Engine{
		closeFns: []func(){
			func() { calls = append(calls, 1) },
			func() { calls = append(calls, 2) },
			func() { calls = append(calls, 3) },
		},
	}

	e.Close()

	expected := []int{3, 2, 1}
	if !slices.Equal(calls, expected) {
		t.Fatalf("unexpected call order; want %v, got %v", expected, calls)
	}

	// calling Close again should be a no-op and not append additional values.
	e.Close()
	if !slices.Equal(calls, expected) {
		t.Fatalf("multiple Close calls mutated order; want %v, got %v", expected, calls)
	}
}
