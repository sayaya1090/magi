using System.Diagnostics;
using System.Net.Sockets;
using System.Reflection;
using System.Runtime.InteropServices;
using System.Text;

namespace Magi.Office.Start;

/// <summary>
/// Office 애플리케이션 기동 시 헬퍼 프로세스(<c>magi office</c>)의 실행 여부를 감지하고 자동 기동하는 COM 추가 기능입니다.
///
/// 도입 배경:
/// 작업창(웹 애드인) UI 자산은 로컬 헬퍼 프로세스가 제공하므로, 헬퍼가 사전 기동되어 있지 않으면 빈 창이 표시됩니다.
/// 시스템 로그인 시 자동 등록(<c>Run</c> 키) 방식을 배제하고, Office 프로세스 실행 수명 주기에 맞춰 인프로세스 COM 추가 기능으로
/// 헬퍼를 온디맨드 구동합니다. Office 실행 시 헬퍼가 기동되고, 비실행 시 계정 내 잔여 프로세스가 상주하지 않습니다.
///
/// 동작 원칙:
/// 1) 기동 상태 확인: 포트 26411이 이미 수신 대기 중이면 중복 실행을 생략하여 단일 인스턴스를 유지합니다.
/// 2) 상호 배제: 여러 오피스 호스트가 동시 기동될 때 경쟁 상태를 방지하기 위해 네임드 뮤텍스(<c>Local\magi-office-start</c>)를 사용합니다.
/// 3) 무예외 보장: 처리되지 않은 예외 발생 시 Office가 <c>LoadBehavior</c>를 2로 강등하므로, 모든 내부 예외를 포착하여 진단 로그에 기록합니다.
///
/// vtable 무결성:
/// 관리 코드 진입 전 언매니지드 COM vtable 디스패치 단계에서 예외가 발생할 경우 프로세스 충돌로 이어지므로,
/// <see cref="IDTExtensibility2"/>의 vtable 슬롯 배치를 엄격히 준수합니다.
/// </summary>
[ComVisible(true)]
[Guid("38162D7F-4C03-4B36-9F55-15D83EEA5EF3")]
[ProgId(ProgIdName)]
[ClassInterface(ClassInterfaceType.None)]
public sealed class Starter : IDTExtensibility2, ICustomQueryInterface
{
    public const string ProgIdName = "Magi.Office.Start";

    /// <summary>헬퍼 수신 포트 번호. 매니페스트 및 인증서 구성과 일치해야 합니다.</summary>
    public const int HelperPort = 26411;

    private const int S_OK = 0;
    private const int E_NOTIMPL = unchecked((int)0x80004001);
    private const int DISP_E_MEMBERNOTFOUND = unchecked((int)0x80020003);

    private static readonly Guid IID_IDispatch = new("00020400-0000-0000-C000-000000000046");

    // ── IDTExtensibility2 DISPID (표준 타입 라이브러리 정의 값) ──────────────────────────────────
    private const int DispidOnConnection = 1;
    private const int DispidOnDisconnection = 2;
    private const int DispidOnAddInsUpdate = 3;
    private const int DispidOnStartupComplete = 4;
    private const int DispidOnBeginShutdown = 5;

    // ── IDispatch 전면 슬롯 (Dual 인터페이스 vtable 오프셋 보정용) ──────────────────────────────
    // Dual 인터페이스의 vtable은 IDispatch 메서드로 시작하므로, 아래 4개 슬롯이 누락되면
    // Office 호스트가 호출하는 OnConnection의 vtable 오프셋이 4슬롯 어긋나는 결함이 발생합니다.
    // .NET Core의 EnableComHosting은 타입 라이브러리(TLB) 자동 생성을 지원하지 않으므로 직접 구현합니다.

    public int GetTypeInfoCount(out uint pctinfo)
    {
        pctinfo = 0;   // 타입 정보가 지원되지 않음을 명시적으로 반환합니다.
        return S_OK;
    }

    public int GetTypeInfo(uint iTInfo, uint lcid, out IntPtr ppTInfo)
    {
        ppTInfo = IntPtr.Zero;
        return E_NOTIMPL;
    }

    /// <summary>
    /// DISPID 조회 메서드입니다. 호스트의 늦은 바인딩(Late-binding) 호출을 지원하기 위해 5개 메서드 식별자를 반환합니다.
    /// 인식할 수 없는 식별자의 경우 <c>DISP_E_MEMBERNOTFOUND</c>를 반환합니다.
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
    /// 늦은 바인딩 디스패치 호출을 처리합니다. 매개변수 마샬링 위험을 방지하기 위해 전달 인자를 역직렬화하지 않고
    /// 실행 이벤트 수신 여부만 감지하여 헬퍼 프로세스를 확인합니다. 반환값은 항상 <c>S_OK</c>를 유지합니다.
    /// </summary>
    public int Invoke(int dispIdMember, ref Guid riid, uint lcid, ushort wFlags, IntPtr pDispParams,
                      IntPtr pVarResult, IntPtr pExcepInfo, IntPtr puArgErr)
    {
        if (dispIdMember is DispidOnConnection or DispidOnStartupComplete) Safely(EnsureHelper);
        return S_OK;
    }

    // ── IDTExtensibility2 메서드 구현 ──────────────────────────────────────────────────────────
    // 매개변수는 IntPtr로 선언하여 불필요한 SAFEARRAY/VARIANT 마샬링 오버헤드를 배제합니다.

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
    /// <c>IID_IDispatch</c> 질의 시 동일한 Dual vtable 포인터를 직접 반환하여,
    /// 타입 라이브러리 등록을 요구하는 CLR 기본 디스패치 처리기로 분기하는 결함을 방지합니다.
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
            Log("IDispatch 인터페이스 반환 실패: " + e.Message);
            ppv = IntPtr.Zero;
            return CustomQueryInterfaceResult.NotHandled;
        }
    }

    /// <summary>헬퍼 프로세스가 실행 중이 아닐 경우 기동합니다. 이미 수신 대기 중이면 동작을 생략합니다.</summary>
    internal static void EnsureHelper()
    {
        if (PortIsOpen(HelperPort)) return;
        // 여러 오피스 프로그램이 동시 실행될 경우 단일 인스턴스만 기동하도록 뮤텍스로 동기화합니다.
        using var only = new Mutex(false, @"Local\magi-office-start");
        var mine = false;
        try { mine = only.WaitOne(TimeSpan.FromSeconds(20)); } catch (AbandonedMutexException) { mine = true; }
        try
        {
            if (PortIsOpen(HelperPort)) return;
            var exe = HelperExe();
            if (exe is null) { Log("헬퍼 실행 파일을 찾지 못했습니다"); return; }
            var psi = new ProcessStartInfo(exe)
            {
                Arguments = HelperArgs(exe),
                WorkingDirectory = Path.GetDirectoryName(exe)!,
                UseShellExecute = false,
                CreateNoWindow = true,
            };
            Process.Start(psi);
            Log($"헬퍼를 띄웠습니다: {exe} {psi.Arguments}");
            // 프로세스 기동 완료를 최대 5초간 대기합니다.
            for (var i = 0; i < 20 && !PortIsOpen(HelperPort); i++) Thread.Sleep(250);
        }
        finally { if (mine) only.ReleaseMutex(); }
    }

    /// <summary>헬퍼 바이너리 경로를 탐색합니다. 설치 디렉토리 구조상 상위 디렉토리 및 로컬 앱 데이터를 우선 확인합니다.</summary>
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
    /// 헬퍼 기동 명령줄 인자를 반환합니다. DLL 인접 경로의 helper-args.txt 파일이 존재하면 해당 인자를 사용하며,
    /// 부재 시 기본값 "office"를 사용합니다.
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
        catch { /* 기본값 폴백 */ }
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

    /// <summary>예외가 외부 COM 호스트로 누출되지 않도록 안전하게 실행합니다.</summary>
    private static void Safely(Action run)
    {
        try { run(); }
        catch (Exception e) { Log("실패: " + e.Message); }
    }

    /// <summary>
    /// 현재 DLL이 위치한 디렉토리 경로를 반환합니다. 단일 파일 배포 환경을 고려하여 BaseDirectory를 함께 확인합니다.
    /// </summary>
    private static string? HereDir()
    {
        try
        {
            var at = Assembly.GetExecutingAssembly().Location;
            if (!string.IsNullOrEmpty(at)) return Path.GetDirectoryName(at);
        }
        catch { /* 폴백 */ }
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
        catch { /* 진단 로깅 실패 시 무시 */ }
    }
}

/// <summary>
/// Office 호스트가 추가 기능과 통신하기 위한 표준 COM 인터페이스 규약입니다.
///
/// Dual 인터페이스 구조로 vtable이 IUnknown(3개) + IDispatch(4개) + IDTExtensibility2 메서드(5개)로 구성됩니다.
/// 따라서 <c>InterfaceIsIUnknown</c>으로 선언하고 4개의 IDispatch 슬롯을 명시적으로 배치해야 정상 동작합니다.
/// <c>InterfaceIsIDispatch</c>로 선언할 경우 CLR이 7슬롯 축약 vtable을 생성하여, Office 호스트가 8번째 슬롯에서
/// <c>OnConnection</c>을 호출할 때 잘못된 메모리 주소로 분기하여 <c>AccessViolationException</c>(0xc0000005) 크래시가 발생합니다.
///
/// 매개변수를 <c>IntPtr</c>로 선언하고 <c>[PreserveSig]</c>로 HRESULT를 직접 반환함으로써
/// 불필요한 VARIANT/SAFEARRAY 마샬링 오버헤드 및 런타임 호환성 문제를 배제합니다.
/// </summary>
[ComVisible(true)]
[Guid("B65AD801-ABAF-11D0-BB8B-00A0C90F2744")]
[InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IDTExtensibility2
{
    // IDispatch — Dual 인터페이스 전면 슬롯. 해당 슬롯이 누락되면 하위 5개 메서드의 vtable 오프셋이 어긋납니다.
    [PreserveSig] int GetTypeInfoCount(out uint pctinfo);
    [PreserveSig] int GetTypeInfo(uint iTInfo, uint lcid, out IntPtr ppTInfo);
    [PreserveSig] int GetIDsOfNames(ref Guid riid, IntPtr rgszNames, uint cNames, uint lcid, IntPtr rgDispId);
    [PreserveSig] int Invoke(int dispIdMember, ref Guid riid, uint lcid, ushort wFlags, IntPtr pDispParams,
                             IntPtr pVarResult, IntPtr pExcepInfo, IntPtr puArgErr);

    // IDTExtensibility2 메서드
    [PreserveSig] int OnConnection(IntPtr application, int connectMode, IntPtr addInInst, IntPtr custom);
    [PreserveSig] int OnDisconnection(int removeMode, IntPtr custom);
    [PreserveSig] int OnAddInsUpdate(IntPtr custom);
    [PreserveSig] int OnStartupComplete(IntPtr custom);
    [PreserveSig] int OnBeginShutdown(IntPtr custom);
}
