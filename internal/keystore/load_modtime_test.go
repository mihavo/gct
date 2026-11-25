package keystore

import (
	"os"
	"testing"
	"time"
)

func TestLoadReportsEachKeyFilesModificationTime(t *testing.T) {
	dir := t.TempDir()
	older := time.Date(2026, time.November, 1, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, time.November, 20, 15, 30, 0, 0, time.UTC)

	olderKid, olderPath, err := Generate(dir)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	newerKid, newerPath, err := Generate(dir)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if err := os.Chtimes(olderPath, older, older); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}
	if err := os.Chtimes(newerPath, newer, newer); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	keys, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got := map[string]time.Time{}
	for _, key := range keys {
		got[key.Kid] = key.LastModified
	}
	if !got[olderKid].Equal(older) {
		t.Fatalf("LastModified of %s = %v, want %v", olderKid, got[olderKid], older)
	}
	if !got[newerKid].Equal(newer) {
		t.Fatalf("LastModified of %s = %v, want %v", newerKid, got[newerKid], newer)
	}
}
