using System.Text.Json;

namespace Magi.Ppt.Hand;

/// <summary>이 연결이 어떻게 끝났나. Dismissed 는 헬퍼가 물린 것이라 **다시 붙으면 안 된다**.</summary>
public enum ConnectionEnd { Stopped, Dismissed }

/// <summary>
/// 덱 하나와 헬퍼 사이의 연결 하나. 붙고, call 을 받고, 답을 올려 보내고, 끊기면 다시 붙는다.
///
/// 이 클래스는 COM 을 모른다 — <see cref="IOps"/> 만 안다. 그래서 mac 에서 <c>--fake</c> 로도 돌고,
/// Windows 에서는 <see cref="Supervisor"/> 가 덱마다 하나씩 돌린다.
/// </summary>
public static class Connection
{
    public static async Task<ConnectionEnd> RunAsync(string helperUrl, IOps ops, CancellationToken ct)
    {
        var client = new HelperClient(helperUrl);
        Console.WriteLine($"magi-ppt-hand: {ops.Label} ({ops.DocumentKey}) → {helperUrl}");
        var backoff = 1000;
        while (!ct.IsCancellationRequested)
        {
            try
            {
                await client.FetchTokenAsync(ct);
                Hand? hand = null;
                await foreach (var f in client.StreamAsync(ops.DocumentKey, ops.Label, ct))
                {
                    backoff = 1000;
                    if (f.Event == "hello")
                    {
                        var hello = JsonSerializer.Deserialize<Hello>(f.Data, Json.Options)!;
                        hand = new Hand(ops, hello.Epoch, hello.Document);
                        Console.WriteLine($"붙었습니다 — 문서 {hello.Document} · epoch {hello.Epoch}");
                        continue;
                    }
                    if (f.Event == "bye")
                    {
                        // 같은 덱에 새 어댑터가 붙었다 — 헬퍼가 이것을 물린 것이다. 다시 붙으면 둘이 번갈아 서로를 밀어낸다.
                        Console.WriteLine($"헬퍼가 이 덱의 어댑터를 물렸습니다({f.Data}). 이 연결을 끝냅니다.");
                        return ConnectionEnd.Dismissed;
                    }
                    if (f.Event != "call" || hand is null) continue;
                    var call = JsonSerializer.Deserialize<HandCall>(f.Data, Json.Options)!;
                    var reply = hand.Handle(call);
                    Console.WriteLine($"{call.Op} → {(reply.Error is null ? "ok" : "error: " + reply.Error)}");
                    await client.ReplyAsync(reply, ct);
                }
                Console.WriteLine("스트림이 끝났습니다 — 다시 붙습니다");
            }
            catch (OperationCanceledException) { break; }
            catch (Exception e)
            {
                Console.Error.WriteLine($"헬퍼에 못 붙었습니다: {e.Message} — {backoff / 1000}초 뒤 다시");
            }
            try { await Task.Delay(backoff, ct); } catch (OperationCanceledException) { break; }
            backoff = Math.Min(backoff * 2, 15000);
        }
        return ConnectionEnd.Stopped;
    }
}
