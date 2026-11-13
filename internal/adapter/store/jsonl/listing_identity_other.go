//go:build !darwin && !linux

package jsonl

import "os"

// Unknown file identity semantics: use the ordinary log reader on this platform.
func listingIdentity(os.FileInfo) string { return "" }
