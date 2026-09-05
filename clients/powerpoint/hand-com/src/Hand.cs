using System.Text.Json;

namespace Magi.Ppt.Hand;

/// <summary>
/// 헬퍼의 call 하나를 받아 덱 동작(IOps)으로 옮기고 봉투(HandReply)를 짓는다. JS 손(OfficeHand.js)과
/// 같은 규약이다: 답의 <c>changed</c> 는 사람이 읽는 한국어 한 줄씩, <c>result</c> 는 도구가 광고한
/// 모양, 실패는 <c>error</c> 한 문장. 모르는 op 는 「이 손이 아직 모른다」로 답한다 — 조용히 삼키지 않는다.
/// </summary>
public sealed partial class Hand
{
    private readonly IOps ops;
    private int count;
    public int Epoch { get; }
    /// <summary>헬퍼가 hello 로 준 문서 키("pid-…"). 답의 document 는 이것이어야 헬퍼가 길을 찾는다 — 손의 제 키가 아니다.</summary>
    public string Document { get; }

    public Hand(IOps ops, int epoch, string? document = null) { this.ops = ops; Epoch = epoch; Document = document ?? ops.DocumentKey; }

    /// <summary>도구 48개 전부 — 헬퍼 catalogue 와 같은 이름들. 하나라도 빠지면 시험이 잡는다(KnowsEveryTool).</summary>
    public static readonly IReadOnlySet<string> Known = new HashSet<string> {
        "list_slides", "read_slide", "find_shapes", "render_slide", "export_slide_ooxml", "list_layouts", "describe_style", "read_notes", "render_shape",
        "read_theme_colors", "read_tags", "read_animation", "read_suggestions", "snapshot_slide", "advise", "clear_advice",
        "add_slide", "add_slides", "delete_slide", "apply_style", "duplicate_slide", "set_text", "format_shape", "move_shape", "add_shape", "align_shapes",
        "add_chart", "add_image", "set_notes", "format_table_cells", "set_background", "set_theme_colors", "set_tag", "animate_slide", "suggest",
        "drop_suggestion", "delete_shape", "apply_layout", "reorder_slide", "set_hyperlink", "add_table", "replace_table", "set_table_cells", "edit_table",
        "format_text", "group_shapes", "ungroup_shapes", "restore_slide",
    };

    public HandReply Handle(HandCall call)
    {
        var reply = new HandReply { Id = call.Id, Document = Document, Label = ops.Label, Epoch = Epoch };
        try
        {
            var (result, changed) = Dispatch(call.Op, new Args(call.Args));
            reply.Result = result;
            reply.Changed = changed;
        }
        catch (HandError e) { reply.Error = e.Message; }
        catch (Exception e) { reply.Error = $"{call.Op}: {e.GetType().Name} — {e.Message}"; }
        reply.Count = count;
        return reply;
    }

    private (Dictionary<string, object?> Result, List<string> Changed) Dispatch(string op, Args a)
    {
        switch (op)
        {
            case "list_slides":
            {
                var slides = ops.ListSlides();
                return (new() { ["slides"] = slides.Select(s => new Dictionary<string, object?> { ["slide"] = s.Slide, ["slide_id"] = s.SlideId, ["layout"] = s.Layout, ["shapes"] = s.Shapes, ["title"] = s.Title }).ToList(), ["count"] = slides.Count },
                        new() { $"장 {slides.Count}개" });
            }
            case "read_slide":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id"));
                var d = ops.ReadSlide(n);
                return (new() { ["slide"] = d.Slide, ["slide_id"] = d.SlideId, ["layout"] = d.Layout, ["notes"] = d.Notes,
                        ["shapes"] = d.Shapes.Select(s => new Dictionary<string, object?> { ["shape_id"] = s.ShapeId, ["name"] = s.Name, ["type"] = s.Type, ["placeholder"] = s.Placeholder, ["text"] = s.Text, ["left"] = s.Left, ["top"] = s.Top, ["width"] = s.Width, ["height"] = s.Height }).ToList() },
                        new() { $"슬라이드 {d.Slide}(id {d.SlideId}) — 도형 {d.Shapes.Count}개" });
            }
            case "list_layouts":
            {
                var ls = ops.ListLayouts();
                return (new() { ["masters"] = new[] { new Dictionary<string, object?> { ["layouts"] = ls.Select(l => new Dictionary<string, object?> { ["layout"] = l.Name, ["placeholders"] = l.Placeholders }).ToList() } } },
                        new() { $"레이아웃 {ls.Count}개" });
            }
            case "add_slide":
            {
                var (n, id) = ops.AddSlide(a.Int("at"), a.Str("layout"), a.Str("title"), a.Str("body"));
                Mutated();
                var notes = TitleNotes(a.Str("title"));
                return (new() { ["slide"] = n, ["slide_id"] = id, ["notes"] = notes }, new() { $"슬라이드 {n}(id {id}) 를 만들었습니다" + (notes.Count > 0 ? " · " + string.Join(" · ", notes) : "") });
            }
            case "add_slides":
            {
                var rows = new List<Dictionary<string, object?>>();
                var lines = new List<string>();
                foreach (var s in a.Objects("slides"))
                {
                    var (n, id) = ops.AddSlide(null, s.Str("layout"), s.Str("title"), s.Str("body"));
                    var notes = TitleNotes(s.Str("title"));
                    rows.Add(new() { ["slide"] = n, ["slide_id"] = id, ["notes"] = notes });
                    lines.Add($"{n}\"{Clip(s.Str("title") ?? "", 30)}\"" + (notes.Count > 0 ? " " + string.Join(" ", notes) : ""));
                }
                if (rows.Count == 0) throw new HandError("만들 장이 하나도 안 왔습니다 — slides 가 비었습니다");
                Mutated();
                return (new() { ["slides"] = rows, ["made"] = rows.Count }, new() { $"장 {rows.Count}개를 만들었습니다 — " + string.Join(" · ", lines) });
            }
            case "delete_slide":
            {
                if (a.Int("slide") is null && a.Str("slide_id") is null) throw new HandError("어느 장을 지울지 slide 나 slide_id 로 정확히 말해 주세요");
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id"));
                ops.DeleteSlide(n); Mutated();
                return (new() { ["deleted"] = n }, new() { $"슬라이드 {n} 를 지웠습니다 — {n} 번 뒤의 번호는 하나씩 당겨졌습니다" });
            }
            case "reorder_slide":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id"));
                var to = a.Int("to") ?? throw new HandError("어디로 옮길지 to 가 없습니다");
                ops.MoveSlide(n, to); Mutated();
                return (new() { ["from"] = n, ["to"] = to }, new() { $"슬라이드 {n} 를 {to} 번으로 옮겼습니다" });
            }
            case "duplicate_slide":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id"));
                var (m, id) = ops.DuplicateSlide(n); Mutated();
                return (new() { ["slide"] = m, ["slide_id"] = id }, new() { $"슬라이드 {n} 를 복제해 {m} 번에 두었습니다(id {id})" });
            }
            case "set_text":
            {
                var text = a.Str("text") ?? throw new HandError("text 가 없습니다");
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id"));
                var (sid, before) = ops.SetText(n, a.Str("shape_id"), a.Str("placeholder"), text); Mutated();
                var notes = a.Str("placeholder") == "title" ? TitleNotes(text) : new List<string>();
                return (new() { ["slide"] = n, ["shape_id"] = sid, ["text"] = text, ["note"] = notes.FirstOrDefault() }, new() { $"슬라이드 {n} · 도형 {sid}: \"{Clip(before, 40)}\" → \"{Clip(text, 40)}\"" + (notes.Count > 0 ? " · " + notes[0] : "") });
            }
            case "read_notes":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id"));
                var t = ops.ReadNotes(n);
                // **365 판은 이 칸을 `notes` 로 부른다.** 같은 도구가 손마다 다른 이름을 쓰면, 모델이
                // 한쪽에서 배운 것이 다른 쪽에서 안 맞는다 — 2021 실측에서 그 화면을 봤다(2026-09-05:
                // 노트는 잘 적혔는데 읽는 쪽이 `notes` 를 찾다 「없다」로 읽었다).
                //
                // **옛 이름을 없애지는 않는다.** 이 손의 답을 `text` 로 읽는 것이 이미 있을 수 있고,
                // 칸 하나 더 싣는 값이 그것을 깨뜨리는 값보다 싸다.
                return (new() { ["slide"] = n, ["has_notes"] = t.Length > 0, ["notes"] = t.Length > 0 ? t : null, ["text"] = t }, new() { t.Length > 0 ? $"슬라이드 {n} 노트 {t.Length}자" : $"슬라이드 {n} 노트 없음" });
            }
            case "set_notes":
            {
                var text = a.Str("text") ?? a.Str("notes") ?? throw new HandError("text 가 없습니다");
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id"));
                ops.SetNotes(n, text); Mutated();
                return (new() { ["slide"] = n, ["text"] = text }, new() { $"슬라이드 {n} 노트를 적었습니다({text.Length}자)" });
            }
            case "render_slide":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id"));
                var w = Math.Clamp(a.Int("max_width") ?? 1024, 160, 4096);
                var r = ops.RenderSlide(n, w);
                return (new() { ["slide"] = n, ["image_base64"] = r.Base64Png, ["image_mime"] = "image/png", ["image_bytes"] = r.Bytes, ["max_width"] = w },
                        new() { $"슬라이드 {n} 를 {r.Width}×{r.Height} 으로 떴습니다({r.Bytes} 바이트)" });
            }
            case "add_shape":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id"));
                var kind = a.Str("kind") ?? "rectangle";
                var id = ops.AddShape(n, kind, a.Num("left") ?? 40, a.Num("top") ?? 40, a.Num("width") ?? 200, a.Num("height") ?? 80, a.Str("text"), a.Str("fill"), a.Num("size"), a.Str("color"), a.Bool("bold") ?? false);
                Mutated();
                return (new() { ["slide"] = n, ["shape_id"] = id, ["kind"] = kind }, new() { $"슬라이드 {n}: {kind} 도형 {id} 추가" });
            }
            case "delete_shape":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id"));
                var id = a.Str("shape_id") ?? throw new HandError("shape_id 가 없습니다");
                ops.DeleteShape(n, id); Mutated();
                return (new() { ["slide"] = n, ["deleted"] = id }, new() { $"슬라이드 {n}: 도형 {id} 를 지웠습니다" });
            }
            case "apply_style":
            {
                var t = a.Object("title"); var b = a.Object("body");
                var n = ops.ApplyStyle(t?.Str("font"), t?.Num("size"), t?.Str("color"), t?.Bool("bold"), b?.Str("font"), b?.Num("size"), b?.Str("color"), a.Str("ea_font"));
                Mutated();
                return (new() { ["styled"] = n }, new() { $"장 {n}개의 제목·본문 서식을 맞췄습니다" + (a.Str("ea_font") is not null ? $" · 한글 글꼴 {a.Str("ea_font")}" : "") });
            }
            default:
                return Shapes(op, a) ?? Tables(op, a) ?? Deck(op, a) ?? Memory(op, a)
                    ?? throw new HandError($"이 손(COM, Office 2021)은 {op} 를 모릅니다 — 아는 것: {string.Join(", ", Known)}");
        }
    }

    private void Mutated() => count++;

    /// <summary>JS 손의 fitNote 와 같은 자: 44pt·폭 828pt 기준으로 한글 약 18자를 넘는 제목은 접힌다.</summary>
    internal static List<string> TitleNotes(string? title)
    {
        var notes = new List<string>();
        if (string.IsNullOrEmpty(title)) return notes;
        var chars = title.EnumerateRunes().Count();
        if (chars > 18) notes.Add($"⚠ 제목이 2줄로 접힐 수 있습니다({chars}자) — 이 자리는 한글 약 18자까지 한 줄입니다");
        return notes;
    }

    private static string Clip(string s, int n) => s.Length <= n ? s : s[..n] + "…";
}

public sealed class HandError : Exception { public HandError(string m) : base(m) { } }

/// <summary>call.args 를 읽는 작은 자. 없는 키는 null, 틀린 형은 관대하게(문자열 숫자도 숫자로).</summary>
public sealed class Args
{
    private readonly Dictionary<string, JsonElement> d;
    public Args(Dictionary<string, JsonElement>? d) { this.d = d ?? new(); }
    public string? Str(string k) => d.TryGetValue(k, out var v) && v.ValueKind != JsonValueKind.Null ? (v.ValueKind == JsonValueKind.String ? v.GetString() : v.ToString()) : null;
    public int? Int(string k) => Num(k) is double x ? (int)Math.Round(x) : null;
    public double? Num(string k)
    {
        if (!d.TryGetValue(k, out var v)) return null;
        if (v.ValueKind == JsonValueKind.Number) return v.GetDouble();
        if (v.ValueKind == JsonValueKind.String && double.TryParse(v.GetString(), out var x)) return x;
        return null;
    }
    public bool? Bool(string k) => d.TryGetValue(k, out var v) ? v.ValueKind switch { JsonValueKind.True => true, JsonValueKind.False => false, JsonValueKind.String => v.GetString() is "true" or "1", _ => null } : null;
    public Args? Object(string k) => d.TryGetValue(k, out var v) && v.ValueKind == JsonValueKind.Object ? new Args(v.EnumerateObject().ToDictionary(p => p.Name, p => p.Value)) : null;
    public bool Has(string k) => d.TryGetValue(k, out var v) && v.ValueKind != JsonValueKind.Null;
    public JsonElement? Raw(string k) => d.TryGetValue(k, out var v) ? v : null;
    public IEnumerable<string> Keys => d.Keys;
    public List<string> Strings(string k) => Elements(k).Select(e => e.ValueKind == JsonValueKind.String ? e.GetString() ?? "" : e.ToString()).ToList();
    public List<double> Numbers(string k) => Elements(k).Select(e => e.ValueKind == JsonValueKind.Number ? e.GetDouble() : double.TryParse(e.ToString(), out var x) ? x : throw new HandError($"{k} 에 숫자가 아닌 값이 있습니다: {e}")).ToList();
    public List<int> Ints(string k) => Numbers(k).Select(x => (int)Math.Round(x)).ToList();
    /// <summary>줄 단위 배열의 배열(표의 values).</summary>
    public List<List<string>> Rows(string k) => Elements(k).Select(r => r.ValueKind == JsonValueKind.Array ? r.EnumerateArray().Select(e => e.ValueKind == JsonValueKind.String ? e.GetString() ?? "" : e.ValueKind == JsonValueKind.Null ? "" : e.ToString()).ToList() : throw new HandError($"{k} 는 줄마다 배열이어야 합니다")).ToList();
    private IEnumerable<JsonElement> Elements(string k) { if (d.TryGetValue(k, out var v) && v.ValueKind == JsonValueKind.Array) foreach (var e in v.EnumerateArray()) yield return e; }
    public IEnumerable<Args> Objects(string k)
    {
        if (!d.TryGetValue(k, out var v) || v.ValueKind != JsonValueKind.Array) yield break;
        foreach (var e in v.EnumerateArray()) if (e.ValueKind == JsonValueKind.Object) yield return new Args(e.EnumerateObject().ToDictionary(p => p.Name, p => p.Value));
    }
}
