package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (a *archive) list(ctx context.Context, album string, out io.Writer) error {
	prefix := "albums/"
	if album != "" {
		if err := a.requireAlbum(ctx, album); err != nil {
			return err
		}
		prefix = photoPrefix(album)
	}
	names, err := a.names(ctx, prefix, album == "")
	if err != nil {
		return err
	}
	for _, name := range names {
		if _, err := fmt.Fprintln(out, name); err != nil {
			return err
		}
	}
	return nil
}

func (a *archive) upload(ctx context.Context, album, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("Каталог недоступен: %w", err)
	}
	var photos []string
	for _, entry := range entries {
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if entry.Type().IsRegular() && (ext == ".jpg" || ext == ".jpeg") {
			photos = append(photos, entry.Name())
		}
	}
	if len(photos) == 0 {
		return errors.New("В каталоге нет фотографий")
	}
	if err := a.put(ctx, albumKey(album), strings.NewReader(""), "application/octet-stream"); err != nil {
		return err
	}
	for _, photo := range photos {
		file, err := os.Open(filepath.Join(dir, photo))
		if err != nil {
			return err
		}
		putErr := a.put(ctx, photoKey(album, photo), file, "image/jpeg")
		if err := errors.Join(putErr, file.Close()); err != nil {
			return err
		}
	}
	return nil
}

func (a *archive) download(ctx context.Context, album, dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("Каталог недоступен: %w", err)
	}
	if !info.IsDir() {
		return errors.New("Каталог недоступен")
	}
	// Check writability even when the album is empty.
	probe, err := os.CreateTemp(dir, ".cloudphoto-*")
	if err != nil {
		return fmt.Errorf("Каталог недоступен: %w", err)
	}
	if err := errors.Join(probe.Close(), os.Remove(probe.Name())); err != nil {
		return err
	}
	if err := a.requireAlbum(ctx, album); err != nil {
		return err
	}
	photos, err := a.names(ctx, photoPrefix(album), false)
	if err != nil {
		return err
	}
	for _, photo := range photos {
		if photo == "." || photo == ".." || filepath.Base(photo) != photo {
			return fmt.Errorf("Некорректное имя файла: %q", photo)
		}
		if err := a.downloadPhoto(ctx, album, photo, dir); err != nil {
			return err
		}
	}
	return nil
}

func (a *archive) downloadPhoto(ctx context.Context, album, photo, dir string) error {
	object, err := a.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(photoKey(album, photo))})
	if err != nil {
		return err
	}
	defer object.Body.Close()
	// Rename a complete file so a failed transfer preserves the existing photograph.
	file, err := os.CreateTemp(dir, ".cloudphoto-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, copyErr := io.Copy(file, object.Body)
	if err := errors.Join(copyErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(dir, photo))
}

func (a *archive) delete(ctx context.Context, album, photo string) error {
	if err := a.requireAlbum(ctx, album); err != nil {
		return err
	}
	if photo != "" {
		key := photoKey(album, photo)
		if err := a.requireObject(ctx, key, fmt.Sprintf("В альбоме нет фотографии с именем %s", photo)); err != nil {
			return err
		}
		return a.remove(ctx, key)
	}
	keys, err := a.keys(ctx, photoPrefix(album))
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := a.remove(ctx, key); err != nil {
			return err
		}
	}
	return a.remove(ctx, albumKey(album))
}

func (a *archive) mksite(ctx context.Context) error {
	// Bucket ACLs alone do not grant access to objects uploaded with private ACLs.
	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":%q}]}`, "arn:aws:s3:::"+a.bucket+"/*")
	_, err := a.client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{Bucket: aws.String(a.bucket), Policy: aws.String(policy)})
	if err != nil {
		return err
	}
	_, err = a.client.PutBucketWebsite(ctx, &s3.PutBucketWebsiteInput{Bucket: aws.String(a.bucket), WebsiteConfiguration: &types.WebsiteConfiguration{
		IndexDocument: &types.IndexDocument{Suffix: aws.String("index.html")}, ErrorDocument: &types.ErrorDocument{Key: aws.String("error.html")},
	}})
	if err != nil {
		return err
	}
	albums, err := a.names(ctx, "albums/", true)
	if err != nil {
		return err
	}
	for i, album := range albums {
		photos, err := a.names(ctx, photoPrefix(album), false)
		if err != nil {
			return err
		}
		if err := a.put(ctx, fmt.Sprintf("album%d.html", i+1), strings.NewReader(generateAlbumHTML(album, photos)), "text/html; charset=utf-8"); err != nil {
			return err
		}
	}
	if err := a.put(ctx, "error.html", strings.NewReader(generateErrorHTML()), "text/html; charset=utf-8"); err != nil {
		return err
	}
	return a.put(ctx, "index.html", strings.NewReader(generateIndexHTML(albums)), "text/html; charset=utf-8")
}
