# GitHub용 Magi

Chrome 사이드 패널에서 현재 github.com 저장소·이슈·PR을 Magi에 연결합니다. 로컬 헬퍼는 지정한 체크아웃의 `origin`이 선택한 저장소와 일치하는지 확인합니다. 코딩 에이전트는 그 체크아웃에서 작업합니다. 확장은 GitHub 페이지를 수정하거나 댓글을 제출하지 않습니다.

## 설치와 설정

저장소 루트에서 Magi와 헬퍼를 빌드합니다.

```sh
go build -o /tmp/magi ./cmd/magi
go build -o /tmp/magi-github ./clients/github/cmd
export MAGI_BINARY=/tmp/magi
/tmp/magi-github -repo /absolute/path/to/checkout -repository owner/repository
```

연결 전에 Magi에 모델을 설정합니다. `MAGI_CONFIG_DIR`은 설정 디렉터리를 지정합니다. 헬퍼는 `127.0.0.1:8737`에서 수신하므로 같은 포트에서 하나만 실행할 수 있습니다. `MAGI_GITHUB_TOKEN`으로 로컬 접속 토큰을 지정할 수 있으며, 생략하면 헬퍼가 출력한 토큰을 사용합니다. 토큰을 공개하지 마세요.

Chrome의 `chrome://extensions`에서 개발자 모드를 켜고 **압축해제된 확장 프로그램을 로드합니다**로 `clients/github/extension`을 선택합니다. `https://github.com`의 저장소·이슈·PR을 열고 확장 버튼으로 사이드 패널을 엽니다. 로컬 토큰을 붙여 넣고 **현재 GitHub 페이지 연결**을 누릅니다. 확장은 `sidePanel`, 세션 `storage`, github.com 및 루프백 헬퍼 접근 권한을 요청합니다. 콘텐츠 스크립트와 원격 스크립트는 없습니다. 토큰은 Chrome 세션 동안 저장합니다.

`GITHUB_TOKEN`은 확장에 넣지 않고 헬퍼 프로세스에 전달합니다. 공개 저장소는 토큰 없이 읽을 수 있습니다. 비공개 저장소에는 메타데이터·이슈·PR 읽기 권한으로 제한한 토큰을 사용하세요. GitHub 쓰기 도구는 등록하지 않습니다. 인증정보를 도구 결과에 넣지 않습니다. 소스 변경 뒤 헬퍼를 다시 빌드하고 UI 변경 뒤 압축해제된 확장을 새로고칩니다.

## 사용과 세션

저장소 페이지는 `owner/repository`, 이슈·PR 페이지는 번호가 있는 대상에 연결합니다. 같은 저장소의 다른 경로는 저장소에 연결합니다. 다른 탭으로 전환하거나 연결한 탭의 URL이 바뀌면 활성 연결을 해제하므로 다시 연결한 뒤 요청합니다. 백그라운드 탭의 탐색은 현재 대화를 해제하지 않습니다. 늦게 도착한 요청·새 세션 응답은 탐색으로 해제한 패널을 다시 활성화하지 못합니다.

코딩 요청을 보내고 **작업 중단**으로 중단합니다. 모델명을 입력한 뒤 **모델 변경**을 누르거나 **새 세션**을 시작할 수 있습니다. 새 세션은 같은 GitHub 대상과 로컬 체크아웃을 사용합니다. 입력창은 원래 요청이 같은 연결에서 성공했고 사용자가 중간에 수정하지 않았을 때만 비웁니다. 패널을 닫아도 Magi 작업은 계속될 수 있습니다. 다시 연결하면 저장된 이벤트를 읽습니다.

승인 카드는 도구명과 인자를 보여주며 허용·거부를 원래 연결에 전달합니다. 질문에는 선택지와 직접 답변 입력을 제공합니다. 코딩 에이전트는 Magi 코딩 도구를 사용할 수 있으므로 변경을 허용하기 전에 승인 내용을 검토하세요. GitHub 본문은 신뢰하지 않는 외부 자료입니다. 댓글·푸시·병합은 명시적인 사용자 요청이 필요하며 추가 GitHub 도구는 읽기만 수행합니다.

## 지원 범위와 복구

`github_repository`, `github_issue`, `github_pull`은 연결한 대상만 읽으며 빈 인자 객체를 요구합니다. 선택한 메타데이터·이슈/PR 본문·PR 브랜치 참조와 SHA를 반환합니다. 1 MiB를 넘는 HTTP 응답은 거부하고 12,000자를 넘는 문자열은 잘라 `truncated:true`로 알립니다. 댓글·파일·체크 결과·페이지 순회·첨부파일은 조회하지 않습니다. github.com만 지원하며 GitHub Enterprise는 지원하지 않습니다.

패널은 Markdown 없이 일반 텍스트를 표시합니다. 이벤트 연결은 1초 후 재연결하고 이미 본 번호 있는 이벤트를 건너뜁니다. 번호 없는 임시 질문은 반복될 수 있으나 call ID로 중복 조작 버튼을 거릅니다. 요청 실패 시 입력을 보존하고 헬퍼 오류를 표시합니다. 인증 실패 시 로컬 토큰을 확인하세요. 저장소 검사가 실패하면 `git remote get-url origin`과 지정한 소유자/저장소가 일치하는지 확인합니다. HTTPS와 `git@github.com:owner/repository.git` 형식을 인식합니다. Magi 실행 실패 시 `MAGI_BINARY`, 모델 인증정보, 설정 디렉터리를 확인합니다.

PR 선택은 브랜치를 자동 전환하거나 PR head를 가져오지 않습니다. 편집 전에 에이전트에게 로컬 브랜치를 확인하도록 요청하세요. 다른 저장소는 헬퍼를 종료한 뒤 해당 체크아웃과 대상으로 다시 실행합니다.

## 테스트와 실물 인수

`go test -race ./clients/github/...`를 실행합니다. API 테스트는 잘못된 대상·대상별 도구·빈 인자·응답 필드·문자열 생략·큰 본문·안전한 HTTP 오류를 검사합니다. `npm ci --prefix clients/web/e2e`로 브라우저 의존성을 설치하고 Playwright Chromium을 설치한 뒤 `node clients/github/test/ui-playwright.mjs`를 실행합니다. Chrome API와 로컬 HTTP를 모의 처리하여 늦은 요청·새 세션 응답 격리, 텍스트 표시, SSE 재연결 중복 방지, 백그라운드 탭 탐색, manifest 권한을 검증합니다.

실제 Chrome 확장 설치·GitHub 계정·비공개 저장소 권한·운영 API 제한·코딩 에이전트 실행은 아직 검증하지 않았습니다. 배포 전 Chrome에서 저장소·이슈·PR 탐색, 모델 실패 복구, 승인·거부, 새 세션, 패널 닫기·다시 열기, 헬퍼 단절을 확인하세요. 폐기 가능한 체크아웃에서 승인한 파일 편집과 원격 쓰기 거부를 확인합니다. 표시한 대상·origin·브랜치가 모두 일치하는지 확인한 뒤 검증하세요.

## 공식 인터페이스 검토 (2026-10-12)

헬퍼는 REST 버전 `2026-03-10`을 지정하도록 갱신했습니다. 이전 `2022-11-28`도 2028-03-10까지 지원되므로 만료된 API를 쓰던 문제는 아닙니다. 갱신 전에 [버전 정책](https://docs.github.com/en/rest/about-the-rest-api/api-versions)과 [호환성 변경 목록](https://docs.github.com/en/rest/about-the-rest-api/breaking-changes)을 읽었습니다. 현재 반환 필드는 제거된 저장소 설정과 PR의 `merge_commit_sha`를 사용하지 않습니다. head/base SHA는 별도 필드입니다.

[이슈 조회 계약](https://docs.github.com/en/rest/issues/issues#get-an-issue)은 제한된 저장소에 Issues 읽기 권한을 요구합니다. [PR 조회 계약](https://docs.github.com/en/rest/pulls/pulls#get-a-pull-request)은 Pull requests 읽기 또는 Contents 읽기를 허용합니다. [인증 안내](https://docs.github.com/en/rest/authentication/authenticating-to-the-rest-api)에 따르면 404는 비공개 자료가 숨겨졌거나 접근 권한이 없는 경우에도 발생합니다. 로컬 헬퍼 인증과 GitHub 인증은 별개입니다.

[Side Panel API](https://developer.chrome.com/docs/extensions/reference/api/sidePanel)는 Chrome 114 이상·MV3에서 지원하므로 manifest에 최소 버전을 명시했습니다. `side_panel.default_path`, `action`, `setPanelBehavior({openPanelOnActionClick:true})`는 툴바에서 여는 계약과 일치합니다. 사용자 동작 없이 `open()`을 호출하지 않습니다. 전역 패널을 사용하며 github.com 전용 연결은 탭별 패널 활성화 대신 클라이언트가 검사합니다.

[Tabs API](https://developer.chrome.com/docs/extensions/reference/api/tabs)는 일치하는 github.com URL을 host 권한으로 읽도록 허용하므로 광범위한 `tabs` 권한은 필요하지 않습니다. 권한 없는 사이트로 탐색하면 URL이 숨겨질 수 있어 `status:loading`에도 연결한 탭의 세션을 해제하도록 보완했습니다. 백그라운드 탭의 로딩은 무시합니다. [세션 저장소](https://developer.chrome.com/docs/extensions/reference/api/storage)는 신뢰한 확장 컨텍스트로 제한하며 브라우저 재시작 외에도 확장 비활성화·새로고침·업데이트 때 비워집니다. 토큰 복원과 백그라운드 초기화 실패에 안내를 추가했습니다. 공식 문서 대조와 모의 테스트는 실물 Chrome·GitHub 인수를 대신하지 않습니다.
