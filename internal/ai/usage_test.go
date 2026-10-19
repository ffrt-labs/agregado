package ai

import (
	"math"
	"testing"
)

func TestNeuronsEstimate(t *testing.T) {
	got, ok := usage{Prompt: 2000, Completion: 100}.neurons("@cf/google/gemma-4-26b-a4b-it")
	want := (2000*9091 + 100*27273) / 1e6
	if !ok || math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %v %v, want %v", got, ok, want)
	}
	if _, ok := (usage{Prompt: 1}).neurons("unknown"); ok {
		t.Fatal("unpriced model must report ok=false")
	}
}
