package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"time"
)

// 포트를 잡는다 — 또는 이미 우리가 서 있다고 말한다(DESIGN.md §5.3·§5.5.1).
//
// # 번호는 설치 때 굳고 기동 때 고르지 않는다
//
// `<SourceLocation>` 이 그것을 강제한다. 헬퍼에게 포트는 **찾는 것이 아니라 받는 것**이다:
// 사이드로드하거나 카탈로그에 올릴 때 정해서 매니페스트와 헬퍼 설정 양쪽에 같은 값으로 적고,
// 헬퍼는 그 값만 시도한다.
//
// # 못 잡으면 다른 번호로 흘러가지 않는다
//
// 흘러가면 매니페스트가 가리키는 자리와 어긋나 애드인이 영영 못 붙는데 **헬퍼는 자기가 떴다고
// 믿는다.** 끝만 알리고 어긋난 것은 안 알리는, 이 저장소가 여러 번 만난 그 모양이다. 그러니
// 실패는 **말하고 멈춘다.**

// ClaimResult 는 포트를 두드려 본 결과.
type ClaimResult int

const (
	// ClaimFree 는 아무도 없다 — 우리가 선다.
	ClaimFree ClaimResult = iota
	// ClaimOurs 는 **우리 인증서를 내미는 것**이 이미 서 있다. 헬퍼는 사용자당 하나이므로
	// (§5.2) 이건 정상이고, 두 번째 프로세스는 조용히 물러난다.
	ClaimOurs
	// ClaimStranger 는 그 번호에 남이 서 있다. **지우지도 붙지도 않는다** — 소켓과 달리 포트의
	// 이름 공간은 머신에 하나라 남의 프로세스도 그 번호에 설 수 있고(§5.2), 붙으면 덱 내용과
	// 도구 호출이 그리로 간다(§5.3 ⚠).
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

// probeTimeout 은 그 번호를 두드려 보는 데 드는 시간의 천장.
const probeTimeout = 1500 * time.Millisecond

// Probe 는 그 번호에 누가 있는지, 있다면 우리인지 본다.
//
// **dial 을 TLS 핸드셰이크까지 민다**(§5.5.1). 평문이 답하거나 다른 인증서를 내밀면 남의
// 리스너다. 이것이 거는 것은 **오식별 방지지 신원 증명이 아니다** — 키를 읽을 수 있는 것만
// 우리 이름으로 설 수 있다는, 위 인증서 파일의 바구니만큼만 참이다.
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
		// 평문이 답했거나 TLS 가 아니다. 남의 리스너다.
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

// Listen 은 포트를 잡는다. **다른 번호로 안 흘러간다.**
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

// Acquire 는 §5.3 의 「획득-또는-접속」을 포트 위에서 돈다.
//
// 그림의 갈래 중 **이 함수가 지는 것은 셋**이다: 아무도 없으면 선다, 우리가 이미 서 있으면
// 물러난다, 남이 서 있으면 말하고 멈춘다. 되살리기·유예·크래시 루프는 감독자의 몫이고(§5.4),
// 여기서 하지 않는 이유는 그쪽이 「사람이 껐다」와 「죽었다」를 안 가르기 때문이다.
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
