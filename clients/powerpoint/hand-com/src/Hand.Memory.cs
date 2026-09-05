using System.Text.Json;

namespace Magi.Ppt.Hand;

/// <summary>
/// 덱 안의 메모·제안·애니메이션과 창 안내 아홉 도구. 제안은 365 판과 같은 태그 규약(MAGI.FIX.* 키, JSON 값)으로
/// 덱 파일에 남는다 — 같은 덱을 두 판이 번갈아 열어도 서로 읽는다(addin/src/domain/Suggestion.js).
/// </summary>
public sealed partial class Hand
{
    internal const string FixPrefix = "MAGI.FIX.";
    private static readonly string[] Fixable = { "set_text", "format_shape", "move_shape", "align_shapes", "delete_shape", "set_notes", "set_hyperlink" };
    private static readonly string[] Effects = { "appear", "fade", "wipe", "zoom" };
    private static readonly string[] Starts = { "on_click", "with_previous", "after_previous" };

    private static List<Dictionary<string, object?>> Pairs(IReadOnlyDictionary<string, string> tags) => tags.Select(kv => new Dictionary<string, object?> { ["key"] = kv.Key, ["value"] = kv.Value }).ToList();
    private static Dictionary<string, object?> DecodeFix(string key, string value, int slide, string slideId, string? shapeId)
    {
        var row = new Dictionary<string, object?> { ["key"] = key, ["slide"] = slide, ["slide_id"] = slideId, ["shape_id"] = shapeId };
        try
        {
            var doc = JsonDocument.Parse(value).RootElement;
            var what = doc.TryGetProperty("what", out var w) && w.ValueKind == JsonValueKind.String ? w.GetString()!.Trim() : "";
            if (what.Length == 0) throw new JsonException();
            row["what"] = what; row["why"] = doc.TryGetProperty("why", out var y) && y.ValueKind == JsonValueKind.String ? y.GetString() : "";
            row["fix"] = doc.TryGetProperty("fix", out var f) && f.ValueKind == JsonValueKind.Object && f.TryGetProperty("tool", out _) ? JsonSerializer.Deserialize<Dictionary<string, object?>>(f.GetRawText()) : null;
            row["broken"] = false;
        }
        catch (JsonException) { row["what"] = "읽을 수 없는 제안입니다"; row["why"] = ""; row["fix"] = null; row["broken"] = true; }
        return row;
    }

    private (Dictionary<string, object?>, List<string>)? Memory(string op, Args a)
    {
        switch (op)
        {
            case "read_tags":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var sid = ops.ListSlides()[n - 1].SlideId;
                if (a.Str("shape_id") is string id) { ShapeOn(n, id); var one = ops.ReadTags(n, id); return (new() { ["slide"] = n, ["slide_id"] = sid, ["shape_id"] = id, ["tags"] = Pairs(one) }, new() { $"도형 {id} 의 메모 {one.Count}개" }); }
                var mine = ops.ReadTags(n, null);
                var onShapes = ops.ReadSlide(n).Shapes.Select(s => (s.ShapeId, Tags: ops.ReadTags(n, s.ShapeId))).Where(x => x.Tags.Count > 0).Select(x => new Dictionary<string, object?> { ["shape_id"] = x.ShapeId, ["tags"] = Pairs(x.Tags) }).ToList();
                return (new() { ["slide"] = n, ["slide_id"] = sid, ["tags"] = Pairs(mine), ["shapes"] = onShapes }, new() { $"슬라이드 {n} 의 메모 {mine.Count}개 · 메모 있는 도형 {onShapes.Count}개" });
            }
            case "set_tag":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var key = a.Str("key")?.Trim(); if (string.IsNullOrEmpty(key)) throw new HandError("key 가 없습니다");
                if (a.Str("shape_id") is string sid0) ShapeOn(n, sid0);
                var value = a.Str("value"); var had = ops.ReadTags(n, a.Str("shape_id")).Keys.Any(k => string.Equals(k, key, StringComparison.OrdinalIgnoreCase));
                var stored = ops.SetTag(n, a.Str("shape_id"), key, value); Mutated();
                var where = a.Str("shape_id") is string s ? $"도형 {s}" : $"슬라이드 {n}";
                var line = value is null ? $"{where} 의 메모 {key.ToUpperInvariant()} 를 지웠습니다" + (had ? "" : " — 그런 이름의 메모가 원래 없었습니다") : $"{where} 에 메모를 붙였습니다 — {stored}" + (stored != key ? $" (PowerPoint 가 '{key}' 를 '{stored}' 로 바꿔 저장했습니다 — 다음에 찾을 때는 이 이름으로)" : "");
                return (new() { ["slide"] = n, ["shape_id"] = a.Str("shape_id"), ["key"] = stored ?? key.ToUpperInvariant(), ["asked"] = key, ["removed"] = value is null && had }, new() { line });
            }
            case "read_animation":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var r = ops.ReadAnimation(n);
                var steps = r.Steps.Select(s => new Dictionary<string, object?> { ["shape_id"] = s.ShapeId, ["effect"] = s.Effect, ["start"] = s.Start, ["duration_ms"] = s.DurationMs, ["paragraphs"] = s.EachParagraph ? "each" : "all" }).ToList();
                return (new() { ["slide"] = n, ["has_animation"] = steps.Count > 0 || r.Unreadable > 0, ["steps"] = steps, ["unreadable"] = r.Unreadable, ["all_known"] = r.Unreadable == 0, ["effects_known"] = Effects },
                        new() { steps.Count == 0 && r.Unreadable == 0 ? $"슬라이드 {n}: 애니메이션 없음" : $"슬라이드 {n}: 걸음 {steps.Count}개" + (r.Unreadable > 0 ? $" · 못 읽는 효과 {r.Unreadable}개(덮어쓰면 사라집니다)" : "") });
            }
            case "animate_slide":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id"));
                if (!a.Has("steps")) throw new HandError("steps 가 없습니다 — 빈 배열이면 전부 지웁니다");
                var steps = new List<AnimStep>();
                foreach (var s in a.Objects("steps"))
                {
                    var id = s.Str("shape_id") ?? throw new HandError("steps 의 항목마다 shape_id 가 있어야 합니다"); ShapeOn(n, id);
                    var effect = (s.Str("effect") ?? "fade").ToLowerInvariant(); if (!Effects.Contains(effect)) throw new HandError($"effect 는 {string.Join(", ", Effects)} 중 하나입니다 — '{effect}'. 나가기·강조·이동 경로는 이 손이 안 합니다");
                    var start = (s.Str("start") ?? "on_click").ToLowerInvariant(); if (!Starts.Contains(start)) throw new HandError($"start 는 {string.Join(", ", Starts)} 중 하나입니다 — '{start}'");
                    steps.Add(new AnimStep(id, effect, start, Math.Max(1, s.Int("duration_ms") ?? 500), string.Equals(s.Str("paragraphs"), "each", StringComparison.OrdinalIgnoreCase)));
                }
                var was = ops.ReadAnimation(n); ops.SetAnimation(n, steps); Mutated();
                var clicks = steps.Count(s => s.Start == "on_click");
                var lines = new List<string> { steps.Count == 0 ? $"슬라이드 {n} 의 애니메이션을 전부 지웠습니다({was.Steps.Count + was.Unreadable}개)" : $"슬라이드 {n}: 걸음 {steps.Count}개 · 클릭 {clicks}번" + (was.Steps.Count + was.Unreadable > 0 ? $" — 있던 효과 {was.Steps.Count + was.Unreadable}개는 지웠습니다" : "") };
                if (was.Unreadable > 0) lines.Add($"⚠ 이 손이 못 읽던 효과 {was.Unreadable}개가 함께 사라졌습니다 — 되살릴 수 없습니다");
                return (new() { ["slide"] = n, ["steps"] = steps.Count, ["clicks"] = clicks, ["removed"] = was.Steps.Count + was.Unreadable }, lines);
            }
            case "suggest":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var what = a.Str("what")?.Trim(); if (string.IsNullOrEmpty(what)) throw new HandError("무엇을 고치자는 말이 없습니다 — what 을 주세요");
                var fix = a.Object("fix"); string? tool = fix?.Str("tool");
                if (fix is not null && tool is null) throw new HandError("fix 에 tool 이 없습니다 — {tool, args}");
                if (tool is not null && !Fixable.Contains(tool)) throw new HandError($"제안으로 누를 수 있는 손이 아닙니다 — '{tool}'. 누를 수 있는 것: {string.Join(", ", Fixable)}");
                if (a.Str("shape_id") is string sid0) ShapeOn(n, sid0);
                var taken = ops.ReadTags(n, a.Str("shape_id")).Keys.ToHashSet(StringComparer.OrdinalIgnoreCase);
                var seed = FixPrefix + Base36(DateTimeOffset.UtcNow.ToUnixTimeMilliseconds()) + Base36(Random.Shared.Next(1_000_000));
                var key = seed; for (var k = 1; taken.Contains(key); k++) key = $"{seed}-{k}";
                var body = new Dictionary<string, object?> { ["what"] = what }; if (a.Str("why") is string why) body["why"] = why.Trim();
                if (tool is not null) body["fix"] = new Dictionary<string, object?> { ["tool"] = tool, ["args"] = fix!.Raw("args") is JsonElement e && e.ValueKind == JsonValueKind.Object ? JsonSerializer.Deserialize<Dictionary<string, object?>>(e.GetRawText()) : new Dictionary<string, object?>() };
                var stored = ops.SetTag(n, a.Str("shape_id"), key, JsonSerializer.Serialize(body)); Mutated();
                var where = a.Str("shape_id") is string s ? $"도형 {s}" : $"슬라이드 {n}";
                return (new() { ["slide"] = n, ["shape_id"] = a.Str("shape_id"), ["suggestion"] = stored, ["fixable"] = tool is not null },
                        new() { $"{where} 에 제안을 붙였습니다 — {what}. **이건 아직 안 고친 것입니다** — 덱 파일에 메모로만 남고, 카드는 365 작업창에서 보입니다(이 손은 창이 없습니다)" });
            }
            case "read_suggestions":
            {
                int? one = a.Has("slide") || a.Has("slide_id") ? ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")) : null;
                var rows = new List<Dictionary<string, object?>>();
                foreach (var s in ops.ListSlides())
                {
                    if (one is int o && s.Slide != o) continue;
                    foreach (var kv in ops.ReadTags(s.Slide, null)) if (kv.Key.StartsWith(FixPrefix, StringComparison.OrdinalIgnoreCase)) rows.Add(DecodeFix(kv.Key, kv.Value, s.Slide, s.SlideId, null));
                    foreach (var sh in ops.ReadSlide(s.Slide).Shapes) foreach (var kv in ops.ReadTags(s.Slide, sh.ShapeId)) if (kv.Key.StartsWith(FixPrefix, StringComparison.OrdinalIgnoreCase)) rows.Add(DecodeFix(kv.Key, kv.Value, s.Slide, s.SlideId, sh.ShapeId));
                }
                return (new() { ["scope"] = one, ["count"] = rows.Count, ["suggestions"] = rows }, new() { one is int ? $"슬라이드 {one} 의 제안 {rows.Count}개" : $"덱 전체의 제안 {rows.Count}개" });
            }
            case "drop_suggestion":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var key = a.Str("key")?.Trim() ?? throw new HandError("key 가 없습니다 — read_suggestions 의 key");
                if (!key.StartsWith(FixPrefix, StringComparison.OrdinalIgnoreCase)) throw new HandError($"제안의 키가 아닙니다 — '{key}'. set_tag 로 남긴 메모는 set_tag 로 지우세요");
                if (a.Str("shape_id") is string sid0) ShapeOn(n, sid0);
                var had = ops.ReadTags(n, a.Str("shape_id")).Keys.Any(k => string.Equals(k, key, StringComparison.OrdinalIgnoreCase));
                if (!had) throw new HandError($"그런 제안이 없습니다 — {key}" + (a.Str("shape_id") is null ? ". 도형에 붙은 것이면 shape_id 를 주세요" : ""));
                ops.SetTag(n, a.Str("shape_id"), key, null); Mutated();
                return (new() { ["slide"] = n, ["dropped"] = key.ToUpperInvariant() }, new() { $"제안 {key.ToUpperInvariant()} 을 뗐습니다 — 고치지는 않았습니다" });
            }
            case "advise":
            case "clear_advice":
            {
                var items = op == "advise" ? a.Objects("items").Count() : 0;
                if (op == "advise" && items == 0) throw new HandError("items 가 비었습니다 — [{message, why, slide_id?, shape_ids?}]");
                return (new() { ["pinned"] = items, ["shown"] = false, ["note"] = "이 손(COM, 2021)은 작업창이 없어 안내를 꽂을 자리가 없습니다 — 안내는 답글로만 전합니다" }, new());
            }
        }
        return null;
    }
    private static string Base36(long v) { const string d = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"; var s = ""; do { s = d[(int)(v % 36)] + s; v /= 36; } while (v > 0); return s; }
}
