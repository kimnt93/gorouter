package views

import (
	"embed"
	"html/template"
	"io"
	"io/fs"
	"strconv"
	"time"
)

//go:embed templates/*.html static/*
var files embed.FS

var Templates = template.Must(template.New("root").Funcs(template.FuncMap{
	"mul":     func(a, b float64) float64 { return a * b },
	"rfc3339": func(value time.Time) string { return value.UTC().Format(time.RFC3339) },
	"relativeTime": func(value time.Time) string {
		now := time.Now().UTC()
		seconds := int(now.Sub(value.UTC()).Seconds())
		if seconds < 0 {
			seconds = 0
		}
		if seconds < 60 {
			return strconv.Itoa(seconds) + " seconds ago"
		}
		return value.UTC().Format(time.RFC3339)
	},
	"groupInt": func(value int64) string {
		negative := value < 0
		if negative {
			value = -value
		}
		raw := strconv.FormatInt(value, 10)
		for i := len(raw) - 3; i > 0; i -= 3 {
			raw = raw[:i] + "," + raw[i:]
		}
		if negative {
			return "-" + raw
		}
		return raw
	},
}).ParseFS(files, "templates/*.html"))

func Render(w io.Writer, name string, data any) error {
	return Templates.ExecuteTemplate(w, name, data)
}

func Static() fs.FS {
	assets, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	return assets
}

func ReadAsset(name string) ([]byte, error) {
	return fs.ReadFile(Static(), name)
}
