using Magi.Ppt.Hand;

// magi-ppt-hand — Office 2021 용 COM 어댑터. 헬퍼(magi office 의 /ppt)에 붙어 call 을 받고 PowerPoint 를 COM 으로 움직인다.
//   magi-ppt-hand [--helper https://127.0.0.1:3000/ppt] [--presentation <파일 경로>] [--fake]
//
// **인자 없이 띄우면 열린 덱 전부를 맡는다**(Supervisor) — 덱마다 연결 하나, 닫히면 거두고, PowerPoint 가 끝나면 같이
// 끝난다. 이것을 띄우는 것은 헬퍼다(clients/office/helper/adapter.go): 로그인 때 뜨는 등록은 헬퍼 하나면 되고, 열린 덱을
// 세는 일은 COM 이 손에 있는 이 프로그램이 한다(2026-09-07 이전에는 PowerShell 감시기가 그 둘을 다 했다).
//
// --presentation 은 그 덱 하나에만 붙는다(개발·진단용). --fake 는 PowerPoint 없이 메모리 덱으로 붙는다(mac 에서도 돈다).
var helperUrl = "https://127.0.0.1:3000/ppt"; // magi office 는 한 포트에서 /ppt·/xl·/word 를 내준다 — 파워포인트 몫이 /ppt
var fake = false;
string? presentation = null;
for (var i = 0; i < args.Length; i++)
{
    if (args[i] == "--helper" && i + 1 < args.Length) helperUrl = args[++i];
    else if (args[i] == "--presentation" && i + 1 < args.Length) presentation = args[++i];
    else if (args[i] == "--fake") fake = true;
    else if (args[i] is "-h" or "--help") { Console.WriteLine("magi-ppt-hand [--helper URL] [--presentation PATH] [--fake]"); return 0; }
}

using var cts = new CancellationTokenSource();
Console.CancelKeyPress += (_, e) => { e.Cancel = true; cts.Cancel(); };

if (fake)
{
    await Connection.RunAsync(helperUrl, new FakeOps(), cts.Token);
    return 0;
}
if (!OperatingSystem.IsWindows())
{
    Console.Error.WriteLine("PowerPoint COM 은 Windows 에서만 붙습니다 — 여기서는 --fake 로 규약만 돌려 볼 수 있습니다.");
    return 2;
}
if (presentation is not null)
{
    await Connection.RunAsync(helperUrl, InteropOps.AttachToRunning(presentation), cts.Token);
    return 0;
}
return await Supervisor.RunAsync(helperUrl, cts.Token);
