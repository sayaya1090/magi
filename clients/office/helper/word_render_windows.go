//go:build windows

package office

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// renderWordPageOS 는 Word 가 스스로 그린 쪽 그림(EMF)을 PNG 로 만든다 — pdftoppm 이 없는 Windows 의 눈.
//
// ⚠ Word.js 는 쪽 그림을 안 주고 문서 전체를 PDF 로만 준다. 그래서 render_page 는 pdftoppm(poppler)에 기댔고, 그것이
// 없는 Windows 에서는 「도구가 없습니다」로 죽었다 — 실측 2026-09-26, LTSC 2021 과 이 머신 둘 다. 그런데 Word 는 COM 으로
// 쪽마다 제 손으로 그린 그림을 준다(`Pane.Pages(n).EnhMetaFileBits`). 그것을 PNG 로 옮기는 일은 GDI+ 가 하고, GDI+ 는
// Windows 에 늘 있다 — PowerShell 의 System.Drawing 을 빌린다. 남의 도구를 깔 일이 없다.
//
// 같은 문서인지는 여기서도 MAGI.DOC 표식으로 가른다(word_com.go).
func renderWordPageOS(docKey string, page, width int) ([]byte, error) {
	id := strings.TrimPrefix(docKey, "wd-")
	if id == "" || id == docKey {
		return nil, fmt.Errorf("문서 표식(MAGI.DOC)을 모릅니다(키 %q)", docKey)
	}
	dir, err := os.MkdirTemp("", "magi-wpage-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	script := filepath.Join(dir, "page.ps1")
	out := filepath.Join(dir, "page.png")
	// BOM — PowerShell 5.1 은 BOM 없는 파일을 ANSI 로 읽는다(한글이 든 스크립트가 깨진다).
	if err := os.WriteFile(script, append([]byte{0xEF, 0xBB, 0xBF}, []byte(wordPageScript)...), 0o600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script,
		"-id", id, "-page", strconv.Itoa(page), "-width", strconv.Itoa(width), "-out", out)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW — 창이 번쩍이지 않게
	said, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(said))
	if err != nil {
		if strings.HasPrefix(text, "PAGES:") {
			return nil, fmt.Errorf("이 문서는 %s쪽입니다 — %d쪽은 없습니다", strings.TrimPrefix(text, "PAGES:"), page)
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("Word 가 %d쪽을 45초 안에 못 그렸습니다", page)
		}
		return nil, fmt.Errorf("Word 로 쪽을 그리지 못했습니다: %s", lastLine(text))
	}
	return os.ReadFile(out)
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

const wordPageScript = `param([string]$id, [int]$page, [int]$width, [string]$out)
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.Encoding]::UTF8
Add-Type -AssemblyName System.Drawing
$wd = [Runtime.InteropServices.Marshal]::GetActiveObject('Word.Application')
$doc = $null
foreach ($d in $wd.Documents) {
  $p = $d.CustomDocumentProperties
  $n = [System.__ComObject].InvokeMember('Count', 'GetProperty', $null, $p, $null)
  for ($i = 1; $i -le $n; $i++) {
    $it = [System.__ComObject].InvokeMember('Item', 'GetProperty', $null, $p, @($i))
    if ([System.__ComObject].InvokeMember('Name', 'GetProperty', $null, $it, $null) -eq 'MAGI.DOC' -and
        [System.__ComObject].InvokeMember('Value', 'GetProperty', $null, $it, $null) -eq $id) { $doc = $d }
  }
}
if (-not $doc) { throw "표식 $id 인 Word 문서가 없습니다" }
$pane = $doc.ActiveWindow.Panes.Item(1)
$count = $pane.Pages.Count
if ($page -gt $count) { Write-Output "PAGES:$count"; exit 3 }
$bits = $pane.Pages.Item($page).EnhMetaFileBits
$ms = New-Object IO.MemoryStream(,[byte[]]$bits)
$mf = New-Object Drawing.Imaging.Metafile($ms)
$h = [int]($width * $mf.Height / $mf.Width)
$bmp = New-Object Drawing.Bitmap($width, $h)
$g = [Drawing.Graphics]::FromImage($bmp)
$g.SmoothingMode = [Drawing.Drawing2D.SmoothingMode]::AntiAlias
$g.TextRenderingHint = [Drawing.Text.TextRenderingHint]::AntiAliasGridFit
$g.Clear([Drawing.Color]::White)
$g.DrawImage($mf, 0, 0, $width, $h)
$g.Dispose()
$bmp.Save($out, [Drawing.Imaging.ImageFormat]::Png)
Write-Output 'OK'
`
