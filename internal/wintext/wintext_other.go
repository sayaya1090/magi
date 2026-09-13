//go:build !windows

package wintext

// ToUTF8 returns b unchanged: on these platforms a program's output is already UTF-8, and guessing
// at a code page would be the defect rather than the fix.
func ToUTF8(b []byte) []byte { return b }
