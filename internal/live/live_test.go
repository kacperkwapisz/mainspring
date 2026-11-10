package live

import (
	"slices"
	"testing"
)

func TestBurstCollapsesIntoOneSet(t *testing.T) {
	b := New()
	s, drop := b.Subscribe()
	defer drop()
	for i := 0; i < 100; i++ {
		b.Publish(Meter, Pool)
	}
	b.Publish(Keys)
	<-s.Wake
	got := s.Take()
	slices.Sort(got)
	if !slices.Equal(got, []string{Keys, Meter, Pool}) {
		t.Fatalf("got %v", got)
	}
	if len(s.Take()) != 0 {
		t.Fatal("Take must clear")
	}
}

func TestNilBusAndDroppedSubscriber(t *testing.T) {
	var nb *Bus
	nb.Publish(Pool) // no panic
	b := New()
	s, drop := b.Subscribe()
	drop()
	b.Publish(Pool)
	if len(s.Take()) != 0 {
		t.Fatal("dropped subscriber still receives")
	}
}
