package office

import (
	"os"
	"strings"
	"testing"
)

// Office 를 켤 때 헬퍼를 띄우는 COM 추가 기능(`addin-com/src/Starter.cs`)의 **vtable 모양**을 잰다.
//
// 왜 소스를 글자로 읽는가 — 이 결함은 Go 로도 C# 로도 못 잡는다. 틀리면 관리 코드에 닿기 전에 **프로세스가
// 죽기** 때문이다. 2026-09-07 실물(LTSC 2021): 이 인터페이스가 `InterfaceIsIDispatch` 로 선언돼 있어서 CLR 이
// IDispatch 일곱 슬롯짜리 vtable 만 만들었고, Office 가 `OnConnection` 을 여덟째 슬롯에서 부르자 빈 자리로 뛰어
// **PowerPoint 가 통째로 죽었다**(`AccessViolationException`, 켠 지 20초쯤 뒤 창이 사라진다). Office 는
// 「'magi.office.start' 추가 기능을 사용할 경우 문제가 발생합니다」를 이벤트 로그에 남겼다.
//
// `IDTExtensibility2` 는 **dual** 인터페이스다 — vtable 이 IUnknown(3) + IDispatch(4) + 메서드(5) 다. 그래서
// `InterfaceIsIUnknown` 으로 선언하고 IDispatch 넷을 손으로 앞에 적어야 자리가 맞는다. 이 시험이 그 둘을 문다.
//
// 이 파일이 사라지거나 선언이 되돌아가면 **사람의 Office 가 죽는다.** 재는 자리를 여기 두는 이유가 그것이다.
func TestTheComAddinKeepsTheDualVtableShape(t *testing.T) {
	const at = "../addin-com/src/Starter.cs"
	src, err := os.ReadFile(at)
	if err != nil {
		// 옆에 없으면 건너뛰지 않는다 — 건너뛴 시험은 화면에서 초록과 구별이 안 되고, 이 시험이 지키는 것은
		// 「사람의 Office 가 안 죽는다」다.
		t.Fatalf("%s 를 못 읽었다: %v", at, err)
	}
	s := string(src)

	i := strings.Index(s, "public interface IDTExtensibility2")
	if i < 0 {
		t.Fatal("Starter.cs 에 IDTExtensibility2 선언이 없다")
	}
	// 선언 바로 위의 특성 줄들을 본다.
	head := s[:i]
	if j := strings.LastIndex(head, "[ComVisible(true)]"); j >= 0 {
		head = head[j:]
	}

	if strings.Contains(head, "InterfaceIsIDispatch") {
		t.Error("IDTExtensibility2 가 InterfaceIsIDispatch 로 선언돼 있다 — CLR 이 IDispatch 일곱 슬롯만 만들어 " +
			"Office 가 OnConnection 을 부르는 순간 프로세스가 죽는다(2026-09-07 실물). InterfaceIsIUnknown 이어야 한다")
	}
	if !strings.Contains(head, "InterfaceIsIUnknown") {
		t.Error("IDTExtensibility2 는 InterfaceIsIUnknown 으로 선언해야 한다 — dual 인터페이스의 vtable 을 우리가 직접 맞춘다")
	}

	// IDispatch 넷이 IDTExtensibility2 다섯보다 **앞에** 있어야 한다. 하나라도 빠지면 그 뒤 전부가 한 칸씩 밀린다.
	body := s[i:]
	if e := strings.Index(body, "\n}"); e > 0 {
		body = body[:e]
	}
	want := []string{
		"GetTypeInfoCount", "GetTypeInfo", "GetIDsOfNames", "Invoke",
		"OnConnection", "OnDisconnection", "OnAddInsUpdate", "OnStartupComplete", "OnBeginShutdown",
	}
	at0 := 0
	for n, name := range want {
		k := strings.Index(body[at0:], name)
		if k < 0 {
			t.Fatalf("IDTExtensibility2 의 %d번째 자리에 %s 가 없다 — 자리가 빠지면 그 뒤 슬롯이 전부 밀린다", n+1, name)
		}
		at0 += k + len(name)
	}

	// HRESULT 를 직접 답해야 한다 — 던지면 Office 가 LoadBehavior 를 2 로 내린다.
	if strings.Count(body, "[PreserveSig]") < len(want) {
		t.Errorf("아홉 메서드가 전부 [PreserveSig] 여야 한다 — 지금 %d개", strings.Count(body, "[PreserveSig]"))
	}
	// 인자를 푸는 순간 VARIANT/SAFEARRAY 마샬링을 탄다. 우리는 인자를 하나도 안 쓴다.
	if strings.Contains(body, "ref Array") || strings.Contains(body, "object application") {
		t.Error("인자는 전부 IntPtr 이어야 한다 — object·ref Array 는 .NET (Core) 에서 안 열린 마샬링을 탄다")
	}
}

// 이 추가 기능이 **로그인 등록 없이** 헬퍼를 띄우는 유일한 자리라는 것을 못박는다(사용자 요구 2026-09-07:
// 「윈도우 로그인 때 자동 켜지는 거 하지 말라고」, 「오피스를 직접 켜는 것 외에 사용자가 따로 켜거나 관리하는
// 프로그램은 없어야 한다」). 셋을 한꺼번에 켜도 헬퍼는 하나여야 하므로 포트 검사와 뮤텍스가 둘 다 있어야 한다.
func TestTheComAddinStartsOneHelperAndNothingElse(t *testing.T) {
	src, err := os.ReadFile("../addin-com/src/Starter.cs")
	if err != nil {
		t.Fatalf("Starter.cs 를 못 읽었다: %v", err)
	}
	s := string(src)
	for _, want := range []string{
		`PortIsOpen(HelperPort)`,  // 이미 떠 있으면 아무것도 안 한다
		`Local\magi-office-start`, // 셋이 동시에 와도 하나만 띄운다
		`CreateNoWindow = true`,   // 검은 창을 안 띄운다
		`UseShellExecute = false`, // 콘솔을 물려주지 않는다
	} {
		if !strings.Contains(s, want) {
			t.Errorf("Starter.cs 에 %q 가 없다", want)
		}
	}
	if strings.Contains(s, `CurrentVersion\Run`) {
		t.Error("이 추가 기능은 로그인 등록을 건드리지 않는다 — 그것을 없애려고 있는 물건이다")
	}
}
