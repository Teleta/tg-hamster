package main

import (
    "testing"
    "strings"
)

func TestProgressBar(t *testing.T) {
    bar := progressBar(5, 10, 10)
    if !strings.Contains(bar, "█") {
        t.Errorf("expected filled bar, got %s", bar)
    }
}

func TestPickPhrase(t *testing.T) {
    p := pickPhrase()
    if p == "" {
        t.Errorf("expected non-empty phrase")
    }
}

func TestPickIconForPhrase(t *testing.T) {
    icon := pickIconForPhrase("Я пришёл с миром")
    if icon == "" {
        t.Errorf("expected icon")
    }
}

func TestRandHex(t *testing.T) {
    h := randHex(8)
    if len(h) != 8 {
        t.Errorf("expected length 8, got %d", len(h))
    }
}
