//go:build tools

// Package tools records development-tool dependencies for reproducible local
// and CI workflows without adding them to Hardcore's runtime module.
package tools

import (
	_ "github.com/conventionalcommit/commitlint"
	_ "gotest.tools/gotestsum"
)
