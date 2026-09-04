// Package bookmark parses the Netscape Bookmark File Format (HTML).
// The folder hierarchy of each entry is collected into Entry.Folder;
// tag conversion (deepest folder only) is done by the importer.
package bookmark

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/net/html"
)

// Entry is a single bookmark with folder-derived tags.
type Entry struct {
	Title        string
	URL          string
	DateAdded    string // Unix seconds, as stored in ADD_DATE
	LastModified string // Unix seconds, as stored in LAST_MODIFIED
	Tags         []string
	Folder       []string // full folder path, top-down
}

// ParseFile reads a Netscape bookmark HTML file and returns all entries.
// The folder hierarchy is collected into Entry.Folder; tags are taken
// from the TAGS attribute (if present) and the folder path.
func ParseFile(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads bookmark HTML from r.
func Parse(r io.Reader) ([]Entry, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}

	var entries []Entry
	var folder []string

	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "h3":
				// A folder heading. Its following sibling is a <DL> containing children.
				name := strings.TrimSpace(text(n))
				if name != "" {
					folder = append(folder, name)
				}
			case "a":
				entries = append(entries, entryFromNode(n, folder))
			case "dl":
				// Recurse into the container, then pop the folder level
				// added by the immediately preceding H3.
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c)
				}
				if len(folder) > 0 {
					folder = folder[:len(folder)-1]
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return entries, nil
}

func entryFromNode(n *html.Node, folder []string) Entry {
	e := Entry{
		Title:  strings.TrimSpace(text(n)),
		Folder: append([]string{}, folder...),
	}
	for _, attr := range n.Attr {
		switch attr.Key {
		case "href":
			e.URL = attr.Val
		case "add_date":
			e.DateAdded = attr.Val
		case "last_modified":
			e.LastModified = attr.Val
		case "tags":
			for _, t := range strings.Split(attr.Val, ",") {
				if t = strings.TrimSpace(t); t != "" {
					e.Tags = append(e.Tags, t)
				}
			}
		}
	}
	return e
}

// text returns the concatenated text content of a node.
func text(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}
