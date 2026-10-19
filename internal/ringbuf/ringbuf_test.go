package ringbuf

import (
	"fmt"
	"reflect"
	"testing"
)

func TestFillAndWrap(t *testing.T) {
	b := New(3)
	if b.Len() != 0 || len(b.Lines()) != 0 {
		t.Fatal("new buffer should be empty")
	}
	b.Push("a")
	b.Push("b")
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("got %v", got)
	}
	for _, s := range []string{"c", "d", "e"} {
		b.Push(s)
	}
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"c", "d", "e"}) {
		t.Errorf("got %v", got)
	}
	if b.Dropped() != 2 {
		t.Errorf("Dropped = %d, want 2", b.Dropped())
	}
}

func TestBounded(t *testing.T) {
	b := New(100)
	for i := 0; i < 10000; i++ {
		b.Push(fmt.Sprint(i))
	}
	if b.Len() != 100 {
		t.Fatalf("Len = %d", b.Len())
	}
	lines := b.Lines()
	if lines[0] != "9900" || lines[99] != "9999" {
		t.Errorf("unexpected window %q..%q", lines[0], lines[99])
	}
}

func TestMinCapacity(t *testing.T) {
	b := New(0)
	b.Push("x")
	b.Push("y")
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"y"}) {
		t.Errorf("got %v", got)
	}
}
