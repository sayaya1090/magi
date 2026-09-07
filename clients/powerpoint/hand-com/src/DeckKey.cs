using System.Security.Cryptography;
using System.Text;

namespace Magi.Ppt.Hand;

/// <summary>
/// 덱의 문서 키 — 파일 경로의 짧은 지문. **작업창과 손이 같은 규칙으로 짓는다.**
///
/// 2021 에서 작업창은 태그 칸이 없어 덱 이름을 못 지어, 손이 하나뿐일 때는 허브가 「가장 최근 손」을 보여 줬다.
/// 덱을 둘 열면 두 창이 같은 손을 보고 답이 섞인다(실물 2026-09-07). 그래서 창은 Office.context.document.url 로,
/// 손은 pres.FullName 으로 **같은 키**를 짓는다 — 둘이 같은 파일이면 글자 모양이 달라도(역슬래시·file:///·대소문자)
/// 같은 키가 나오게 여기서 고른다. 저장 안 한 덱은 URL 이 없어 창이 키를 못 짓는다 — 그건 여기서 못 푼다.
/// OfficeDeck.js 의 comDeckId 와 짝이고, 양쪽 시험이 같은 벡터를 문다.
/// </summary>
public static class DeckKey
{
    public static string Normalize(string path)
    {
        var s = (path ?? "").Trim();
        if (s.StartsWith("file:///", StringComparison.OrdinalIgnoreCase)) s = s[8..];
        else if (s.StartsWith("file://", StringComparison.OrdinalIgnoreCase)) s = s[7..];
        try { s = Uri.UnescapeDataString(s); } catch { /* 그대로 */ }
        return s.Replace('\\', '/').ToLowerInvariant();
    }

    public static string Of(string fullName) =>
        "com-" + Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(Normalize(fullName))))[..16].ToLowerInvariant();
}
