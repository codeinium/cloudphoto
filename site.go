package main

import (
	"fmt"
	"html"
	"net/url"
	"strings"
)

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
</body>
</html>`
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
</body>
</html>`)
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

	encodedAlbum := encodeName(album)
	for _, photo := range photos {
		encodedPhoto := encodeName(photo)
		escapedPhoto := html.EscapeString(photo)
		photoURL := "photos/" + url.PathEscape(encodedAlbum) + "/" + url.PathEscape(encodedPhoto)
		b.WriteString(fmt.Sprintf("            <img src=\"%s\" data-title=\"%s\">\n", html.EscapeString(photoURL), escapedPhoto))
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
