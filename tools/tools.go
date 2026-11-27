//go:build tools

// Package tools records development-tool dependencies for reproducible local
// and CI workflows without adding them to Hardcore's runtime module.
package tools

import (
	// Blank import pins commitlint, used by the commit-msg hook.
	_ "github.com/conventionalcommit/commitlint"
	// Blank import pins gotestsum, used by make test.
	_ "gotest.tools/gotestsum"
)
