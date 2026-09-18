package office

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"time"
)

// 오피스 헬퍼 HTTPS 포트 점유 및 프로세스 중복 실행 판별 모듈(DESIGN.md §5.3·§5.5.1).
//
// # 고정 포트 바인딩 원칙
// 애드인 매니페스트 XML의 `<SourceLocation>` 태그에 포트 번호가 하드코딩되므로,
// 런타임에 동적 포트를 할당하지 않고 사전에 정의된 고정 포트만을 시도합니다.
//
// # 임의 포트 변경 금지
// 바인딩 실패 시 임의의 다른 포트로 자동 변경할 경우, 매니페스트가 가리키는 엔드포인트와
// 불일치가 발생하여 애드인이 영구적으로 연결되지 않는 장애를 방지하기 위해 즉시 오류를 반환하고 기동을 중단합니다.

// ClaimResult 는 대상 포트 탐침(Probe) 결과 상태를 정의합니다.
type ClaimResult int

const (
	// ClaimFree 는 해당 포트를 사용하는 프로세스가 없어 신규 바인딩이 가능한 상태입니다.
	ClaimFree ClaimResult = iota
	// ClaimOurs 는 동일한 TLS 인증서 지문을 제시하는 헬퍼 인스턴스가 이미 정상 서비스 중인 상태입니다.
	// 헬퍼는 사용자/머신당 단일 인스턴스로 운용되므로(§5.2), 후속 프로세스는 정상 상태로 인식하고 조용히 종료합니다.
	ClaimOurs
	// ClaimStranger 는 알 수 없는 타 프로세스 또는 다른 인증서를 사용하는 서버가 해당 포트를 점유 중인 상태입니다.
	// 보안상 문서 데이터 및 도구 호출이 비인가 프로세스로 누출되는 위험을 방지하기 위해 강제 종료나 연결 시도를 일체 하지 않습니다(§5.3).
	ClaimStranger
)

func (c ClaimResult) String() string {
	switch c {
	case ClaimFree:
		return "free"
	case ClaimOurs:
		return "ours"
	default:
		return "stranger"
	}
}

// probeTimeout 은 포트 탐침 시 적용되는 네트워크 타임아웃 상한(1,500ms)입니다.
const probeTimeout = 1500 * time.Millisecond

// Probe 는 지정 주소의 리스너 상태를 검사하여 가용 여부 및 자사 헬퍼 인스턴스 여부를 확인합니다.
//
// 단순 TCP 연결에 그치지 않고 TLS 핸드셰이크까지 수행하여 자체 서명 인증서의 지문(Fingerprint)을 검증합니다(§5.5.1).
// 평문 HTTP 응답 또는 인증서 불일치 시 타 프로세스 점유(`ClaimStranger`)로 판정합니다.
func Probe(addr, wantFingerprint string) (ClaimResult, string) {
	raw, err := net.DialTimeout("tcp", addr, probeTimeout)
	if err != nil {
		return ClaimFree, ""
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(probeTimeout))

	conn := tls.Client(raw, &tls.Config{
		// 자기 서명이라 검증은 끄고 **지문으로 견준다.** 신뢰 사슬을 묻는 것이 아니라
		// 「이게 우리 것인가」를 묻는 자리다.
		InsecureSkipVerify: true,
		ServerName:         Host,
	})
	if err := conn.Handshake(); err != nil {
		// 평문 HTTP 응답이거나 비정상 TLS 연결인 경우 타 프로세스 점유로 판정합니다.
		return ClaimStranger, "그 번호에 TLS 가 아닌 것이 서 있습니다: " + err.Error()
	}
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return ClaimStranger, "그 번호에 선 것이 인증서를 안 내밀었습니다"
	}
	got := fingerprintOf(state.PeerCertificates[0].Raw)
	if wantFingerprint != "" && got == wantFingerprint {
		return ClaimOurs, ""
	}
	return ClaimStranger, "그 번호에 다른 인증서를 내미는 것이 서 있습니다(지문 " + short(got) + ")"
}

// Listen 은 지정된 TCP 주소에 리스너를 바인딩하고 TLS 1.2 이상을 적용합니다. 고정 포트 원칙에 따라 타 포트로 우회하지 않습니다.
func Listen(addr string, cert tls.Certificate) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf(
			"%s 를 못 잡았습니다: %w\n"+
				"다른 번호로 옮기지 않습니다 — 매니페스트의 <SourceLocation> 이 이 번호를 가리키고 있어서, "+
				"옮기면 헬퍼는 떴다고 믿는데 애드인은 영영 못 붙습니다. 그 번호를 쓰는 것을 끄거나, "+
				"매니페스트와 헬퍼 양쪽의 번호를 같이 바꿔 주세요.", addr, err)
	}
	return tls.NewListener(ln, &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}), nil
}

// Acquire 는 포트 상태를 사전 점검(Probe)한 후 안전하게 바인딩을 수행합니다(DESIGN.md §5.3).
//
// 1) 포트 가용 시(`ClaimFree`): 리스너를 정상 생성하여 반환합니다.
// 2) 자사 헬퍼 기동 중(`ClaimOurs`): 정상 중복 기동으로 판단하여 리스너 없이 반환합니다.
// 3) 타 프로세스 점유(`ClaimStranger`): 보안 격리를 위해 바인딩을 즉시 중단하고 오류를 반환합니다.
func Acquire(addr string, cert tls.Certificate) (net.Listener, ClaimResult, error) {
	what, why := Probe(addr, Fingerprint(cert))
	switch what {
	case ClaimOurs:
		return nil, ClaimOurs, nil
	case ClaimStranger:
		return nil, ClaimStranger, errors.New(why +
			"\n남의 리스너를 지우지도, 그쪽에 붙지도 않습니다 — 붙으면 덱 내용과 도구 호출이 그리로 갑니다.")
	}
	ln, err := Listen(addr, cert)
	if err != nil {
		return nil, ClaimFree, err
	}
	return ln, ClaimFree, nil
}

func short(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12] + "…"
}
