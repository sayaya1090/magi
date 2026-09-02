# 도구 전수 점검 — 실물 PowerPoint 에 대고 28개를 하나씩 불러 본다.
#
# 층 1~4 는 「우리가 정한 것을 지키는가」를 재고, 이 파일은 **호스트가 실제로 어떻게 답하는가**를
# 잰다(docs/TESTING.ko.md §5). 결과는 판정만이 아니라 **무엇을 몇 개 봤는지**를 같이 적는다.
$ErrorActionPreference = 'Continue'
$token = (Get-Content "$env:TEMP\ppt-token.txt" -Raw).Trim()
$h = @{ Authorization = "Bearer $token"; Accept = "application/json, text/event-stream" }

$script:pass = 0
$script:fail = 0
function Call($name, $argmap) {
  $json = @{ jsonrpc = "2.0"; id = 1; method = "tools/call"; params = @{ name = $name; arguments = $argmap } } |
    ConvertTo-Json -Depth 12 -Compress
  $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
  try {
    $r = Invoke-WebRequest -Uri https://127.0.0.1:3000/mcp -Method Post -Headers $h `
      -ContentType "application/json; charset=utf-8" -Body $bytes -UseBasicParsing
    ([System.Text.Encoding]::UTF8.GetString($r.RawContentStream.ToArray()) | ConvertFrom-Json).result
  } catch {
    @{ isError = $true; content = @(@{ text = "HTTP: $($_.Exception.Message)" }) }
  }
}
function Try1($label, $name, $argmap, [switch]$ExpectError) {
  $r = Call $name $argmap
  $bad = $r.isError -eq $true
  $txt = ""
  if ($r.content) { $txt = ($r.content[0].text -replace "`r?`n", " ") }
  $good = if ($ExpectError) { $bad } else { -not $bad }
  # **판정은 파이프라인이 아니라 콘솔로 보낸다.** 파이프로 흘리면 `| Out-Null` 이 결과까지
  # 같이 삼켜서, 실패가 몇 개인지는 알아도 **무엇이 실패했는지**가 화면에서 사라진다.
  if ($good) { $script:pass++; Write-Host "  ok   $label" }
  else { $script:fail++; Write-Host "  FAIL $label — $($txt.Substring(0, [Math]::Min(200, $txt.Length)))" }
  $r
}
function JsonOf($r) {
  if (-not $r.content) { return $null }
  try { return $r.content[0].text | ConvertFrom-Json } catch { return $null }
}

# ── 준비: 깨끗한 장 하나를 만들어 그 위에서만 논다 ──────────────────────────
"== 준비"
$r = Try1 "list_layouts 가 이 덱의 레이아웃을 준다" "list_layouts" @{}
$layouts = (JsonOf $r).masters[0].layouts
$titleOnly = ($layouts | Where-Object { $_.placeholders -contains 'Title' } | Select-Object -First 1).layout
"     레이아웃 $($layouts.Count)개, 시험에 쓸 것: $titleOnly"

$r = Try1 "add_slide 로 장을 만든다" "add_slide" @{ layout = $titleOnly; title = "도구 전수 점검" }
$made = JsonOf $r
$sid = $made.slide_id
"     새 장: $($made.slide) (id $sid) filled=$($made.filled -join ',')"

# ── 읽기 ────────────────────────────────────────────────────────────────────
"== 읽기"
$r = Try1 "list_slides" "list_slides" @{}
"     총 $((JsonOf $r).total) 장"
Try1 "read_slide" "read_slide" @{ slide_id = $sid } | Out-Null
Try1 "find_shapes" "find_shapes" @{ text = "점검" } | Out-Null
Try1 "render_slide" "render_slide" @{ slide_id = $sid } | Out-Null
Try1 "export_slide_ooxml" "export_slide_ooxml" @{ slide_id = $sid; part = "list" } | Out-Null
$r = Try1 "snapshot_slide" "snapshot_slide" @{ slide_id = $sid }
$snap = (JsonOf $r).snapshot
"     스냅샷: $snap"

# ── 도형 ────────────────────────────────────────────────────────────────────
"== 도형"
$r = Try1 "add_shape (글상자)" "add_shape" @{ slide_id = $sid; kind = "textbox"; text = "글상자"; left = 40; top = 300; width = 200; height = 40 }
$box = (JsonOf $r).shape_id
$r = Try1 "add_shape (화살표)" "add_shape" @{ slide_id = $sid; kind = "rightArrow"; text = "흐름"; left = 260; top = 300; width = 160; height = 60 }
$arrow = (JsonOf $r).shape_id
$r = Try1 "add_shape (한국어 이름: 별)" "add_shape" @{ slide_id = $sid; kind = "별"; left = 440; top = 300; width = 80; height = 80 }
$star = (JsonOf $r).shape_id
Try1 "add_shape (모르는 이름은 거절)" "add_shape" @{ slide_id = $sid; kind = "우주선" } -ExpectError | Out-Null
Try1 "set_text" "set_text" @{ slide_id = $sid; shape_id = $box; text = "글자 바꿈" } | Out-Null
Try1 "format_shape" "format_shape" @{ slide_id = $sid; shape_id = $box; size = 20; bold = $true; color = "#B7472A" } | Out-Null
Try1 "move_shape" "move_shape" @{ slide_id = $sid; shape_id = $box; left = 40; top = 380; width = 240; height = 50 } | Out-Null
Try1 "set_hyperlink" "set_hyperlink" @{ slide_id = $sid; shape_id = $box; url = "https://example.com" } | Out-Null

# ── 줄 세우기 ───────────────────────────────────────────────────────────────
# 셋을 비뚤게 놓고 맞춘 뒤 **COM 으로 읽어** 정말 섰는지 본다. 도구가 「맞췄습니다」라고
# 답하는 것과 화면이 그런 것은 다른 이야기이고, 이 파일은 후자를 재는 자리다.
"== 줄 세우기"
$ids = @()
foreach ($sp in @(@(60,470,90), @(200,500,120), @(400,455,60))) {
  $r = Call "add_shape" @{ slide_id = $sid; kind = "rectangle"; left = $sp[0]; top = $sp[1]; width = $sp[2]; height = 30 }
  $ids += (JsonOf $r).shape_id
}
Try1 "align_shapes (위 맞춤)" "align_shapes" @{ slide_id = $sid; how = "top"; shape_ids = $ids } | Out-Null
Try1 "align_shapes (하나만 고르면 거절)" "align_shapes" @{ slide_id = $sid; how = "left"; shape_ids = @($ids[0]) } -ExpectError | Out-Null
Try1 "align_shapes (없는 도형 id 는 거절)" "align_shapes" @{ slide_id = $sid; how = "left"; shape_ids = @($ids[0], "없는-것") } -ExpectError | Out-Null
Try1 "align_shapes (모르는 정렬은 거절)" "align_shapes" @{ slide_id = $sid; how = "대각선"; shape_ids = $ids } -ExpectError | Out-Null
Try1 "align_shapes (간격 고르게)" "align_shapes" @{ slide_id = $sid; how = "distribute_h"; shape_ids = $ids } | Out-Null
# **화면을 읽는다.** 세 상자의 top 이 같고 사이 틈이 같아야 한다.
try {
  $ppt = [Runtime.InteropServices.Marshal]::GetActiveObject("PowerPoint.Application")
  $slide = $ppt.ActivePresentation.Slides | Where-Object { $_.SlideID -ne $null } | ForEach-Object { $_ }
  $target = $null
  foreach ($sl in $ppt.ActivePresentation.Slides) {
    foreach ($sh in $sl.Shapes) { if ($sh.HasTextFrame -and $sh.TextFrame.HasText -and $sh.TextFrame.TextRange.Text -eq "도구 전수 점검") { $target = $sl } }
  }
  if ($target) {
    $band = @()
    foreach ($sh in $target.Shapes) { if ($sh.Top -gt 440 -and $sh.Top -lt 520 -and $sh.Height -lt 40) { $band += [pscustomobject]@{ L=[int]$sh.Left; R=[int]($sh.Left+$sh.Width); T=[int]$sh.Top } } }
    $band = $band | Sort-Object L
    if ($band.Count -ge 3) {
      $tops = ($band | ForEach-Object { $_.T } | Sort-Object -Unique)
      if ($tops.Count -eq 1) { $script:pass++; Write-Host "  ok   화면에서도 위가 맞았다 — top=$($tops[0])" }
      else { $script:fail++; Write-Host "  FAIL 화면에서는 안 맞았다 — top=$($tops -join ',')" }
      $gaps = @(); for ($i = 1; $i -lt $band.Count; $i++) { $gaps += ($band[$i].L - $band[$i-1].R) }
      $spread = ($gaps | Measure-Object -Maximum -Minimum)
      if (($spread.Maximum - $spread.Minimum) -le 1) { $script:pass++; Write-Host "  ok   화면에서도 틈이 고르다 — $($gaps -join ', ')" }
      else { $script:fail++; Write-Host "  FAIL 화면에서는 틈이 제각각 — $($gaps -join ', ')" }
    } else { Write-Host "  ..   상자를 못 찾아 화면 확인은 건너뜀 ($($band.Count)개)" }
  } else { Write-Host "  ..   장을 못 찾아 화면 확인은 건너뜀" }
} catch { Write-Host "  ..   COM 을 못 잡아 화면 확인은 건너뜀: $($_.Exception.Message)" }

# ── 표 ──────────────────────────────────────────────────────────────────────
"== 표"
$r = Try1 "add_table" "add_table" @{ slide_id = $sid; rows = 2; columns = 3; left = 560; top = 300; width = 340; height = 100 }
$tbl = (JsonOf $r).shape_id
"     표 id $tbl · 경고=$((JsonOf $r).tables_before)"
Try1 "set_table_cells" "set_table_cells" @{ slide_id = $sid; shape_id = $tbl; cells = @(@{row=0;column=0;text="가"}, @{row=1;column=2;text="나"}) } | Out-Null
$r = Try1 "replace_table (열 하나 더)" "replace_table" @{ slide_id = $sid; columns = 4 }
$rep = JsonOf $r
"     $($rep.was.rows)x$($rep.was.columns) → $($rep.rows)x$($rep.columns), 새 id $($rep.shape_id)"

# ── 스타일 ──────────────────────────────────────────────────────────────────
"== 스타일"
$r = Try1 "describe_style" "describe_style" @{}
$st = JsonOf $r
"     자리표시자 $($st.seen)개를 보고 정함"
Try1 "apply_style (고른 장만)" "apply_style" @{ slides = @($made.slide); title = @{ size = 30 } } | Out-Null
Try1 "apply_style (바꿀 것 없으면 거절)" "apply_style" @{} -ExpectError | Out-Null
$r = Try1 "add_slides (개요 한 번에)" "add_slides" @{ slides = @(
  @{ layout = $titleOnly; title = "한 번에 1" },
  @{ layout = $titleOnly; title = "한 번에 2" }) }
$batch = JsonOf $r
"     $($batch.made)장 생성"
foreach ($row in $batch.slides) { Try1 "batch 정리: $($row.slide)번 삭제" "delete_slide" @{ slide_id = $row.slide_id } | Out-Null }

# ── 안내 ────────────────────────────────────────────────────────────────────
"== 안내"
Try1 "advise" "advise" @{ items = @(@{ message = "점검용 안내"; why = "전수 점검"; slide_id = $sid; shape_ids = @($star) }) } | Out-Null
Try1 "clear_advice" "clear_advice" @{} | Out-Null

# ── 장 다루기 ───────────────────────────────────────────────────────────────
"== 장"
Try1 "apply_layout" "apply_layout" @{ slide_id = $sid; layout = $titleOnly } | Out-Null
$r = Try1 "duplicate_slide" "duplicate_slide" @{ slide_id = $sid }
$copy = (JsonOf $r).slide_id
Try1 "reorder_slide" "reorder_slide" @{ slide_id = $copy; to = 1 } | Out-Null
Try1 "delete_slide (생략은 거절)" "delete_slide" @{} -ExpectError | Out-Null
Try1 "delete_slide" "delete_slide" @{ slide_id = $copy } | Out-Null
Try1 "delete_shape" "delete_shape" @{ slide_id = $sid; shape_id = $arrow } | Out-Null
Try1 "restore_slide" "restore_slide" @{ snapshot = $snap } | Out-Null

"== 결과: 통과 $script:pass · 실패 $script:fail"
