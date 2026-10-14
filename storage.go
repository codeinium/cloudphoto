package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"io"
	"net/url"
	"sort"
	"strings"
)

type archive struct {
	client *s3.Client
	bucket string
}

func newArchive(c settings) *archive {
	client := s3.New(s3.Options{
		Region: "ru-central1", BaseEndpoint: aws.String("https://storage.yandexcloud.net"),
		Credentials: credentials.NewStaticCredentialsProvider(c.accessKey, c.secretKey, ""),
	})
	return &archive{client, c.bucket}
}

// Escape dot-only components as well: browsers normalize literal . and .. in paths.
func encodeName(name string) string {
	if name == "." {
		return "%2E"
	}
	if name == ".." {
		return "%2E%2E"
	}
	return url.PathEscape(name)
}

func albumKey(album string) string        { return "albums/" + encodeName(album) + "/" }
func photoPrefix(album string) string     { return "photos/" + encodeName(album) + "/" }
func photoKey(album, photo string) string { return photoPrefix(album) + encodeName(photo) }

// Only a missing object is a missing album; authentication and transport errors propagate.
func (a *archive) requireObject(ctx context.Context, key, message string) error {
	_, err := a.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(key)})
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "404") {
		return errors.New(message)
	}
	return err
}
func (a *archive) requireAlbum(ctx context.Context, album string) error {
	return a.requireObject(ctx, albumKey(album), fmt.Sprintf("В фотоархиве нет альбома с именем %s", album))
}

func (a *archive) keys(ctx context.Context, prefix string) ([]string, error) {
	pager := s3.NewListObjectsV2Paginator(a.client, &s3.ListObjectsV2Input{Bucket: aws.String(a.bucket), Prefix: aws.String(prefix)})
	var keys []string
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, object := range page.Contents {
			keys = append(keys, aws.ToString(object.Key))
		}
	}
	return keys, nil
}
func (a *archive) names(ctx context.Context, prefix string, albums bool) ([]string, error) {
	keys, err := a.keys(ctx, prefix)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, key := range keys {
		suffix := strings.TrimPrefix(key, prefix)
		if albums {
			if !strings.HasSuffix(suffix, "/") {
				continue
			}
			suffix = strings.TrimSuffix(suffix, "/")
		}
		if suffix == "" || strings.Contains(suffix, "/") {
			continue
		}
		name, err := url.PathUnescape(suffix)
		if err != nil || !validName(name) {
			return nil, fmt.Errorf("Некорректный ключ объекта: %q", key)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
func (a *archive) put(ctx context.Context, key string, body io.Reader, contentType string) error {
	_, err := a.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(key), Body: body, ContentType: aws.String(contentType)})
	return err
}
func (a *archive) remove(ctx context.Context, key string) error {
	_, err := a.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(key)})
	return err
}
