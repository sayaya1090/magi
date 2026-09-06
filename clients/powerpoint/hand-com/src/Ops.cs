namespace Magi.Ppt.Hand;

/// <summary>한 장의 요약(list_slides·read_slide 가 낸다). JS 손의 답 모양을 따른다.</summary>
public sealed record SlideInfo(int Slide, string SlideId, string Layout, int Shapes, string Title);
/// <summary>도형 하나. 서체 넷은 글이 있는 도형만 채운다(없으면 null) — describe_style·find_shapes 가 읽는다.</summary>
public sealed record ShapeInfo(string ShapeId, string Name, string Type, string? Placeholder, string Text, double Left, double Top, double Width, double Height,
    string? Font = null, double? Size = null, string? Color = null, bool? Bold = null);
public sealed record SlideDetail(int Slide, string SlideId, string Layout, IReadOnlyList<ShapeInfo> Shapes, string Notes);
public sealed record LayoutInfo(string Name, IReadOnlyList<string> Placeholders);
public sealed record Rendered(string Base64Png, int Width, int Height, int Bytes);

/// <summary>format_shape 의 요구. null 은 안 건드린다. 이름은 도구 인자 그대로다.</summary>
public sealed record ShapeFormat(string? Font, double? Size, bool? Bold, bool? Italic, string? Color, string? Fill, string? Align,
    string? Line, double? LineWeight, string? LineDash, double? Transparency, string? Underline, bool? Strikethrough, bool? Subscript,
    bool? Superscript, bool? AllCaps, bool? SmallCaps, string? VAlign, bool? Wrap, string? Autosize, bool? Bullet, string? BulletType,
    string? BulletStyle, int? Indent, double? Rotation, bool? Visible, string? AltTitle, string? AltText, bool? Decorative);
/// <summary>format_text — 글 일부(start, length)에 거는 서식.</summary>
public sealed record RunFormat(string? Font, double? Size, bool? Bold, bool? Italic, string? Color, string? Underline, bool? Strikethrough,
    bool? Subscript, bool? Superscript, string? Url, string? ScreenTip);
public sealed record CellFormat(string? Fill, string? Color, double? Size, bool? Bold, bool? Italic, string? Align, string? VAlign, string? Borders, double? BorderWeight);
public sealed record TableSpec(int Rows, int Columns, double? Left, double? Top, double? Width, double? Height, IReadOnlyList<IReadOnlyList<string>>? Values,
    string? Font, double? Size, bool HeaderBold, string? Borders, string? TableStyle, bool? HeaderRow, bool? BandedRows, bool? BandedColumns, bool? FirstColumn,
    IReadOnlyList<double>? ColumnWidths, IReadOnlyList<double>? RowHeights, IReadOnlyList<(int Row, int Column, int Rows, int Columns)>? Merge, string? VAlign);
public sealed record TableEdit(int AddRows, int? AddRowsAt, int AddColumns, int? AddColumnsAt, IReadOnlyList<int> DeleteRows, IReadOnlyList<int> DeleteColumns,
    IReadOnlyList<double>? ColumnWidths, IReadOnlyList<double>? RowHeights, IReadOnlyList<(int Row, int Column, int Rows, int Columns)>? Merge,
    string? TableStyle, bool? HeaderRow, bool? BandedRows, bool? BandedColumns, bool? FirstColumn);
public sealed record TableInfo(string ShapeId, int Rows, int Columns, IReadOnlyList<IReadOnlyList<string>> Cells, double Left, double Top, double Width, double Height);
public sealed record ChartSpec(string Kind, string KindKo, IReadOnlyList<string> Categories, IReadOnlyList<(string Name, IReadOnlyList<double> Values)> Series,
    string? Title, double Left, double Top, double Width, double Height);
public sealed record BackgroundSpec(string Kind, string? Color, double? Transparency, string? Gradient, string? Pattern, string? Background, string? Path, bool? HideGraphics);
/// <summary>애니메이션 한 걸음. Effect 는 appear/fade/wipe/zoom, Start 는 on_click/with_previous/after_previous.</summary>
public sealed record AnimStep(string ShapeId, string Effect, string Start, int DurationMs, bool EachParagraph);
public sealed record AnimRead(IReadOnlyList<AnimStep> Steps, int Unreadable);

/// <summary>
/// 덱에 닿는 손의 동작. COM(InteropOps)이 실물이고 FakeOps 는 시험·mac 개발용이다. 판단(⚠·문구·정렬 계산)은
/// Hand 가 하고, 여기는 덱을 읽고 쓰는 것만 한다 — JS 의 OfficeHand 와 같은 자리다. 도구 48개가 전부
/// 이 계약 위에 선다: 못 하는 것은 HandError 로 이유를 대고 거절한다(조용한 no-op 금지).
/// </summary>
public interface IOps
{
    string DocumentKey { get; }   // 헬퍼에 붙을 때 presentation 으로 주는 값(파일 경로 해시)
    string Label { get; }         // 사람이 부르는 이름(파일 이름)

    // ── 장 ──
    IReadOnlyList<SlideInfo> ListSlides();
    SlideDetail ReadSlide(int slide);
    IReadOnlyList<LayoutInfo> ListLayouts();
    (int Slide, string SlideId) AddSlide(int? at, string? layout, string? title, string? body);
    void DeleteSlide(int slide);
    void MoveSlide(int slide, int to);
    (int Slide, string SlideId) DuplicateSlide(int slide);
    void ApplyLayout(int slide, string layout);
    int ResolveSlide(int? slide, string? slideId);
    /// <summary>slide 도 slide_id 도 안 왔을 때의 장 — 사람이 보고 있는 장(365 손의 getSelectedSlides 와 같은 규약). 장이 없거나 보는 장을 모르면(창 없음·여러 장 보기) 거절.</summary>
    int CurrentSlide();
    string ReadNotes(int slide);
    void SetNotes(int slide, string text);
    Rendered RenderSlide(int slide, int maxWidth);
    /// <summary>장의 OOXML 조각 — part 는 slide/notes/chart, 목록은 SlideParts.</summary>
    IReadOnlyList<(string Name, int Bytes)> SlideParts(int slide);
    string SlidePart(int slide, string part, string? shapeId);
    /// <returns>스냅숏 id 와 크기(바이트)</returns>
    (string Id, int Bytes) SnapshotSlide(int slide);
    /// <returns>되살린 장의 (번호, 새 id)</returns>
    (int Slide, string SlideId) RestoreSlide(string snapshotId, int? slide);

    // ── 도형 ──
    (string ShapeId, string Before) SetText(int slide, string? shapeId, string? placeholder, string text);
    string AddShape(int slide, string kind, double left, double top, double width, double height, string? text, string? fill, double? size, string? color, bool bold);
    void DeleteShape(int slide, string shapeId);
    void FormatShape(int slide, string shapeId, ShapeFormat f);
    void MoveShape(int slide, string shapeId, double? left, double? top, double? width, double? height, string? zOrder);
    string GroupShapes(int slide, IReadOnlyList<string> shapeIds);
    IReadOnlyList<string> UngroupShape(int slide, string shapeId);
    void SetHyperlink(int slide, string shapeId, string url, string? screenTip);
    void FormatRun(int slide, string shapeId, int start, int length, RunFormat f);
    Rendered RenderShape(int slide, string shapeId, int maxWidth);
    int ApplyStyle(string? titleFont, double? titleSize, string? titleColor, bool? titleBold, string? bodyFont, double? bodySize, string? bodyColor, string? eaFont);

    // ── 표·차트·그림 ──
    TableInfo? TableOf(int slide, string shapeId);
    IReadOnlyList<TableInfo> TablesOn(int slide);
    string AddTable(int slide, TableSpec spec);
    void SetCells(int slide, string shapeId, IReadOnlyList<(int Row, int Column, string Text)> cells);
    void FormatCells(int slide, string shapeId, IReadOnlyList<(int Row, int Column)> cells, CellFormat f);
    void EditTable(int slide, string shapeId, TableEdit e);
    (int Slide, string SlideId, string ShapeId) AddChart(int slide, ChartSpec spec);
    /// <returns>도형 id 와 실제로 놓인 크기</returns>
    (string ShapeId, double Width, double Height) AddPicture(int slide, string path, double left, double top, double? width, double? height, string? alt, string? name);

    // ── 배경·테마 ──
    void SetBackground(int slide, BackgroundSpec b);
    IReadOnlyDictionary<string, string> ReadThemeColors(int slide, string scope);
    void SetThemeColors(int slide, string scope, IReadOnlyDictionary<string, string> colors);

    // ── 덱 안의 메모·애니메이션 ──
    IReadOnlyDictionary<string, string> ReadTags(int slide, string? shapeId);
    /// <returns>저장된 키(PowerPoint 는 대문자로 둔다), 지웠으면 null</returns>
    string? SetTag(int slide, string? shapeId, string key, string? value);
    AnimRead ReadAnimation(int slide);
    void SetAnimation(int slide, IReadOnlyList<AnimStep> steps);
}
