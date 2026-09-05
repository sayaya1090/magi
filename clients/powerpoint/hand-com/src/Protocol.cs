using System.Text.Json;
using System.Text.Json.Serialization;

namespace Magi.Ppt.Hand;

/// <summary>
/// 헬퍼 ↔ 손 계약(helper/hand.go). 헬퍼는 SSE 로 <c>hello{document,label,epoch}</c> 를 먼저 보내고
/// 이어 <c>call{id,op,args}</c> 를 보낸다. 손은 <c>POST /hand/reply</c> 로 답한다. 이 파일은 그 모양뿐이다
/// — 판단은 없다.
/// </summary>
public sealed record HandCall(
    [property: JsonPropertyName("id")] string Id,
    [property: JsonPropertyName("op")] string Op,
    [property: JsonPropertyName("args")] Dictionary<string, JsonElement>? Args);

public sealed record Hello(
    [property: JsonPropertyName("document")] string Document,
    [property: JsonPropertyName("label")] string Label,
    [property: JsonPropertyName("epoch")] int Epoch);

public sealed class HandReply
{
    [JsonPropertyName("id")] public string Id { get; set; } = "";
    [JsonPropertyName("error")][JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] public string? Error { get; set; }
    [JsonPropertyName("document")] public string Document { get; set; } = "";
    [JsonPropertyName("label")][JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] public string? Label { get; set; }
    [JsonPropertyName("result")][JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] public Dictionary<string, object?>? Result { get; set; }
    [JsonPropertyName("changed")][JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] public List<string>? Changed { get; set; }
    [JsonPropertyName("epoch")] public int Epoch { get; set; }
    [JsonPropertyName("count")] public int Count { get; set; }
}

/// <summary>SSE 프레임 하나: <c>event:</c> 한 줄과 <c>data:</c> 줄들. 빈 줄이 경계다. 주석(<c>: ping</c>)은 버린다.</summary>
public sealed record SseFrame(string Event, string Data);

public static class Sse
{
    /// <summary>줄 단위로 먹여 프레임이 완성될 때마다 낸다. 순수 함수 — 시험은 여기서 잰다.</summary>
    public static IEnumerable<SseFrame> Parse(IEnumerable<string> lines)
    {
        string ev = "message"; var data = new List<string>();
        foreach (var raw in lines)
        {
            var line = raw.TrimEnd('\r');
            if (line.Length == 0)
            {
                if (data.Count > 0) yield return new SseFrame(ev, string.Join("\n", data));
                ev = "message"; data.Clear();
                continue;
            }
            if (line.StartsWith(':')) continue;
            var colon = line.IndexOf(':');
            var field = colon < 0 ? line : line[..colon];
            var value = colon < 0 ? "" : line[(colon + 1)..].TrimStart(' ');
            if (field == "event") ev = value;
            else if (field == "data") data.Add(value);
        }
        if (data.Count > 0) yield return new SseFrame(ev, string.Join("\n", data));
    }
}

public static class Json
{
    public static readonly JsonSerializerOptions Options = new() { PropertyNameCaseInsensitive = true, Encoder = System.Text.Encodings.Web.JavaScriptEncoder.UnsafeRelaxedJsonEscaping };
}
