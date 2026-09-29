package web

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
)

// staticHandler serves the embedded assets. Embedded files carry no
// modification time, so http.FileServer alone sends no validator and the
// browser fetched every stylesheet, script, font and the icon sprite again on
// each page. Each file gets an ETag from its content instead: the browser
// revalidates with a cheap 304 and picks up a new release at once. Fonts never
// change under the same name, so they are cached outright (rename a font file
// when replacing it).
func staticHandler() http.Handler {
	etags := map[string]string{}
	_ = fs.WalkDir(staticFS, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := staticFS.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		etags["/"+p] = `"` + hex.EncodeToString(sum[:12]) + `"`
		return nil
	})
	files := http.FileServer(http.FS(staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tag, ok := etags[r.URL.Path]; ok {
			w.Header().Set("ETag", tag)
			if strings.HasPrefix(r.URL.Path, "/static/fonts/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
		}
		files.ServeHTTP(w, r)
	})
}
