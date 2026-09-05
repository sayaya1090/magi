namespace Magi.Ppt.Hand;

/// <summary>도형 아홉 도구: 찾기·서식·이동·정렬·그룹·링크·글 일부 서식·도형 그림.</summary>
public sealed partial class Hand
{
    private static readonly string[] AlignHow = { "left", "right", "center", "top", "bottom", "middle", "distribute_h", "distribute_v" };
    private static readonly string[] Underlines = { "None", "Single", "Double", "Heavy", "Dotted", "DottedHeavy", "Dash", "DashHeavy", "DashLong", "DashLongHeavy", "DotDash", "DotDashHeavy", "DotDotDash", "DotDotDashHeavy", "Wavy", "WavyHeavy", "WavyDouble" };
    private static readonly string[] VAligns = { "Top", "Middle", "Bottom", "TopCentered", "MiddleCentered", "BottomCentered" };
    private static readonly string[] Autosizes = { "AutoSizeNone", "AutoSizeShapeToFitText", "AutoSizeTextToFitShape" };
    private static readonly string[] LineDashes = { "Solid", "Dash", "DashDot", "DashDotDot", "LongDash", "LongDashDot", "LongDashDotDot", "RoundDot", "SquareDot", "SystemDash", "SystemDashDot", "SystemDot" };
    private static readonly string[] ZOrders = { "BringToFront", "BringForward", "SendBackward", "SendToBack" };

    private static string? OneOf(Args a, string key, string[] allowed, bool ci = false)
    {
        var v = a.Str(key); if (v is null) return null;
        var hit = allowed.FirstOrDefault(x => ci ? string.Equals(x, v, StringComparison.OrdinalIgnoreCase) : x == v);
        return hit ?? throw new HandError($"{key} 는 {string.Join(", ", allowed)} 중 하나입니다 — '{v}' 는 아닙니다");
    }
    private static string? Hex(Args a, string key, bool noneOk = false)
    {
        var v = a.Str(key); if (v is null) return null;
        if (noneOk && v.Equals("none", StringComparison.OrdinalIgnoreCase)) return "none";
        var h = v.TrimStart('#');
        if (h.Length != 6 || !h.All(Uri.IsHexDigit)) throw new HandError($"{key} 는 #RRGGBB 로 주세요 — '{v}'");
        return "#" + h.ToUpperInvariant();
    }
    private ShapeInfo ShapeOn(int n, string id)
    {
        var d = ops.ReadSlide(n);
        return d.Shapes.FirstOrDefault(s => s.ShapeId == id) ?? throw new HandError($"슬라이드 {n} 에 도형 {id} 이 없습니다 — 이 장의 도형: {(d.Shapes.Count == 0 ? "없음" : string.Join(", ", d.Shapes.Select(s => s.ShapeId)))}");
    }
    private static Dictionary<string, object?> ShapeRow(int slide, string slideId, ShapeInfo s, bool text) => new()
    {
        ["slide"] = slide, ["slide_id"] = slideId, ["shape_id"] = s.ShapeId, ["name"] = s.Name, ["type"] = s.Type, ["placeholder"] = s.Placeholder,
        ["font"] = s.Font, ["text"] = text ? Clip(s.Text, 120) : null, ["left"] = s.Left, ["top"] = s.Top, ["width"] = s.Width, ["height"] = s.Height,
    };

    private (Dictionary<string, object?>, List<string>)? Shapes(string op, Args a)
    {
        switch (op)
        {
            case "find_shapes":
            {
                var limit = a.Int("limit") ?? 50;
                var one = a.Has("slide") || a.Has("slide_id") ? ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")) : (int?)null;
                var text = a.Str("text")?.ToLowerInvariant(); var name = a.Str("name"); var type = a.Str("type"); var ph = a.Str("placeholder")?.ToLowerInvariant(); var font = a.Str("font");
                var hits = new List<Dictionary<string, object?>>(); var seen = 0;
                foreach (var s in ops.ListSlides())
                {
                    if (one is int o && s.Slide != o) continue;
                    foreach (var sh in ops.ReadSlide(s.Slide).Shapes)
                    {
                        seen++;
                        if (type is not null && !string.Equals(sh.Type, type, StringComparison.OrdinalIgnoreCase)) continue;
                        if (name is not null && !sh.Name.Contains(name, StringComparison.OrdinalIgnoreCase)) continue;
                        if (ph is not null && FakeOps.Role(sh.Placeholder) != ph) continue;
                        if (font is not null && !string.Equals(sh.Font, font, StringComparison.OrdinalIgnoreCase)) continue;
                        if (text is not null && !sh.Text.ToLowerInvariant().Contains(text)) continue;
                        hits.Add(ShapeRow(s.Slide, s.SlideId, sh, text is not null));
                    }
                }
                return (new() { ["shapes"] = hits.Take(limit).ToList(), ["matched"] = hits.Count, ["searched"] = seen }, new() { $"도형 {seen}개 중 {hits.Count}개가 맞았습니다" + (hits.Count > limit ? $" (앞 {limit}개만)" : "") });
            }
            case "format_shape":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var id = a.Str("shape_id") ?? throw new HandError("shape_id 가 없습니다");
                ShapeOn(n, id);
                var tr = a.Num("transparency"); if (tr is double t && (t < 0 || t > 1)) throw new HandError($"transparency 는 0~1 입니다 — {t}");
                var f = new ShapeFormat(a.Str("font"), a.Num("size"), a.Bool("bold"), a.Bool("italic"), Hex(a, "color"), Hex(a, "fill", true), OneOf(a, "align", new[] { "left", "center", "right", "justify" }, true),
                    Hex(a, "line", true), a.Num("line_weight"), OneOf(a, "line_dash", LineDashes), tr, OneOf(a, "underline", Underlines), a.Bool("strikethrough"), a.Bool("subscript"), a.Bool("superscript"),
                    a.Bool("all_caps"), a.Bool("small_caps"), OneOf(a, "valign", VAligns), a.Bool("wrap"), OneOf(a, "autosize", Autosizes), a.Bool("bullet"), OneOf(a, "bullet_type", new[] { "None", "Numbered", "Unnumbered" }),
                    a.Str("bullet_style"), a.Int("indent"), a.Num("rotation"), a.Bool("visible"), a.Str("alt_title"), a.Str("alt_text"), a.Bool("decorative"));
                var lines = new List<string>();
                void L(string k, object? v, string label) { if (v is not null) lines.Add($"{label} → {v}"); }
                L("font", f.Font, "글꼴"); L("size", f.Size, "크기"); L("bold", f.Bold, "굵게"); L("italic", f.Italic, "기울임"); L("color", f.Color, "글자색"); L("fill", f.Fill, "채우기"); L("align", f.Align, "정렬");
                L("line", f.Line, "윤곽선"); L("line_weight", f.LineWeight, "선 굵기"); L("line_dash", f.LineDash, "선 모양"); L("transparency", f.Transparency, "투명도"); L("underline", f.Underline, "밑줄"); L("strikethrough", f.Strikethrough, "취소선");
                L("subscript", f.Subscript, "아래 첨자"); L("superscript", f.Superscript, "위 첨자"); L("all_caps", f.AllCaps, "대문자"); L("small_caps", f.SmallCaps, "작은 대문자"); L("valign", f.VAlign, "세로 정렬"); L("wrap", f.Wrap, "줄바꿈"); L("autosize", f.Autosize, "자동 맞춤");
                L("bullet", f.Bullet, "글머리"); L("bullet_type", f.BulletType, "글머리 종류"); L("bullet_style", f.BulletStyle, "번호 모양"); L("indent", f.Indent, "들여쓰기"); L("rotation", f.Rotation, "회전"); L("visible", f.Visible, "보임"); L("alt_title", f.AltTitle, "대체 제목"); L("alt_text", f.AltText, "대체 텍스트"); L("decorative", f.Decorative, "장식");
                if (lines.Count == 0) throw new HandError("바꿀 것이 하나도 안 왔습니다 — font, size, bold, color, fill, align … 중 하나는 주세요");
                ops.FormatShape(n, id, f); Mutated();
                return (new() { ["slide"] = n, ["shape_id"] = id, ["changed"] = lines.Count }, new() { $"슬라이드 {n} · 도형 {id}: " + string.Join(", ", lines) });
            }
            case "move_shape":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var id = a.Str("shape_id") ?? throw new HandError("shape_id 가 없습니다");
                var was = ShapeOn(n, id); var z = OneOf(a, "z_order", ZOrders);
                if (!a.Has("left") && !a.Has("top") && !a.Has("width") && !a.Has("height") && z is null) throw new HandError("left, top, width, height, z_order 중 하나는 주세요");
                ops.MoveShape(n, id, a.Num("left"), a.Num("top"), a.Num("width"), a.Num("height"), z); Mutated();
                var now = ShapeOn(n, id);
                return (new() { ["slide"] = n, ["shape_id"] = id, ["left"] = now.Left, ["top"] = now.Top, ["width"] = now.Width, ["height"] = now.Height, ["z_order"] = z },
                        new() { $"슬라이드 {n} · 도형 {id}: ({R(was.Left)},{R(was.Top)}) {R(was.Width)}×{R(was.Height)} → ({R(now.Left)},{R(now.Top)}) {R(now.Width)}×{R(now.Height)}" + (z is not null ? $" · {z}" : "") });
            }
            case "align_shapes":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id"));
                var how = OneOf(a, "how", AlignHow) ?? throw new HandError("how 가 없습니다 — " + string.Join(", ", AlignHow));
                var all = ops.ReadSlide(n).Shapes; var ids = a.Strings("shape_ids");
                var pick = ids.Count == 0 ? all.ToList() : ids.Select(i => all.FirstOrDefault(s => s.ShapeId == i) ?? throw new HandError($"슬라이드 {n} 에 도형 {i} 이 없습니다 — 이 장의 도형: {string.Join(", ", all.Select(s => s.ShapeId))}")).ToList();
                var need = how.StartsWith("distribute") ? 3 : 2;
                if (pick.Count < need) throw new HandError($"{how} 는 도형이 {need}개 이상이어야 합니다 — {pick.Count}개");
                var target = Aligned(pick, how);
                var moved = 0;
                foreach (var (s, (l, t)) in pick.Zip(target))
                    if (Math.Abs(l - s.Left) > 0.5 || Math.Abs(t - s.Top) > 0.5) { ops.MoveShape(n, s.ShapeId, l, t, null, null, null); moved++; }
                if (moved > 0) Mutated();
                var after = pick.Zip(target).Select(p => (p.First.ShapeId, L: p.Second.Item1, T: p.Second.Item2, p.First.Width, p.First.Height)).ToList();
                var overlaps = 0;
                for (var i = 0; i < after.Count; i++) for (var j = i + 1; j < after.Count; j++)
                    if (after[i].L < after[j].L + after[j].Width && after[j].L < after[i].L + after[i].Width && after[i].T < after[j].T + after[j].Height && after[j].T < after[i].T + after[i].Height) overlaps++;
                var line = $"슬라이드 {n}: 도형 {pick.Count}개를 {how} 로 맞췄습니다 — {moved}개 움직임" + (overlaps > 0 ? $" · ⚠ 겹치는 쌍 {overlaps} — 축을 잘못 골랐을 수 있습니다(옆으로 늘어선 것은 top/middle, 위아래로 쌓인 것은 left/center)" : "");
                return (new() { ["slide"] = n, ["how"] = how, ["of"] = pick.Count, ["moved"] = moved, ["overlaps"] = overlaps, ["reference"] = "picked shapes, not the slide" }, new() { line });
            }
            case "group_shapes":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var ids = a.Strings("shape_ids");
                if (ids.Count < 2) throw new HandError("shape_ids 는 둘 이상이어야 합니다");
                foreach (var i in ids) ShapeOn(n, i);
                var g = ops.GroupShapes(n, ids); Mutated();
                return (new() { ["slide"] = n, ["shape_id"] = g, ["members"] = ids }, new() { $"슬라이드 {n}: 도형 {string.Join(", ", ids)} 를 그룹 {g} 으로 묶었습니다" });
            }
            case "ungroup_shapes":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var id = a.Str("shape_id") ?? throw new HandError("shape_id 가 없습니다");
                ShapeOn(n, id); var back = ops.UngroupShape(n, id); Mutated();
                return (new() { ["slide"] = n, ["ungrouped"] = id, ["shape_ids"] = back }, new() { $"슬라이드 {n}: 그룹 {id} 을 풀었습니다 — 도형 {string.Join(", ", back)}" });
            }
            case "set_hyperlink":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var id = a.Str("shape_id") ?? throw new HandError("shape_id 가 없습니다");
                var url = a.Str("url") ?? throw new HandError("url 이 없습니다 — 지우려면 빈 문자열");
                ShapeOn(n, id); ops.SetHyperlink(n, id, url, a.Str("screen_tip")); Mutated();
                return (new() { ["slide"] = n, ["shape_id"] = id, ["url"] = url, ["screen_tip"] = a.Str("screen_tip") }, new() { url.Length == 0 ? $"슬라이드 {n} · 도형 {id}: 링크를 지웠습니다" : $"슬라이드 {n} · 도형 {id}: 링크 → {url}" });
            }
            case "format_text":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var id = a.Str("shape_id") ?? throw new HandError("shape_id 가 없습니다");
                var sh = ShapeOn(n, id); int start, length;
                if (a.Str("find") is string find)
                {
                    var occ = Math.Max(1, a.Int("occurrence") ?? 1); var at = -1;
                    for (var k = 0; k < occ; k++) { at = sh.Text.IndexOf(find, at + 1, StringComparison.Ordinal); if (at < 0) throw new HandError(k == 0 ? $"도형 {id} 의 글에 '{find}' 가 없습니다 — 글: \"{Clip(sh.Text, 80)}\"" : $"'{find}' 는 {k}번뿐입니다 — occurrence {occ} 는 없습니다"); }
                    start = at; length = find.Length;
                }
                else if (a.Int("start") is int st) { start = st; length = a.Int("length") ?? throw new HandError("start 에는 length 가 따라야 합니다"); }
                else throw new HandError("find 나 start+length 로 어느 글자인지 말해 주세요");
                if (start < 0 || length <= 0 || start + length > sh.Text.Length) throw new HandError($"글 범위가 도형 밖입니다 — 글은 {sh.Text.Length}자, 요구는 {start}+{length}");
                var f = new RunFormat(a.Str("font"), a.Num("size"), a.Bool("bold"), a.Bool("italic"), Hex(a, "color"), OneOf(a, "underline", Underlines), a.Bool("strikethrough"), a.Bool("subscript"), a.Bool("superscript"), a.Str("url"), a.Str("screen_tip"));
                var what = new List<string>();
                if (f.Font is not null) what.Add("글꼴 " + f.Font); if (f.Size is not null) what.Add("크기 " + f.Size); if (f.Bold is not null) what.Add("굵게 " + f.Bold); if (f.Italic is not null) what.Add("기울임 " + f.Italic);
                if (f.Color is not null) what.Add("색 " + f.Color); if (f.Underline is not null) what.Add("밑줄 " + f.Underline); if (f.Strikethrough is not null) what.Add("취소선"); if (f.Subscript is not null) what.Add("아래 첨자"); if (f.Superscript is not null) what.Add("위 첨자"); if (f.Url is not null) what.Add("링크 " + f.Url);
                if (what.Count == 0) throw new HandError("걸 서식이 하나도 안 왔습니다 — bold, color, size, underline, url … 중 하나는 주세요");
                ops.FormatRun(n, id, start, length, f); Mutated();
                var picked = sh.Text.Substring(start, length);
                return (new() { ["slide"] = n, ["shape_id"] = id, ["start"] = start, ["length"] = length, ["text"] = picked }, new() { $"슬라이드 {n} · 도형 {id}: \"{Clip(picked, 40)}\" 에 {string.Join(", ", what)}" });
            }
            case "render_shape":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var id = a.Str("shape_id") ?? throw new HandError("shape_id 가 없습니다");
                ShapeOn(n, id); var w = Math.Clamp(a.Int("max_width") ?? 640, 80, 4096);
                var r = ops.RenderShape(n, id, w);
                return (new() { ["slide"] = n, ["shape_id"] = id, ["image_base64"] = r.Base64Png, ["image_mime"] = "image/png", ["image_bytes"] = r.Bytes, ["max_width"] = w }, new() { $"슬라이드 {n} · 도형 {id} 를 {r.Width}×{r.Height} 으로 떴습니다({r.Bytes} 바이트)" });
            }
        }
        return null;
    }

    /// <summary>정렬 계산 — 기준은 고른 도형들의 테두리 상자(장 크기는 안 읽는다). 순수 함수라 시험이 잰다.</summary>
    internal static List<(double Left, double Top)> Aligned(IReadOnlyList<ShapeInfo> pick, string how)
    {
        double minL = pick.Min(s => s.Left), maxR = pick.Max(s => s.Left + s.Width), minT = pick.Min(s => s.Top), maxB = pick.Max(s => s.Top + s.Height);
        var outp = pick.Select(s => (s.Left, s.Top)).ToList();
        switch (how)
        {
            case "left": return pick.Select(s => (minL, s.Top)).ToList();
            case "right": return pick.Select(s => (maxR - s.Width, s.Top)).ToList();
            case "center": { var cx = (minL + maxR) / 2; return pick.Select(s => (cx - s.Width / 2, s.Top)).ToList(); }
            case "top": return pick.Select(s => (s.Left, minT)).ToList();
            case "bottom": return pick.Select(s => (s.Left, maxB - s.Height)).ToList();
            case "middle": { var cy = (minT + maxB) / 2; return pick.Select(s => (s.Left, cy - s.Height / 2)).ToList(); }
            case "distribute_h":
            {
                var order = pick.Select((s, i) => (s, i)).OrderBy(p => p.s.Left).ToList();
                var gap = (maxR - minL - pick.Sum(s => s.Width)) / (pick.Count - 1); var x = minL;
                foreach (var (s, i) in order) { outp[i] = (x, s.Top); x += s.Width + gap; }
                return outp;
            }
            case "distribute_v":
            {
                var order = pick.Select((s, i) => (s, i)).OrderBy(p => p.s.Top).ToList();
                var gap = (maxB - minT - pick.Sum(s => s.Height)) / (pick.Count - 1); var y = minT;
                foreach (var (s, i) in order) { outp[i] = (s.Left, y); y += s.Height + gap; }
                return outp;
            }
        }
        return outp;
    }
    private static string R(double x) => Math.Round(x).ToString();
}
