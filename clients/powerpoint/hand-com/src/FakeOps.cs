namespace Magi.Ppt.Hand;

/// <summary>메모리 덱. 시험과 mac 개발용 — PowerPoint 없이 손의 규약을 잰다.</summary>
public sealed class FakeOps : IOps
{
    private sealed class Shape { public int Id; public string Name = ""; public string Type = "Placeholder"; public string? Placeholder; public string Text = ""; public double L, T, W = 828, H = 60; }
    private sealed class Slide { public int Id; public string Layout = "제목 및 내용"; public List<Shape> Shapes = new(); public string Notes = ""; }
    private readonly List<Slide> slides = new();
    private int nextSlide = 256, nextShape = 1;
    public string DocumentKey { get; }
    public string Label { get; }
    public FakeOps(string key = "fake-deck", string label = "fake.pptx") { DocumentKey = key; Label = label; AddSlide(null, "제목 슬라이드", null, null); }

    public IReadOnlyList<SlideInfo> ListSlides() => slides.Select((s, i) => new SlideInfo(i + 1, s.Id.ToString(), s.Layout, s.Shapes.Count, s.Shapes.FirstOrDefault(x => x.Placeholder is "Title" or "CenterTitle")?.Text ?? "")).ToList();
    public SlideDetail ReadSlide(int n) { var s = At(n); return new SlideDetail(n, s.Id.ToString(), s.Layout, s.Shapes.Select(x => new ShapeInfo(x.Id.ToString(), x.Name, x.Type, x.Placeholder, x.Text, x.L, x.T, x.W, x.H)).ToList(), s.Notes); }
    public IReadOnlyList<LayoutInfo> ListLayouts() => new[] { new LayoutInfo("제목 슬라이드", new[] { "CenterTitle", "Subtitle" }), new LayoutInfo("제목 및 내용", new[] { "Title", "Body" }), new LayoutInfo("제목만", new[] { "Title" }), new LayoutInfo("빈 화면", Array.Empty<string>()) };
    public (int, string) AddSlide(int? at, string? layout, string? title, string? body)
    {
        var s = new Slide { Id = nextSlide++, Layout = layout ?? (body is null ? "제목만" : "제목 및 내용") };
        var lay = ListLayouts().FirstOrDefault(l => l.Name == s.Layout) ?? throw new HandError($"이 덱에 '{s.Layout}' 레이아웃이 없습니다 — list_layouts 로 이름을 보세요");
        foreach (var p in lay.Placeholders) s.Shapes.Add(new Shape { Id = nextShape++, Name = p, Placeholder = p, T = p.Contains("Title") ? 40 : 140, H = p.Contains("Title") ? 60 : 300 });
        if (title is not null) (s.Shapes.FirstOrDefault(x => x.Placeholder is "Title" or "CenterTitle") ?? throw new HandError("title 자리가 없는 레이아웃입니다")).Text = title;
        if (body is not null) (s.Shapes.FirstOrDefault(x => x.Placeholder is "Body" or "Subtitle") ?? throw new HandError("body 자리가 없는 레이아웃입니다")).Text = body;
        var idx = at is int i && i >= 1 && i <= slides.Count + 1 ? i - 1 : slides.Count;
        slides.Insert(idx, s);
        return (idx + 1, s.Id.ToString());
    }
    public void DeleteSlide(int n) { if (slides.Count == 1) throw new HandError("마지막 장은 지울 수 없습니다"); slides.RemoveAt(n - 1); }
    public void MoveSlide(int n, int to) { var s = At(n); slides.RemoveAt(n - 1); slides.Insert(Math.Clamp(to, 1, slides.Count + 1) - 1, s); }
    public (int, string) DuplicateSlide(int n) { var s = At(n); var c = new Slide { Id = nextSlide++, Layout = s.Layout, Notes = s.Notes, Shapes = s.Shapes.Select(x => new Shape { Id = nextShape++, Name = x.Name, Type = x.Type, Placeholder = x.Placeholder, Text = x.Text, L = x.L, T = x.T, W = x.W, H = x.H }).ToList() }; slides.Insert(n, c); return (n + 1, c.Id.ToString()); }
    public (string, string) SetText(int n, string? shapeId, string? placeholder, string text)
    {
        var s = At(n);
        Shape? sh = shapeId is not null ? s.Shapes.FirstOrDefault(x => x.Id.ToString() == shapeId) : null;
        if (sh is null && placeholder is not null) sh = s.Shapes.FirstOrDefault(x => Role(x.Placeholder) == placeholder.ToLowerInvariant());
        if (sh is null) throw new HandError(shapeId is not null ? $"슬라이드 {n} 에 도형 {shapeId} 이 없습니다" : $"이 장에 '{placeholder}' 자리가 없습니다 — 이 장의 자리: {string.Join(", ", s.Shapes.Select(x => x.Placeholder ?? x.Type))}");
        var before = sh.Text; sh.Text = text; return (sh.Id.ToString(), before);
    }
    public string ReadNotes(int n) => At(n).Notes;
    public void SetNotes(int n, string text) => At(n).Notes = text;
    public Rendered RenderSlide(int n, int maxWidth) { At(n); var png = Convert.ToBase64String(new byte[] { 0x89, 0x50, 0x4E, 0x47 }); return new Rendered(png, maxWidth, maxWidth * 9 / 16, 4); }
    public string AddShape(int n, string kind, double l, double t, double w, double h, string? text, string? fill, double? size, string? color, bool bold) { var s = At(n); var sh = new Shape { Id = nextShape++, Name = kind, Type = kind, Text = text ?? "", L = l, T = t, W = w, H = h }; s.Shapes.Add(sh); return sh.Id.ToString(); }
    public void DeleteShape(int n, string id) { var s = At(n); if (s.Shapes.RemoveAll(x => x.Id.ToString() == id) == 0) throw new HandError($"슬라이드 {n} 에 도형 {id} 이 없습니다"); }
    public int ApplyStyle(string? tf, double? ts, string? tc, bool? tb, string? bf, double? bs, string? bc) => slides.Count;
    public int ResolveSlide(int? slide, string? slideId)
    {
        if (slideId is not null) { var i = slides.FindIndex(s => s.Id.ToString() == slideId); if (i < 0) throw new HandError($"슬라이드 id {slideId} 가 없습니다 — 지워졌거나 다시 지어졌으니 list_slides 로 목차를 다시 읽으세요"); return i + 1; }
        if (slide is int n) { if (n < 1 || n > slides.Count) throw new HandError($"슬라이드 {n} 이 없습니다 — 이 덱은 지금 {slides.Count}장입니다. list_slides 로 목차를 다시 읽고 그 번호·id 로 부르세요"); return n; }
        throw new HandError("어느 장인지 slide 나 slide_id 로 말해 주세요");
    }
    private Slide At(int n) => slides[ResolveSlide(n, null) - 1];
    internal static string Role(string? p) => p switch { "Title" or "CenterTitle" => "title", "Body" or "Subtitle" or "Content" or "Object" => "body", _ => (p ?? "").ToLowerInvariant() };
}
