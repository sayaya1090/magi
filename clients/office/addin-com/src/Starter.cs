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
///
/// ⚠ ③ 은 관리 코드 안에서만 참이다. <b>vtable 이 틀리면 던지기 전에 죽는다</b> — 2026-09-07 실물(LTSC 2021)에서
/// 이 추가 기능이 PowerPoint 를 통째로 죽였다(`AccessViolationException`, Office 는 「'magi.office.start' 추가 기능을
/// 사용할 경우 문제가 발생합니다」를 남겼다). 사유는 <see cref="IDTExtensibility2"/> 에 적어 뒀다.
/// </summary>
[ComVisible(true)]
[Guid("38162D7F-4C03-4B36-9F55-15D83EEA5EF3")]
[ProgId(ProgIdName)]
[ClassInterface(ClassInterfaceType.None)]
public sealed class Starter : IDTExtensibility2, ICustomQueryInterface
{
    public const string ProgIdName = "Magi.Office.Start";

    /// <summary>헬퍼가 듣는 자리. 매니페스트·인증서와 같은 한 문자열이다.</summary>
    public const int HelperPort = 3000;

    private const int S_OK = 0;
    private const int E_NOTIMPL = unchecked((int)0x80004001);
    private const int DISP_E_MEMBERNOTFOUND = unchecked((int)0x80020003);

    private static readonly Guid IID_IDispatch = new("00020400-0000-0000-C000-000000000046");

    // ── IDTExtensibility2 의 DISPID. 타입 라이브러리가 정한 값이라 그대로 적는다. ────────────────
    private const int DispidOnConnection = 1;
    private const int DispidOnDisconnection = 2;
    private const int DispidOnAddInsUpdate = 3;
    private const int DispidOnStartupComplete = 4;
    private const int DispidOnBeginShutdown = 5;

    // ── IDispatch 앞머리 ────────────────────────────────────────────────────────────────────────
    // 이 넷은 **자리를 채우려고** 있다. dual 인터페이스의 vtable 은 IDispatch 로 시작하므로, 이 넷이 없으면
    // Office 가 부르는 OnConnection 의 슬롯 번호가 넷 밀린다. 넷 다 우리가 직접 구현한다 — .NET (Core) 의
    // IDispatch 는 타입 라이브러리를 요구하는데(`Typelib export: Type library is not registered`),
    // `EnableComHosting` 은 TLB 를 만들지도 등록하지도 않는다.

    public int GetTypeInfoCount(out uint pctinfo)
    {
        pctinfo = 0;   // 타입 정보가 없다고 정직하게 답한다. 있다고 하면 GetTypeInfo 로 다시 온다.
        return S_OK;
    }

    public int GetTypeInfo(uint iTInfo, uint lcid, out IntPtr ppTInfo)
    {
        ppTInfo = IntPtr.Zero;
        return E_NOTIMPL;
    }

    /// <summary>
    /// 이름 → DISPID. Office 는 dual 인터페이스를 대개 vtable 로 부르지만, 늦은 바인딩으로 오는 호스트도 있어
    /// 다섯 이름은 답해 준다. 모르는 이름은 <c>DISP_E_MEMBERNOTFOUND</c> — 짐작해서 아무 번호나 주지 않는다.
    /// </summary>
    public int GetIDsOfNames(ref Guid riid, IntPtr rgszNames, uint cNames, uint lcid, IntPtr rgDispId)
    {
        try
        {
            if (rgszNames == IntPtr.Zero || rgDispId == IntPtr.Zero || cNames == 0) return E_NOTIMPL;
            var found = S_OK;
            for (var i = 0; i < cNames; i++)
            {
                var namePtr = Marshal.ReadIntPtr(rgszNames, i * IntPtr.Size);
                var name = namePtr == IntPtr.Zero ? null : Marshal.PtrToStringUni(namePtr);
                var id = DispidOf(name);
                if (id == 0) found = DISP_E_MEMBERNOTFOUND;
                Marshal.WriteInt32(rgDispId, i * sizeof(int), id == 0 ? -1 : id);
            }
            return found;
        }
        catch (Exception e) { Log("GetIDsOfNames 실패: " + e.Message); return E_NOTIMPL; }
    }

    private static int DispidOf(string? name) => name switch
    {
        "OnConnection" => DispidOnConnection,
        "OnDisconnection" => DispidOnDisconnection,
        "OnAddInsUpdate" => DispidOnAddInsUpdate,
        "OnStartupComplete" => DispidOnStartupComplete,
        "OnBeginShutdown" => DispidOnBeginShutdown,
        _ => 0,
    };

    /// <summary>
    /// 늦은 바인딩으로 오는 호출. 인자는 안 읽는다 — 우리가 쓰는 것은 「불렸다」는 사실뿐이고, VARIANT 를 푸는 것은
    /// 위험만 늘린다. <b>S_OK 만 답한다</b>: 여기서 실패를 돌려주면 Office 가 추가 기능을 끈다.
    /// </summary>
    public int Invoke(int dispIdMember, ref Guid riid, uint lcid, ushort wFlags, IntPtr pDispParams,
                      IntPtr pVarResult, IntPtr pExcepInfo, IntPtr puArgErr)
    {
        if (dispIdMember is DispidOnConnection or DispidOnStartupComplete) Safely(EnsureHelper);
        return S_OK;
    }

    // ── IDTExtensibility2 ───────────────────────────────────────────────────────────────────────
    // 인자는 전부 `IntPtr` 다. 원래 시그니처는 `object`·`ref Array` 인데, 그 마샬링은 SAFEARRAY/VARIANT 규약을
    // 타고 .NET (Core) 에서 안 열린 자리가 있다. **우리는 인자를 하나도 안 쓴다** — 안 푸는 것이 제일 안전하다.

    public int OnConnection(IntPtr application, int connectMode, IntPtr addInInst, IntPtr custom)
    {
        Safely(EnsureHelper);
        return S_OK;
    }

    public int OnDisconnection(int removeMode, IntPtr custom) => S_OK;

    public int OnAddInsUpdate(IntPtr custom) => S_OK;

    public int OnStartupComplete(IntPtr custom)
    {
        Safely(EnsureHelper);
        return S_OK;
    }

    public int OnBeginShutdown(IntPtr custom) => S_OK;

    /// <summary>
    /// <c>IID_IDispatch</c> 로 물으면 **우리 vtable** 을 준다. dual 인터페이스라 앞머리가 IDispatch 이므로 그게 맞는
    /// 답이고, 이렇게 해야 늦은 바인딩 호출이 CLR 의 IDispatch(타입 라이브러리를 요구해 실패하는 그것)로 안 간다.
    /// </summary>
    public CustomQueryInterfaceResult GetInterface(ref Guid iid, out IntPtr ppv)
    {
        ppv = IntPtr.Zero;
        if (iid != IID_IDispatch) return CustomQueryInterfaceResult.NotHandled;
        try
        {
            ppv = Marshal.GetComInterfaceForObject(this, typeof(IDTExtensibility2), CustomQueryInterfaceMode.Ignore);
            return CustomQueryInterfaceResult.Handled;
        }
        catch (Exception e)
        {
            Log("IDispatch 를 못 내줬습니다: " + e.Message);
            ppv = IntPtr.Zero;
            return CustomQueryInterfaceResult.NotHandled;
        }
    }

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
        var here = HereDir();
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
            var here = HereDir();
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

    /// <summary>
    /// 이 DLL 이 있는 폴더. `Assembly.Location` 은 단일 파일 배포에서 빈 문자열이라 <see cref="AppContext.BaseDirectory"/>
    /// 를 먼저 본다 — 로그와 `helper-args.txt` 가 둘 다 이 자리에 걸려 있다.
    /// </summary>
    private static string? HereDir()
    {
        try
        {
            var at = Assembly.GetExecutingAssembly().Location;
            if (!string.IsNullOrEmpty(at)) return Path.GetDirectoryName(at);
        }
        catch { /* 아래로 */ }
        try { return AppContext.BaseDirectory; } catch { return null; }
    }

    private static void Log(string what)
    {
        try
        {
            var here = HereDir();
            if (here is null) return;
            File.AppendAllText(Path.Combine(here, "start.log"),
                $"{DateTime.Now:yyyy-MM-dd HH:mm:ss} [{Process.GetCurrentProcess().ProcessName}] {what}{Environment.NewLine}", Encoding.UTF8);
        }
        catch { /* 로그도 못 적으면 할 것이 없다 */ }
    }
}

/// <summary>
/// Office 가 추가 기능에게 말을 거는 규약. IID 는 정해진 값이라 그대로 적는다(Extensibility PIA 를 안 끌어오려고
/// 여기서 선언한다).
///
/// <b>이것은 dual 인터페이스다 — vtable 이 IUnknown(3) + IDispatch(4) + 메서드(5) 다.</b> 그래서
/// <c>InterfaceIsIUnknown</c> 으로 선언하고 IDispatch 넷을 <b>손으로 앞에 적는다</b>. 앞 판은
/// <c>InterfaceIsIDispatch</c> 였는데, 그러면 CLR 이 IDispatch 일곱 슬롯짜리 vtable 만 만든다 — Office 가
/// <c>OnConnection</c> 을 여덟째 슬롯에서 부르면 <b>빈 자리로 뛰어 프로세스가 죽는다.</b> 2026-09-07 에 실물
/// LTSC 2021 에서 PowerPoint 가 그렇게 죽었다(`AccessViolationException`, 20초쯤 뒤 창이 사라진다). 관리 코드에
/// 닿기 전에 죽는 것이라 <c>try/catch</c> 로는 못 막는다 — <b>막는 자리는 이 선언 하나뿐이다.</b>
///
/// 인자를 전부 <c>IntPtr</c> 로 두고 <c>[PreserveSig]</c> 로 HRESULT 를 직접 답하는 것도 같은 이유다: 원래
/// 시그니처의 <c>object</c>·<c>ref Array</c> 는 VARIANT/SAFEARRAY 마샬링을 타는데, 우리는 그 인자를 하나도 안 쓴다.
/// 안 푸는 것이 제일 안전하다.
/// </summary>
[ComVisible(true)]
[Guid("B65AD801-ABAF-11D0-BB8B-00A0C90F2744")]
[InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IDTExtensibility2
{
    // IDispatch — dual 인터페이스의 앞머리. 자리를 비우면 아래 다섯의 슬롯이 밀린다.
    [PreserveSig] int GetTypeInfoCount(out uint pctinfo);
    [PreserveSig] int GetTypeInfo(uint iTInfo, uint lcid, out IntPtr ppTInfo);
    [PreserveSig] int GetIDsOfNames(ref Guid riid, IntPtr rgszNames, uint cNames, uint lcid, IntPtr rgDispId);
    [PreserveSig] int Invoke(int dispIdMember, ref Guid riid, uint lcid, ushort wFlags, IntPtr pDispParams,
                             IntPtr pVarResult, IntPtr pExcepInfo, IntPtr puArgErr);

    // IDTExtensibility2
    [PreserveSig] int OnConnection(IntPtr application, int connectMode, IntPtr addInInst, IntPtr custom);
    [PreserveSig] int OnDisconnection(int removeMode, IntPtr custom);
    [PreserveSig] int OnAddInsUpdate(IntPtr custom);
    [PreserveSig] int OnStartupComplete(IntPtr custom);
    [PreserveSig] int OnBeginShutdown(IntPtr custom);
}
