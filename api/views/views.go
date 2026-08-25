package views

import (
	"embed"
	"html/template"
	"io"
	"io/fs"
)

//go:embed templates/*.html static/*
var files embed.FS

var Templates = template.Must(template.New("root").Funcs(template.FuncMap{
	"mul": func(a, b float64) float64 { return a * b },
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
