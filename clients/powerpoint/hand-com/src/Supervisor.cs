using System.Diagnostics;
using System.Runtime.Versioning;

namespace Magi.Ppt.Hand;

/// <summary>
/// 어댑터 **하나가 열린 덱 전부**를 맡는다 — 덱마다 연결 하나.
///
/// 앞 판은 PowerShell 감시기(`hand-watch.ps1`)가 열린 덱을 세어 덱마다 프로세스를 하나씩 띄웠다. 그 감시기는
/// 로그인 때 뜨는 등록을 하나 더 쓰고, 관리자 권한으로 뜨면 보통 권한 PowerPoint 의 COM 을 못 봤고, 덱이 닫힐
/// 때마다 프로세스를 죽여야 했다. 사용자가 그 자리를 짚었다(2026-09-07: 「2021 은 그런 거 없어도 되잖아, 직접
/// 프로세스 띄울 수 있지 않아」) — 헬퍼는 이미 떠 있으니 헬퍼가 이것을 띄우면 되고, 열린 덱을 세는 일은 COM 이
/// 이미 손에 있는 **이 프로그램**이 제일 잘한다.
///
/// 그래서 규칙이 셋이다. ① PowerPoint 프로세스가 없어지면 끝낸다(헬퍼가 다음에 다시 띄운다). ② 열린 덱마다
/// 연결 하나, 닫힌 덱의 연결은 거둔다. ③ 헬퍼가 `bye` 로 물린 덱에는 **다시 안 붙는다** — 안 그러면 둘이
/// 번갈아 서로를 밀어낸다.
/// </summary>
[SupportedOSPlatform("windows")]
public static class Supervisor
{
    /// <summary>PowerPoint 가 떠 있는지 다시 보는 사이. 시험이 줄여 잡는다.</summary>
    public static TimeSpan Every = TimeSpan.FromSeconds(3);

    public static async Task<int> RunAsync(string helperUrl, CancellationToken ct)
    {
        var live = new Dictionary<string, (CancellationTokenSource Cts, Task<ConnectionEnd> Task)>();
        var dismissed = new HashSet<string>();
        Console.WriteLine("열린 덱을 맡습니다 — PowerPoint 가 끝나면 같이 끝냅니다.");
        while (!ct.IsCancellationRequested)
        {
            if (!PowerPointIsRunning())
            {
                Console.WriteLine("PowerPoint 가 없습니다 — 끝냅니다(다음에 헬퍼가 다시 띄웁니다).");
                break;
            }
            // null 은 「못 닿았다」이지 「덱이 없다」가 아니다 — 뜨는 중이거나 권한 수준이 다른 것이라, 그때는
            // 아무것도 거두지 않고 기다린다. 멀쩡한 연결을 죽이는 자리가 여기다.
            var decks = InteropOps.OpenDecks();
            if (decks is not null)
            {
                var open = new HashSet<string>();
                foreach (var path in decks) open.Add(DeckKey.Normalize(path));
                foreach (var path in decks)
                {
                    var key = DeckKey.Normalize(path);
                    if (live.ContainsKey(key) || dismissed.Contains(key)) continue;
                    IOps ops;
                    try { ops = InteropOps.AttachToRunning(path); }
                    catch (Exception e)
                    {
                        // 세는 사이에 닫혔거나 COM 이 답을 안 했다. 다음 바퀴에 다시 본다.
                        Console.Error.WriteLine($"덱에 못 붙었습니다({path}): {e.Message}");
                        continue;
                    }
                    var cts = CancellationTokenSource.CreateLinkedTokenSource(ct);
                    live[key] = (cts, Connection.RunAsync(helperUrl, ops, cts.Token));
                }
                foreach (var key in new List<string>(live.Keys))
                {
                    var (cts, task) = live[key];
                    if (!open.Contains(key))
                    {
                        Console.WriteLine($"덱이 닫혔습니다 — 연결을 거둡니다: {key}");
                        cts.Cancel();
                        live.Remove(key);
                        continue;
                    }
                    if (!task.IsCompleted) continue;
                    live.Remove(key);
                    if (task.Status == TaskStatus.RanToCompletion && task.Result == ConnectionEnd.Dismissed)
                    {
                        dismissed.Add(key);
                        Console.WriteLine($"헬퍼가 물린 덱입니다 — 다시 안 붙습니다: {key}");
                    }
                }
            }
            try { await Task.Delay(Every, ct); } catch (OperationCanceledException) { break; }
        }
        foreach (var (cts, _) in live.Values) cts.Cancel();
        return 0;
    }

    /// <summary>PowerPoint 프로세스가 하나라도 있나. COM 이 아니라 **프로세스**로 재는 자리다 — COM 은 뜨는 중에도 실패한다.</summary>
    public static bool PowerPointIsRunning() => Process.GetProcessesByName("POWERPNT").Length > 0;
}
