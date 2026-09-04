package main

import (
	"reflect"
	"testing"

	"bookmark-sync/internal/bookmark"
)

func TestAppendFolderTags(t *testing.T) {
	cases := []struct {
		name   string
		folder []string
		want   []string
	}{
		{
			name:   "deepest folder only",
			folder: []string{"Mozilla Firefox", "etc.", "Coffee"},
			want:   []string{"Coffee"},
		},
		{
			name:   "single level with space",
			folder: []string{"Mozilla Firefox"},
			want:   []string{"Mozilla-Firefox"},
		},
		{
			name:   "single level plain",
			folder: []string{"Health"},
			want:   []string{"Health"},
		},
		{
			name:   "no folder",
			folder: nil,
			want:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := appendFolderTags(bookmark.Entry{Folder: tc.folder})
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("appendFolderTags(%v) = %v, want %v", tc.folder, got, tc.want)
			}
		})
	}
}
