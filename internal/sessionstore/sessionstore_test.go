package sessionstore

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/pierrec/lz4/v4"
)

func TestParsePlainJSON(t *testing.T) {
	raw := []byte(`{"windows":[{"tabs":[
		{"entries":[{"url":"https://example.com","title":"Example"}],"lastAccessed":1700000000000,"index":1},
		{"entries":[{"url":"https://a.example/prev","title":"Prev"},{"url":"https://a.example/current","title":"Current"}],"lastAccessed":1700001000000,"index":2}
	]}]}`)
	tabs, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(tabs) != 2 {
		t.Fatalf("got %d tabs, want 2", len(tabs))
	}
	// first tab: basic fields + ms→s conversion
	if tabs[0].URL != "https://example.com" || tabs[0].Title != "Example" {
		t.Errorf("tab0 = %+v", tabs[0])
	}
	if tabs[0].LastUsedUnix != 1700000000 {
		t.Errorf("tab0 lastUsed = %d, want 1700000000", tabs[0].LastUsedUnix)
	}
	// second tab: index 2 -> entries[1] is the current page
	if tabs[1].URL != "https://a.example/current" {
		t.Errorf("tab1 URL = %q, want https://a.example/current (entry at index 2)", tabs[1].URL)
	}
	if tabs[1].LastUsedUnix != 1700001000 {
		t.Errorf("tab1 lastUsed = %d, want 1700001000", tabs[1].LastUsedUnix)
	}
}

func TestParseMozLz4(t *testing.T) {
	raw := []byte(`{"windows":[{"tabs":[{"entries":[{"url":"https://lz.example","title":"LZ"}],"lastAccessed":1700000000000,"index":1}]}]}`)
	block := make([]byte, lz4.CompressBlockBound(len(raw)))
	n, err := lz4.CompressBlock(raw, block, nil)
	if err != nil || n == 0 {
		t.Fatalf("compress: n=%d err=%v", n, err)
	}
	// mozLz4 layout: 8-byte magic + 4-byte uncompressed size (uint32 LE) + block.
	var b bytes.Buffer
	b.WriteString(mozLz4Magic)
	binary.Write(&b, binary.LittleEndian, uint32(len(raw)))
	b.Write(block[:n])
	tabs, err := Parse(b.Bytes())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(tabs) != 1 || tabs[0].URL != "https://lz.example" {
		t.Fatalf("tabs = %+v", tabs)
	}
}

func TestParseMozLz4RejectsBadSizePrefix(t *testing.T) {
	// Payload whose size prefix is out of range must be rejected before
	// attempting any LZ4 work (no giant allocation, no confusing block error).
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint32(payload[:4], 0x7fffffff) // >512 MB
	if _, err := decompressMozLz4(payload); err == nil {
		t.Fatal("expected error for out-of-range size, got nil")
	}
	if _, err := decompressMozLz4([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error for truncated payload, got nil")
	}
}

func TestParseSkipsEmptyAndBad(t *testing.T) {
	raw := []byte(`{"windows":[{"tabs":[
		{"entries":[],"lastAccessed":1700000000000,"index":1},
		{"entries":[{"title":"no url"}],"lastAccessed":1700000000000,"index":1},
		{"entries":[{"url":"https://ok.example","title":"OK"}],"lastAccessed":1700000000000,"index":0}
	]}]}`)
	tabs, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(tabs) != 1 || tabs[0].URL != "https://ok.example" {
		t.Fatalf("tabs = %+v", tabs)
	}
}

func TestLastUsedSeconds(t *testing.T) {
	cases := []struct {
		input int64
		want  int64
	}{
		{0, 0},
		{-1, 0},
		{1700000000000, 1700000000},   // ms → s (normal Firefox value)
		{1700000000000000, 1700000000}, // µs → s (defensive branch)
	}
	for _, c := range cases {
		got := lastUsedSeconds(c.input)
		if got != c.want {
			t.Errorf("lastUsedSeconds(%d) = %d, want %d", c.input, got, c.want)
		}
	}
}

func TestParseEmptyWindows(t *testing.T) {
	raw := []byte(`{"windows":[]}`)
	tabs, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(tabs) != 0 {
		t.Fatalf("got %d tabs, want 0", len(tabs))
	}
}