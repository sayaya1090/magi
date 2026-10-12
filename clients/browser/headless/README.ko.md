# 헤드리스 Chromium 백엔드

동일한 브라우저 도구를 격리된 헤드리스 Chromium에서 실행합니다. 테스트는 사용자 Chrome 프로필 대신 이 모드를 사용합니다. 로그인 쿠키·확장·사용자 프로필을 가져오지 않습니다. Node와 `clients/web/e2e`의 Playwright 및 Chromium 설치가 필요합니다.

`New(context, Config{AllowedOrigins: []string{"http://127.0.0.1:PORT"}, InitialURL: "...", AllowActions: true})`는 탭 하나(`tabId: 1`)를 생성합니다. 허용 목록에는 경로 없는 HTTP(S) origin을 명시합니다. localhost도 자동 허용하지 않습니다. 리다이렉트와 하위 자원을 포함한 HTTP 요청을 검사하며 WebSocket과 서비스 워커는 차단합니다. 추가 창은 닫습니다. `AllowActions` 기본값은 false입니다. true는 테스트 대상 이동·클릭·입력을 실행 시작 때 허용한다는 의미이며, 확장의 사용자 확인과는 별개입니다.

`Call(ctx, target, name, jsonArgs)`는 `list_tabs`, `read_page`, `navigate`, `click`, `fill`을 제공합니다. 비어 있지 않은 target마다 별도 임시 프로필을 사용하는 브라우저를 격리 생성합니다. `Release(target)`으로 해제하며 호출자가 세션 귀속을 검사해야 합니다. 기존 확장의 페이지 구현을 사용하여 비밀번호를 제외하고 본문 32,000자·요소 100개로 제한합니다. 조작 후 스냅샷은 무효화합니다. 임의 JavaScript 실행 도구는 제공하지 않습니다. 호출은 직렬 처리하며 응답은 256 KiB로 제한합니다. 타임아웃은 실행기를 종료하고 자동 재시도하지 않습니다. 정상 종료 및 종료 신호 처리 시 임시 프로필을 삭제합니다. `Close`로 자식 프로세스를 회수해야 합니다. 강제 종료나 시스템 장애 시 임시 프로필이 남을 수 있습니다.

실제 Chromium 검증:

```sh
MAGI_HEADLESS_LIVE=1 go test ./clients/browser/headless -count=1 -v
```

격리된 로컬 HTTP 테스트 페이지에서 origin 거부·비밀번호 제외·입력·오래된 요소 ID 거부·클릭 결과·기본 조작 차단을 검사합니다. 로그인된 사이트나 사용자 Chrome 프로필 검증은 아닙니다.
