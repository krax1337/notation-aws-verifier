package cache

import (
	"fmt"
	"testing"
	"time"

	"github.com/krax1337/notation-aws-verifier/internal/types"
)

func TestCacheIsBoundedByMaxSize(t *testing.T) {
	const maxSize = 10
	c, err := New(WithCacheEnabled(true), WithMaxSize(maxSize), WithTTLDuration(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	const inserted = 1000
	for i := range inserted {
		if err := c.AddImage("tp", fmt.Sprintf("registry.example/app:%d", i), types.Image{}); err != nil {
			t.Fatal(err)
		}
	}
	c.(*cache).ristretto.Wait()

	present := 0
	for i := range inserted {
		if _, ok := c.GetImage("tp", fmt.Sprintf("registry.example/app:%d", i)); ok {
			present++
		}
	}
	if present > maxSize {
		t.Fatalf("%d entries cached, want at most %d", present, maxSize)
	}
}

func TestDisabledCacheNeverHits(t *testing.T) {
	c, err := New(WithCacheEnabled(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.AddImage("tp", "registry.example/app:1", types.Image{}); err != nil {
		t.Fatal(err)
	}
	c.(*cache).ristretto.Wait()
	if _, ok := c.GetImage("tp", "registry.example/app:1"); ok {
		t.Fatal("disabled cache returned an entry")
	}
}
