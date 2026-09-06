package main

import (
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// 인증서가 **하나의 이름**으로 난다(DESIGN.md §5.5 ⚠ — 넷이 같은 문자열이어야 한다).
func TestTheCertificateCoversTheOneName(t *testing.T) {
	dir := t.TempDir()
	cert, err := LoadOrCreateCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Leaf == nil {
		t.Fatal("leaf 가 없다")
	}
	if !coversHost(cert.Leaf) {
		t.Fatalf("SAN 이 %v / %v 다 — %q 를 덮어야 한다", cert.Leaf.IPAddresses, cert.Leaf.DNSNames, Host)
	}
	// 이름을 하나만 넣는다. 여기에 `localhost` 를 같이 넣으면 넷이 다섯이 되고, 다섯째는
	// 아무도 안 본다.
	if len(cert.Leaf.DNSNames) != 0 {
		t.Errorf("DNS 이름이 %v 로 같이 들어 있다", cert.Leaf.DNSNames)
	}
	if len(cert.Leaf.IPAddresses) != 1 {
		t.Errorf("IP SAN 이 %v 다", cert.Leaf.IPAddresses)
	}
}

// 두 번째 기동은 **같은 인증서를 다시 쓴다.** 매번 새로 내면 사람이 신뢰 저장소에 넣어 둔 것이
// 기동마다 죽고, 그 증상은 「어제는 됐는데 오늘 안 된다」다.
func TestTheCertificateSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadOrCreateCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	if Fingerprint(first) != Fingerprint(second) {
		t.Fatal("다시 뜨면서 인증서가 새로 났다")
	}
}

// 키는 0600 이다 — 그 한 줄이 §5.2 가 포트에서 잃었다고 적은 것을 되돌려 준다.
// **윈도우에서는 이 비트가 유닉스만큼 서지 않는다**(파일 모드가 ACL 로 안 옮겨진다). 그래서
// 이 시험은 유닉스에서만 무는데, **그 사실을 로그로 남긴다** — 안 남기면 「초록이었다」와
// 「이 플랫폼에서는 볼 것이 없었다」가 같은 글자가 된다(§9).
func TestThePrivateKeyIsOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadOrCreateCert(dir); err != nil {
		t.Fatal(err)
	}
	_, keyPath := CertPaths(dir)
	fi, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		if isWindows() {
			t.Logf("윈도우라 파일 모드가 %v 로 남는다 — 이 방어는 여기서 ACL 몫이고, 이 시험은 그것을 못 잰다", perm)
			return
		}
		t.Fatalf("키가 %v 다 — 0600 이어야 한다", perm)
	}
}

// 아무도 없으면 선다. 그리고 **선 다음에는 우리 것으로 보인다.**
func TestAnEmptyPortIsTakenAndThenReadsAsOurs(t *testing.T) {
	dir := t.TempDir()
	cert, err := LoadOrCreateCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	addr := freeAddr(t)

	ln, what, err := Acquire(addr, cert)
	if err != nil || what != ClaimFree {
		t.Fatalf("빈 번호를 못 잡았다: %v (%v)", err, what)
	}
	srv := &http.Server{Handler: http.NotFoundHandler()}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	// 두 번째 프로세스가 같은 자리를 두드리면 **우리**라고 답해야 한다 — 헬퍼는 사용자당
	// 하나이므로(§5.2) 이건 정상이고, 두 번째는 조용히 물러난다.
	got, why := Probe(addr, Fingerprint(cert))
	if got != ClaimOurs {
		t.Fatalf("우리가 선 자리를 %v 로 읽었다: %s", got, why)
	}
	if _, what, err := Acquire(addr, cert); what != ClaimOurs || err != nil {
		t.Fatalf("두 번째 기동이 %v / %v 다", what, err)
	}
}

// 남이 서 있으면 **지우지도 붙지도 않는다**(§5.3 ⚠).
//
// 소켓과 다른 자리가 여기다: 소켓은 홈 아래 0600 이라 그 자리에 설 수 있는 것이 사실상 우리 옛
// 빌드뿐이었지만, 포트의 이름 공간은 머신에 하나라 남의 프로세스도 그 번호에 선다. 그러면
// 「응답함」으로 읽고 붙는 순간 **덱 내용과 도구 호출이 그리로 간다.**
func TestAStrangerOnThePortIsRefusedNotAdopted(t *testing.T) {
	dir := t.TempDir()
	ours, err := LoadOrCreateCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 남의 리스너 — 평문 HTTP.
	plain, err := net.Listen("tcp", Host+":0")
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	go func() { _ = http.Serve(plain, http.NotFoundHandler()) }()

	got, why := Probe(plain.Addr().String(), Fingerprint(ours))
	if got != ClaimStranger {
		t.Fatalf("평문 리스너를 %v 로 읽었다", got)
	}
	if !strings.Contains(why, "TLS") {
		t.Errorf("사유가 %q 다", why)
	}
	if _, what, err := Acquire(plain.Addr().String(), ours); what != ClaimStranger || err == nil {
		t.Fatalf("남의 자리에 %v / %v 로 답했다", what, err)
	} else if !strings.Contains(err.Error(), "지우지도") {
		t.Errorf("무엇을 안 하는지가 안 적혔다: %v", err)
	}
}

// 다른 인증서를 내미는 TLS 리스너도 남이다. **TLS 라는 것만으로는 우리가 아니다.**
func TestATLSStrangerIsStillAStranger(t *testing.T) {
	oursDir, theirsDir := t.TempDir(), t.TempDir()
	ours, err := LoadOrCreateCert(oursDir)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := LoadOrCreateCert(theirsDir)
	if err != nil {
		t.Fatal(err)
	}
	if Fingerprint(ours) == Fingerprint(theirs) {
		t.Fatal("서로 다른 디렉토리인데 같은 인증서가 났다")
	}
	ln, err := Listen(Host+":0", theirs)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { _ = http.Serve(ln, http.NotFoundHandler()) }()

	got, why := Probe(ln.Addr().String(), Fingerprint(ours))
	if got != ClaimStranger {
		t.Fatalf("남의 인증서를 %v 로 읽었다", got)
	}
	if !strings.Contains(why, "다른 인증서") {
		t.Errorf("사유가 %q 다", why)
	}
}

// 번호를 못 잡으면 **다른 번호로 안 흘러간다**(§5.5.1). 흘러가면 헬퍼는 떴다고 믿고 애드인은
// 영영 못 붙는다 — 끝만 알리고 어긋난 것은 안 알리는 그 모양이다.
func TestATakenPortIsAStopNotADetour(t *testing.T) {
	dir := t.TempDir()
	cert, err := LoadOrCreateCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	taken, err := net.Listen("tcp", Host+":0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()

	_, err = Listen(taken.Addr().String(), cert)
	if err == nil {
		t.Fatal("이미 잡힌 번호에 또 섰다")
	}
	if !strings.Contains(err.Error(), "SourceLocation") {
		t.Errorf("왜 옮기지 않는지가 안 적혔다: %v", err)
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", Host+":0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	// 방금 놓은 자리라 잠깐은 비어 있다. 다시 잡히는 경쟁은 시험 안에서 무시할 만하다.
	time.Sleep(10 * time.Millisecond)
	return addr
}

func isWindows() bool { return os.PathSeparator == '\\' }

var _ = tls.Certificate{}
