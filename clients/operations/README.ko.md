# 운영 헬퍼

`go build -o /tmp/magi-operations ./clients/operations/cmd`로 빌드합니다. Magi 모델 설정과 Docker/Kubernetes 역할을 지원하는 코어가 필요합니다. `/tmp/magi-operations -runtime docker -context default -resources container-name` 또는 `-runtime kubernetes -context test-cluster -namespace assigned -resources pod-name`으로 실행합니다. Kubernetes는 kubectl, Docker는 CLI와 연결 가능한 호환 데몬이 필요합니다.

헬퍼는 루프백 8738 포트에서 연결 토큰을 출력합니다. `MAGI_OPERATIONS_TOKEN`은 고정 토큰, `MAGI_BINARY`는 코어 실행 파일을 지정합니다. 같은 포트에는 헬퍼 하나만 실행합니다. 대상 키는 `runtime:context:namespace`이며 인증된 appbridge API에 정확한 target으로 연결한 뒤 반환 binding으로 채팅·이벤트·승인을 처리합니다. http://127.0.0.1:8738을 열고 로컬 토큰을 입력합니다. 화면은 서버의 고정 대상에 연결하고 채팅·중단·승인·질문을 처리합니다. `node clients/operations/test/ui-playwright.mjs`로 모의 브라우저 검증을 실행합니다.

`status`와 `logs`는 시작 시 허용한 `{resource}`만 받습니다. Docker 상태는 환경변수를 제외한 컨테이너 State, Kubernetes 상태는 Pod 이름·phase·conditions만 조회합니다. 로그는 최근 10분·200줄, 출력 64KiB, 실행 15초로 제한합니다. 출력은 신뢰하지 않는 데이터이며 앱 비밀이 포함될 수 있으므로 불필요하게 조회하지 않습니다. exec·Secret·apply·delete 도구는 등록하지 않습니다. 재시작은 아래의 별도 승인 절차를 사용합니다.

`go test -race ./clients/operations/... ./clients/appbridge`와 `go vet ./clients/operations/...`로 검사합니다. 고정 context·namespace 인자, 리소스 거절, 추가 인자 거절, 출력 상한을 검증합니다. 이 Mac의 실제 Docker 조회는 Podman 레지스트리 drop-in TOML 오류로 실패했으며 kubectl은 없었습니다. 실물 인수 통과로 취급하지 않습니다. 사용 전 테스트 컨테이너·Pod의 상태와 stdout/stderr 로그를 확인합니다. 실제 재시작 인수는 테스트 환경에서 별도로 수행해야 합니다.

## 승인 기반 재시작

기본값은 재시작 비활성입니다. `-enable-restart`로 제안 도구를 켭니다. Docker는 resources의 컨테이너만 재시작하고, Kubernetes는 `-deployments app-name`으로 지정한 Deployment만 재시작합니다. 조회용 resources는 Pod 목록으로 유지합니다.

모델의 propose_restart 호출은 실행하지 않고 5분 수명의 제안을 만듭니다. 운영 화면은 환경·대상·중단 영향·복구 한계를 표시하고, 재시작 승인 버튼과 확인 대화상자를 거쳐 일회용 승인 ID를 제출합니다. 거부·만료·재사용은 실행하지 않습니다. core permission=allow도 이 확인을 우회하지 못합니다.

승인 후 Docker restart와 State 조회 또는 Kubernetes rollout restart와 rollout status(60초)를 실행합니다. 전체 제한은 90초입니다. Docker의 running/health 상태나 Kubernetes rollout 완료만 확인하며 서비스 수준 응답 검증은 별도입니다. 변경 성공과 상태 검증 실패를 구분하고, 명령 실패·시간 초과는 결과 불확실로 표시합니다. 자동 재시도·자동 rollback은 하지 않습니다. 승인 대기 목록은 프로세스 메모리에만 있으며 재시작하면 사라집니다.
