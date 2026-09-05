namespace Magi.Ppt.Hand;

/// <summary>한 장의 요약(list_slides·read_slide 가 낸다). JS 손의 답 모양을 따른다.</summary>
public sealed record SlideInfo(int Slide, string SlideId, string Layout, int Shapes, string Title);
public sealed record ShapeInfo(string ShapeId, string Name, string Type, string? Placeholder, string Text, double Left, double Top, double Width, double Height);
public sealed record SlideDetail(int Slide, string SlideId, string Layout, IReadOnlyList<ShapeInfo> Shapes, string Notes);
public sealed record LayoutInfo(string Name, IReadOnlyList<string> Placeholders);
public sealed record Rendered(string Base64Png, int Width, int Height, int Bytes);

/// <summary>
/// 덱에 닿는 손의 동작. COM(InteropOps)이 실물이고 FakeOps 는 시험·mac 개발용이다. 판단(⚠·문구)은
/// Hand 가 하고, 여기는 덱을 읽고 쓰는 것만 한다 — JS 의 OfficeHand 와 같은 자리다.
/// </summary>
public interface IOps
{
    string DocumentKey { get; }   // 헬퍼에 붙을 때 presentation 으로 주는 값(파일 경로 해시)
    string Label { get; }         // 사람이 부르는 이름(파일 이름)
    IReadOnlyList<SlideInfo> ListSlides();
    SlideDetail ReadSlide(int slide);
    IReadOnlyList<LayoutInfo> ListLayouts();
    /// <returns>새 장의 (번호, id)</returns>
    (int Slide, string SlideId) AddSlide(int? at, string? layout, string? title, string? body);
    void DeleteSlide(int slide);
    void MoveSlide(int slide, int to);
    (int Slide, string SlideId) DuplicateSlide(int slide);
    /// <returns>바꾼 도형 id 와 이전 글</returns>
    (string ShapeId, string Before) SetText(int slide, string? shapeId, string? placeholder, string text);
    string ReadNotes(int slide);
    void SetNotes(int slide, string text);
    Rendered RenderSlide(int slide, int maxWidth);
    string AddShape(int slide, string kind, double left, double top, double width, double height, string? text, string? fill, double? size, string? color, bool bold);
    void DeleteShape(int slide, string shapeId);
    /// <summary>제목·본문 자리표시자의 서체·크기·색을 덱 전체에. null 은 안 건드린다.</summary>
    int ApplyStyle(string? titleFont, double? titleSize, string? titleColor, bool? titleBold, string? bodyFont, double? bodySize, string? bodyColor);
    int ResolveSlide(int? slide, string? slideId);
}
