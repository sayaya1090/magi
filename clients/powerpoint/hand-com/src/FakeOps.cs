using System.Text.Json;

namespace Magi.Ppt.Hand;

/// <summary>
/// 메모리 덱. 시험과 mac 개발용 — PowerPoint 없이 손의 규약을 잰다. 도구 48개가 전부 여기 위에서 돌아야
/// Hand 의 판단(정렬 계산·⚠·문구)을 PowerPoint 없이 잴 수 있다. 실물이 못 하는 것을 여기서 되게 만들지
/// 않는다 — 여기서 되고 COM 에서 안 되면 그 차이가 거짓 초록이다.
/// </summary>
public sealed class FakeOps : IOps
{
    internal sealed class Shape
    {
        public int Id; public string Name = ""; public string Type = "Placeholder"; public string? Placeholder; public string Text = "";
        public double L, T, W = 828, H = 60; public string? Font; public double? Size; public string? Color; public bool? Bold;
        public string? Fill; public string? Url; public double Rotation; public bool Visible = true; public string? AltText;
        public Dictionary<string, string> Tags = new(StringComparer.OrdinalIgnoreCase);
        public List<List<string>>? Cells;           // 표일 때
        public List<int>? Members;                  // 그룹일 때
        public string? Path;                        // 그림일 때
        public Dictionary<string, object?> Runs = new(); // format_text 가 남긴 자국(시험용)
    }
    internal sealed class Slide
    {
        public int Id; public string Layout = "제목 및 내용"; public List<Shape> Shapes = new(); public string Notes = "";
        public Dictionary<string, string> Tags = new(StringComparer.OrdinalIgnoreCase);
        public List<AnimStep> Anim = new(); public BackgroundSpec? Background;
        public Dictionary<string, string> Theme = new(DefaultTheme, StringComparer.OrdinalIgnoreCase);
    }
    internal static readonly Dictionary<string, string> DefaultTheme = new(StringComparer.OrdinalIgnoreCase)
    {
        ["dark1"] = "#000000", ["light1"] = "#FFFFFF", ["dark2"] = "#44546A", ["light2"] = "#E7E6E6", ["accent1"] = "#4472C4", ["accent2"] = "#ED7D31",
        ["accent3"] = "#A5A5A5", ["accent4"] = "#FFC000", ["accent5"] = "#5B9BD5", ["accent6"] = "#70AD47", ["hyperlink"] = "#0563C1", ["followedHyperlink"] = "#954F72",
    };
    private readonly List<Slide> slides = new();
    private readonly Dictionary<string, string> snapshots = new(); // id → 장을 JSON 으로 얼린 것
    private readonly Dictionary<string, string> masterTheme = new(DefaultTheme, StringComparer.OrdinalIgnoreCase);
    private int nextSlide = 256, nextShape = 1, nextSnap = 1;
    public string DocumentKey { get; }
    public string Label { get; }
    public FakeOps(string key = "fake-deck", string label = "fake.pptx") { DocumentKey = key; Label = label; AddSlide(null, "제목 슬라이드", null, null); }

    // ── 장 ──
    public IReadOnlyList<SlideInfo> ListSlides() => slides.Select((s, i) => new SlideInfo(i + 1, s.Id.ToString(), s.Layout, s.Shapes.Count, s.Shapes.FirstOrDefault(x => Role(x.Placeholder) == "title")?.Text ?? "")).ToList();
    public SlideDetail ReadSlide(int n) { var s = At(n); return new SlideDetail(n, s.Id.ToString(), s.Layout, s.Shapes.Select(Info).ToList(), s.Notes); }
    private static ShapeInfo Info(Shape x) => new(x.Id.ToString(), x.Name, x.Type, x.Placeholder, x.Text, x.L, x.T, x.W, x.H,
        x.Text.Length > 0 ? x.Font ?? "맑은 고딕" : null, x.Text.Length > 0 ? x.Size ?? (Role(x.Placeholder) == "title" ? 44 : 18) : null, x.Text.Length > 0 ? x.Color ?? "#000000" : null, x.Text.Length > 0 ? x.Bold ?? false : null);
    public IReadOnlyList<LayoutInfo> ListLayouts() => new[] { new LayoutInfo("제목 슬라이드", new[] { "CenterTitle", "Subtitle" }), new LayoutInfo("제목 및 내용", new[] { "Title", "Body" }), new LayoutInfo("제목만", new[] { "Title" }), new LayoutInfo("빈 화면", Array.Empty<string>()) };
    public (int, string) AddSlide(int? at, string? layout, string? title, string? body)
    {
        var s = new Slide { Id = nextSlide++, Layout = layout ?? (body is null ? "제목만" : "제목 및 내용") };
        var lay = ListLayouts().FirstOrDefault(l => l.Name == s.Layout) ?? throw new HandError($"이 덱에 '{s.Layout}' 레이아웃이 없습니다 — list_layouts 로 이름을 보세요");
        foreach (var p in lay.Placeholders) s.Shapes.Add(new Shape { Id = nextShape++, Name = p, Placeholder = p, T = p.Contains("Title") ? 40 : 140, L = 66, H = p.Contains("Title") ? 60 : 300 });
        if (title is not null) (s.Shapes.FirstOrDefault(x => x.Placeholder is "Title" or "CenterTitle") ?? throw new HandError("title 자리가 없는 레이아웃입니다")).Text = title;
        if (body is not null) (s.Shapes.FirstOrDefault(x => x.Placeholder is "Body" or "Subtitle") ?? throw new HandError("body 자리가 없는 레이아웃입니다")).Text = body;
        var idx = at is int i && i >= 1 && i <= slides.Count + 1 ? i - 1 : slides.Count;
        slides.Insert(idx, s);
        return (idx + 1, s.Id.ToString());
    }
    public void DeleteSlide(int n) { if (slides.Count == 1) throw new HandError("마지막 장은 지울 수 없습니다"); slides.RemoveAt(n - 1); }
    public void MoveSlide(int n, int to) { var s = At(n); slides.RemoveAt(n - 1); slides.Insert(Math.Clamp(to, 1, slides.Count + 1) - 1, s); }
    public (int, string) DuplicateSlide(int n) { var c = Clone(At(n)); c.Id = nextSlide++; slides.Insert(n, c); return (n + 1, c.Id.ToString()); }
    private Slide Clone(Slide s)
    {
        var c = new Slide { Layout = s.Layout, Notes = s.Notes, Background = s.Background, Anim = new(s.Anim), Theme = new(s.Theme, StringComparer.OrdinalIgnoreCase) };
        foreach (var (k, v) in s.Tags) c.Tags[k] = v;
        foreach (var x in s.Shapes) c.Shapes.Add(new Shape { Id = nextShape++, Name = x.Name, Type = x.Type, Placeholder = x.Placeholder, Text = x.Text, L = x.L, T = x.T, W = x.W, H = x.H, Font = x.Font, Size = x.Size, Color = x.Color, Bold = x.Bold, Fill = x.Fill, Cells = x.Cells?.Select(r => r.ToList()).ToList(), Tags = new(x.Tags, StringComparer.OrdinalIgnoreCase) });
        return c;
    }
    public void ApplyLayout(int n, string layout) { if (ListLayouts().All(l => l.Name != layout)) throw new HandError($"이 덱에 '{layout}' 레이아웃이 없습니다 — list_layouts 로 이름을 보세요"); At(n).Layout = layout; }
    public string ReadNotes(int n) => At(n).Notes;
    public void SetNotes(int n, string text) => At(n).Notes = text;
    public Rendered RenderSlide(int n, int maxWidth) { At(n); var png = Convert.ToBase64String(new byte[] { 0x89, 0x50, 0x4E, 0x47 }); return new Rendered(png, maxWidth, maxWidth * 9 / 16, 4); }
    public IReadOnlyList<(string, int)> SlideParts(int n) { var s = At(n); var parts = new List<(string, int)> { ("slide", 800 + s.Shapes.Count * 120) }; if (s.Notes.Length > 0) parts.Add(("notes", 300 + s.Notes.Length)); foreach (var x in s.Shapes.Where(x => x.Type == "Chart")) parts.Add(($"chart:{x.Id}", 2000)); return parts; }
    public string SlidePart(int n, string part, string? shapeId)
    {
        var s = At(n);
        return part switch
        {
            "slide" => "<p:sld>" + string.Concat(s.Shapes.Select(x => $"<p:sp id=\"{x.Id}\">{x.Text}</p:sp>")) + "</p:sld>",
            "notes" => s.Notes.Length > 0 ? $"<p:notes>{s.Notes}</p:notes>" : throw new HandError("이 장에는 노트 부분이 없습니다"),
            "chart" => s.Shapes.Any(x => x.Type == "Chart" && (shapeId is null || x.Id.ToString() == shapeId)) ? "<c:chartSpace/>" : throw new HandError("이 장에는 차트 부분이 없습니다"),
            _ => throw new HandError($"모르는 부분입니다: {part} — slide, notes, chart, list 중 하나"),
        };
    }
    public (string, int) SnapshotSlide(int n)
    {
        var s = At(n); var id = $"snap-{nextSnap++}";
        var json = JsonSerializer.Serialize(new { s.Layout, s.Notes, Shapes = s.Shapes.Select(x => new { x.Name, x.Type, x.Placeholder, x.Text, x.L, x.T, x.W, x.H, x.Cells }) });
        snapshots[id] = json; return (id, json.Length);
    }
    public (int, string) RestoreSlide(string id, int? n)
    {
        if (!snapshots.TryGetValue(id, out var json)) throw new HandError($"그런 스냅숏이 없습니다: {id} — snapshot_slide 가 준 id 를 주세요(이 손이 뜬 뒤 찍은 것만 압니다)");
        var at = n ?? slides.Count;
        var doc = JsonDocument.Parse(json).RootElement;
        var c = new Slide { Id = nextSlide++, Layout = doc.GetProperty("Layout").GetString() ?? "", Notes = doc.GetProperty("Notes").GetString() ?? "" };
        foreach (var x in doc.GetProperty("Shapes").EnumerateArray())
            c.Shapes.Add(new Shape { Id = nextShape++, Name = x.GetProperty("Name").GetString() ?? "", Type = x.GetProperty("Type").GetString() ?? "", Placeholder = x.GetProperty("Placeholder").GetString(), Text = x.GetProperty("Text").GetString() ?? "", L = x.GetProperty("L").GetDouble(), T = x.GetProperty("T").GetDouble(), W = x.GetProperty("W").GetDouble(), H = x.GetProperty("H").GetDouble(),
                Cells = x.GetProperty("Cells").ValueKind == JsonValueKind.Array ? x.GetProperty("Cells").EnumerateArray().Select(r => r.EnumerateArray().Select(v => v.GetString() ?? "").ToList()).ToList() : null });
        if (n is int) slides[at - 1] = c; else slides.Add(c);
        return (n is int ? at : slides.Count, c.Id.ToString());
    }
    /// <summary>시험이 정하는 「보고 있는 장」. 0 이면 창이 없는 것 — 거절.</summary>
    public int Current = 1;
    public int CurrentSlide()
    {
        if (slides.Count == 0) throw new HandError("이 덱에는 장이 없습니다");
        if (Current < 1 || Current > slides.Count) throw new HandError("어느 장인지 알 수 없습니다 — 이 덱의 창이 없습니다. slide 나 slide_id 를 주세요");
        return Current;
    }
    public int ResolveSlide(int? slide, string? slideId)
    {
        if (slideId is not null) { var i = slides.FindIndex(s => s.Id.ToString() == slideId); if (i < 0) throw new HandError($"슬라이드 id {slideId} 가 없습니다 — 지워졌거나 다시 지어졌으니 list_slides 로 목차를 다시 읽으세요"); return i + 1; }
        if (slide is int n) { if (n < 1 || n > slides.Count) throw new HandError($"슬라이드 {n} 이 없습니다 — 이 덱은 지금 {slides.Count}장입니다. list_slides 로 목차를 다시 읽고 그 번호·id 로 부르세요"); return n; }
        throw new HandError("어느 장인지 slide 나 slide_id 로 말해 주세요");
    }

    // ── 도형 ──
    public (string, string) SetText(int n, string? shapeId, string? placeholder, string text)
    {
        var s = At(n);
        Shape? sh = shapeId is not null ? s.Shapes.FirstOrDefault(x => x.Id.ToString() == shapeId) : null;
        if (sh is null && placeholder is not null) sh = s.Shapes.FirstOrDefault(x => Role(x.Placeholder) == placeholder.ToLowerInvariant());
        if (sh is null) throw new HandError(shapeId is not null ? $"슬라이드 {n} 에 도형 {shapeId} 이 없습니다" : $"이 장에 '{placeholder}' 자리가 없습니다 — 이 장의 자리: {string.Join(", ", s.Shapes.Select(x => Role(x.Placeholder)).Where(r => r is not null))}");
        var before = sh.Text; sh.Text = text; return (sh.Id.ToString(), before);
    }
    public string AddShape(int n, string kind, double l, double t, double w, double h, string? text, string? fill, double? size, string? color, bool bold)
    { var sh = new Shape { Id = nextShape++, Name = kind, Type = kind == "textbox" ? "TextBox" : kind == "line" ? "Line" : "AutoShape", Text = text ?? "", L = l, T = t, W = w, H = h, Fill = fill, Size = size, Color = color, Bold = bold }; At(n).Shapes.Add(sh); return sh.Id.ToString(); }
    public void DeleteShape(int n, string id) { var s = At(n); if (s.Shapes.RemoveAll(x => x.Id.ToString() == id) == 0) throw new HandError($"슬라이드 {n} 에 도형 {id} 이 없습니다"); }
    public void FormatShape(int n, string id, ShapeFormat f)
    {
        var sh = ShapeAt(n, id);
        if (f.Decorative is not null) throw new HandError("decorative 는 이 손(COM, Office 2021)이 못 겁니다 — 그 속성이 2021 객체 모델에 없습니다. alt_text 로 대신 적으세요");
        if (f.Font is not null) sh.Font = f.Font; if (f.Size is double sz) sh.Size = sz; if (f.Bold is bool b) sh.Bold = b; if (f.Color is not null) sh.Color = f.Color;
        if (f.Fill is not null) sh.Fill = f.Fill == "none" ? null : f.Fill; if (f.Rotation is double r) sh.Rotation = r; if (f.Visible is bool v) sh.Visible = v; if (f.AltText is not null) sh.AltText = f.AltText;
        if (f.Transparency is double tr && (tr < 0 || tr > 1)) throw new HandError($"transparency 는 0~1 입니다 — {tr}");
    }
    public void MoveShape(int n, string id, double? l, double? t, double? w, double? h, string? z)
    {
        var sh = ShapeAt(n, id);
        if (z is not null && z is not ("BringToFront" or "BringForward" or "SendBackward" or "SendToBack")) throw new HandError($"z_order 는 BringToFront, BringForward, SendBackward, SendToBack 중 하나입니다 — {z}");
        if (l is double a) sh.L = a; if (t is double b) sh.T = b; if (w is double c) sh.W = c; if (h is double d) sh.H = d;
        if (z is "BringToFront") { var s = At(n); s.Shapes.Remove(sh); s.Shapes.Add(sh); } else if (z is "SendToBack") { var s = At(n); s.Shapes.Remove(sh); s.Shapes.Insert(0, sh); }
    }
    public string GroupShapes(int n, IReadOnlyList<string> ids)
    {
        var s = At(n); var members = ids.Select(i => ShapeAt(n, i)).ToList();
        var g = new Shape { Id = nextShape++, Name = "그룹", Type = "Group", L = members.Min(m => m.L), T = members.Min(m => m.T), Members = members.Select(m => m.Id).ToList() };
        g.W = members.Max(m => m.L + m.W) - g.L; g.H = members.Max(m => m.T + m.H) - g.T;
        foreach (var m in members) s.Shapes.Remove(m);
        hidden.AddRange(members); s.Shapes.Add(g); return g.Id.ToString();
    }
    private readonly List<Shape> hidden = new();
    public IReadOnlyList<string> UngroupShape(int n, string id)
    {
        var g = ShapeAt(n, id); if (g.Members is null) throw new HandError($"도형 {id} 은 그룹이 아닙니다({g.Type})");
        var s = At(n); s.Shapes.Remove(g); var back = hidden.Where(h => g.Members.Contains(h.Id)).ToList(); s.Shapes.AddRange(back); hidden.RemoveAll(h => g.Members.Contains(h.Id));
        return back.Select(b => b.Id.ToString()).ToList();
    }
    public void SetHyperlink(int n, string id, string url, string? tip) => ShapeAt(n, id).Url = url.Length == 0 ? null : url;
    public void FormatRun(int n, string id, int start, int length, RunFormat f) { var sh = ShapeAt(n, id); if (start < 0 || start + length > sh.Text.Length) throw new HandError($"글 범위가 도형 밖입니다 — 글은 {sh.Text.Length}자, 요구는 {start}+{length}"); sh.Runs[$"{start}+{length}"] = f; }
    public Rendered RenderShape(int n, string id, int maxWidth) { var sh = ShapeAt(n, id); return new Rendered(Convert.ToBase64String(new byte[] { 0x89, 0x50, 0x4E, 0x47 }), maxWidth, (int)(maxWidth * sh.H / Math.Max(1, sh.W)), 4); }
    public int ApplyStyle(string? tf, double? ts, string? tc, bool? tb, string? bf, double? bs, string? bc, string? ea)
    {
        var touched = 0;
        foreach (var s in slides)
        {
            var any = false;
            foreach (var x in s.Shapes)
            {
                var role = Role(x.Placeholder);
                if (role == "title") { if (tf is not null) x.Font = tf; if (ts is double a) x.Size = a; if (tc is not null) x.Color = tc; if (tb is bool b) x.Bold = b; any = true; }
                else if (role == "body") { if (bf is not null) x.Font = bf; if (bs is double c) x.Size = c; if (bc is not null) x.Color = bc; any = true; }
            }
            if (any) touched++;
        }
        return touched;
    }

    // ── 표·차트·그림 ──
    public TableInfo? TableOf(int n, string id) { var sh = ShapeAt(n, id); return sh.Cells is null ? null : Table(sh); }
    private static TableInfo Table(Shape sh) => new(sh.Id.ToString(), sh.Cells!.Count, sh.Cells[0].Count, sh.Cells, sh.L, sh.T, sh.W, sh.H);
    public IReadOnlyList<TableInfo> TablesOn(int n) => At(n).Shapes.Where(x => x.Cells is not null).Select(Table).ToList();
    public string AddTable(int n, TableSpec t)
    {
        if (t.TableStyle is not null && !TableStyles.ById.ContainsKey(t.TableStyle)) throw new HandError($"이 손이 아는 표 스타일이 아닙니다: {t.TableStyle} — 아는 것: {string.Join(", ", TableStyles.ById.Keys)}");
        var cells = Enumerable.Range(0, t.Rows).Select(r => Enumerable.Range(0, t.Columns).Select(c => t.Values is not null && r < t.Values.Count && c < t.Values[r].Count ? t.Values[r][c] : "").ToList()).ToList();
        var sh = new Shape { Id = nextShape++, Name = "표", Type = "Table", L = t.Left ?? 60, T = t.Top ?? 120, W = t.Width ?? 600, H = t.Height ?? 40 * t.Rows, Cells = cells, Font = t.Font, Size = t.Size, Bold = t.HeaderBold };
        At(n).Shapes.Add(sh); return sh.Id.ToString();
    }
    public void SetCells(int n, string id, IReadOnlyList<(int Row, int Column, string Text)> cells)
    {
        var sh = ShapeAt(n, id); if (sh.Cells is null) throw new HandError($"도형 {id} 은 표가 아닙니다({sh.Type})");
        foreach (var (r, c, text) in cells) { if (r < 0 || r >= sh.Cells.Count || c < 0 || c >= sh.Cells[0].Count) throw new HandError($"칸 ({r},{c}) 이 표 밖입니다 — 이 표는 {sh.Cells.Count}×{sh.Cells[0].Count}"); sh.Cells[r][c] = text; }
    }
    public void FormatCells(int n, string id, IReadOnlyList<(int Row, int Column)> cells, CellFormat f)
    {
        var sh = ShapeAt(n, id); if (sh.Cells is null) throw new HandError($"도형 {id} 은 표가 아닙니다({sh.Type})");
        foreach (var (r, c) in cells) if (r < 0 || r >= sh.Cells.Count || c < 0 || c >= sh.Cells[0].Count) throw new HandError($"칸 ({r},{c}) 이 표 밖입니다 — 이 표는 {sh.Cells.Count}×{sh.Cells[0].Count}");
        sh.Runs["cells"] = f;
    }
    public void EditTable(int n, string id, TableEdit e)
    {
        var sh = ShapeAt(n, id); if (sh.Cells is null) throw new HandError($"도형 {id} 은 표가 아닙니다({sh.Type})");
        foreach (var r in e.DeleteRows.OrderByDescending(x => x)) { if (r < 0 || r >= sh.Cells.Count) throw new HandError($"줄 {r} 이 없습니다"); sh.Cells.RemoveAt(r); }
        foreach (var c in e.DeleteColumns.OrderByDescending(x => x)) foreach (var row in sh.Cells) { if (c < 0 || c >= row.Count) throw new HandError($"칸 {c} 이 없습니다"); row.RemoveAt(c); }
        var cols = sh.Cells.Count > 0 ? sh.Cells[0].Count : 0;
        for (var i = 0; i < e.AddRows; i++) sh.Cells.Insert(Math.Clamp(e.AddRowsAt ?? sh.Cells.Count, 0, sh.Cells.Count), Enumerable.Repeat("", cols).ToList());
        for (var i = 0; i < e.AddColumns; i++) foreach (var row in sh.Cells) row.Insert(Math.Clamp(e.AddColumnsAt ?? row.Count, 0, row.Count), "");
        if (e.TableStyle is not null && !TableStyles.ById.ContainsKey(e.TableStyle)) throw new HandError($"이 손이 아는 표 스타일이 아닙니다: {e.TableStyle} — 아는 것: {string.Join(", ", TableStyles.ById.Keys)}");
    }
    public (int, string, string) AddChart(int n, ChartSpec c)
    {
        var s = At(n);
        var sh = new Shape { Id = nextShape++, Name = "차트", Type = "Chart", L = c.Left, T = c.Top, W = c.Width, H = c.Height, Text = c.Title ?? "" };
        s.Shapes.Add(sh); return (n, s.Id.ToString(), sh.Id.ToString());
    }
    public (string, double, double) AddPicture(int n, string path, double l, double t, double? w, double? h, string? alt, string? name)
    {
        var sh = new Shape { Id = nextShape++, Name = name ?? "그림", Type = "Picture", L = l, T = t, W = w ?? 400, H = h ?? 300, Path = path, AltText = alt };
        At(n).Shapes.Add(sh); return (sh.Id.ToString(), sh.W, sh.H);
    }

    // ── 배경·테마 ──
    public void SetBackground(int n, BackgroundSpec b) => At(n).Background = b.Kind == "theme" ? null : b;
    public IReadOnlyDictionary<string, string> ReadThemeColors(int n, string scope) => scope == "master" ? masterTheme : At(n).Theme;
    public void SetThemeColors(int n, string scope, IReadOnlyDictionary<string, string> colors)
    {
        var target = scope == "master" ? masterTheme : At(n).Theme;
        foreach (var (k, v) in colors) { if (!DefaultTheme.ContainsKey(k)) throw new HandError($"테마 색 이름이 아닙니다: {k} — {string.Join(", ", DefaultTheme.Keys)}"); target[k] = v; }
        if (scope == "master") foreach (var s in slides) foreach (var (k, v) in colors) s.Theme[k] = v;
    }

    // ── 메모·애니메이션 ──
    public IReadOnlyDictionary<string, string> ReadTags(int n, string? id) => id is null ? At(n).Tags : ShapeAt(n, id).Tags;
    public string? SetTag(int n, string? id, string key, string? value)
    {
        var tags = id is null ? At(n).Tags : ShapeAt(n, id).Tags; var stored = key.ToUpperInvariant();
        if (value is null) { tags.Remove(stored); return null; }
        tags[stored] = value; return stored;
    }
    public AnimRead ReadAnimation(int n) => new(At(n).Anim, 0);
    public void SetAnimation(int n, IReadOnlyList<AnimStep> steps) { foreach (var st in steps) ShapeAt(n, st.ShapeId); At(n).Anim = steps.ToList(); }

    internal Slide At(int n) => slides[ResolveSlide(n, null) - 1];
    internal Shape ShapeAt(int n, string id) => At(n).Shapes.FirstOrDefault(x => x.Id.ToString() == id) ?? throw new HandError($"슬라이드 {n} 에 도형 {id} 이 없습니다 — 이 장의 도형: {string.Join(", ", At(n).Shapes.Select(x => x.Id))}");
    internal static string? Role(string? p) => p switch { "Title" or "CenterTitle" => "title", "Body" or "Subtitle" or "Content" or "Object" or "Text" => "body", null => null, _ => p.ToLowerInvariant() };
}
