# 운영 헬퍼

`go build -o /tmp/magi-operations ./clients/operations/cmd`로 빌드합니다. Magi 모델 설정과 Docker/Kubernetes 역할을 지원하는 코어가 필요합니다. `/tmp/magi-operations -runtime docker -context default -resources container-name` 또는 `-runtime kubernetes -context test-cluster -namespace assigned -resources pod-name`으로 실행합니다. Kubernetes는 kubectl, Docker는 CLI와 연결 가능한 호환 데몬이 필요합니다.

헬퍼는 루프백 8738 포트에서 연결 토큰을 출력합니다. `MAGI_OPERATIONS_TOKEN`은 고정 토큰, `MAGI_BINARY`는 코어 실행 파일을 지정합니다. 같은 포트에는 헬퍼 하나만 실행합니다. 대상 키는 `runtime:context:namespace`이며 인증된 appbridge API에 정확한 target으로 연결한 뒤 반환 binding으로 채팅·이벤트·승인을 처리합니다. 전용 브라우저 UI는 아직 없습니다.

`status`와 `logs`는 시작 시 허용한 `{resource}`만 받습니다. Docker 상태는 환경변수를 제외한 컨테이너 State, Kubernetes 상태는 Pod 이름·phase·conditions만 조회합니다. 로그는 최근 10분·200줄, 출력 64KiB, 실행 15초로 제한합니다. 출력은 신뢰하지 않는 데이터이며 앱 비밀이 포함될 수 있으므로 불필요하게 조회하지 않습니다. exec·Secret·apply·delete·restart 도구는 등록하지 않습니다.

`go test -race ./clients/operations/... ./clients/appbridge`와 `go vet ./clients/operations/...`로 검사합니다. 고정 context·namespace 인자, 리소스 거절, 추가 인자 거절, 출력 상한을 검증합니다. 이 Mac의 실제 Docker 조회는 Podman 레지스트리 drop-in TOML 오류로 실패했으며 kubectl은 없었습니다. 실물 인수 통과로 취급하지 않습니다. 사용 전 테스트 컨테이너·Pod의 상태와 stdout/stderr 로그를 확인합니다. 변경·승인 기반 재시작은 후속 구현입니다.
