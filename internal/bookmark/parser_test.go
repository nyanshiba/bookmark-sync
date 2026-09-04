package bookmark

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCoffee(t *testing.T) {
	// Go のテストはパッケージディレクトリに cd して実行される。
	// プロジェクトルートの bookmarks_coffee.html を指す相対パス。
	path := filepath.Join("..", "..", "bookmarks_coffee.html")

	entries, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	if len(entries) != 20 {
		t.Fatalf("expected 20 entries, got %d", len(entries))
	}

	// 全エントリのフォルダ階層が正しいか確認。
	expectedFolder := []string{"Mozilla Firefox", "etc.", "Coffee"}
	for i, e := range entries {
		if len(e.Folder) != len(expectedFolder) {
			t.Errorf("entry[%d]: expected %d folder levels, got %d (%v)",
				i, len(expectedFolder), len(e.Folder), e.Folder)
			continue
		}
		for j, f := range e.Folder {
			if f != expectedFolder[j] {
				t.Errorf("entry[%d]: folder[%d] = %q, want %q", i, j, f, expectedFolder[j])
			}
		}
	}

	// 先頭エントリの詳細確認。
	first := entries[0]
	if first.Title != "マキネッタのお手入れ方法｜初めてのエスプレッソを簡単に！ │ パイナッポ" {
		t.Errorf("first title: %q", first.Title)
	}
	if first.URL != "https://pina817.com/aftermacchinetta/" {
		t.Errorf("first URL: %q", first.URL)
	}
	if first.DateAdded != "1601816117" {
		t.Errorf("first DateAdded: %q, want 1601816117", first.DateAdded)
	}
	if first.LastModified != "1723957193" {
		t.Errorf("first LastModified: %q, want 1723957193", first.LastModified)
	}

	// 末尾の X エントリ（ADD_DATE が 1775820xxx）の確認。
	last := entries[19]
	if last.Title != "XユーザーのY Tambeさん: 「コーヒーの酸味について、こないだの早稲田のオンライン講義の配布資料から何枚か。 https://t.co/mkmibLcyDK」 / X" {
		t.Errorf("last title: %q", last.Title)
	}
	if last.DateAdded != "1775820884" {
		t.Errorf("last DateAdded: %q, want 1775820884", last.DateAdded)
	}

	// 特定のエントリが存在するか。
	var foundMIT bool
	for _, e := range entries {
		if e.URL == "https://news.mit.edu/2025/coffee-fix-mit-students-decode-science-behind-perfect-cup-0107" {
			foundMIT = true
			if e.DateAdded != "1736495360" {
				t.Errorf("MIT entry DateAdded: %q, want 1736495360", e.DateAdded)
			}
			break
		}
	}
	if !foundMIT {
		t.Error("MIT entry not found")
	}

	// TAGS 属性はこのファイルには無いが、空でよいことを確認。
	for _, e := range entries {
		if len(e.Tags) != 0 {
			t.Errorf("expected no TAGS, got %v for %q", e.Tags, e.URL)
		}
	}
}

// TestParseFileNotFound は存在しないファイルのエラーケース。
func TestParseFileNotFound(t *testing.T) {
	_, err := ParseFile("/nonexistent/bookmarks.html")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

// TestParseEmptyFile は空ファイルのエラーケース。
func TestParseEmptyFile(t *testing.T) {
	f, err := os.CreateTemp("", "bookmarks-*.html")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	// 空の HTML はパースエラーにならない（0 エントリ）。
	entries, err := ParseFile(f.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}