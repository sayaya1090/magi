using System.Diagnostics;
using System.Net.Sockets;
using System.Reflection;
using System.Runtime.InteropServices;
using System.Text;

namespace Magi.Office.Start;

/// <summary>
/// Office 가 뜰 때 **헬퍼를 띄우는** COM 추가 기능. 그것 말고는 아무것도 안 한다.
///
/// 왜 있나 — 작업창(웹 애드인)의 페이지는 헬퍼가 내준다. 그래서 헬퍼가 먼저 떠 있지 않으면 사람이 리본의 Magi 를
/// 눌러도 빈 창이 뜬다. 여태는 그것을 로그인 등록(Run 키)으로 풀었는데, 사용자가 그것을 물렸다(2026-09-07:
/// 「윈도우 로그인 때 자동 켜지는 거 하지 말라고」, 「오피스에서 플러그인 켤 때 COM 이랑 .NET 으로 프로세스 못 띄우냐」).
/// COM 추가 기능은 <b>Office 프로세스 안에서</b> 뜨므로 그 자리가 정확히 맞다: Office 를 켜면 헬퍼가 서고, Office 를
/// 안 켜면 이 계정에 magi 는 하나도 없다.
///
/// 규칙 셋. ① 이미 떠 있으면(포트가 열려 있으면) 아무것도 안 한다 — 프로그램 셋이 동시에 떠도 헬퍼는 하나다.
/// ② 뮤텍스로 그 「동시에」를 막는다 — 포트를 보고 띄우기까지 사이가 있다. ③ <b>무슨 일이 있어도 안 던진다</b> —
/// 추가 기능이 던지면 Office 가 LoadBehavior 를 2 로 내려 다음부터 아예 안 부른다. 사유는 로그에 적는다.
/// </summary>
[ComVisible(true)]
[Guid("38162D7F-4C03-4B36-9F55-15D83EEA5EF3")]
[ProgId(ProgIdName)]
[ClassInterface(ClassInterfaceType.None)]
public sealed class Starter : IDTExtensibility2
{
    public const string ProgIdName = "Magi.Office.Start";

    /// <summary>헬퍼가 듣는 자리. 매니페스트·인증서와 같은 한 문자열이다.</summary>
    public const int HelperPort = 3000;

    public void OnConnection(object application, int connectMode, object addInInst, ref Array custom) => Safely(EnsureHelper);
    public void OnDisconnection(int removeMode, ref Array custom) { }
    public void OnAddInsUpdate(ref Array custom) { }
    public void OnStartupComplete(ref Array custom) => Safely(EnsureHelper);
    public void OnBeginShutdown(ref Array custom) { }

    /// <summary>헬퍼가 없으면 띄운다. 이미 있으면 아무것도 안 한다.</summary>
    internal static void EnsureHelper()
    {
        if (PortIsOpen(HelperPort)) return;
        // 프로그램 셋을 한꺼번에 켜면 셋이 같은 순간에 여기 온다. 먼저 잡은 하나만 띄우고, 나머지는 그 뒤에 포트를 다시 본다.
        using var only = new Mutex(false, @"Local\magi-office-start");
        var mine = false;
        try { mine = only.WaitOne(TimeSpan.FromSeconds(20)); } catch (AbandonedMutexException) { mine = true; }
        try
        {
            if (PortIsOpen(HelperPort)) return;
            var exe = HelperExe();
            if (exe is null) { Log("헬퍼 실행 파일을 못 찾았습니다"); return; }
            var psi = new ProcessStartInfo(exe)
            {
                Arguments = HelperArgs(exe),
                WorkingDirectory = Path.GetDirectoryName(exe)!,
                UseShellExecute = false,
                CreateNoWindow = true,
            };
            Process.Start(psi);
            Log($"헬퍼를 띄웠습니다: {exe} {psi.Arguments}");
            // 곧 뜬다. 여기서 오래 기다리면 Office 가 그만큼 늦게 그려진다 — 짧게만 본다.
            for (var i = 0; i < 20 && !PortIsOpen(HelperPort); i++) Thread.Sleep(250);
        }
        finally { if (mine) only.ReleaseMutex(); }
    }

    /// <summary>헬퍼는 이 DLL 의 **위 폴더**에 있다 — 설치기가 `<Dest>\start\` 에 이것을, `<Dest>\magi.exe` 에 헬퍼를 놓는다.</summary>
    internal static string? HelperExe()
    {
        foreach (var at in Candidates())
        {
            if (at is not null && File.Exists(at)) return at;
        }
        return null;
    }

    private static IEnumerable<string?> Candidates()
    {
        var here = Path.GetDirectoryName(Assembly.GetExecutingAssembly().Location);
        if (!string.IsNullOrEmpty(here))
        {
            yield return Path.GetFullPath(Path.Combine(here, "..", "magi.exe"));
            yield return Path.Combine(here, "magi.exe");
        }
        var local = Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData);
        if (!string.IsNullOrEmpty(local)) yield return Path.Combine(local, "magi", "office", "magi.exe");
    }

    /// <summary>
    /// 명령줄은 설치기가 적어 둔다(`helper-args.txt`, DLL 옆). 설정·소켓 자리가 그 머신마다 다르고, 그것을 아는 것은
    /// 설치기이기 때문이다. 없으면 기본값으로 — 헬퍼는 설정 디렉토리와 MAGI_SOCKET_DIR 을 스스로 안다.
    /// </summary>
    internal static string HelperArgs(string exe)
    {
        try
        {
            var here = Path.GetDirectoryName(Assembly.GetExecutingAssembly().Location);
            var at = here is null ? null : Path.Combine(here, "helper-args.txt");
            if (at is not null && File.Exists(at))
            {
                var line = File.ReadAllText(at, Encoding.UTF8).Trim();
                if (line.Length > 0) return line;
            }
        }
        catch { /* 아래 기본값으로 */ }
        return "office";
    }

    internal static bool PortIsOpen(int port)
    {
        try
        {
            using var tcp = new TcpClient();
            return tcp.ConnectAsync("127.0.0.1", port).Wait(TimeSpan.FromMilliseconds(700)) && tcp.Connected;
        }
        catch { return false; }
    }

    /// <summary>추가 기능은 던지면 안 된다 — Office 가 LoadBehavior 를 2 로 내려 다음부터 안 부른다.</summary>
    private static void Safely(Action run)
    {
        try { run(); }
        catch (Exception e) { Log("실패: " + e.Message); }
    }

    private static void Log(string what)
    {
        try
        {
            var here = Path.GetDirectoryName(Assembly.GetExecutingAssembly().Location);
            if (here is null) return;
            File.AppendAllText(Path.Combine(here, "start.log"),
                $"{DateTime.Now:yyyy-MM-dd HH:mm:ss} [{Process.GetCurrentProcess().ProcessName}] {what}{Environment.NewLine}", Encoding.UTF8);
        }
        catch { /* 로그도 못 적으면 할 것이 없다 */ }
    }
}

/// <summary>
/// Office 가 추가 기능에게 말을 거는 규약. IID 는 정해진 값이라 그대로 적는다(Extensibility PIA 를 안 끌어오려고
/// 여기서 선언한다 — 우리가 쓰는 것은 <c>OnConnection</c> 하나다).
/// </summary>
[ComVisible(true)]
[Guid("B65AD801-ABAF-11D0-BB8B-00A0C90F2744")]
[InterfaceType(ComInterfaceType.InterfaceIsIDispatch)]
public interface IDTExtensibility2
{
    void OnConnection(object application, int connectMode, object addInInst, ref Array custom);
    void OnDisconnection(int removeMode, ref Array custom);
    void OnAddInsUpdate(ref Array custom);
    void OnStartupComplete(ref Array custom);
    void OnBeginShutdown(ref Array custom);
}
