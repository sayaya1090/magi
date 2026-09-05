using System.Net.Http;
using System.Text;
using System.Text.Json;
using System.Text.RegularExpressions;

namespace Magi.Ppt.Hand;

/// <summary>
/// 헬퍼(magi-ppt)에 손으로 붙는다. 토큰은 창이 받는 것과 같은 것 — 헬퍼가 taskpane.html 에 박아 주는
/// 값을 읽는다(루프백 전용 서버라 그 페이지는 이 머신만 받는다). 자체 서명 인증서라 검증은 끈다.
/// </summary>
public sealed class HelperClient
{
    private readonly HttpClient http;
    public string Origin { get; }
    public string? Token { get; private set; }

    public HelperClient(string origin)
    {
        Origin = origin.TrimEnd('/');
        http = new HttpClient(new HttpClientHandler { ServerCertificateCustomValidationCallback = (_, _, _, _) => true }) { Timeout = Timeout.InfiniteTimeSpan };
    }

    public async Task<string> FetchTokenAsync(CancellationToken ct)
    {
        var html = await http.GetStringAsync($"{Origin}/taskpane.html", ct);
        var m = Regex.Match(html, "\"token\"\\s*:\\s*\"([0-9a-f]+)\"");
        if (!m.Success) throw new InvalidOperationException("헬퍼 페이지에서 토큰을 못 찾았습니다 — magi-ppt 가 그 주소에 떠 있습니까?");
        Token = m.Groups[1].Value;
        return Token;
    }

    /// <summary>손 스트림을 열고 프레임을 낸다. 끊기면 끝난다 — 다시 붙는 것은 부르는 쪽이 정한다.</summary>
    public async IAsyncEnumerable<SseFrame> StreamAsync(string presentation, string label, [System.Runtime.CompilerServices.EnumeratorCancellation] CancellationToken ct)
    {
        var url = $"{Origin}/hand/stream?token={Uri.EscapeDataString(Token ?? "")}&presentation={Uri.EscapeDataString(presentation)}&label={Uri.EscapeDataString(label)}";
        using var req = new HttpRequestMessage(HttpMethod.Get, url);
        using var res = await http.SendAsync(req, HttpCompletionOption.ResponseHeadersRead, ct);
        res.EnsureSuccessStatusCode();
        using var stream = await res.Content.ReadAsStreamAsync(ct);
        using var reader = new StreamReader(stream, Encoding.UTF8);
        var lines = new List<string>();
        while (!ct.IsCancellationRequested)
        {
            var line = await reader.ReadLineAsync(ct);
            if (line is null) yield break;
            if (line.Length == 0)
            {
                foreach (var f in Sse.Parse(lines.Append(""))) yield return f;
                lines.Clear();
                continue;
            }
            lines.Add(line);
        }
    }

    public async Task ReplyAsync(HandReply reply, CancellationToken ct)
    {
        var body = JsonSerializer.Serialize(reply, Json.Options);
        using var content = new StringContent(body, Encoding.UTF8, "application/json");
        using var res = await http.PostAsync($"{Origin}/hand/reply?token={Uri.EscapeDataString(Token ?? "")}", content, ct);
        if (!res.IsSuccessStatusCode)
            throw new InvalidOperationException($"답을 못 넘겼습니다 ({(int)res.StatusCode}): {await res.Content.ReadAsStringAsync(ct)}");
    }
}
