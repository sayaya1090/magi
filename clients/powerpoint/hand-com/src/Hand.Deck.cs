using System.Text.Json;

namespace Magi.Ppt.Hand;

/// <summary>덱 열 도구: 버릇 읽기·OOXML·차트·그림·배경·테마 색·레이아웃·스냅숏/복원.</summary>
public sealed partial class Hand
{
    private static readonly Dictionary<string, (string Kind, string Ko)> ChartKinds = new(StringComparer.OrdinalIgnoreCase)
    {
        ["bar"] = ("bar", "세로 막대"), ["column"] = ("bar", "세로 막대"), ["막대"] = ("bar", "세로 막대"), ["세로막대"] = ("bar", "세로 막대"),
        ["hbar"] = ("hbar", "가로 막대"), ["가로막대"] = ("hbar", "가로 막대"), ["line"] = ("line", "꺾은선"), ["꺾은선"] = ("line", "꺾은선"), ["선"] = ("line", "꺾은선"),
        ["pie"] = ("pie", "원"), ["원"] = ("pie", "원"), ["파이"] = ("pie", "원"),
    };
    private static readonly string[] ThemeNames = { "dark1", "dark2", "light1", "light2", "accent1", "accent2", "accent3", "accent4", "accent5", "accent6", "hyperlink", "followedHyperlink" };

    private (Dictionary<string, object?>, List<string>)? Deck(string op, Args a)
    {
        switch (op)
        {
            case "describe_style":
            {
                var titles = new List<ShapeInfo>(); var bodies = new List<ShapeInfo>();
                foreach (var s in ops.ListSlides())
                    foreach (var sh in ops.ReadSlide(s.Slide).Shapes)
                    {
                        if (sh.Font is null) continue;
                        var role = FakeOps.Role(sh.Placeholder);
                        if (role == "title") titles.Add(sh); else if (role == "body") bodies.Add(sh);
                    }
                static Dictionary<string, object?>? Mode(List<ShapeInfo> xs) => xs.Count == 0 ? null
                    : xs.GroupBy(x => (x.Font, x.Size, x.Color)).OrderByDescending(g => g.Count()).Select(g => new Dictionary<string, object?> { ["font"] = g.Key.Font, ["size"] = g.Key.Size, ["color"] = g.Key.Color, ["of"] = g.Count() }).First();
                var t = Mode(titles); var b = Mode(bodies);
                var note = t is null && b is null ? "이 덱에는 따라갈 만한 일관된 버릇이 없습니다 — 새 장은 테마 기본으로 섭니다" : "새 장은 이 값을 따라갑니다(match_style: false 로 끌 수 있습니다)";
                return (new() { ["title"] = t, ["body"] = b, ["seen"] = new Dictionary<string, object?> { ["titles"] = titles.Count, ["bodies"] = bodies.Count }, ["read"] = true, ["note"] = note },
                        new() { $"제목 {titles.Count}개·본문 {bodies.Count}개를 보고 정했습니다" });
            }
            case "export_slide_ooxml":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var part = a.Str("part") ?? "slide";
                if (part == "list") { var parts = ops.SlideParts(n); return (new() { ["slide"] = n, ["parts"] = parts.Select(p => new Dictionary<string, object?> { ["name"] = p.Name, ["bytes"] = p.Bytes }).ToList() }, new() { $"슬라이드 {n} 의 부분 {parts.Count}개" }); }
                if (part is not ("slide" or "notes" or "chart")) throw new HandError($"part 는 slide, notes, chart, list 중 하나입니다 — '{part}'");
                var xml = ops.SlidePart(n, part, a.Str("shape_id"));
                return (new() { ["slide"] = n, ["part"] = part, ["xml"] = xml, ["read_only"] = true }, new() { $"슬라이드 {n} 의 {part} 부분 {xml.Length}자 — 읽기만 됩니다" });
            }
            case "add_chart":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id"));
                var kindName = a.Str("kind") ?? "bar";
                if (!ChartKinds.TryGetValue(kindName, out var kind)) throw new HandError($"모르는 차트 종류입니다: {kindName} — bar/column(세로 막대), hbar(가로 막대), line(꺾은선), pie(원)");
                var cats = a.Strings("categories"); if (cats.Count == 0) throw new HandError("categories 가 비었습니다");
                var series = new List<(string, IReadOnlyList<double>)>();
                foreach (var s in a.Objects("series"))
                {
                    var vals = s.Numbers("values");
                    if (vals.Count != cats.Count) throw new HandError($"계열 '{s.Str("name")}' 의 값이 {vals.Count}개인데 항목은 {cats.Count}개입니다 — 수가 같아야 합니다");
                    series.Add((s.Str("name") ?? $"계열 {series.Count + 1}", vals));
                }
                if (series.Count == 0) throw new HandError("series 가 비었습니다 — [{name, values}]");
                if (kind.Kind == "pie" && series.Count > 1) throw new HandError("원 차트는 계열 하나만 그립니다 — 계열이 여럿이면 bar 나 line");
                var target = n; string? freshNote = null;
                if (a.Bool("new_slide") == true) { var (m, _) = ops.AddSlide(n + 1, null, null, null); target = m; freshNote = $"새 장으로 {m} 번에 끼워 넣었으므로 그 뒤의 번호는 전부 하나씩 밀렸습니다 — 들고 있던 목차가 있으면 다시 읽으세요"; }
                var spec = new ChartSpec(kind.Kind, kind.Ko, cats, series, a.Str("title"), a.Num("left") ?? 60, a.Num("top") ?? 90, a.Num("width") ?? 600, a.Num("height") ?? 400);
                var (slide, sid, shape) = ops.AddChart(target, spec); Mutated();
                var lines = new List<string> { $"슬라이드 {slide}(id {sid}) 에 {kind.Ko} 차트 {shape} 를 넣었습니다 — 항목 {cats.Count}개 · 계열 {series.Count}개. COM 으로 만든 진짜 차트라 「데이터 편집」도 열립니다" };
                if (freshNote is not null) lines.Add(freshNote);
                return (new() { ["slide"] = slide, ["slide_id"] = sid, ["shape_id"] = shape, ["chart"] = kind.Ko, ["categories"] = cats.Count, ["series"] = series.Count, ["data_sheet"] = true }, lines);
            }
            case "add_image":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id"));
                var path = a.Str("path") ?? throw new HandError("path 가 없습니다 — 그림 파일의 경로");
                var nw = a.Num("image_width") ?? 0; var nh = a.Num("image_height") ?? 0; var w = a.Num("width"); var h = a.Num("height");
                bool kept = true;
                if (w is null && h is null && nw > 0 && nh > 0) { var k = Math.Min(600 / nw, 400 / nh); w = nw * k; h = nh * k; }
                else if (w is null && h is double hh && nw > 0 && nh > 0) w = hh * nw / nh;
                else if (h is null && w is double ww && nw > 0 && nh > 0) h = ww * nh / nw;
                else if (w is not null && h is not null && nw > 0 && nh > 0) kept = Math.Abs(w.Value / h.Value - nw / nh) < 0.02;
                var target = n; string? freshNote = null;
                if (a.Bool("new_slide") == true) { var (m, _) = ops.AddSlide(n + 1, null, null, null); target = m; freshNote = $"새 장으로 {m} 번에 끼워 넣었으므로 그 뒤의 번호는 전부 하나씩 밀렸습니다"; }
                var (id, pw, ph) = ops.AddPicture(target, path, a.Num("left") ?? 60, a.Num("top") ?? 90, w, h, a.Str("alt"), a.Str("name")); Mutated();
                var lines = new List<string> { $"슬라이드 {target} 에 그림 {id} 를 넣었습니다 — {Path.GetFileName(path)}, {R(pw)}×{R(ph)}pt" + (kept ? "" : " · ⚠ 비율이 원본과 다릅니다") + (a.Str("alt") is null ? " · 대체 텍스트가 없어 파일 이름을 씁니다" : "") };
                if (freshNote is not null) lines.Add(freshNote);
                return (new() { ["slide"] = target, ["shape_id"] = id, ["path"] = path, ["format"] = a.Str("image_ext"), ["bytes"] = a.Int("image_bytes") ?? 0, ["natural"] = new Dictionary<string, object?> { ["width"] = nw, ["height"] = nh }, ["placed"] = new Dictionary<string, object?> { ["width"] = Math.Round(pw), ["height"] = Math.Round(ph) }, ["aspect_kept"] = kept }, lines);
            }
            case "set_background":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id"));
                var kind = (a.Str("kind") ?? "solid").ToLowerInvariant(); var color = a.Str("color");
                if (kind is not ("solid" or "gradient" or "pattern" or "picture")) throw new HandError($"kind 는 solid, gradient, pattern, picture 중 하나입니다 — '{kind}'");
                var tr = a.Num("transparency"); if (tr is double t && (t < 0 || t > 1)) throw new HandError($"transparency 는 0~1 입니다 — {t}");
                if (kind == "solid" && (color is null || color.Equals("theme", StringComparison.OrdinalIgnoreCase))) kind = "theme";
                if (kind == "picture" && a.Str("path") is null) throw new HandError("kind=picture 에는 path 가 있어야 합니다");
                if (kind is "solid" or "gradient" or "pattern") color = Hex(a, "color") ?? throw new HandError($"kind={kind} 에는 color 가 있어야 합니다");
                if (kind == "gradient" && a.Str("gradient") is string g && g is not ("linear" or "radial" or "rectangle" or "path")) throw new HandError($"gradient 는 linear, radial, rectangle, path 중 하나입니다 — '{g}'");
                var b = new BackgroundSpec(kind, color, tr, a.Str("gradient") ?? (kind == "gradient" ? "linear" : null), a.Str("pattern") ?? (kind == "pattern" ? "diagonalCross" : null), Hex(a, "background") ?? (kind == "pattern" ? "#FFFFFF" : null), a.Str("path"), a.Bool("hide_graphics"));
                ops.SetBackground(n, b); Mutated();
                var said = kind switch { "theme" => "테마 배경으로 되돌렸습니다", "solid" => $"배경을 {color} 로 칠했습니다", "gradient" => $"배경에 {b.Gradient} 그라데이션({color})", "pattern" => $"배경에 {b.Pattern} 무늬({color}/{b.Background})", _ => $"배경에 그림 {Path.GetFileName(b.Path!)}" };
                return (new() { ["slide"] = n, ["background"] = kind == "solid" ? color : kind, ["transparency"] = tr ?? 0, ["hide_graphics"] = b.HideGraphics }, new() { $"슬라이드 {n}: {said}" + (b.HideGraphics == true ? " · 마스터 그림 숨김" : "") });
            }
            case "read_theme_colors":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var scope = a.Str("scope") ?? "slide";
                if (scope is not ("slide" or "layout" or "master")) throw new HandError("scope 는 slide, layout, master 중 하나입니다");
                var theme = ops.ReadThemeColors(n, scope);
                return (new() { ["slide"] = n, ["scope"] = scope, ["theme"] = theme.ToDictionary(k => k.Key, k => (object?)k.Value) }, new() { $"슬라이드 {n} 의 {scope} 층 테마 색 {theme.Count}개" });
            }
            case "set_theme_colors":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var scope = a.Str("scope") ?? "slide";
                if (scope is not ("slide" or "layout" or "master")) throw new HandError("scope 는 slide, layout, master 중 하나입니다");
                var colors = a.Object("colors") ?? throw new HandError("colors 가 없습니다 — {\"accent1\": \"#1F4E79\"} 꼴");
                var set = new Dictionary<string, string>();
                foreach (var k in colors.Keys)
                {
                    var name = ThemeNames.FirstOrDefault(t => string.Equals(t, k, StringComparison.OrdinalIgnoreCase)) ?? throw new HandError($"테마 색 이름이 아닙니다: {k} — {string.Join(", ", ThemeNames)}");
                    set[name] = Hex(colors, k) ?? throw new HandError($"{k} 의 값이 비었습니다");
                }
                if (set.Count == 0) throw new HandError("colors 가 비었습니다");
                var before = new Dictionary<string, string>(ops.ReadThemeColors(n, scope), StringComparer.OrdinalIgnoreCase); // 사본 — 손이 산 사전을 그대로 주면 바꾼 뒤 비교가 제 값과 제 값이 된다
                ops.SetThemeColors(n, scope, set); Mutated();
                var faint = set.Where(kv => before.TryGetValue(kv.Key, out var was) && Near(was, kv.Value)).Select(kv => kv.Key).ToList();
                var lines = new List<string> { $"{scope} 층의 테마 색을 바꿨습니다: {string.Join(", ", set.Select(kv => $"{kv.Key}={kv.Value}"))}",
                    "테마 색이 보이는 자리: 자리표시자 글자(dark1)·배경(light1)·차트 계열(accent1~6)·표 스타일·테마 색으로 칠한 도형. #RRGGBB 로 직접 칠한 것은 안 바뀝니다",
                    scope == "master" ? "이 마스터를 쓰는 장 전부에 걸립니다" : scope == "layout" ? "이 레이아웃을 쓰는 장에 걸립니다 — 덱 전체는 scope:\"master\"" : "이 장에 걸립니다 — 덱 전체는 scope:\"master\"" };
                if (faint.Count > 0) lines.Add($"⚠ 거의 같은 색으로 바꿨습니다(눈에 안 띕니다): {string.Join(", ", faint)}");
                return (new() { ["slide"] = n, ["scope"] = scope, ["set"] = set.Count, ["fonts_unchanged"] = true }, lines);
            }
            case "apply_layout":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var layout = a.Str("layout") ?? throw new HandError("layout 이 없습니다 — list_layouts 의 이름");
                var was = ops.ListSlides()[n - 1].Layout; ops.ApplyLayout(n, layout); Mutated();
                return (new() { ["slide"] = n, ["layout"] = layout, ["was"] = was }, new() { $"슬라이드 {n}: 레이아웃 '{was}' → '{layout}'" });
            }
            case "snapshot_slide":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var sid = ops.ListSlides()[n - 1].SlideId;
                var (id, bytes) = ops.SnapshotSlide(n);
                return (new() { ["snapshot"] = id, ["slide"] = n, ["slide_id"] = sid, ["bytes"] = bytes }, new() { $"슬라이드 {n} 를 {id} 로 찍어 두었습니다({bytes} 바이트) — 이 손이 떠 있는 동안만 압니다" });
            }
            case "restore_slide":
            {
                var snap = a.Str("snapshot") ?? throw new HandError("snapshot 이 없습니다 — snapshot_slide 가 준 id");
                int? n = a.Has("slide") || a.Has("slide_id") ? ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")) : null;
                var was = n is int k ? ops.ListSlides()[k - 1].SlideId : null;
                var (m, id) = ops.RestoreSlide(snap, n); Mutated();
                return (new() { ["slide"] = m, ["slide_id"] = id, ["replaced"] = was }, new() { was is null ? $"스냅숏 {snap} 을 {m} 번 장으로 되살렸습니다(id {id})" : $"슬라이드 {m} 를 스냅숏 {snap} 으로 되돌렸습니다 — **id 가 {was} 에서 {id} 로 바뀌었습니다**" });
            }
        }
        return null;
    }
    private static bool Near(string a, string b)
    {
        static (int, int, int) Rgb(string h) { h = h.TrimStart('#'); return (Convert.ToInt32(h[..2], 16), Convert.ToInt32(h[2..4], 16), Convert.ToInt32(h[4..], 16)); }
        try { var (r1, g1, b1) = Rgb(a); var (r2, g2, b2) = Rgb(b); return Math.Abs(r1 - r2) + Math.Abs(g1 - g2) + Math.Abs(b1 - b2) < 24; } catch { return false; }
    }
}
