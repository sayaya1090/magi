// Package wintext turns text a Windows program wrote in the machine's own code page into UTF-8.
//
// # What it is for
//
// The bash tool runs `powershell -NoProfile -Command …` on Windows (builtin.Shell), and PowerShell
// 5.1 writes its output — including its own error messages — in the console's code page, not UTF-8.
// On a Korean install that is CP949. Measured 2026-09-13 on this machine, `sed … && rm …` (a model
// writing POSIX in a shell that has no `&&`):
//
//	"\xc0\xa7ġ \xc1\xd9:1 \xb9\xae\xc0\xda:27\r\n … '&&' \xc5\xe4ū\xc0\xba …"
//
// Not valid UTF-8. Those bytes then go through JSON on the way to the model, where every one of
// them becomes U+FFFD — so the agent reads `위치 줄:1` as `??ġ ??:1` and cannot tell a syntax
// error from a missing file. It retries blindly, and the reason it should have read is gone before
// anyone can look at it. The same is true of every non-ASCII Windows program the tool runs.
//
// # What it does, and what it deliberately does not
//
// It converts only bytes that are **already unreadable**: text that is valid UTF-8 is returned
// untouched (so a program that writes UTF-8 — most of the toolchain — is never re-interpreted), and
// so is anything carrying a NUL, because that is not text at all and this tree already decides that
// question one way (builtin.isBinary, the read tool's refusal). It never fails: bytes it cannot
// place come back exactly as they arrived, which is no worse than what happens today.
//
// It is a no-op on every other platform, where a program's output is UTF-8 and reinterpreting it
// would be the bug.
package wintext
