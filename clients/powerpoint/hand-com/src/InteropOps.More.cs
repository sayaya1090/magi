using System.IO.Compression;
using System.Runtime.Versioning;
using System.Text.RegularExpressions;
using Office = Microsoft.Office.Core;
using PowerPoint = Microsoft.Office.Interop.PowerPoint;

namespace Magi.Ppt.Hand;

/// <summary>InteropOps 의 나머지 — 도형 서식·표·차트·그림·배경·테마·메모·애니메이션·OOXML·스냅숏. 전부 COM 객체 모델 문서로 썼고 실물 측정은 아직이다.</summary>
[SupportedOSPlatform("windows")]
public sealed partial class InteropOps
{
    private PowerPoint.Shape Find(int n, string id)
    {
        foreach (PowerPoint.Shape sh in pres.Slides[n].Shapes) if (sh.Id.ToString() == id) return sh;
        throw new HandError($"슬라이드 {n} 에 도형 {id} 이 없습니다");
    }
    private static T EnumOf<T>(string prefix, string name, string what) where T : struct, Enum
        => Enum.TryParse<T>(prefix + name, true, out var v) ? v : throw new HandError($"{what} 값을 이 손이 모릅니다: {name}");

    // ── 장 ──
    public void ApplyLayout(int n, string layout)
    {
        foreach (PowerPoint.CustomLayout l in pres.SlideMaster.CustomLayouts) if (l.Name == layout) { pres.Slides[n].CustomLayout = l; return; }
        throw new HandError($"이 덱에 '{layout}' 레이아웃이 없습니다 — list_layouts 로 이름을 보세요");
    }

    /// <summary>덱 사본을 임시 .pptx 로 저장한다 — OOXML 읽기와 스냅숏이 이 위에 선다. 파일은 손이 사는 동안 남는다.</summary>
    private string SaveCopy()
    {
        var path = Path.Combine(Path.GetTempPath(), $"magi-hand-{Guid.NewGuid():N}.pptx");
        pres.SaveCopyAs(path, PowerPoint.PpSaveAsFileType.ppSaveAsOpenXMLPresentation);
        return path;
    }
    /// <summary>presentation.xml 의 sldIdLst 순서로 n 번째 장의 part 경로를 찾는다(파일 번호 ≠ 장 번호).</summary>
    private static string SlideEntry(ZipArchive zip, int n)
    {
        var presXml = Read(zip, "ppt/presentation.xml"); var rels = Read(zip, "ppt/_rels/presentation.xml.rels");
        var ids = Regex.Matches(presXml, "<p:sldId [^>]*r:id=\"([^\"]+)\"").Select(m => m.Groups[1].Value).ToList();
        if (n < 1 || n > ids.Count) throw new HandError($"저장본에 슬라이드 {n} 이 없습니다({ids.Count}장)");
        var target = Regex.Match(rels, $"<Relationship [^>]*Id=\"{Regex.Escape(ids[n - 1])}\"[^>]*Target=\"([^\"]+)\"").Groups[1].Value;
        if (target.Length == 0) target = Regex.Match(rels, $"<Relationship [^>]*Target=\"([^\"]+)\"[^>]*Id=\"{Regex.Escape(ids[n - 1])}\"").Groups[1].Value;
        return "ppt/" + target.TrimStart('/').Replace("ppt/", "");
    }
    private static string Read(ZipArchive zip, string name) { var e = zip.GetEntry(name) ?? throw new HandError($"저장본에 {name} 이 없습니다"); using var r = new StreamReader(e.Open()); return r.ReadToEnd(); }
    private static List<(string Kind, string Path)> Related(ZipArchive zip, string slideEntry)
    {
        var dir = Path.GetDirectoryName(slideEntry)!.Replace('\\', '/'); var relsName = $"{dir}/_rels/{Path.GetFileName(slideEntry)}.rels";
        var outp = new List<(string, string)>(); if (zip.GetEntry(relsName) is null) return outp;
        foreach (Match m in Regex.Matches(Read(zip, relsName), "<Relationship [^>]*/>"))
        {
            var type = Regex.Match(m.Value, "Type=\"[^\"]*/([a-zA-Z]+)\"").Groups[1].Value; var target = Regex.Match(m.Value, "Target=\"([^\"]+)\"").Groups[1].Value;
            var full = target.StartsWith("../") ? "ppt/" + target[3..] : $"{dir}/{target}";
            outp.Add((type, full));
        }
        return outp;
    }
    public IReadOnlyList<(string, int)> SlideParts(int n)
    {
        using var zip = ZipFile.OpenRead(SaveCopy()); var entry = SlideEntry(zip, n);
        var parts = new List<(string, int)> { ("slide", (int)zip.GetEntry(entry)!.Length) };
        foreach (var (kind, path) in Related(zip, entry)) if (kind is "notesSlide" or "chart" && zip.GetEntry(path) is ZipArchiveEntry e) parts.Add((kind == "notesSlide" ? "notes" : "chart", (int)e.Length));
        return parts;
    }
    public string SlidePart(int n, string part, string? shapeId)
    {
        using var zip = ZipFile.OpenRead(SaveCopy()); var entry = SlideEntry(zip, n);
        if (part == "slide") return Read(zip, entry);
        var want = part == "notes" ? "notesSlide" : "chart";
        var hits = Related(zip, entry).Where(r => r.Kind == want).ToList();
        if (hits.Count == 0) throw new HandError($"이 장에는 {part} 부분이 없습니다");
        return Read(zip, hits[0].Path);
    }
    private readonly Dictionary<string, (string Path, int Index)> snapshots = new();
    private int nextSnap = 1;
    public (string, int) SnapshotSlide(int n)
    {
        var path = SaveCopy(); var id = $"snap-{nextSnap++}"; snapshots[id] = (path, n);
        return (id, (int)new FileInfo(path).Length);
    }
    public (int, string) RestoreSlide(string id, int? n)
    {
        if (!snapshots.TryGetValue(id, out var snap)) throw new HandError($"그런 스냅숏이 없습니다: {id} — snapshot_slide 가 준 id 를 주세요(이 손이 뜬 뒤 찍은 것만 압니다)");
        var at = n ?? pres.Slides.Count + 1;
        // InsertFromFile 의 Index 는 「그 뒤에」다 — at 자리에 넣으려면 at-1.
        pres.Slides.InsertFromFile(snap.Path, at - 1, snap.Index, snap.Index);
        if (n is int) pres.Slides[at + 1].Delete();
        return (at, pres.Slides[at].SlideID.ToString());
    }

    // ── 도형 ──
    public void FormatShape(int n, string id, ShapeFormat f)
    {
        var sh = Find(n, id);
        if (f.Decorative is not null) throw new HandError("decorative 는 이 손(COM, Office 2021)이 못 겁니다 — 그 속성이 2021 객체 모델에 없습니다. alt_text 로 대신 적으세요");
        var hasText = sh.HasTextFrame == Office.MsoTriState.msoTrue;
        if (!hasText && (f.Font ?? f.Align ?? f.Underline ?? f.VAlign ?? f.Autosize ?? f.BulletType ?? f.BulletStyle) is not null) throw new HandError($"도형 {id} 에는 글틀이 없습니다({sh.Type}) — 글 서식은 걸 수 없습니다");
        if (hasText)
        {
            var tr = sh.TextFrame.TextRange; var font = tr.Font; var f2 = sh.TextFrame2.TextRange.Font;
            if (f.Font is not null) font.Name = f.Font; if (f.Size is double s) font.Size = (float)s; if (f.Bold is bool b) font.Bold = Tri(b); if (f.Italic is bool i) font.Italic = Tri(i); if (f.Color is not null) font.Color.RGB = Bgr(f.Color);
            if (f.Align is not null) tr.ParagraphFormat.Alignment = f.Align.ToLowerInvariant() switch { "left" => PowerPoint.PpParagraphAlignment.ppAlignLeft, "center" => PowerPoint.PpParagraphAlignment.ppAlignCenter, "right" => PowerPoint.PpParagraphAlignment.ppAlignRight, _ => PowerPoint.PpParagraphAlignment.ppAlignJustify };
            if (f.Underline is not null) f2.UnderlineStyle = f.Underline == "None" ? Office.MsoTextUnderlineType.msoNoUnderline : EnumOf<Office.MsoTextUnderlineType>("mso", f.Underline + "Line", "underline");
            if (f.Strikethrough is bool st) f2.Strike = st ? Office.MsoTextStrike.msoSingleStrike : Office.MsoTextStrike.msoNoStrike;
            if (f.Subscript is bool sub) font.Subscript = Tri(sub); if (f.Superscript is bool sup) font.Superscript = Tri(sup);
            if (f.AllCaps is bool ac) f2.Allcaps = Tri(ac); if (f.SmallCaps is bool sc) f2.Smallcaps = Tri(sc);
            if (f.VAlign is not null)
            {
                var v = f.VAlign.Replace("Centered", "");
                sh.TextFrame.VerticalAnchor = v switch { "Top" => Office.MsoVerticalAnchor.msoAnchorTop, "Middle" => Office.MsoVerticalAnchor.msoAnchorMiddle, _ => Office.MsoVerticalAnchor.msoAnchorBottom };
                if (f.VAlign.EndsWith("Centered")) sh.TextFrame.HorizontalAnchor = Office.MsoHorizontalAnchor.msoAnchorCenter;
            }
            if (f.Wrap is bool w) sh.TextFrame.WordWrap = Tri(w);
            if (f.Autosize is not null) sh.TextFrame2.AutoSize = f.Autosize switch { "AutoSizeNone" => Office.MsoAutoSize.msoAutoSizeNone, "AutoSizeShapeToFitText" => Office.MsoAutoSize.msoAutoSizeShapeToFitText, _ => Office.MsoAutoSize.msoAutoSizeTextToFitShape };
            if (f.Bullet is bool bu) tr.ParagraphFormat.Bullet.Visible = Tri(bu);
            if (f.BulletType is not null) tr.ParagraphFormat.Bullet.Type = f.BulletType switch { "None" => PowerPoint.PpBulletType.ppBulletNone, "Numbered" => PowerPoint.PpBulletType.ppBulletNumbered, _ => PowerPoint.PpBulletType.ppBulletUnnumbered };
            if (f.BulletStyle is not null) tr.ParagraphFormat.Bullet.Style = EnumOf<PowerPoint.PpNumberedBulletStyle>("ppBullet", f.BulletStyle, "bullet_style");
            if (f.Indent is int ind) tr.IndentLevel = Math.Clamp(ind, 1, 5);
        }
        if (f.Fill is not null) { if (f.Fill == "none") sh.Fill.Visible = Office.MsoTriState.msoFalse; else { sh.Fill.Visible = Office.MsoTriState.msoTrue; sh.Fill.Solid(); sh.Fill.ForeColor.RGB = Bgr(f.Fill); } }
        if (f.Transparency is double tp) sh.Fill.Transparency = (float)tp;
        if (f.Line is not null) { if (f.Line == "none") sh.Line.Visible = Office.MsoTriState.msoFalse; else { sh.Line.Visible = Office.MsoTriState.msoTrue; sh.Line.ForeColor.RGB = Bgr(f.Line); } }
        if (f.LineWeight is double lw) sh.Line.Weight = (float)lw;
        if (f.LineDash is not null) sh.Line.DashStyle = EnumOf<Office.MsoLineDashStyle>("msoLine", f.LineDash.Replace("System", "Sys"), "line_dash");
        if (f.Rotation is double r) sh.Rotation = (float)r; if (f.Visible is bool vis) sh.Visible = Tri(vis);
        if (f.AltText is not null) sh.AlternativeText = f.AltText; if (f.AltTitle is not null) sh.Title = f.AltTitle;
    }
    public void MoveShape(int n, string id, double? l, double? t, double? w, double? h, string? z)
    {
        var sh = Find(n, id);
        if (l is double a) sh.Left = (float)a; if (t is double b) sh.Top = (float)b; if (w is double c) sh.Width = (float)c; if (h is double d) sh.Height = (float)d;
        if (z is not null) sh.ZOrder(EnumOf<Office.MsoZOrderCmd>("mso", z, "z_order"));
    }
    public string GroupShapes(int n, IReadOnlyList<string> ids)
    {
        var s = pres.Slides[n]; var idx = new List<object>();
        for (var i = 1; i <= s.Shapes.Count; i++) if (ids.Contains(s.Shapes[i].Id.ToString())) idx.Add(i);
        if (idx.Count != ids.Count) throw new HandError("shape_ids 중 이 장에 없는 것이 있습니다");
        return s.Shapes.Range(idx.ToArray()).Group().Id.ToString();
    }
    public IReadOnlyList<string> UngroupShape(int n, string id)
    {
        var sh = Find(n, id);
        if (sh.Type != Office.MsoShapeType.msoGroup) throw new HandError($"도형 {id} 은 그룹이 아닙니다({sh.Type})");
        var range = sh.Ungroup(); var outp = new List<string>();
        for (var i = 1; i <= range.Count; i++) outp.Add(range[i].Id.ToString());
        return outp;
    }
    public void SetHyperlink(int n, string id, string url, string? tip)
    {
        var link = Find(n, id).ActionSettings[PowerPoint.PpMouseActivation.ppMouseClick].Hyperlink;
        if (url.Length == 0) { link.Delete(); return; }
        link.Address = url; if (tip is not null) link.ScreenTip = tip;
    }
    public void FormatRun(int n, string id, int start, int length, RunFormat f)
    {
        var sh = Find(n, id); if (sh.HasTextFrame != Office.MsoTriState.msoTrue) throw new HandError($"도형 {id} 에는 글틀이 없습니다");
        var run = sh.TextFrame.TextRange.Characters(start + 1, length); var font = run.Font;
        if (f.Font is not null) font.Name = f.Font; if (f.Size is double s) font.Size = (float)s; if (f.Bold is bool b) font.Bold = Tri(b); if (f.Italic is bool i) font.Italic = Tri(i); if (f.Color is not null) font.Color.RGB = Bgr(f.Color);
        if (f.Underline is not null) font.Underline = Tri(f.Underline != "None");
        if (f.Subscript is bool sub) font.Subscript = Tri(sub); if (f.Superscript is bool sup) font.Superscript = Tri(sup);
        if (f.Strikethrough is not null) { var r2 = sh.TextFrame2.TextRange.Characters[start + 1, length]; r2.Font.Strike = f.Strikethrough == true ? Office.MsoTextStrike.msoSingleStrike : Office.MsoTextStrike.msoNoStrike; }
        if (f.Url is not null) { var link = run.ActionSettings[PowerPoint.PpMouseActivation.ppMouseClick].Hyperlink; link.Address = f.Url; if (f.ScreenTip is not null) link.ScreenTip = f.ScreenTip; }
    }
    public Rendered RenderShape(int n, string id, int maxWidth)
    {
        var sh = Find(n, id); var path = Path.Combine(Path.GetTempPath(), $"magi-shape-{Guid.NewGuid():N}.png");
        var scale = maxWidth / Math.Max(1.0, sh.Width);
        sh.Export(path, PowerPoint.PpShapeFormat.ppShapeFormatPNG, (int)(sh.Width * scale), (int)(sh.Height * scale), PowerPoint.PpExportMode.ppScaleXY);
        var bytes = File.ReadAllBytes(path); File.Delete(path);
        return new Rendered(Convert.ToBase64String(bytes), (int)(sh.Width * scale), (int)(sh.Height * scale), bytes.Length);
    }

    // ── 표 ──
    private PowerPoint.Table TableShape(int n, string id) { var sh = Find(n, id); return sh.HasTable == Office.MsoTriState.msoTrue ? sh.Table : throw new HandError($"도형 {id} 은 표가 아닙니다({sh.Type})"); }
    private static TableInfo TableInfoOf(PowerPoint.Shape sh)
    {
        var t = sh.Table; var cells = new List<IReadOnlyList<string>>();
        for (var r = 1; r <= t.Rows.Count; r++) { var row = new List<string>(); for (var c = 1; c <= t.Columns.Count; c++) row.Add(t.Cell(r, c).Shape.TextFrame.TextRange.Text); cells.Add(row); }
        return new TableInfo(sh.Id.ToString(), t.Rows.Count, t.Columns.Count, cells, sh.Left, sh.Top, sh.Width, sh.Height);
    }
    public TableInfo? TableOf(int n, string id) { var sh = Find(n, id); return sh.HasTable == Office.MsoTriState.msoTrue ? TableInfoOf(sh) : null; }
    public IReadOnlyList<TableInfo> TablesOn(int n) { var outp = new List<TableInfo>(); foreach (PowerPoint.Shape sh in pres.Slides[n].Shapes) if (sh.HasTable == Office.MsoTriState.msoTrue) outp.Add(TableInfoOf(sh)); return outp; }
    public string AddTable(int n, TableSpec t)
    {
        var sh = pres.Slides[n].Shapes.AddTable(t.Rows, t.Columns, (float)(t.Left ?? 60), (float)(t.Top ?? 120), (float)(t.Width ?? 600), (float)(t.Height ?? 40 * t.Rows));
        var table = sh.Table;
        if (t.Values is not null) for (var r = 0; r < t.Values.Count; r++) for (var c = 0; c < t.Values[r].Count; c++) table.Cell(r + 1, c + 1).Shape.TextFrame.TextRange.Text = t.Values[r][c];
        for (var r = 1; r <= t.Rows; r++) for (var c = 1; c <= t.Columns; c++)
        {
            var tr = table.Cell(r, c).Shape.TextFrame.TextRange;
            if (t.Font is not null) tr.Font.Name = t.Font; if (t.Size is double s) tr.Font.Size = (float)s; if (t.HeaderBold && r == 1) tr.Font.Bold = Office.MsoTriState.msoTrue;
            if (t.VAlign is not null) table.Cell(r, c).Shape.TextFrame.VerticalAnchor = t.VAlign.StartsWith("Top") ? Office.MsoVerticalAnchor.msoAnchorTop : t.VAlign.StartsWith("Middle") ? Office.MsoVerticalAnchor.msoAnchorMiddle : Office.MsoVerticalAnchor.msoAnchorBottom;
            if (t.Borders is not null) Border(table.Cell(r, c), t.Borders, null);
        }
        Style(table, t.TableStyle, t.HeaderRow, t.BandedRows, t.BandedColumns, t.FirstColumn, t.ColumnWidths, t.RowHeights, t.Merge);
        return sh.Id.ToString();
    }
    private static void Border(PowerPoint.Cell cell, string color, double? weight)
    {
        foreach (var side in new[] { PowerPoint.PpBorderType.ppBorderTop, PowerPoint.PpBorderType.ppBorderBottom, PowerPoint.PpBorderType.ppBorderLeft, PowerPoint.PpBorderType.ppBorderRight })
        {
            var b = cell.Borders[side];
            if (color == "none") { b.Visible = Office.MsoTriState.msoFalse; continue; }
            b.Visible = Office.MsoTriState.msoTrue; b.ForeColor.RGB = Bgr(color); if (weight is double w) b.Weight = (float)w;
        }
    }
    private static void Style(PowerPoint.Table table, string? style, bool? header, bool? bandedRows, bool? bandedCols, bool? firstCol, IReadOnlyList<double>? widths, IReadOnlyList<double>? heights, IReadOnlyList<(int Row, int Column, int Rows, int Columns)>? merge)
    {
        if (style is not null) table.ApplyStyle(TableStyles.ById.TryGetValue(style, out var guid) ? guid : throw new HandError($"이 손이 아는 표 스타일이 아닙니다: {style} — 아는 것: {string.Join(", ", TableStyles.ById.Keys)}"), true);
        if (header is bool h) table.FirstRow = h; if (bandedRows is bool br) table.HorizBanding = br; if (bandedCols is bool bc) table.VertBanding = bc; if (firstCol is bool fc) table.FirstCol = fc;
        if (widths is not null) for (var c = 0; c < widths.Count && c < table.Columns.Count; c++) table.Columns[c + 1].Width = (float)widths[c];
        if (heights is not null) for (var r = 0; r < heights.Count && r < table.Rows.Count; r++) table.Rows[r + 1].Height = (float)heights[r];
        if (merge is not null) foreach (var m in merge)
        {
            var r2 = m.Row + m.Rows; var c2 = m.Column + m.Columns;
            if (m.Row < 0 || m.Column < 0 || r2 > table.Rows.Count || c2 > table.Columns.Count) throw new HandError($"병합 범위가 표 밖입니다 — ({m.Row},{m.Column}) {m.Rows}×{m.Columns}, 표는 {table.Rows.Count}×{table.Columns.Count}");
            table.Cell(m.Row + 1, m.Column + 1).Merge(table.Cell(r2, c2));
        }
    }
    public void SetCells(int n, string id, IReadOnlyList<(int Row, int Column, string Text)> cells) { var t = TableShape(n, id); foreach (var (r, c, text) in cells) t.Cell(r + 1, c + 1).Shape.TextFrame.TextRange.Text = text; }
    public void FormatCells(int n, string id, IReadOnlyList<(int Row, int Column)> cells, CellFormat f)
    {
        var t = TableShape(n, id);
        foreach (var (r, c) in cells)
        {
            var cell = t.Cell(r + 1, c + 1); var sh = cell.Shape; var tr = sh.TextFrame.TextRange;
            if (f.Fill is not null) { if (f.Fill == "none") sh.Fill.Visible = Office.MsoTriState.msoFalse; else { sh.Fill.Visible = Office.MsoTriState.msoTrue; sh.Fill.Solid(); sh.Fill.ForeColor.RGB = Bgr(f.Fill); } }
            if (f.Color is not null) tr.Font.Color.RGB = Bgr(f.Color); if (f.Size is double s) tr.Font.Size = (float)s; if (f.Bold is bool b) tr.Font.Bold = Tri(b); if (f.Italic is bool i) tr.Font.Italic = Tri(i);
            if (f.Align is not null) tr.ParagraphFormat.Alignment = f.Align.ToLowerInvariant() switch { "left" => PowerPoint.PpParagraphAlignment.ppAlignLeft, "center" => PowerPoint.PpParagraphAlignment.ppAlignCenter, "right" => PowerPoint.PpParagraphAlignment.ppAlignRight, _ => PowerPoint.PpParagraphAlignment.ppAlignJustify };
            if (f.VAlign is not null) sh.TextFrame.VerticalAnchor = f.VAlign.StartsWith("Top") ? Office.MsoVerticalAnchor.msoAnchorTop : f.VAlign.StartsWith("Middle") ? Office.MsoVerticalAnchor.msoAnchorMiddle : Office.MsoVerticalAnchor.msoAnchorBottom;
            if (f.Borders is not null) Border(cell, f.Borders, f.BorderWeight);
        }
    }
    public void EditTable(int n, string id, TableEdit e)
    {
        var t = TableShape(n, id);
        foreach (var r in e.DeleteRows.OrderByDescending(x => x)) { if (r < 0 || r >= t.Rows.Count) throw new HandError($"줄 {r} 이 없습니다"); t.Rows[r + 1].Delete(); }
        foreach (var c in e.DeleteColumns.OrderByDescending(x => x)) { if (c < 0 || c >= t.Columns.Count) throw new HandError($"칸 {c} 이 없습니다"); t.Columns[c + 1].Delete(); }
        for (var i = 0; i < e.AddRows; i++) { if (e.AddRowsAt is int at) t.Rows.Add(Math.Clamp(at, 0, t.Rows.Count) + 1); else t.Rows.Add(-1); }
        for (var i = 0; i < e.AddColumns; i++) { if (e.AddColumnsAt is int at) t.Columns.Add(Math.Clamp(at, 0, t.Columns.Count) + 1); else t.Columns.Add(-1); }
        Style(t, e.TableStyle, e.HeaderRow, e.BandedRows, e.BandedColumns, e.FirstColumn, e.ColumnWidths, e.RowHeights, e.Merge);
    }

    // ── 차트·그림 ──
    public (int, string, string) AddChart(int n, ChartSpec c)
    {
        var s = pres.Slides[n];
        var type = c.Kind switch { "hbar" => Office.XlChartType.xlBarClustered, "line" => Office.XlChartType.xlLineMarkers, "pie" => Office.XlChartType.xlPie, _ => Office.XlChartType.xlColumnClustered };
        var sh = s.Shapes.AddChart2(-1, type, (float)c.Left, (float)c.Top, (float)c.Width, (float)c.Height, true);
        var chart = sh.Chart;
        // 값은 품은 통합 문서에 적는다 — Excel 이 깔려 있어야 한다(Office 2021 은 있다). 그래서 「데이터 편집」도 열린다.
        chart.ChartData.Activate();
        dynamic wb = chart.ChartData.Workbook;
        try
        {
            dynamic ws = wb.Worksheets[1]; ws.Cells.Clear();
            for (var i = 0; i < c.Categories.Count; i++) ws.Cells[i + 2, 1].Value = c.Categories[i];
            for (var j = 0; j < c.Series.Count; j++) { ws.Cells[1, j + 2].Value = c.Series[j].Name; for (var i = 0; i < c.Categories.Count; i++) ws.Cells[i + 2, j + 2].Value = c.Series[j].Values[i]; }
            var last = ColumnName(c.Series.Count + 1);
            chart.SetSourceData($"='{ws.Name}'!$A$1:${last}${c.Categories.Count + 1}", Office.XlRowCol.xlColumns);
        }
        finally { wb.Close(); }
        if (c.Title is not null) { chart.HasTitle = true; chart.ChartTitle.Text = c.Title; } else chart.HasTitle = false;
        return (n, s.SlideID.ToString(), sh.Id.ToString());
    }
    private static string ColumnName(int i) { var s = ""; while (i > 0) { i--; s = (char)('A' + i % 26) + s; i /= 26; } return s; }
    public (string, double, double) AddPicture(int n, string path, double l, double t, double? w, double? h, string? alt, string? name)
    {
        if (!File.Exists(path)) throw new HandError($"그림 파일이 없습니다: {path}");
        var sh = pres.Slides[n].Shapes.AddPicture(path, Office.MsoTriState.msoFalse, Office.MsoTriState.msoTrue, (float)l, (float)t, -1, -1);
        if (w is double ww && h is double hh) { sh.LockAspectRatio = Office.MsoTriState.msoFalse; sh.Width = (float)ww; sh.Height = (float)hh; }
        else if (w is double w1) { sh.LockAspectRatio = Office.MsoTriState.msoTrue; sh.Width = (float)w1; }
        else if (h is double h1) { sh.LockAspectRatio = Office.MsoTriState.msoTrue; sh.Height = (float)h1; }
        sh.Name = name ?? "그림"; sh.AlternativeText = alt ?? Path.GetFileName(path);
        return (sh.Id.ToString(), sh.Width, sh.Height);
    }

    // ── 배경·테마 ──
    public void SetBackground(int n, BackgroundSpec b)
    {
        var s = pres.Slides[n];
        if (b.HideGraphics is bool hg) s.DisplayMasterShapes = Tri(!hg);
        if (b.Kind == "theme") { s.FollowMasterBackground = Office.MsoTriState.msoTrue; return; }
        s.FollowMasterBackground = Office.MsoTriState.msoFalse; var fill = s.Background.Fill;
        switch (b.Kind)
        {
            case "solid": fill.Solid(); fill.ForeColor.RGB = Bgr(b.Color!); break;
            case "gradient":
                fill.ForeColor.RGB = Bgr(b.Color!); fill.BackColor.RGB = Bgr(b.Background ?? "#FFFFFF");
                fill.TwoColorGradient(b.Gradient switch { "radial" or "path" => Office.MsoGradientStyle.msoGradientFromCenter, "rectangle" => Office.MsoGradientStyle.msoGradientFromCorner, _ => Office.MsoGradientStyle.msoGradientHorizontal }, 1); break;
            case "pattern":
                fill.ForeColor.RGB = Bgr(b.Color!); fill.BackColor.RGB = Bgr(b.Background ?? "#FFFFFF");
                fill.Patterned(EnumOf<Office.MsoPatternType>("msoPattern", b.Pattern ?? "DiagonalCross", "pattern")); break;
            case "picture":
                if (!File.Exists(b.Path!)) throw new HandError($"그림 파일이 없습니다: {b.Path}");
                fill.UserPicture(b.Path!); break;
        }
        if (b.Transparency is double tp) fill.Transparency = (float)tp;
    }
    private static readonly (string Name, Office.MsoThemeColorSchemeIndex Index)[] ThemeSlots =
    {
        ("dark1", Office.MsoThemeColorSchemeIndex.msoThemeDark1), ("light1", Office.MsoThemeColorSchemeIndex.msoThemeLight1), ("dark2", Office.MsoThemeColorSchemeIndex.msoThemeDark2), ("light2", Office.MsoThemeColorSchemeIndex.msoThemeLight2),
        ("accent1", Office.MsoThemeColorSchemeIndex.msoThemeAccent1), ("accent2", Office.MsoThemeColorSchemeIndex.msoThemeAccent2), ("accent3", Office.MsoThemeColorSchemeIndex.msoThemeAccent3), ("accent4", Office.MsoThemeColorSchemeIndex.msoThemeAccent4),
        ("accent5", Office.MsoThemeColorSchemeIndex.msoThemeAccent5), ("accent6", Office.MsoThemeColorSchemeIndex.msoThemeAccent6), ("hyperlink", Office.MsoThemeColorSchemeIndex.msoThemeHyperlink), ("followedHyperlink", Office.MsoThemeColorSchemeIndex.msoThemeFollowedHyperlink),
    };
    private Office.ThemeColorScheme Scheme(int n, string scope) => scope switch { "master" => pres.Slides[n].Master.Theme.ThemeColorScheme, "layout" => pres.Slides[n].CustomLayout.ThemeColorScheme, _ => pres.Slides[n].ThemeColorScheme };
    public IReadOnlyDictionary<string, string> ReadThemeColors(int n, string scope) { var sc = Scheme(n, scope); return ThemeSlots.ToDictionary(t => t.Name, t => Hex(sc.Colors(t.Index).RGB)); }
    public void SetThemeColors(int n, string scope, IReadOnlyDictionary<string, string> colors)
    {
        var sc = Scheme(n, scope);
        foreach (var (k, v) in colors) sc.Colors(ThemeSlots.First(t => string.Equals(t.Name, k, StringComparison.OrdinalIgnoreCase)).Index).RGB = Bgr(v);
    }

    // ── 메모·애니메이션 ──
    private PowerPoint.Tags TagsOf(int n, string? id) => id is null ? pres.Slides[n].Tags : Find(n, id).Tags;
    public IReadOnlyDictionary<string, string> ReadTags(int n, string? id)
    {
        var tags = TagsOf(n, id); var outp = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
        for (var i = 1; i <= tags.Count; i++) outp[tags.Name(i)] = tags.Value(i);
        return outp;
    }
    public string? SetTag(int n, string? id, string key, string? value)
    {
        var tags = TagsOf(n, id);
        if (value is null) { tags.Delete(key); return null; }
        tags.Add(key, value);
        for (var i = 1; i <= tags.Count; i++) if (string.Equals(tags.Name(i), key, StringComparison.OrdinalIgnoreCase)) return tags.Name(i);
        throw new HandError("메모를 붙였는데 되읽으니 없습니다 — 이 덱이 메모를 못 받는 모양입니다");
    }
    public AnimRead ReadAnimation(int n)
    {
        var seq = pres.Slides[n].TimeLine.MainSequence; var steps = new List<AnimStep>(); var unreadable = 0;
        for (var i = 1; i <= seq.Count; i++)
        {
            var e = seq[i];
            var effect = e.EffectType switch { PowerPoint.MsoAnimEffect.msoAnimEffectAppear => "appear", PowerPoint.MsoAnimEffect.msoAnimEffectFade => "fade", PowerPoint.MsoAnimEffect.msoAnimEffectWipe => "wipe", PowerPoint.MsoAnimEffect.msoAnimEffectZoom => "zoom", _ => null };
            if (effect is null || e.Exit == Office.MsoTriState.msoTrue) { unreadable++; continue; }
            var start = e.Timing.TriggerType switch { PowerPoint.MsoAnimTriggerType.msoAnimTriggerWithPrevious => "with_previous", PowerPoint.MsoAnimTriggerType.msoAnimTriggerAfterPrevious => "after_previous", _ => "on_click" };
            steps.Add(new AnimStep(e.Shape.Id.ToString(), effect, start, (int)Math.Round(e.Timing.Duration * 1000), e.EffectInformation.BuildByLevelEffect != PowerPoint.MsoAnimateByLevel.msoAnimateLevelNone));
        }
        return new AnimRead(steps, unreadable);
    }
    public void SetAnimation(int n, IReadOnlyList<AnimStep> steps)
    {
        var seq = pres.Slides[n].TimeLine.MainSequence;
        for (var i = seq.Count; i >= 1; i--) seq[i].Delete();
        foreach (var st in steps)
        {
            var sh = Find(n, st.ShapeId);
            var effect = st.Effect switch { "appear" => PowerPoint.MsoAnimEffect.msoAnimEffectAppear, "wipe" => PowerPoint.MsoAnimEffect.msoAnimEffectWipe, "zoom" => PowerPoint.MsoAnimEffect.msoAnimEffectZoom, _ => PowerPoint.MsoAnimEffect.msoAnimEffectFade };
            var trigger = st.Start switch { "with_previous" => PowerPoint.MsoAnimTriggerType.msoAnimTriggerWithPrevious, "after_previous" => PowerPoint.MsoAnimTriggerType.msoAnimTriggerAfterPrevious, _ => PowerPoint.MsoAnimTriggerType.msoAnimTriggerOnPageClick };
            var level = st.EachParagraph ? PowerPoint.MsoAnimateByLevel.msoAnimateTextByFirstLevel : PowerPoint.MsoAnimateByLevel.msoAnimateLevelNone;
            var e = seq.AddEffect(sh, effect, level, trigger, -1);
            e.Timing.Duration = st.DurationMs / 1000f;
        }
    }
}
