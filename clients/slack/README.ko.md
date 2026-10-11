# Slack용 Magi

Socket Mode 앱으로 Slack DM과 멘션 스레드에서 Magi와 대화하고 App Home 안내를 볼 수 있습니다. 워크스페이스·사용자·채널·스레드 조합마다 별도의 Magi 연결을 만듭니다. 대화를 읽고 초안을 준비하는 협업 에이전트이며, 등록된 Slack 도구로 다른 채널에 메시지를 보내거나 코드를 실행할 수 없습니다.

## 설정

1. Slack 앱을 만들고 Socket Mode를 켭니다. `connections:write` 권한의 앱 수준 토큰을 만듭니다.
2. 봇 권한으로 `app_mentions:read`, `chat:write`, `im:history`를 추가합니다. 채널 스레드 읽기에는 `channels:history` 또는 `groups:history`도 필요하며, 해당 채널에 봇을 초대합니다. 관리자 권한은 필요하지 않습니다.
3. `app_mention`, `message.im`, `app_home_opened` 이벤트를 구독합니다. Home·Messages 탭과 승인 버튼용 Interactivity를 켭니다.
4. 워크스페이스에 앱을 설치합니다. 로컬 비밀 관리 도구나 환경변수로 `SLACK_APP_TOKEN`, `SLACK_BOT_TOKEN`을 전달합니다. 토큰을 커밋하지 않습니다.
5. `magi` 실행 파일을 PATH에 두고 모델 제공자를 설정한 뒤 `go run ./clients/slack/cmd`를 실행합니다.

DM을 보내거나 채널에서 `@Magi`를 멘션합니다. 답변은 시작한 스레드에만 게시합니다. 승인·선택 버튼은 해당 스레드를 시작한 사용자만 누를 수 있습니다. 질문의 판단 근거 보고서도 표시 길이 제한 안에서 함께 보여줍니다. 선택지가 없는 질문은 같은 스레드에서 `답변 요청번호 답변내용`으로 응답합니다. DM에서도 기존 대화를 이어가려면 스레드 답장을 사용합니다. 새로운 최상위 DM은 별도의 연결을 만듭니다.

## 도구와 제한

- `slack_read_thread`는 연결된 스레드의 메시지 최대 30개를 읽고 추가 메시지 존재 여부를 알려줍니다. 본문은 메시지당 1,200자·전체 12,000자로 제한하고 생략 여부를 표시합니다. 링크 탐색·첨부 다운로드·다른 채널 검색은 하지 않습니다.
- `slack_draft_message`는 `sent: false`인 초안을 반환합니다. 전송 도구는 등록하지 않습니다.
- 답변은 2,800자로 제한하고 긴 출력은 생략 사실을 표시합니다. 봇 이벤트와 메시지 편집 이벤트는 요청으로 처리하지 않습니다.

Socket Mode가 수신 이벤트를 인증합니다. 승인 토큰은 한 번만 사용할 수 있으며 팀·사용자·채널·스레드에 귀속됩니다. 이벤트 ID 중복 제거는 최근 4,096개를 메모리에 보관합니다. 연결·중복 기록·대기 버튼은 프로세스를 재시작하면 유지되지 않습니다. 전송 실패 시 해당 이벤트를 보존하고 1초 간격으로 재시도하며, 성공한 뒤에만 커서를 전진합니다. Slack에서 게시를 수락한 직후 네트워크가 끊기면 중복 게시가 생길 수 있습니다. Slack과 Magi 사이에는 원자적 전송 계약이 없습니다. 승인 제출이 실패하면 버튼을 유지하여 재시도할 수 있습니다. 새로 연결하기 전의 과거 이벤트는 다시 게시하지 않습니다. 연결은 재시작 전까지 최대 256개를 보관합니다. Slack 전송의 영구 재시도(일시적인 호출 제한은 최대 세 번 재시도)·파일 업로드·조직 전체 설치는 구현하지 않았습니다. API 오류 로그에 토큰이나 Slack 응답 본문을 기록하지 않습니다.

## 검증

`go test -race ./clients/slack/...`를 실행합니다. 모의 전송으로 스레드 분리·재전송 중복 제거·요청 없는 메시지 미전송·당사자만 가능한 단일 승인·읽기 및 초안 도구를 확인합니다. 브라우저 화면은 없습니다. 실제 Slack 워크스페이스·호출 제한·토큰 폐기는 아직 검증하지 않았으므로 운영 배포 전에 확인해야 합니다.

## 공식 API 대조 (2026-10-12)

[Socket Mode](https://docs.slack.dev/apis/events-api/using-socket-mode/), [App Home 이벤트](https://docs.slack.dev/reference/events/app_home_opened/), [멘션](https://docs.slack.dev/reference/events/app_mention/), [메시지 이벤트](https://docs.slack.dev/reference/events/message/), [Block Kit 조작](https://docs.slack.dev/reference/interaction-payloads/block_actions-payload/), [스레드 조회](https://docs.slack.dev/reference/methods/conversations.replies/), [호출 제한](https://docs.slack.dev/apis/web-api/rate-limits/)의 공식 문서를 읽고 구현과 비교했습니다.

모델 작업 전에 수신 봉투에 확인 응답을 보내며, Socket Mode에는 공개 콜백 URL이 필요하지 않습니다. Home 화면은 `tab: home` 이벤트에서만 게시합니다. 버튼은 `action_id`와 불투명한 `value`를 사용하며 팀·사용자·채널·스레드를 확인합니다. `message.im`만 처리하고 그룹 DM은 처리하지 않습니다. `/answer`는 Slack이 미등록 슬래시 명령으로 가로챌 수 있어 자유 답변은 `답변`으로 시작하는 일반 메시지를 사용합니다.

게시는 채널마다 초당 한 번 이하로 예약합니다. HTTP 429의 `Retry-After`가 30초를 넘더라도 지정된 전체 시간을 기다리며 취소 요청으로 대기를 종료할 수 있습니다. 현재 공식 스레드 조회 문서에는 봇·사용자의 history 권한이 나열되어 있습니다. 내부 자체 구축 앱은 Tier 3이고, 2025-05-29 이후 신규 상업 배포된 비 Marketplace 앱에는 분당 1회·최대 15개 제한이 적용됩니다. 이 클라이언트는 내부 설치를 대상으로 최대 30개를 요청하고 자동 페이지 조회는 하지 않습니다. 상업 배포에 내부 설치의 호출 제한을 그대로 적용해서는 안 됩니다. 현재 Socket Mode 앱은 공개 Slack Marketplace에 등록할 수 없습니다.
