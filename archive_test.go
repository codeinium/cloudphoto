package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type storedObject struct {
	body        []byte
	contentType string
}
type fakeS3 struct {
	sync.Mutex
	objects         map[string]storedObject
	policy, website string
	bucketACL       string
	forbidden       bool
	pages           int
}

func testArchive(t *testing.T) (*archive, *fakeS3) {
	t.Helper()
	store := &fakeS3{objects: make(map[string]storedObject)}
	server := httptest.NewServer(http.HandlerFunc(store.serveHTTP))
	t.Cleanup(server.Close)
	a := newArchive(settings{"test-bucket", "test", "test"})
	a.client = s3.NewFromConfig(aws.Config{Region: "ru-central1", Credentials: a.client.Options().Credentials}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
		o.RetryMaxAttempts = 1
	})
	return a, store
}
func (s *fakeS3) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.Lock()
	defer s.Unlock()
	w.Header().Set("Content-Type", "application/xml")
	if s.forbidden {
		w.WriteHeader(403)
		io.WriteString(w, `<Error><Code>AccessDenied</Code></Error>`)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/test-bucket/")
	q := r.URL.Query()
	if q.Has("acl") {
		s.bucketACL = r.Header.Get("X-Amz-Acl")
		return
	}
	if q.Has("policy") || q.Has("website") {
		if r.Method == "GET" && q.Has("policy") {
			if s.policy == "" {
				w.WriteHeader(404)
				io.WriteString(w, `<Error><Code>NoSuchBucketPolicy</Code></Error>`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, s.policy)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if q.Has("policy") {
			s.policy = string(body)
		} else {
			s.website = string(body)
		}
		return
	}
	if q.Get("list-type") == "2" {
		s.pages++
		var keys []string
		for key := range s.objects {
			if strings.HasPrefix(key, q.Get("prefix")) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		if token := q.Get("continuation-token"); token != "" {
			i := sort.SearchStrings(keys, token)
			if i < len(keys) && keys[i] == token {
				i++
			}
			keys = keys[i:]
		}
		truncated := len(keys) > 2 // Small pages exercise pagination in every operation.
		if truncated {
			keys = keys[:2]
		}
		fmt.Fprintf(w, `<ListBucketResult><IsTruncated>%t</IsTruncated>`, truncated)
		if truncated {
			io.WriteString(w, "<NextContinuationToken>")
			xml.EscapeText(w, []byte(keys[len(keys)-1]))
			io.WriteString(w, "</NextContinuationToken>")
		}
		for _, key := range keys {
			io.WriteString(w, "<Contents><Key>")
			xml.EscapeText(w, []byte(key))
			io.WriteString(w, "</Key></Contents>")
		}
		io.WriteString(w, "</ListBucketResult>")
		return
	}
	switch r.Method {
	case "PUT":
		body, _ := io.ReadAll(r.Body)
		s.objects[key] = storedObject{body, r.Header.Get("Content-Type")}
	case "HEAD", "GET":
		object, ok := s.objects[key]
		if !ok {
			w.WriteHeader(404)
			if r.Method == "GET" {
				io.WriteString(w, `<Error><Code>NoSuchKey</Code></Error>`)
			}
			return
		}
		w.Header().Set("Content-Type", object.contentType)
		if r.Method == "GET" {
			w.Write(object.body)
		}
	case "DELETE":
		delete(s.objects, key)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func TestArchiveLifecycle(t *testing.T) {
	a, store := testArchive(t)
	ctx := context.Background()
	dir := t.TempDir()
	album := `Лето & "100%" + #?`
	photos := []string{"a.JPG", "A.jpeg", "Фото & 100% + #?.jpg"}
	for _, name := range photos {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "ignore.png"), []byte("ignored"), 0600)
	os.Mkdir(filepath.Join(dir, "nested.jpg"), 0700)
	if err := a.upload(ctx, album, dir); err != nil {
		t.Fatal(err)
	}
	if len(store.objects) != 4 {
		t.Fatalf("objects: %v", store.objects)
	}
	// Same names replace existing objects, while case remains significant.
	os.WriteFile(filepath.Join(dir, "a.JPG"), []byte("replacement"), 0600)
	if err := a.upload(ctx, album, dir); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outDir, "a.JPG"), []byte("old download"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.download(ctx, album, outDir); err != nil {
		t.Fatal(err)
	}
	for _, photo := range photos {
		want := photo
		if photo == "a.JPG" {
			want = "replacement"
		}
		data, err := os.ReadFile(filepath.Join(outDir, photo))
		if err != nil || string(data) != want {
			t.Fatalf("%s: %q, %v", photo, data, err)
		}
	}
	var out bytes.Buffer
	if err := a.list(ctx, album, &out); err != nil {
		t.Fatal(err)
	}
	sort.Strings(photos)
	if out.String() != strings.Join(photos, "\n")+"\n" {
		t.Fatal(out.String())
	}
	if err := a.mksite(ctx); err != nil {
		t.Fatal(err)
	}
	if store.bucketACL != "public-read" || !strings.Contains(store.policy, "s3:GetObject") || !strings.Contains(store.website, "error.html") {
		t.Fatal("hosting not configured")
	}
	page := string(store.objects["album1.html"].body)
	if strings.Contains(page, album) || !strings.Contains(page, "&amp;") {
		t.Fatal("album HTML was not escaped")
	}
	for _, photo := range photos {
		src := "photos/" + url.PathEscape(url.PathEscape(album)) + "/" + url.PathEscape(url.PathEscape(photo))
		if !strings.Contains(page, html.EscapeString(src)) {
			t.Fatalf("missing photo URL %s", src)
		}
		key, err := url.PathUnescape(src)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := store.objects[key]; !ok {
			t.Fatalf("URL does not resolve to stored key: %s", key)
		}
	}
	for _, photo := range photos {
		if err := a.delete(ctx, album, photo); err != nil {
			t.Fatal(err)
		}
	}
	out.Reset()
	if err := a.list(ctx, "", &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != album+"\n" {
		t.Fatal("empty album disappeared")
	}
	if err := a.mksite(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(store.objects["album1.html"].body), "Фотоальбом пустой.") {
		t.Fatal("missing empty album page")
	}
	if err := a.delete(ctx, album, ""); err != nil {
		t.Fatal(err)
	}
	if err := a.mksite(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(store.objects["index.html"].body), "Фотоархив пустой.") {
		t.Fatal("missing empty archive page")
	}
}

func TestPaginationAndSorting(t *testing.T) {
	a, store := testArchive(t)
	names := []string{"z", "Я", "a", "A", "А"}
	for _, name := range names {
		store.objects[albumKey(name)] = storedObject{}
	}
	sort.Strings(names)
	var out bytes.Buffer
	if err := a.list(context.Background(), "", &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != strings.Join(names, "\n")+"\n" || store.pages != 3 {
		t.Fatalf("%q, pages=%d", out.String(), store.pages)
	}
	album := "large"
	store.objects[albumKey(album)] = storedObject{}
	for i := 0; i < 7; i++ {
		store.objects[photoKey(album, fmt.Sprintf("%d.jpg", i))] = storedObject{}
	}
	if err := a.delete(context.Background(), album, ""); err != nil {
		t.Fatal(err)
	}
	for key := range store.objects {
		if strings.HasPrefix(key, photoPrefix(album)) {
			t.Fatalf("object left after delete: %s", key)
		}
	}
}

func TestErrors(t *testing.T) {
	a, store := testArchive(t)
	ctx := context.Background()
	if err := a.requireAlbum(ctx, "missing"); err == nil || err.Error() != "В фотоархиве нет альбома с именем missing" {
		t.Fatal(err)
	}
	store.forbidden = true
	if err := a.requireAlbum(ctx, "missing"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("access error masked: %v", err)
	}
	if err := a.mksite(ctx); err == nil {
		t.Fatal("policy failure ignored")
	}
	store.forbidden = false
	if err := a.upload(ctx, "empty", t.TempDir()); err == nil {
		t.Fatal("empty directory accepted")
	}
	if len(store.objects) != 0 {
		t.Fatal("empty upload created album")
	}
	store.objects[albumKey("a")] = storedObject{}
	if err := a.delete(ctx, "a", "missing.jpg"); err == nil {
		t.Fatal("missing photo accepted")
	}
	store.objects[photoPrefix("a")+"%2E%2E"] = storedObject{}
	if err := a.download(ctx, "a", t.TempDir()); err == nil {
		t.Fatal("unsafe filename accepted")
	}
}

func TestMksitePreservesManagementPolicy(t *testing.T) {
	a, store := testArchive(t)
	store.policy = `{"Version":"2012-10-17","Statement":[{"Sid":"Owner","Effect":"Allow","Principal":{"CanonicalUser":"owner-id"},"Action":"s3:*","Resource":["arn:aws:s3:::test-bucket","arn:aws:s3:::test-bucket/*"]}]}`
	for i := 0; i < 2; i++ {
		if err := a.mksite(context.Background()); err != nil {
			t.Fatal(err)
		}
		var policy struct{ Statement []map[string]any }
		if err := json.Unmarshal([]byte(store.policy), &policy); err != nil {
			t.Fatal(err)
		}
		if len(policy.Statement) != 2 || policy.Statement[0]["Sid"] != "Owner" || policy.Statement[1]["Sid"] != "PublicRead" {
			t.Fatalf("management rule lost or read rule duplicated: %s", store.policy)
		}
	}
}

func TestMksiteKeepsPolicyOrder(t *testing.T) {
	a, store := testArchive(t)
	store.policy = `{"Version":"2012-10-17","Statement":[{"Sid":"PublicRead","Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::test-bucket/*"},{"Sid":"Owner","Effect":"Allow","Principal":{"CanonicalUser":"owner-id"},"Action":"s3:*","Resource":["arn:aws:s3:::test-bucket","arn:aws:s3:::test-bucket/*"]}]}`
	before := store.policy
	if err := a.configurePublicRead(context.Background()); err != nil {
		t.Fatal(err)
	}
	var want, got any
	json.Unmarshal([]byte(before), &want)
	json.Unmarshal([]byte(store.policy), &got)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("policy changed on repeated publish: %s", store.policy)
	}
}
