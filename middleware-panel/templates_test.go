package main

import (
	"html/template"
	"path/filepath"
	"testing"
)

func TestTemplates(t *testing.T) {
	funcMap := template.FuncMap{
		"add": func(a, b int) int { return a + b },
	}

	// Test ParseGlob as Gin does in main/routes
	tmpl := template.New("").Funcs(funcMap)
	if _, err := tmpl.ParseGlob("templates/*"); err != nil {
		t.Fatalf("ParseGlob error: %v", err)
	}

	files, err := filepath.Glob("templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		singleTmpl := template.New(filepath.Base(f)).Funcs(funcMap)
		if _, err := singleTmpl.ParseFiles(f); err != nil {
			t.Errorf("Error in %s: %v", f, err)
		}
	}
}
