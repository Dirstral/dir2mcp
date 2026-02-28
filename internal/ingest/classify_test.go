package ingest

import "testing"

func TestClassifyDocType(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "main.go", want: "code"},
		{path: "README.md", want: "md"},
		{path: "notes.txt", want: "text"},
		{path: "dataset.csv", want: "data"},
		{path: "index.html", want: "html"},
		{path: "manual.pdf", want: "pdf"},
		{path: "image.png", want: "image"},
		{path: "audio.mp3", want: "audio"},
		{path: "bundle.zip", want: "archive"},
		{path: "blob.bin", want: "binary_ignored"},
		{path: "Dockerfile", want: "code"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			got := ClassifyDocType(tc.path)
			if got != tc.want {
				t.Fatalf("ClassifyDocType(%q)=%q want=%q", tc.path, got, tc.want)
			}
		})
	}
}
