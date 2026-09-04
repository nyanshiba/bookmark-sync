// Package sessionstore parses Firefox sessionstore files (sessionstore.jsonlz4
// and sessionstore-backups/*.jsonlz4) into a flat list of open tabs.
//
// It handles both the mozLz4-compressed and plain-JSON variants, and converts
// Firefox's per-tab lastAccessed timestamps to Unix seconds so callers can
// preserve them (e.g. as linkding date_added), matching what the normal
// bookmark-sync-sync pipeline does with a tab's lastUsed.
package sessionstore

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/pierrec/lz4/v4"
)

// mozLz4Magic is the 8-byte header Firefox prepends to compressed sessionstore
// files. What follows is an LZ4 block framed with an uncompressed-size prefix:
// 4 bytes (uint32 LE), then the block data.
const mozLz4Magic = "mozLz40\x00"

// Tab is one open tab extracted from a sessionstore file.
type Tab struct {
	URL          string // current page (entry at the tab's index)
	Title        string
	LastUsedUnix int64 // seconds since epoch; 0 if unknown
}

// ParseFile reads a sessionstore file (mozLz4 or plain JSON) and returns the
// open tabs it contains.
func ParseFile(path string) ([]Tab, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse decompresses and parses sessionstore bytes into the open tabs.
func Parse(data []byte) ([]Tab, error) {
	if len(data) >= 8 && string(data[:8]) == mozLz4Magic {
		raw, err := decompressMozLz4(data[8:])
		if err != nil {
			return nil, fmt.Errorf("decompress mozLz4: %w", err)
		}
		data = raw
	}

	var top struct {
		Windows []struct {
			Tabs []struct {
				Entries []struct {
					URL   string `json:"url"`
					Title string `json:"title"`
				} `json:"entries"`
				Index        int   `json:"index"`
				LastAccessed int64 `json:"lastAccessed"`
			} `json:"tabs"`
		} `json:"windows"`
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("parse sessionstore JSON: %w", err)
	}

	var out []Tab
	for _, win := range top.Windows {
		for _, tab := range win.Tabs {
			if len(tab.Entries) == 0 {
				continue
			}
			// tab.Index is 1-based and points at the current history entry.
			idx := tab.Index - 1
			if idx < 0 {
				idx = 0
			}
			if idx >= len(tab.Entries) {
				idx = len(tab.Entries) - 1
			}
			e := tab.Entries[idx]
			if e.URL == "" {
				continue
			}
			out = append(out, Tab{
				URL:          e.URL,
				Title:        e.Title,
				LastUsedUnix: lastUsedSeconds(tab.LastAccessed),
			})
		}
	}
	return out, nil
}

// lastUsedSeconds converts a sessionstore lastAccessed timestamp to Unix
// seconds. Firefox stores it in milliseconds (tab.lastAccessed is set from
// Date.now()); defensively handle microsecond-style values too so a stale or
// third-party file still yields sane dates.
func lastUsedSeconds(lastAccessed int64) int64 {
	if lastAccessed <= 0 {
		return 0
	}
	if lastAccessed > 1e14 { // implausible as ms (would be year 5138+); treat as µs
		lastAccessed /= 1000
	}
	return lastAccessed / 1000
}

// decompressMozLz4 decompresses the payload that follows the 8-byte mozLz4
// magic. That payload is: 4-byte uncompressed size (uint32 LE), then the LZ4
// block data. The size lets us allocate the exact output buffer up front.
func decompressMozLz4(payload []byte) ([]byte, error) {
	if len(payload) < 4 {
		return nil, errors.New("mozLz4 payload too short (missing size prefix)")
	}
	uncompressedSize := int(binary.LittleEndian.Uint32(payload[:4]))
	block := payload[4:]

	// Reject implausible sizes (>512 MB) before allocating. A size of 0 means
	// an empty file; return empty without touching the LZ4 block.
	if uncompressedSize < 0 || uncompressedSize > 512<<20 {
		return nil, fmt.Errorf("mozLz4 uncompressed size %d out of range", uncompressedSize)
	}
	if uncompressedSize == 0 {
		return []byte{}, nil
	}

	dst := make([]byte, uncompressedSize)
	n, err := lz4.UncompressBlock(block, dst)
	if err != nil {
		return nil, err
	}
	return dst[:n], nil
}
