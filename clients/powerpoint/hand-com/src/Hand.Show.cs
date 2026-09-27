namespace Magi.Ppt.Hand;

/// <summary>
/// 발표 설정 도구: 화면 전환·장 숨기기·슬라이드 크기·구역·PDF. Office.js 에는 없고 COM 에는 있는 것들이라 **이 손만 한다** —
/// 헬퍼는 작업창이 손인 호스트(365·Mac)에서는 이 도구들을 모델에게 안 보인다(hostcaps.go pptComHandTools).
/// </summary>
public sealed partial class Hand
{
    /// <summary>화면 전환 이름 — InteropOps.TransitionMap 의 열쇠와 같아야 한다(시험이 대조한다).</summary>
    public static readonly string[] TransitionEffects = {
        "none", "cut", "fade", "push", "wipe", "split", "reveal", "cover", "uncover", "random_bars", "shape", "zoom", "dissolve", "flash",
        "blinds", "checkerboard", "clock", "vortex", "ripple", "honeycomb", "glitter", "shred", "switch", "flip", "gallery", "cube",
        "doors", "box", "orbit", "pan", "ferris_wheel", "conveyor", "rotate", "window", "random",
    };

    /// <summary>슬라이드 크기의 이름 — 포인트. 16:9 는 PowerPoint 2013 이후의 기본(13.333×7.5in).</summary>
    private static readonly Dictionary<string, (double W, double H)> SlideSizes = new(StringComparer.OrdinalIgnoreCase)
    {
        ["16:9"] = (960, 540), ["widescreen"] = (960, 540), ["와이드"] = (960, 540),
        ["4:3"] = (720, 540), ["standard"] = (720, 540), ["표준"] = (720, 540),
        ["16:10"] = (720, 450), ["a4"] = (780, 540), ["letter"] = (720, 540),
    };

    private (Dictionary<string, object?>, List<string>)? Show(string op, Args a)
    {
        switch (op)
        {
            case "set_transition":
            {
                var all = a.Bool("all") == true;
                if (all && (a.Has("slide") || a.Has("slide_id"))) throw new HandError("all 과 slide/slide_id 를 같이 주면 어느 쪽인지 모릅니다 — 하나만 주세요");
                var effect = a.Str("effect")?.Trim().ToLowerInvariant();
                if (effect is not null && !TransitionEffects.Contains(effect)) throw new HandError($"effect 는 {string.Join(", ", TransitionEffects)} 중 하나입니다 — '{effect}'");
                var duration = a.Num("duration");
                if (duration is double d && (d < 0.01 || d > 60)) throw new HandError($"duration 은 초 단위 0.01–60 입니다 — {d}");
                var after = a.Num("advance_after");
                if (after is double t && (t < 0 || t > 3600)) throw new HandError($"advance_after 는 초 단위 0–3600 입니다 — {t}");
                if (effect is null && duration is null && after is null && !a.Has("on_click")) throw new HandError("바꿀 것이 없습니다 — effect, duration, on_click, advance_after 중 하나는 주세요");
                var onClick = a.Bool("on_click");
                if (onClick == false && after is null) throw new HandError("on_click 을 끄면 advance_after(초)가 있어야 합니다 — 안 그러면 그 장에서 발표가 멈춥니다");
                var targets = all ? Enumerable.Range(1, ops.ListSlides().Count).ToList() : new List<int> { ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")) };
                foreach (var n in targets)
                {
                    var was = ops.ReadTransition(n);
                    ops.SetTransition(n, new Transition(effect ?? was.Effect, duration ?? was.Duration, onClick ?? was.OnClick, a.Has("advance_after") ? after : was.AdvanceAfter));
                }
                Mutated();
                var now = ops.ReadTransition(targets[0]);
                var where = all ? $"장 {targets.Count}개 전부" : $"슬라이드 {targets[0]}";
                var how = new List<string> { now.Effect == "none" ? "전환 없음" : $"{now.Effect} {now.Duration:0.##}초" };
                how.Add(now.AdvanceAfter is double s ? $"{s:0.##}초 뒤 자동으로 넘김{(now.OnClick ? "(클릭도 됨)" : "")}" : "클릭으로 넘김");
                return (new() { ["slides"] = targets, ["effect"] = now.Effect, ["duration"] = now.Duration, ["on_click"] = now.OnClick, ["advance_after"] = now.AdvanceAfter },
                        new() { $"{where}: 화면 전환 — {string.Join(", ", how)}" });
            }
            case "hide_slide":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var hide = a.Bool("hidden") ?? true;
                var was = ops.IsHidden(n); ops.SetHidden(n, hide); if (was != hide) Mutated();
                var id = ops.ListSlides()[n - 1].SlideId;
                return (new() { ["slide"] = n, ["slide_id"] = id, ["hidden"] = hide },
                        new() { was == hide ? $"슬라이드 {n} 은 이미 {(hide ? "숨겨져" : "보여")} 있습니다 — 안 바꿨습니다" : hide ? $"슬라이드 {n} 을 숨겼습니다 — 발표 때 건너뜁니다(편집 화면에는 남습니다)" : $"슬라이드 {n} 을 다시 보이게 했습니다" });
            }
            case "set_slide_size":
            {
                var was = ops.SlideSize();
                double w, h; var size = a.Str("size")?.Trim();
                if (size is not null)
                {
                    if (!SlideSizes.TryGetValue(size, out var s)) throw new HandError($"size 는 16:9, 4:3, 16:10, a4, letter 중 하나입니다 — '{size}'. 다른 크기는 width·height(포인트)로 주세요");
                    if (a.Has("width") || a.Has("height")) throw new HandError("size 와 width·height 를 같이 주면 어느 쪽인지 모릅니다 — 하나만 주세요");
                    (w, h) = s;
                }
                else
                {
                    w = a.Num("width") ?? was.Width; h = a.Num("height") ?? was.Height;
                    if (!a.Has("width") && !a.Has("height")) throw new HandError("size(16:9·4:3·16:10·a4·letter) 나 width·height(포인트) 를 주세요");
                    if (w < 72 || h < 72 || w > 4032 || h > 4032) throw new HandError($"슬라이드 크기는 72–4032pt(1–56인치)입니다 — {w}×{h}");
                }
                if (Math.Abs(w - was.Width) < 0.5 && Math.Abs(h - was.Height) < 0.5)
                    return (new() { ["width"] = was.Width, ["height"] = was.Height }, new() { $"이미 {was.Width:0.#}×{was.Height:0.#}pt 입니다 — 안 바꿨습니다" });
                ops.SetSlideSize(w, h); Mutated(); var now = ops.SlideSize();
                return (new() { ["width"] = now.Width, ["height"] = now.Height, ["was"] = new Dictionary<string, object?> { ["width"] = was.Width, ["height"] = was.Height } },
                        new() { $"슬라이드 크기 {was.Width:0.#}×{was.Height:0.#} → {now.Width:0.#}×{now.Height:0.#}pt", "⚠ 도형이 새 크기에 맞게 옮겨지고 늘거나 줄었습니다 — render_slide 로 한 장 확인하세요" });
            }
            case "add_section":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var name = a.Str("name")?.Trim();
                if (string.IsNullOrEmpty(name)) throw new HandError("name 이 없습니다 — 구역 이름");
                var before = ops.Sections().Count; var made = ops.AddSection(n, name); Mutated();
                var list = ops.Sections(); var extra = made && before == 0 && list.Count > 1;
                var lines = new List<string> { made ? $"슬라이드 {n} 부터 구역 「{name}」 을 시작했습니다" : $"슬라이드 {n} 에서 시작하는 구역의 이름을 「{name}」 으로 바꿨습니다" };
                if (extra) lines.Add($"앞의 장들은 PowerPoint 가 만든 구역 「{list[0].Name}」 에 들어갔습니다");
                return (new() { ["slide"] = n, ["created"] = made, ["sections"] = SectionRows(list) }, lines);
            }
            case "remove_section":
            {
                var list = ops.Sections(); var name = a.Str("name")?.Trim();
                if (string.IsNullOrEmpty(name)) throw new HandError("name 이 없습니다 — 지울 구역 이름(list_slides 의 sections)");
                var hits = list.Select((s, i) => (s, i)).Where(x => x.s.Name == name).ToList();
                if (hits.Count == 0) throw new HandError($"구역 「{name}」 이 없습니다 — 있는 구역: {(list.Count == 0 ? "없음" : string.Join(", ", list.Select(s => $"「{s.Name}」")))}");
                if (hits.Count > 1) throw new HandError($"「{name}」 이라는 구역이 {hits.Count}개라 어느 것인지 모릅니다 — add_section 으로 이름을 먼저 가르세요");
                // 첫 구역은 뒤에 구역이 있으면 PowerPoint 가 못 지운다(장이 붙을 앞 구역이 없다) — COM 은 「Illegal value」 한 줄만 준다(실물 2021, 2026-09-27).
                if (hits[0].i == 0 && list.Count > 1)
                    throw new HandError($"첫 구역 「{name}」 은 뒤에 구역이 있는 동안 지울 수 없습니다 — 그 장들이 붙을 앞 구역이 없습니다. 뒤 구역들을 먼저 지우거나, add_section 으로 이름을 바꾸세요");
                ops.DeleteSection(hits[0].i + 1); Mutated();
                return (new() { ["removed"] = name, ["sections"] = SectionRows(ops.Sections()) },
                        new() { hits[0].i == 0 ? $"구역 「{name}」 을 지웠습니다 — 마지막 구역이라 이제 덱에 구역이 없습니다(장은 그대로)" : $"구역 「{name}」 을 지웠습니다 — 그 안의 장 {hits[0].s.Count}개는 그대로 앞 구역에 붙었습니다" });
            }
            case "export_pdf":
            {
                var path = a.Str("path")?.Trim();
                if (string.IsNullOrEmpty(path))
                {
                    // 저장 안 한 덱은 사람의 「문서」 폴더에 덱 이름으로 — Word·Excel 의 export_pdf 와 같은 규칙(헬퍼 com_local.go pdfTarget).
                    var name = string.Concat(System.IO.Path.GetFileNameWithoutExtension(ops.Label).Select(c => System.IO.Path.GetInvalidFileNameChars().Contains(c) ? '_' : c)).Trim();
                    path = ops.FilePath is string from ? System.IO.Path.ChangeExtension(from, ".pdf")
                        : System.IO.Path.Combine(DocumentsDir(), (name.Length == 0 ? "프레젠테이션" : name) + ".pdf");
                }
                if (!System.IO.Path.IsPathFullyQualified(path)) throw new HandError($"path 는 전체 경로여야 합니다 — '{path}'");
                if (!path.EndsWith(".pdf", StringComparison.OrdinalIgnoreCase)) throw new HandError($"path 는 .pdf 로 끝나야 합니다 — '{path}'");
                var dir = System.IO.Path.GetDirectoryName(path);
                if (dir is null || !System.IO.Directory.Exists(dir)) throw new HandError($"폴더가 없습니다 — '{dir}'");
                if (System.IO.File.Exists(path) && a.Bool("overwrite") != true) throw new HandError($"이미 있는 파일입니다 — '{path}'. 덮어쓰려면 overwrite: true");
                var hidden = Enumerable.Range(1, ops.ListSlides().Count).Count(ops.IsHidden);
                ops.ExportPdf(path);
                var bytes = System.IO.File.Exists(path) ? new System.IO.FileInfo(path).Length : 0;
                var lines = new List<string> { $"PDF 로 내보냈습니다 — {path}" + (bytes > 0 ? $" ({bytes / 1024.0:0.#} KB)" : "") };
                if (hidden > 0) lines.Add($"숨긴 장 {hidden}개는 PDF 에 안 들어갔습니다");
                return (new() { ["path"] = path, ["bytes"] = bytes, ["hidden_skipped"] = hidden }, lines);
            }
        }
        return null;
    }

    /// <summary>저장 안 한 덱의 PDF 가 갈 자리. 시험이 바꿔 끼운다.</summary>
    internal static Func<string> DocumentsDir = () => Environment.GetFolderPath(Environment.SpecialFolder.MyDocuments);

    private static List<Dictionary<string, object?>> SectionRows(IReadOnlyList<SectionInfo> list) =>
        list.Select(s => new Dictionary<string, object?> { ["name"] = s.Name, ["first_slide"] = s.First == 0 ? null : s.First, ["slides"] = s.Count }).ToList();
}
