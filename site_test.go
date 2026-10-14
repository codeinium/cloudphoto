package main

import (
	"html"
	"net/url"
	"strings"
	"testing"
)

func TestPhotoLinks(t *testing.T) {
	for _, album := range []string{".", "..", "100%", "a&copy;", "Кириллица + # ?", "<script>"} {
		photo := "Фото &copy; #100%.JPG"
		key := photoKey(album, photo)
		parts := strings.Split(key, "/")
		for i := range parts {
			parts[i] = url.PathEscape(parts[i])
		}
		link := strings.Join(parts, "/")
		page := generateAlbumHTML(album, []string{photo})
		if !strings.Contains(page, `src="`+html.EscapeString(link)+`"`) {
			t.Fatalf("missing escaped URL for %q", album)
		}
		resolved, err := url.PathUnescape(link)
		if err != nil || resolved != key {
			t.Fatalf("%q resolves to %q: %v", link, resolved, err)
		}
		if strings.Contains(page, "<script></") {
			t.Fatal("unescaped album")
		}
	}
}
