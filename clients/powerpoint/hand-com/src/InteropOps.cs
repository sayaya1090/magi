using System.Runtime.InteropServices;
using System.Runtime.Versioning;
using Office = Microsoft.Office.Core;
using PowerPoint = Microsoft.Office.Interop.PowerPoint;

namespace Magi.Ppt.Hand;

/// <summary>
/// PowerPoint COM(PIA) 으로 덱에 닿는 손. Office 2021 은 Office.js 천장이 1.2 라 도형에 못 닿지만
/// COM 객체 모델은 전부 열려 있다(LTSC.md §3). 이 파일은 Windows 에서만 돈다 — mac 에서는 컴파일만 된다.
/// 도구 48개 전부가 이 클래스(+ InteropOps.More.cs) 위에 선다. ⚠ 실물 PowerPoint 2021 에서 아직 안 재 봤다 —
/// 객체 모델 문서로 쓴 것이라, 첫 실측에서 고칠 것이 나온다고 보고 읽어야 한다. 못 하는 것은 HandError 로 이유를 댄다.
/// </summary>
[SupportedOSPlatform("windows")]
public sealed partial class InteropOps : IOps
{
    private readonly PowerPoint.Application app;
    private readonly PowerPoint.Presentation pres;
    public string DocumentKey { get; }
    public string Label { get; }

    private InteropOps(PowerPoint.Application app, PowerPoint.Presentation pres)
    {
        this.app = app; this.pres = pres;
        Label = pres.Name;
        // 헬퍼의 문서 키는 presentation 파라미터에서 나온다("pid-" + 값). 파일 경로의 짧은 지문을 준다.
        DocumentKey = "com-" + Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(System.Text.Encoding.UTF8.GetBytes(pres.FullName)))[..16].ToLowerInvariant();
    }

    /// <summary>떠 있는 PowerPoint 에 붙는다. 없으면 실패한다 — 몰래 띄우지 않는다.</summary>
    /// <summary>
    /// 줄바꿈을 **진짜 문단으로** 만든다.
    ///
    /// PowerPoint 는 \n 을 소프트 줄바꿈으로, \r 을 문단 나누기로 받는다. 보이는 것은 똑같아서
    /// (글머리 자리표시자는 소프트 줄바꿈에도 기호를 붙인다) 이 차이는 눈으로 안 보인다. 그런데
    /// 문단이 아니면 **문단 단위로 할 수 있는 일이 전부 막힌다** — 「한 줄씩 나타나게」가 그렇다.
    ///
    /// 2021 실측에서 그 화면을 봤다(2026-09-05): 세 줄짜리 본문에 paragraphs:"each" 를 걸었는데
    /// 걸음이 하나였다. 애니메이션 코드가 아니라 **문단이 애초에 하나**였다. 365 판이 같은 자리를
    /// 먼저 고쳤고(asParagraphs), 이 손에는 그것이 없었다.
    /// </summary>
    internal static string AsParagraphs(string? text) =>
        (text ?? "").Replace("\r\n", "\r").Replace('\n', '\r');

    public static InteropOps AttachToRunning()
    {
        var app = (PowerPoint.Application)GetActiveObject("PowerPoint.Application");
        if (app.Presentations.Count == 0) throw new InvalidOperationException("PowerPoint 는 떠 있는데 열린 프레젠테이션이 없습니다 — 덱을 먼저 여세요");
        return new InteropOps(app, app.ActivePresentation);
    }

    // .NET 8 에는 Marshal.GetActiveObject 가 없다 — ROT 에서 직접 꺼낸다.
    [DllImport("ole32.dll")] private static extern int CLSIDFromProgID([MarshalAs(UnmanagedType.LPWStr)] string progId, out Guid clsid);
    [DllImport("oleaut32.dll")] private static extern int GetActiveObject(ref Guid clsid, IntPtr reserved, [MarshalAs(UnmanagedType.IUnknown)] out object obj);
    private static object GetActiveObject(string progId)
    {
        Marshal.ThrowExceptionForHR(CLSIDFromProgID(progId, out var clsid));
        var hr = GetActiveObject(ref clsid, IntPtr.Zero, out var obj);
        if (hr != 0) throw new InvalidOperationException("떠 있는 PowerPoint 가 없습니다 — 먼저 PowerPoint 를 열고 덱을 여세요");
        return obj;
    }

    public IReadOnlyList<SlideInfo> ListSlides()
    {
        var list = new List<SlideInfo>();
        for (var i = 1; i <= pres.Slides.Count; i++)
        {
            var s = pres.Slides[i];
            list.Add(new SlideInfo(i, s.SlideID.ToString(), s.CustomLayout?.Name ?? "", s.Shapes.Count, TitleOf(s)));
        }
        return list;
    }

    public SlideDetail ReadSlide(int n)
    {
        var s = pres.Slides[n];
        var shapes = new List<ShapeInfo>();
        foreach (PowerPoint.Shape sh in s.Shapes)
            shapes.Add(Info(sh));
        return new SlideDetail(n, s.SlideID.ToString(), s.CustomLayout?.Name ?? "", shapes, ReadNotes(n));
    }

    public IReadOnlyList<LayoutInfo> ListLayouts()
    {
        var list = new List<LayoutInfo>();
        foreach (PowerPoint.CustomLayout l in pres.SlideMaster.CustomLayouts)
        {
            var ph = new List<string>();
            foreach (PowerPoint.Shape sh in l.Shapes) if (PlaceholderOf(sh) is string p) ph.Add(p);
            list.Add(new LayoutInfo(l.Name, ph));
        }
        return list;
    }

    public (int, string) AddSlide(int? at, string? layout, string? title, string? body)
    {
        PowerPoint.CustomLayout? lay = null;
        var want = layout ?? (body is null ? "제목만" : "제목 및 내용");
        foreach (PowerPoint.CustomLayout l in pres.SlideMaster.CustomLayouts) if (l.Name == want) { lay = l; break; }
        if (lay is null && layout is null) lay = pres.SlideMaster.CustomLayouts[Math.Min(2, pres.SlideMaster.CustomLayouts.Count)];
        if (lay is null) throw new HandError($"이 덱에 '{want}' 레이아웃이 없습니다 — list_layouts 로 이름을 보세요");
        var idx = at is int i && i >= 1 && i <= pres.Slides.Count + 1 ? i : pres.Slides.Count + 1;
        var s = pres.Slides.AddSlide(idx, lay);
        if (title is not null) Fill(s, "title", title);
        if (body is not null) Fill(s, "body", body);
        return (idx, s.SlideID.ToString());
    }

    public void DeleteSlide(int n) { if (pres.Slides.Count == 1) throw new HandError("마지막 장은 지울 수 없습니다"); pres.Slides[n].Delete(); }
    public void MoveSlide(int n, int to) => pres.Slides[n].MoveTo(Math.Clamp(to, 1, pres.Slides.Count));
    public (int, string) DuplicateSlide(int n) { var r = pres.Slides[n].Duplicate(); var s = r[1]; return (s.SlideIndex, s.SlideID.ToString()); }

    public (string, string) SetText(int n, string? shapeId, string? placeholder, string text)
    {
        var s = pres.Slides[n];
        PowerPoint.Shape? target = null;
        foreach (PowerPoint.Shape sh in s.Shapes)
        {
            if (shapeId is not null && sh.Id.ToString() == shapeId) { target = sh; break; }
            if (shapeId is null && placeholder is not null && FakeOps.Role(PlaceholderOf(sh)) == placeholder.ToLowerInvariant()) { target = sh; break; }
        }
        if (target is null) throw new HandError(shapeId is not null ? $"슬라이드 {n} 에 도형 {shapeId} 이 없습니다" : $"이 장에 '{placeholder}' 자리가 없습니다");
        if (target.HasTextFrame != Office.MsoTriState.msoTrue) throw new HandError($"도형 {target.Id} 는 글을 못 받습니다");
        var before = target.TextFrame.TextRange.Text;
        target.TextFrame.TextRange.Text = AsParagraphs(text);
        return (target.Id.ToString(), before);
    }

    public string ReadNotes(int n)
    {
        var page = pres.Slides[n].NotesPage;
        foreach (PowerPoint.Shape sh in page.Shapes)
            if (sh.Type == Office.MsoShapeType.msoPlaceholder && sh.PlaceholderFormat.Type == PowerPoint.PpPlaceholderType.ppPlaceholderBody && sh.HasTextFrame == Office.MsoTriState.msoTrue)
                return sh.TextFrame.TextRange.Text;
        return "";
    }

    public void SetNotes(int n, string text)
    {
        var page = pres.Slides[n].NotesPage;
        foreach (PowerPoint.Shape sh in page.Shapes)
            if (sh.Type == Office.MsoShapeType.msoPlaceholder && sh.PlaceholderFormat.Type == PowerPoint.PpPlaceholderType.ppPlaceholderBody && sh.HasTextFrame == Office.MsoTriState.msoTrue)
            { sh.TextFrame.TextRange.Text = AsParagraphs(text); return; }
        throw new HandError($"슬라이드 {n} 의 노트 자리를 못 찾았습니다");
    }

    public Rendered RenderSlide(int n, int maxWidth)
    {
        var w = maxWidth;
        var h = (int)Math.Round(maxWidth * pres.PageSetup.SlideHeight / pres.PageSetup.SlideWidth);
        var path = Path.Combine(Path.GetTempPath(), $"magi-render-{Guid.NewGuid():N}.png");
        try
        {
            pres.Slides[n].Export(path, "PNG", w, h);
            var bytes = File.ReadAllBytes(path);
            return new Rendered(Convert.ToBase64String(bytes), w, h, bytes.Length);
        }
        finally { try { File.Delete(path); } catch { /* 임시 파일 */ } }
    }

    public string AddShape(int n, string kind, double l, double t, double w, double h, string? text, string? fill, double? size, string? color, bool bold)
    {
        var s = pres.Slides[n];
        PowerPoint.Shape sh = kind switch
        {
            "textbox" => s.Shapes.AddTextbox(Office.MsoTextOrientation.msoTextOrientationHorizontal, (float)l, (float)t, (float)w, (float)h),
            "ellipse" => s.Shapes.AddShape(Office.MsoAutoShapeType.msoShapeOval, (float)l, (float)t, (float)w, (float)h),
            "roundRectangle" => s.Shapes.AddShape(Office.MsoAutoShapeType.msoShapeRoundedRectangle, (float)l, (float)t, (float)w, (float)h),
            "line" => s.Shapes.AddLine((float)l, (float)t, (float)(l + w), (float)(t + h)),
            _ => s.Shapes.AddShape(Office.MsoAutoShapeType.msoShapeRectangle, (float)l, (float)t, (float)w, (float)h),
        };
        if (fill is not null && kind != "line") { sh.Fill.Visible = Office.MsoTriState.msoTrue; sh.Fill.Solid(); sh.Fill.ForeColor.RGB = Bgr(fill); }
        if (text is not null && sh.HasTextFrame == Office.MsoTriState.msoTrue)
        {
            var tr = sh.TextFrame.TextRange; tr.Text = text;
            if (size is double sz) tr.Font.Size = (float)sz;
            if (color is not null) tr.Font.Color.RGB = Bgr(color);
            if (bold) tr.Font.Bold = Office.MsoTriState.msoTrue;
        }
        return sh.Id.ToString();
    }

    public void DeleteShape(int n, string id)
    {
        foreach (PowerPoint.Shape sh in pres.Slides[n].Shapes) if (sh.Id.ToString() == id) { sh.Delete(); return; }
        throw new HandError($"슬라이드 {n} 에 도형 {id} 이 없습니다");
    }

    public int ApplyStyle(string? tf, double? ts, string? tc, bool? tb, string? bf, double? bs, string? bc, string? ea)
    {
        var touched = 0;
        foreach (PowerPoint.Slide s in pres.Slides)
        {
            var any = false;
            foreach (PowerPoint.Shape sh in s.Shapes)
            {
                if (sh.HasTextFrame != Office.MsoTriState.msoTrue) continue;
                var role = FakeOps.Role(PlaceholderOf(sh));
                var f = sh.TextFrame.TextRange.Font;
                if (role == "title") { if (tf is not null) f.Name = tf; if (ts is double x) f.Size = (float)x; if (tc is not null) f.Color.RGB = Bgr(tc); if (tb is bool b) f.Bold = Tri(b); if (ea is not null) f.NameFarEast = ea; any = true; }
                else if (role == "body") { if (bf is not null) f.Name = bf; if (bs is double y) f.Size = (float)y; if (bc is not null) f.Color.RGB = Bgr(bc); if (ea is not null) f.NameFarEast = ea; any = true; }
            }
            if (any) touched++;
        }
        return touched;
    }

    public int CurrentSlide()
    {
        if (pres.Slides.Count == 0) throw new HandError("이 덱에는 장이 없습니다");
        // 사람이 보고 있는 장 — 이 덱의 창이 앞에 있으면 그 창, 아니면 이 덱의 첫 창. 창이 없거나 그 보기에 「보는 장」이
        // 없으면(여러 장 보기) 365 손처럼 거절한다 — 조용히 1장으로 받으면 사람이 안 보는 장의 색을 바꾸게 된다(리뷰 2026-09-07).
        PowerPoint.DocumentWindow? w = null;
        try { var aw = app.ActiveWindow; if (aw.Presentation.FullName == pres.FullName) w = aw; } catch { }
        if (w is null) { try { if (pres.Windows.Count > 0) w = pres.Windows[1]; } catch { } }
        if (w is null) throw new HandError("어느 장인지 알 수 없습니다 — 이 덱의 창이 없습니다. slide 나 slide_id 를 주세요");
        try { return ((PowerPoint.Slide)w.View.Slide).SlideIndex; }
        catch { throw new HandError("어느 장인지 알 수 없습니다 — 지금 보기에는 고른 장이 없습니다(여러 장 보기?). slide 나 slide_id 를 주세요"); }
    }
    public int ResolveSlide(int? slide, string? slideId)
    {
        if (slideId is not null)
        {
            for (var i = 1; i <= pres.Slides.Count; i++) if (pres.Slides[i].SlideID.ToString() == slideId) return i;
            throw new HandError($"슬라이드 id {slideId} 가 없습니다 — 지워졌거나 다시 지어졌으니 list_slides 로 목차를 다시 읽으세요");
        }
        if (slide is int n)
        {
            if (n < 1 || n > pres.Slides.Count) throw new HandError($"슬라이드 {n} 이 없습니다 — 이 덱은 지금 {pres.Slides.Count}장입니다. list_slides 로 목차를 다시 읽고 그 번호·id 로 부르세요");
            return n;
        }
        throw new HandError("어느 장인지 slide 나 slide_id 로 말해 주세요");
    }

    private void Fill(PowerPoint.Slide s, string role, string text)
    {
        foreach (PowerPoint.Shape sh in s.Shapes)
            if (FakeOps.Role(PlaceholderOf(sh)) == role && sh.HasTextFrame == Office.MsoTriState.msoTrue) { sh.TextFrame.TextRange.Text = AsParagraphs(text); return; }
        throw new HandError($"{role} 자리가 없는 레이아웃입니다");
    }

    private static string TitleOf(PowerPoint.Slide s)
    {
        foreach (PowerPoint.Shape sh in s.Shapes) if (FakeOps.Role(PlaceholderOf(sh)) == "title") return TextOf(sh);
        return "";
    }
    private static ShapeInfo Info(PowerPoint.Shape sh)
    {
        var text = TextOf(sh); string? font = null; double? size = null; string? color = null; bool? bold = null;
        if (text.Length > 0)
        {
            var f = sh.TextFrame.TextRange.Font;
            font = f.Name; size = f.Size; color = Hex(f.Color.RGB); bold = f.Bold == Office.MsoTriState.msoTrue;
        }
        var type = sh.HasTable == Office.MsoTriState.msoTrue ? "Table" : sh.HasChart == Office.MsoTriState.msoTrue ? "Chart" : sh.Type.ToString().Replace("mso", "");
        return new ShapeInfo(sh.Id.ToString(), sh.Name, type, PlaceholderOf(sh), text, sh.Left, sh.Top, sh.Width, sh.Height, font, size, color, bold);
    }
    private static Office.MsoTriState Tri(bool b) => b ? Office.MsoTriState.msoTrue : Office.MsoTriState.msoFalse;
    /// <summary>COM 의 BGR 정수를 "#RRGGBB" 로.</summary>
    internal static string Hex(int bgr) => $"#{bgr & 0xFF:X2}{(bgr >> 8) & 0xFF:X2}{(bgr >> 16) & 0xFF:X2}";
    private static string TextOf(PowerPoint.Shape sh) => sh.HasTextFrame == Office.MsoTriState.msoTrue ? sh.TextFrame.TextRange.Text : "";
    private static string? PlaceholderOf(PowerPoint.Shape sh)
    {
        if (sh.Type != Office.MsoShapeType.msoPlaceholder) return null;
        return sh.PlaceholderFormat.Type switch
        {
            PowerPoint.PpPlaceholderType.ppPlaceholderTitle => "Title",
            PowerPoint.PpPlaceholderType.ppPlaceholderCenterTitle => "CenterTitle",
            PowerPoint.PpPlaceholderType.ppPlaceholderSubtitle => "Subtitle",
            PowerPoint.PpPlaceholderType.ppPlaceholderBody => "Body",
            PowerPoint.PpPlaceholderType.ppPlaceholderObject => "Content",
            var t => t.ToString().Replace("ppPlaceholder", ""),
        };
    }
    /// <summary>COM 의 RGB 는 BGR 정수다 — "#RRGGBB" 를 뒤집는다.</summary>
    internal static int Bgr(string hex)
    {
        var h = hex.TrimStart('#');
        if (h.Length != 6) throw new HandError($"색은 #RRGGBB 로 주세요: {hex}");
        var r = Convert.ToInt32(h[..2], 16); var g = Convert.ToInt32(h[2..4], 16); var b = Convert.ToInt32(h[4..], 16);
        return r | (g << 8) | (b << 16);
    }
}
