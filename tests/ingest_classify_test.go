package tests

import (
	"testing"

	"dir2mcp/internal/ingest"
)

func TestClassifyDocType(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "main.go", want: "code"},
		{path: "src/lib/deep/main.go", want: "code"},
		{path: "README.md", want: "md"},
		{path: "README.MD", want: "md"},
		{path: "notes.txt", want: "text"},
		{path: "dataset.csv", want: "data"},
		{path: "index.html", want: "html"},
		{path: "manual.pdf", want: "pdf"},
		{path: "image.png", want: "image"},
		{path: "audio.mp3", want: "audio"},
		{path: "bundle.zip", want: "archive"},
		{path: "blob.bin", want: "binary_ignored"},
		{path: "Dockerfile", want: "code"},
		{path: "Makefile", want: "code"},
		{path: "Jenkinsfile", want: "code"},
		{path: "go.mod", want: "data"},
		{path: ".env", want: "data"},
		{path: ".env.local", want: "data"},
		{path: ".env.production", want: "data"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got := ingest.ClassifyDocType(tc.path)
			if got != tc.want {
				t.Fatalf("ClassifyDocType(%q)=%q want=%q", tc.path, got, tc.want)
			}
		})
	}
}
