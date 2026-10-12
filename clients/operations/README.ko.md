# 운영 헬퍼

`go build -o /tmp/magi-operations ./clients/operations/cmd`로 빌드합니다. Magi 모델 설정과 Docker/Kubernetes 역할을 지원하는 코어가 필요합니다. `/tmp/magi-operations -runtime docker -context default -resources container-name` 또는 `-runtime kubernetes -context test-cluster -namespace assigned -resources pod-name`으로 실행합니다. Kubernetes는 kubectl, Docker는 CLI와 연결 가능한 호환 데몬이 필요합니다.

헬퍼는 루프백 8738 포트에서 연결 토큰을 출력합니다. `MAGI_OPERATIONS_TOKEN`은 고정 토큰, `MAGI_BINARY`는 코어 실행 파일을 지정합니다. 같은 포트에는 헬퍼 하나만 실행합니다. 대상 키는 `runtime:context:namespace`이며 인증된 appbridge API에 정확한 target으로 연결한 뒤 반환 binding으로 채팅·이벤트·승인을 처리합니다. http://127.0.0.1:8738을 열고 로컬 토큰을 입력합니다. 화면은 서버의 고정 대상에 연결하고 채팅·중단·승인·질문을 처리합니다. `node clients/operations/test/ui-playwright.mjs`로 모의 브라우저 검증을 실행합니다.

`status`와 `logs`는 시작 시 허용한 `{resource}`만 받습니다. Docker 상태는 환경변수를 제외한 컨테이너 State, Kubernetes 상태는 Pod 이름·phase·conditions만 조회합니다. 로그는 최근 10분·200줄, 출력 64KiB, 실행 15초로 제한합니다. 출력은 신뢰하지 않는 데이터이며 앱 비밀이 포함될 수 있으므로 불필요하게 조회하지 않습니다. exec·Secret·apply·delete 도구는 등록하지 않습니다. 재시작은 아래의 별도 승인 절차를 사용합니다.

`go test -race ./clients/operations/... ./clients/appbridge`와 `go vet ./clients/operations/...`로 검사합니다. 고정 context·namespace 인자, 리소스 거절, 추가 인자 거절, 출력 상한을 검증합니다. Kubernetes에서 흔히 쓰는 슬래시·콜론·@ 포함 context 이름을 허용합니다. context는 독립된 명령 인자로 전달하며 namespace·리소스 검증은 기존대로 유지합니다. 환경별 검증 결과는 아래 기록을 참고합니다.

## 승인 기반 재시작

기본값은 재시작 비활성입니다. `-enable-restart`로 제안 도구를 켭니다. Docker는 resources의 컨테이너만 재시작하고, Kubernetes는 `-deployments app-name`으로 지정한 Deployment만 재시작합니다. 조회용 resources는 Pod 목록으로 유지합니다.

모델의 propose_restart 호출은 실행하지 않고 5분 수명의 제안을 만듭니다. 운영 화면은 환경·대상·중단 영향·복구 한계를 표시하고, 재시작 승인 버튼과 확인 대화상자를 거쳐 일회용 승인 ID를 제출합니다. 거부·만료·재사용은 실행하지 않습니다. core permission=allow도 이 확인을 우회하지 못합니다.

승인 후 Docker restart와 State 조회 또는 Kubernetes rollout restart와 rollout status(60초)를 실행합니다. 전체 제한은 90초입니다. Docker의 running/health 상태나 Kubernetes rollout 완료만 확인하며 서비스 수준 응답 검증은 별도입니다. 변경 성공과 상태 검증 실패를 구분하고, 명령 실패·시간 초과는 결과 불확실로 표시합니다. 자동 재시도·자동 rollback은 하지 않습니다. 승인 대기 목록은 프로세스 메모리에만 있으며 재시작하면 사라집니다.

## Docker 실물 검증 (2026-10-12)

손상된 Podman 레지스트리 설정을 백업·복구한 뒤 Docker API 5.5.2가 정상 응답했습니다. `MAGI_OPERATIONS_LIVE_DOCKER=1 go test ./clients/operations -run TestLiveDocker -count=1 -v`로 네트워크·볼륨 없는 테스트 컨테이너의 상태, stdout/stderr 로그, 승인 재시작과 상태 확인을 검증했습니다(20.31초 PASS). 테스트 컨테이너를 제거한 것도 확인했습니다. 기존 워크로드는 변경하지 않았습니다. Kubernetes 실물 검증은 아직 미수행입니다.

## 로컬 설치와 Kubernetes 점검 (2026-10-12)

`~/.local/bin`에 `magi`, `magi-web`, `magi-operations`, `magi-figma`, `magi-mail`, `magi-slack`, `magi-github`를 설치했습니다. 기존 실행 파일은 `.before-clients`로 백업했습니다. 다시 빌드한 콘솔의 `/ui/application/application.nocache.js`가 HTTP 200으로 응답했습니다. 내장 자산 검증이며 실제 계정 클라이언트 인수 검증은 아닙니다.

IDE가 소유한 컴패니언 4개를 갱신된 코어로 재시작했습니다. 저장된 이벤트가 없는 빈 세션은 재시작 시 ID가 바뀔 수 있습니다. 이번 점검에서 저장된 대화가 유실되지는 않았습니다. 실제 계정의 Figma·메일·Slack 인수 검증은 남아 있습니다.

kubectl v1.37.1을 설치했습니다. context `handbook-dev/api-sayaya-cloud:6443/sayaya`, namespace `handbook-dev`로 읽기 전용 Pod 목록을 요청했지만 `api.sayaya.cloud:6443`의 연결 거절로 실패했습니다. Kubernetes 변경 명령은 실행하지 않았습니다. 접속이 복구되면 명시적으로 허용한 테스트 Pod의 상태와 제한된 로그를 확인하고, 승인 재시작은 폐기 가능한 테스트 Deployment에서만 검증합니다. context·namespace·명령·결과를 기록하되 인증정보는 포함하지 않습니다.

위 설치 기록의 GitHub 헬퍼는 2026-10-12에 제거했습니다. 브라우저 작업은 Chrome 브라우저 클라이언트를 사용합니다.
