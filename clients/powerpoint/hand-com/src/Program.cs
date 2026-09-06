using System.Text.Json;
using Magi.Ppt.Hand;

// magi-ppt-hand — Office 2021 용 COM 손. 헬퍼(magi-ppt)에 붙어 call 을 받고 PowerPoint 를 COM 으로 움직인다.
//   magi-ppt-hand [--helper https://127.0.0.1:3000/ppt] [--fake]
// --fake 는 PowerPoint 없이 메모리 덱으로 붙는다(개발·시험용, mac 에서도 돈다).
var helperUrl = "https://127.0.0.1:3000/ppt"; // magi office 는 한 포트에서 /ppt·/xl·/word 를 내준다 — 파워포인트 몫이 /ppt
var fake = false;
for (var i = 0; i < args.Length; i++)
{
    if (args[i] == "--helper" && i + 1 < args.Length) helperUrl = args[++i];
    else if (args[i] == "--fake") fake = true;
    else if (args[i] is "-h" or "--help") { Console.WriteLine("magi-ppt-hand [--helper URL] [--fake]"); return 0; }
}

using var cts = new CancellationTokenSource();
Console.CancelKeyPress += (_, e) => { e.Cancel = true; cts.Cancel(); };

IOps ops;
if (fake) ops = new FakeOps();
else if (OperatingSystem.IsWindows()) ops = InteropOps.AttachToRunning();
else { Console.Error.WriteLine("PowerPoint COM 은 Windows 에서만 붙습니다 — 여기서는 --fake 로 규약만 돌려 볼 수 있습니다."); return 2; }

var client = new HelperClient(helperUrl);
Console.WriteLine($"magi-ppt-hand: {ops.Label} ({ops.DocumentKey}) → {helperUrl}");
var backoff = 1000;
while (!cts.IsCancellationRequested)
{
    try
    {
        await client.FetchTokenAsync(cts.Token);
        Hand? hand = null;
        await foreach (var f in client.StreamAsync(ops.DocumentKey, ops.Label, cts.Token))
        {
            backoff = 1000;
            if (f.Event == "hello")
            {
                var hello = JsonSerializer.Deserialize<Hello>(f.Data, Json.Options)!;
                hand = new Hand(ops, hello.Epoch, hello.Document);
                Console.WriteLine($"붙었습니다 — 문서 {hello.Document} · epoch {hello.Epoch}");
                continue;
            }
            if (f.Event != "call" || hand is null) continue;
            var call = JsonSerializer.Deserialize<HandCall>(f.Data, Json.Options)!;
            var reply = hand.Handle(call);
            Console.WriteLine($"{call.Op} → {(reply.Error is null ? "ok" : "error: " + reply.Error)}");
            await client.ReplyAsync(reply, cts.Token);
        }
        Console.WriteLine("스트림이 끝났습니다 — 다시 붙습니다");
    }
    catch (OperationCanceledException) { break; }
    catch (Exception e)
    {
        Console.Error.WriteLine($"헬퍼에 못 붙었습니다: {e.Message} — {backoff / 1000}초 뒤 다시");
    }
    try { await Task.Delay(backoff, cts.Token); } catch (OperationCanceledException) { break; }
    backoff = Math.Min(backoff * 2, 15000);
}
return 0;
