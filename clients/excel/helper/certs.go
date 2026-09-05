package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// 인증서. **조건부가 아니라 필수다**(DESIGN.md §5.5 의 값 1).
//
// Office 는 애드인 소스를 https 로만 받는다. §5.5 가 「헬퍼가 애드인 페이지를 직접 서빙한다」로
// 정하면서 헬퍼 자신이 https 서버가 됐고, 그래서 혼합 콘텐츠 면제니 평문 루프백이니를 따질 자리가
// 애초에 없어졌다 — 어느 플랫폼에서도 인증서가 선다.
//
// # 키가 소켓의 0600 을 되돌려 준다
//
// 포트의 이름 공간은 머신에 하나라 홈 아래라는 것이 없다(§5.2). 그래서 「그 번호를 쥔 것이 우리
// 헬퍼인가」를 자리로는 못 가른다. 인증서가 그 자리를 진다: 그 번호에 **우리 이름으로** 설 수
// 있는 것은 키를 읽을 수 있는 것뿐이고, 키를 홈 아래 0600 으로 두면 그 바구니가 소켓 때와
// 정확히 같아진다 — 다른 계정에는 참이고 **같은 계정의 다른 프로세스에는 거짓**이다.
// 소켓도 딱 거기까지였으므로 포트로 옮기면서 잃은 것은 없다.

const certLifetime = 825 * 24 * time.Hour // 브라우저들이 받아들이는 상한선 언저리

// CertPaths 는 키와 인증서가 사는 자리.
func CertPaths(configDir string) (certPath, keyPath string) {
	return filepath.Join(configDir, "xl-helper-cert.pem"), filepath.Join(configDir, "xl-helper-key.pem")
}

// LoadOrCreateCert 는 있으면 읽고 없거나 낡았으면 만든다.
//
// **이름은 하나다**(§5.5 ⚠). SAN 에 IP 리터럴 `127.0.0.1` 만 넣는다 — 매니페스트도 바인드
// 주소도 전송 URL 도 그 문자열이므로, 여기에 `localhost` 를 같이 넣으면 넷이 다섯이 되고
// 다섯째는 아무도 안 본다.
func LoadOrCreateCert(configDir string) (tls.Certificate, error) {
	certPath, keyPath := CertPaths(configDir)
	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		leaf, perr := x509.ParseCertificate(cert.Certificate[0])
		if perr == nil && time.Now().Before(leaf.NotAfter) && coversHost(leaf) {
			cert.Leaf = leaf
			return cert, nil
		}
		// 낡았거나 다른 이름으로 난 것이다. **덮어쓴다** — 안 맞는 인증서를 들고 서면 증상은
		// 「애드인이 안 붙는다」 하나로 뭉쳐 나오고, 넷 중 어디가 틀렸는지가 화면에 안 나온다.
	}
	return createCert(configDir)
}

func coversHost(leaf *x509.Certificate) bool {
	ip := net.ParseIP(Host)
	for _, got := range leaf.IPAddresses {
		if got.Equal(ip) {
			return true
		}
	}
	for _, name := range leaf.DNSNames {
		if name == Host {
			return true
		}
	}
	return false
}

func createCert(configDir string) (tls.Certificate, error) {
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return tls.Certificate{}, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "magi-xl helper", Organization: []string{"magi"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(certLifetime),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		// 자기 서명이라 **CA 로도 선다.** 사람이 이것을 신뢰 저장소에 넣어야 애드인이 붙는데,
		// 그건 가볍게 볼 일이 아니라고 설계가 적어 뒀다(§5.5, S3). 그래서 넣는 일은 우리가
		// 대신 하지 않고 **명령만 알려 준다** — 목업의 README 가 이미 그 모양이다.
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP(Host)},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	certPath, keyPath := CertPaths(configDir)
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, err
	}
	// **0600.** 이 한 줄이 §5.2 가 포트에서 잃었다고 적은 것을 되돌려 준다. 윈도우에서 이
	// 비트는 ACL 로 옮겨지지 않으므로(파일 모드가 읽기 전용 플래그로만 남는다) 거기서는 이
	// 방어가 유닉스만큼 서지 않는다 — 그 사실을 여기 적어 두는 것이 「지킬 수 없는 약속을 안
	// 하는 것」이다(§8).
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, err
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return tls.Certificate{}, err
	}
	cert.Leaf = leaf
	return cert, nil
}

// Fingerprint 는 인증서의 지문. **정체 확인에 쓴다**(§5.5.1) — 그 번호에 선 것이 우리인지를
// 묻는 자리는 애드인이 아니라 **헬퍼를 띄우려는 프로세스**다.
func Fingerprint(cert tls.Certificate) string {
	if len(cert.Certificate) == 0 {
		return ""
	}
	return fingerprintOf(cert.Certificate[0])
}

func fingerprintOf(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// CertInstallHint 는 사람에게 알려 줄 한 줄. 우리가 대신 심지 않는 이유가 §5.5 에 있다.
func CertInstallHint(configDir string) string {
	certPath, _ := CertPaths(configDir)
	return fmt.Sprintf(
		"Excel 은 애드인을 https 로만 받습니다. 이 인증서를 이 계정의 신뢰 저장소에 넣어 주세요:\n"+
			"  %s\n"+
			"  macOS:   security add-trusted-cert -d -r trustRoot -k ~/Library/Keychains/login.keychain-db %s\n"+
			"  Windows: certutil -user -addstore Root \"%s\"\n"+
			"직접 넣는 일이라 헬퍼가 대신 하지 않습니다 — 신뢰 저장소를 남이 고치는 것은 가볍게 볼 일이 아닙니다.",
		certPath, certPath, certPath)
}
