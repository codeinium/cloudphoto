package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// printErrorAndExit выводит ошибку в stderr и завершает программу с кодом 1
func printErrorAndExit(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}

// initS3Client создает и настраивает клиент для Yandex Object Storage
func initS3Client() (*s3.Client, string) {
	bucket := os.Getenv("BUCKET")
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion("ru-central1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		printErrorAndExit("Ошибка инициализации S3 клиента")
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String("https://storage.yandexcloud.net")
	})

	return client, bucket
}

func main() {
	if len(os.Args) < 2 {
		printErrorAndExit("Ошибка в аргументах")
	}

	requiredEnvVars := []string{"BUCKET", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}
	for _, envVar := range requiredEnvVars {
		if os.Getenv(envVar) == "" {
			printErrorAndExit("Не заданы переменные окружения")
		}
	}

	command := os.Args[1]
	args := os.Args[2:]

	client, bucket := initS3Client()

	var err error
	switch command {
	case "list":
		err = cmdList(client, bucket, args)
	case "upload":
		err = cmdUpload(client, bucket, args)
	case "download":
		err = cmdDownload(client, bucket, args)
	case "delete":
		err = cmdDelete(client, bucket, args)
	case "mksite":
		err = cmdMksite(client, bucket, args)
	default:
		printErrorAndExit("Ошибка в аргументах")
	}

	if err != nil {
		printErrorAndExit(err.Error())
	}
}

// --- ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ ---

func getAlbums(ctx context.Context, client *s3.Client, bucket string) ([]string, error) {
	resp, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Prefix:    aws.String("albums/"),
		Delimiter: aws.String("/"),
	})
	if err != nil {
		return nil, err
	}
	var albums []string
	for _, p := range resp.CommonPrefixes {
		name := strings.TrimSuffix(strings.TrimPrefix(*p.Prefix, "albums/"), "/")
		decoded, err := url.PathUnescape(name)
		if err != nil {
			return nil, err
		}
		albums = append(albums, decoded)
	}
	sort.Strings(albums)
	return albums, nil
}

func getPhotos(ctx context.Context, client *s3.Client, bucket, album string) ([]string, error) {
	encodedAlbum := url.PathEscape(album)
	prefix := "photos/" + encodedAlbum + "/"
	resp, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, err
	}
	var photos []string
	for _, obj := range resp.Contents {
		name := strings.TrimPrefix(*obj.Key, prefix)
		decoded, err := url.PathUnescape(name)
		if err != nil {
			return nil, err
		}
		photos = append(photos, decoded)
	}
	sort.Strings(photos)
	return photos, nil
}

func putStringObject(ctx context.Context, client *s3.Client, bucket, key, content string) error {
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        strings.NewReader(content),
		ContentType: aws.String("text/html; charset=utf-8"),
	})
	return err
}

// --- КОМАНДЫ ---

func cmdList(client *s3.Client, bucket string, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	album := fs.String("album", "", "")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("Ошибка в аргументах")
	}

	ctx := context.TODO()
	if *album == "" {
		albums, err := getAlbums(ctx, client, bucket)
		if err != nil {
			return err
		}
		for _, a := range albums {
			fmt.Println(a)
		}
		return nil
	}

	// Проверка существования альбома
	encodedAlbum := url.PathEscape(*album)
	_, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String("albums/" + encodedAlbum + "/"),
	})
	if err != nil {
		return fmt.Errorf("В фотоархиве нет альбома с именем %s", *album)
	}

	photos, err := getPhotos(ctx, client, bucket, *album)
	if err != nil {
		return err
	}
	for _, p := range photos {
		fmt.Println(p)
	}
	return nil
}

func cmdUpload(client *s3.Client, bucket string, args []string) error {
	fs := flag.NewFlagSet("upload", flag.ContinueOnError)
	album := fs.String("album", "", "")
	path := fs.String("path", ".", "")
	if err := fs.Parse(args); err != nil || *album == "" {
		return fmt.Errorf("Ошибка в аргументах")
	}

	info, err := os.Stat(*path)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("Каталог недоступен")
	}

	entries, err := os.ReadDir(*path)
	if err != nil {
		return fmt.Errorf("Каталог недоступен")
	}

	var photos []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext == ".jpg" || ext == ".jpeg" {
			photos = append(photos, entry.Name())
		}
	}

	if len(photos) == 0 {
		return fmt.Errorf("В каталоге нет фотографий")
	}

	ctx := context.TODO()
	encodedAlbum := url.PathEscape(*album)

	// Создаем маркер альбома
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String("albums/" + encodedAlbum + "/"),
		Body:   strings.NewReader(""),
	})
	if err != nil {
		return err
	}

	for _, photo := range photos {
		filePath := filepath.Join(*path, photo)
		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		encodedPhoto := url.PathEscape(photo)
		key := "photos/" + encodedAlbum + "/" + encodedPhoto
		_, err = client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
			Body:   bytes.NewReader(data),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func cmdDownload(client *s3.Client, bucket string, args []string) error {
	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	album := fs.String("album", "", "")
	path := fs.String("path", ".", "")
	if err := fs.Parse(args); err != nil || *album == "" {
		return fmt.Errorf("Ошибка в аргументах")
	}

	info, err := os.Stat(*path)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("Каталог недоступен")
	}

	ctx := context.TODO()
	encodedAlbum := url.PathEscape(*album)

	_, err = client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String("albums/" + encodedAlbum + "/"),
	})
	if err != nil {
		return fmt.Errorf("В фотоархиве нет альбома с именем %s", *album)
	}

	resp, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String("photos/" + encodedAlbum + "/"),
	})
	if err != nil {
		return err
	}

	for _, obj := range resp.Contents {
		key := *obj.Key
		encodedPhoto := strings.TrimPrefix(key, "photos/"+encodedAlbum+"/")
		photoName, err := url.PathUnescape(encodedPhoto)
		if err != nil {
			return err
		}

		getResp, err := client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			return err
		}

		filePath := filepath.Join(*path, photoName)
		file, err := os.Create(filePath)
		if err != nil {
			getResp.Body.Close()
			return fmt.Errorf("Каталог недоступен")
		}
		_, err = io.Copy(file, getResp.Body)
		file.Close()
		getResp.Body.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func cmdDelete(client *s3.Client, bucket string, args []string) error {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	album := fs.String("album", "", "")
	photo := fs.String("photo", "", "")
	if err := fs.Parse(args); err != nil || *album == "" {
		return fmt.Errorf("Ошибка в аргументах")
	}

	ctx := context.TODO()
	encodedAlbum := url.PathEscape(*album)
	albumKey := "albums/" + encodedAlbum + "/"

	_, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(albumKey),
	})
	if err != nil {
		return fmt.Errorf("В фотоархиве нет альбома с именем %s", *album)
	}

	if *photo != "" {
		encodedPhoto := url.PathEscape(*photo)
		photoKey := "photos/" + encodedAlbum + "/" + encodedPhoto
		_, err := client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(photoKey),
		})
		if err != nil {
			return fmt.Errorf("В альбоме нет фотографии с именем %s", *photo)
		}
		_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(photoKey),
		})
		return err
	}

	resp, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String("photos/" + encodedAlbum + "/"),
	})
	if err != nil {
		return err
	}
	for _, obj := range resp.Contents {
		_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(bucket),
			Key:    obj.Key,
		})
		if err != nil {
			return err
		}
	}
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(albumKey),
	})
	return err
}

func cmdMksite(client *s3.Client, bucket string, args []string) error {
	fs := flag.NewFlagSet("mksite", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("Ошибка в аргументах")
	}

	ctx := context.TODO()

	// Настройка публичного доступа и хостинга
	_, err := client.PutBucketAcl(ctx, &s3.PutBucketAclInput{
		Bucket: aws.String(bucket),
		ACL:    types.BucketCannedACL("public-read"),
	})
	if err != nil {
		// YC S3 API может отвергать эту команду, если бакет уже публичный через Terraform
		fmt.Fprintln(os.Stderr, "Warning: PutBucketAcl:", err)
	}

	_, err = client.PutBucketWebsite(ctx, &s3.PutBucketWebsiteInput{
		Bucket: aws.String(bucket),
		WebsiteConfiguration: &types.WebsiteConfiguration{
			IndexDocument: &types.IndexDocument{Suffix: aws.String("index.html")},
			ErrorDocument: &types.ErrorDocument{Key: aws.String("error.html")},
		},
	})
	if err != nil {
		return err
	}

	albums, err := getAlbums(ctx, client, bucket)
	if err != nil {
		return err
	}

	// Генерация страниц альбомов
	for i, album := range albums {
		photos, err := getPhotos(ctx, client, bucket, album)
		if err != nil {
			return err
		}
		htmlContent := generateAlbumHTML(album, photos)
		key := fmt.Sprintf("album%d.html", i+1)
		if err := putStringObject(ctx, client, bucket, key, htmlContent); err != nil {
			return err
		}
	}

	// Генерация index.html
	indexHTML := generateIndexHTML(albums)
	if err := putStringObject(ctx, client, bucket, "index.html", indexHTML); err != nil {
		return err
	}

	// Генерация error.html
	errorHTML := generateErrorHTML()
	if err := putStringObject(ctx, client, bucket, "error.html", errorHTML); err != nil {
		return err
	}

	return nil
}

// --- HTML ШАБЛОНЫ ---

func generateIndexHTML(albums []string) string {
	if len(albums) == 0 {
		return `<!doctype html>
<html>
    <head>
        <title>Фотоархив</title>
    </head>
<body>
    <h1>Фотоархив</h1>
    <p>Фотоархив пустой.</p>
</body>`
	}
	var b strings.Builder
	b.WriteString(`<!doctype html>
<html>
    <head>
        <title>Фотоархив</title>
    </head>
<body>
    <h1>Фотоархив</h1>
    <ul><!--1-->
`)
	for i, album := range albums {
		escapedAlbum := html.EscapeString(album)
		b.WriteString(fmt.Sprintf("        <li><a href=\"album%d.html\">%s</a></li>\n", i+1, escapedAlbum))
	}
	b.WriteString(`    </ul>
</body>`)
	return b.String()
}

func generateAlbumHTML(album string, photos []string) string {
	escapedAlbum := html.EscapeString(album)
	if len(photos) == 0 {
		return fmt.Sprintf(`<!doctype html>
<html>
    <head>
        <title>Фотоальбом "%s"</title>
    </head>
    <body>
        <h1>Фотоальбом "%s"</h1>
        <p>Фотоальбом пустой.</p>
        <p>Вернуться на <a href="index.html">главную страницу</a> фотоархива</p>
    </body>
</html>`, escapedAlbum, escapedAlbum)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<!doctype html>
<html>
    <head>
        <title>Фотоальбом "%s"</title>
        <link rel="stylesheet" type="text/css" href="https://cdnjs.cloudflare.com/ajax/libs/galleria/1.6.1/themes/classic/galleria.classic.min.css" />
        <style>
            .galleria{ width: 960px; height: 540px; background: #000 }
        </style>
        <script src="https://ajax.googleapis.com/ajax/libs/jquery/3.6.0/jquery.min.js"></script>
        <script src="https://cdnjs.cloudflare.com/ajax/libs/galleria/1.6.1/galleria.min.js"></script>
        <script src="https://cdnjs.cloudflare.com/ajax/libs/galleria/1.6.1/themes/classic/galleria.classic.min.js"></script>
    </head>
    <body>
        <h1>Фотоальбом "%s"</h1>
        <div class="galleria">
`, escapedAlbum, escapedAlbum))

	encodedAlbum := url.PathEscape(album)
	for _, photo := range photos {
		encodedPhoto := url.PathEscape(photo)
		escapedPhoto := html.EscapeString(photo)
		photoURL := fmt.Sprintf("photos/%s/%s", encodedAlbum, encodedPhoto)
		b.WriteString(fmt.Sprintf("            <img src=\"%s\" data-title=\"%s\">\n", photoURL, escapedPhoto))
	}
	b.WriteString(`        </div>
        <p>Вернуться на <a href="index.html">главную страницу</a> фотоархива</p>
        <script>
            (function() {
                Galleria.run('.galleria');
            }());
        </script>
    </body>
</html>`)
	return b.String()
}

func generateErrorHTML() string {
	return `<!doctype html>
<html>
    <head>
        <title>Фотоархив</title>
    </head>
<body>
    <h1>Ошибка</h1>
    <p>Ошибка при доступе к фотоархиву. Вернитесь на <a href="index.html">главную страницу</a> фотоархива.</p>
</body>
</html>`
}
