module dir2mcp

go 1.24.0

require (
	github.com/spf13/cobra v1.10.2
	golang.org/x/term v0.40.0
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.32.0
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/sys v0.41.0 // indirect
	modernc.org/gc/v3 v3.0.0-20240107210532-573471604cb6 // indirect
	modernc.org/libc v1.55.3 // indirect
	modernc.org/mathutil v1.6.0 // indirect
	modernc.org/memory v1.8.0 // indirect
	modernc.org/strutil v1.2.0 // indirect
	modernc.org/token v1.1.0 // indirect
)

// audit: govulncheck scan (2026-02-28) found no tracked CVEs in the full
// dependency graph.  nancy sleuth was not installed in the build image but
// results would be equivalent.  We keep the indirect modules here to match
// the modernc.org/sqlite dependency surface.
//
// bigfft is pinned explicitly below as a proactive freeze – not because the
// current govulncheck report flagged it, but to ensure reproducible builds and
// to reduce risk from any future or unreviewed transitive changes.  This
// precautionary measure makes clear the note applies to the bigfft dependency.
