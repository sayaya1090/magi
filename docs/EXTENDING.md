# magi 확장 가이드 — MCP 서버 & 공유 경험(RAG)

플렉서스에 **외부 툴(MCP)** 과 **팀 공유 메모리/스킬(experience store, D13)** 을 붙이는 단계별
방법. 개념 개요는 [`ARCHITECTURE.md`](ARCHITECTURE.md) §11(Extension points)·§7, 전체 사용법은
[`MANUAL.md`](MANUAL.md) §7·§10을 보라. 이 문서는 "처음 붙이는 사람"을 위한 실전 절차다.

> 관련 확장 수단: **Lua 플러그인**(자체 툴/훅, 핫리로드) → MANUAL §9, **훅**(셸 라이프사이클) →
> MANUAL §하네스. 인증/TLS 같은 *트랜스포트* 관심사는 플러그인/MCP가 아니라 Go
> `http.RoundTripper` 심(`openai.WithHTTPClient`)에 둔다 — ARCHITECTURE §11.

---

## 0. 설정 파일과 우선순위 (공통)

두 기능 모두 `config.toml`로 켠다. 로딩 순서(`cmd/magi/main.go`):

1. **전역**: `<config>/config.toml`
   - macOS: `~/Library/Application Support/magi/config.toml`
   - Linux: `~/.config/magi/config.toml`
2. **프로젝트**: `<workdir>/.magi/config.toml` (팀이 repo에 커밋 → 워크플로가 repo를 따라다님)

병합 규칙:

| 키 | 병합 방식 |
|---|---|
| `hooks`, `allow`, `deny`, `allow_domains` | **append**(전역 + 프로젝트) |
| `experience_dir`, `profile`, `sandbox` 등 스칼라 | 프로젝트가 **override** |
| `[routing]`, `[mcp.*]` 맵 | **키 단위 병합** — 같은 키는 프로젝트가 override |

> 파일이 없어도 에러가 아니다. 둘 다 없으면 기본값으로 동작.

---

## 1. MCP 서버 추가

MCP 서버는 **stdio 또는 HTTP 전송(Streamable HTTP)**으로 연결되고, 핸드셰이크 후 서버가
보고한 툴이 빌트인 툴과 **같은 레지스트리에 자동 등록**된다. stdio 서버 프로세스가 죽거나
HTTP 서버 연결이 끊기면 해당 툴은 자동 제거된다 (`internal/adapter/mcp/`).

### 1.1 선언

`config.toml`에 `[mcp.<name>]` 블록을 추가한다. `<name>`은 관리용 라벨일 뿐(툴 이름과 무관).

**stdio 전송** (로컬 프로세스 spawn):
```toml
# 예: 파일시스템 MCP 서버
[mcp.filesystem]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "."]

# 예: 환경변수가 필요한 서버 (예: GitHub)
[mcp.github]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-github"]
env = ["GITHUB_PERSONAL_ACCESS_TOKEN=ghp_xxx"]   # "KEY=VALUE" 문자열 배열
```

**HTTP 전송** (원격 또는 로컬 HTTP 서버):
```toml
# 예: HTTP로 실행 중인 MCP 서버
[mcp.remote]
url = "http://localhost:3000/mcp"

# 예: 커스텀 헤더와 환경변수 사용
[mcp.authenticated]
url = "${MCP_SERVER_URL}"  # 환경변수에서 읽기
[mcp.authenticated.headers]
Authorization = "Bearer ${MCP_API_TOKEN}"
X-Client-ID = "magi-client"
X-Environment = "${DEPLOY_ENV}"
```

필드 (`config.MCPServer`):

| 필드 | 타입 | 설명 |
|---|---|---|
| `url` | string | HTTP 엔드포인트 (Streamable HTTP 전송). `url`이 있으면 `command` 무시. `${VAR}` 환경변수 확장 지원 |
| `headers` | map[string]string | HTTP 커스텀 헤더 (HTTP 전송용). `${VAR}` 환경변수 확장 지원 |
| `command` | string | 실행할 바이너리 (PATH에서 찾음, stdio 전송용) |
| `args` | []string | 인자 (stdio 전송용) |
| `env` | []string | `"KEY=VALUE"` 형식. **프로세스 환경에 append**됨(기존 env 유지, stdio 전송용) |

> **전송 선택**: `url` 필드가 있으면 HTTP 전송, 없으면 stdio 전송을 사용한다.

> **환경변수 확장**: HTTP `url`과 `headers` 값에서 `${ENV_VAR}` 패턴은 런타임에 환경변수로
> 대체된다. 변수가 없거나 빈 값이면 원본 그대로 유지된다. 시크릿을 config에 하드코딩하지
> 않고 환경변수로 주입할 수 있다.

> **HTTP vs HTTPS**: 둘 다 지원된다. 테스트·개발 환경에서 `http://`를 사용할 수 있고,
> 프로덕션에서는 `https://`를 권장한다.

> ⚠️ **시크릿 주의**: `env`에 토큰을 직접 적으면 `config.toml`에 평문 저장된다. 프로젝트
> `.magi/config.toml`을 repo에 커밋한다면 토큰을 넣지 말 것 — 전역 `config.toml`에 두거나,
> 래퍼 스크립트가 OS 키체인/`MAGI_*` env에서 읽어 자식에 넘기게 하라.

### 1.2 검증

1. 서버 바이너리를 **수동으로 먼저 실행**해 설치/PATH를 확인한다(예: `npx -y <pkg>` 가
   stdin 대기 상태로 멈추면 정상 — Ctrl+C로 종료).
2. 플렉서스 기동. 등록 실패는 stderr로 나온다:
   ```
   magi: mcp "github": <사유>
   ```
   (spawn 실패·핸드셰이크 실패·tools/list 실패 등) — 이 줄이 없으면 등록 성공.
3. TUI에서 **`/tools`** 로 등록된 툴 목록 확인. MCP 툴은 **서버가 보고한 이름 그대로**
   뜬다(접두사 없음). 헤드리스라면 `magi -p "사용 가능한 툴을 나열해줘"`.

### 1.3 동작 & 주의

- 권한: MCP 툴 호출도 일반 툴과 동일한 권한 모드(`ask`/`auto`/`allow`/`deny`)·정책 엔진을
  거친다. 위험한 외부 툴은 `deny`/정책 규칙으로 막을 수 있다.
- **이름 충돌**: 접두사가 없으므로 두 서버(또는 빌트인)가 같은 툴 이름을 내면 나중 등록이
  덮어쓴다. 충돌하면 서버 쪽 툴 이름을 바꾸거나 한쪽만 켜라.
- 서버가 도중에 죽으면 그 툴들만 레지스트리에서 빠지고 세션은 계속된다.

### 1.4 트러블슈팅

| 증상 | 원인/조치 |
|---|---|
| `mcp "x": exec: "cmd": not found` | `command`가 PATH에 없음 → 절대경로 지정 또는 설치 |
| 등록은 됐는데 `/tools`에 없음 | 서버가 `tools/list`에서 빈 목록 반환 → 서버 설정/인자 확인 |
| 호출 시 인증 에러 | `env` 토큰 누락/오타 → 1.1의 env 형식(`"KEY=VALUE"`) 확인 |
| 조용히 아무 일도 없음 | `[mcp.*]`가 잘못된 파일에 있음 → §0 경로/우선순위 재확인 |

---

## 2. 공유 경험(experience store / RAG) 부트스트랩

세션 시작 시 디렉터리의 **메모리·스킬을 키워드로 회수해 시스템 프롬프트에 주입**한다(D13).
`remember` 툴이 새 학습을 리뷰 큐에 기여하고, 디렉터리를 git repo로 두면 팀이 공유한다
(`internal/adapter/experience/git/store.go`).

> ⚠️ **정직한 한계**: 여기서의 "RAG"는 **임베딩 벡터/시맨틱 검색이 아니라 단어 겹침
> (term-overlap) 스코어링**이다. 승급도 자동이 아니라 **수동 리뷰**다. 시맨틱 검색이 필요하면
> 별도 ContextProvider/MCP 서버로 붙여야 한다.

### 2.1 디렉터리 만들기

기본 위치는 `<config>/experience`. 팀 공유하려면 별도 git repo를 만들고 `experience_dir`로
가리킨다.

```bash
mkdir -p /path/to/team-experience/{memories,skills,pending}
cd /path/to/team-experience && git init   # (선택) git이면 기여가 자동 commit됨
```

```toml
# config.toml
experience_dir = "/path/to/team-experience"   # 생략 시 <config>/experience
```

레이아웃:

```
<dir>/
  memories/*.md   # 승인된 메모리 — 파일 전체가 회수 대상 텍스트
  skills/*.md     # 승인된 스킬 — 첫 줄 = 설명, 이후 = 본문
  pending/*.md    # remember가 넣는 리뷰 대기 큐 (회수 안 됨)
```

### 2.2 메모리·스킬 파일 형식

- **메모리** (`memories/<무엇이든>.md`): **파일 전체 텍스트**가 회수 단위. 프론트매터 불필요.
  태그를 넣고 싶으면 본문에 `tags: a, b` 한 줄을 두면 그 단어들도 매칭에 들어간다.
  ```markdown
  이 repo의 통합 테스트는 MAGI_E2E_* env가 있어야 동작한다.
  없으면 t.Skip 되므로 CI 녹색이 곧 통과를 뜻하지 않는다.

  tags: testing, e2e, ci
  ```
- **스킬** (`skills/<이름>.md`): **첫 줄 = 설명**, 나머지 = 본문. 파일명(확장자 제외)이 스킬
  이름이 된다.
  ```markdown
  릴리스 컷 절차
  1. CHANGELOG 갱신 2. vX.Y.Z 태그 3. goreleaser가 CI에서 빌드…
  ```

### 2.3 회수 동작

- 매 세션 시작 시 사용자 프롬프트를 질의로 써서 **메모리 top 5 + 스킬 top 3**을 term-overlap
  점수로 골라 주입한다(`Retrieve`). 점수 0(겹치는 단어 없음)은 제외.
- 파일을 늘려도 주입되는 건 상위 몇 개뿐 — 메모리는 **짧고 단일 사실** 단위로 쪼개는 게
  회수 정확도에 유리하다.

### 2.4 기여 & 리뷰 (`remember`)

- 에이전트(또는 사용자가 시켜서)가 `remember` 툴을 부르면 `pending/`에
  `mem-<타임스탬프>-<n>.md`로 저장되고, git repo면 best-effort로 commit된다.
  **바로 회수되지 않는다** — 리뷰 큐일 뿐.
- **승급(리뷰)**: 사람이 `pending/`의 파일을 확인하고 좋은 것만 `memories/`(또는 `skills/`)로
  옮긴다. 그래야 회수 대상이 된다.
  ```bash
  cd "$EXPDIR"
  mv pending/mem-20260622-120000-0.md memories/   # 검토 후 승급
  git add -A && git commit -m "experience: approve memory"   # 팀 공유 시
  ```
- 🔒 **`remember`는 시크릿을 저장하면 안 된다** — 툴 설명에 명시돼 있고, 기여는 평문 .md로
  남아 git에 박힌다. 토큰/키/비밀번호는 절대 넣지 말 것.

### 2.5 팀 공유

`experience_dir`를 git repo로 두고 팀이 **pull로 받고, 리뷰 후 push**한다. magi는 기여 시
best-effort `git commit`만 한다(자동 push/pull은 안 함) — pull/push는 팀 워크플로에 맡긴다.

### 2.6 트러블슈팅

| 증상 | 원인/조치 |
|---|---|
| 메모리가 주입 안 됨 | 파일이 `pending/`에 있음(승급 필요) / 질의와 겹치는 단어 없음 / 빈 파일 |
| `remember`가 "unavailable" | `experience_dir` 미설정이고 기본 경로도 없음 → §2.1로 디렉터리 생성 |
| commit이 안 됨 | 디렉터리가 git repo가 아님 → `git init`(없어도 파일 저장 자체는 됨) |

---

## 3. 플러그인에서 MCP·Context Provider 등록 (Lua)

`config.toml` 선언 외에, **Lua 플러그인**이 런타임에 직접 MCP 서버나 Context Provider(RAG)를
등록할 수 있다. 플러그인 호스트가 MCP 매니저·컨텍스트 레지스트리·런타임 정보를 주입받았을 때만
활성화된다(`cmd/magi/main.go`).

### 3.1 `magi.register_mcp` — HTTP MCP 서버 등록

```lua
-- 정적 헤더
magi.register_mcp{
  name = "svc",
  url = "http://localhost:3000/mcp",
  headers = { Authorization = "Bearer abc" },
}

-- 동적 헤더: 함수는 매 요청마다 재평가된다(요청 시점 값 반영, 등록 시점 freeze 아님)
magi.register_mcp{
  name = "svc",
  url = "http://localhost:3000/mcp",
  headers = function()
    return {
      ["X-Model"]     = magi.model(),     -- 현재 모델
      ["X-Platform"]  = magi.platform(),  -- darwin/linux/windows
      ["X-Timestamp"] = magi.time(),      -- 요청 시각 (RFC3339)
    }
  end,
}
```

> **정적 vs 동적**: 테이블이면 헤더가 고정(`AddHTTP`), 함수면 **요청마다 호출**(`AddHTTPDynamic`)된다.
> 함수는 플러그인 Lua 락 아래에서 직렬 실행되어 동시성에 안전하다. 시각/모델/토큰처럼 매 요청
> 바뀌는 값에 함수를 쓰라.

런타임 정보 API: `magi.model()`, `magi.platform()`, `magi.time()`, `magi.workdir()`.

> 🔐 **`magi.nonce(nbytes?)`** — `nbytes`(기본 16) 바이트의 암호학적 난수를 hex 문자열로 반환
> (`crypto/rand`). 샌드박스의 `math.random`은 **결정론적으로 시드**되므로(os 제거로 시계 시드 불가)
> OAuth/PKCE `state`·CSRF 토큰·요청 ID 같은 **보안 값엔 절대 `math.random`을 쓰지 말고 `magi.nonce`를 써라.**

### 3.2 `magi.register_context_provider` — RAG 컨텍스트 주입

등록한 provider는 **최상위 에이전트의 매 스텝에서 호출**되어, 반환한 chunk가 시스템 프롬프트의
`# Retrieved context` 섹션으로 주입된다(provider당 5초 타임아웃, 합산 8KB 예산으로 cap, 실패한
provider는 턴을 막지 않고 무시). 서브에이전트는 집중 프롬프트라 호출하지 않는다.

```lua
magi.register_context_provider{
  name = "project-rag",
  provide = function(q)
    -- q.session_id, q.workdir, q.prompt 제공
    local hits = my_search(q.prompt)            -- 임의의 검색 로직
    local chunks = {}
    for _, h in ipairs(hits) do
      table.insert(chunks, { source = h.path, text = h.snippet })
    end
    return chunks                                -- {source=, text=} 배열
  end,
}
```

### 3.3 `magi.register_command` — TUI 슬래시 커맨드 등록

플러그인이 `/login`, `/logout` 같은 슬래시 커맨드를 직접 등록한다(capability `"command"`).
TUI가 내장 커맨드에 없는 슬래시를 받으면 플러그인 커맨드로 위임하고, 팔레트·자동완성에도
동적으로 노출된다. `name`은 슬래시 없이 지정하고(`"login"` → `/login`), `execute`는 커맨드
이후 토큰 배열을 받는다. **비어 있지 않은 문자열을 반환하면 에러 메시지**로 처리되고, `nil`이면
성공이다(스낵바에 `✓`).

```lua
magi.register_command{
  name        = "login",
  description = "Re-authenticate with DS AD SSO",  -- /help·팔레트에 표시
  execute     = function(args)
    -- args = "/login" 이후 공백 분리 토큰
    local ok = do_sso_login(args[1])
    if not ok then return "SSO 로그인 실패" end     -- 에러: 스낵바에 표시
    -- 성공: nil 반환
  end,
}
```

### 3.4 `magi.set_llm_headers` — LLM 백엔드 커스텀 헤더

사내 게이트웨이(LiteLLM 등)가 `X-CLIENT-API-KEY` 같은 헤더를 요구하거나, 브라우저 SSO로 발급한
토큰을 인증키로 써야 할 때 사용한다. 테이블이면 정적, 함수면 **요청마다 재평가**된다.

```lua
-- 정적
magi.set_llm_headers({ ["X-CLIENT-API-KEY"] = "abc" })

-- 동적: 회전 토큰을 매 요청마다 반영 (예: 파일에 갱신되는 SSO 토큰을 읽어 주입)
magi.set_llm_headers(function()
  local tok = magi.read_file(".magi/adsso.token") or ""
  return { Authorization = "Bearer " .. tok }
end)
```

> 정적 키만 필요하면 **플러그인 없이** `config.toml`로도 된다:
> ```toml
> [llm.headers]
> X-CLIENT-API-KEY = "${LITELLM_CLIENT_KEY}"   # ${ENV} 확장 지원
> ```
> 두 경로(config 정적 + 플러그인 동적)는 함께 적용되며, 동적 헤더가 나중에 덮어쓴다.

### 3.5 게이트된 기능: `exec` · `open_url` · `http`

플러그인이 **외부 프로세스 실행 / 브라우저 열기 / HTTP 호출**을 하려면 `plugin.toml`의
`permissions`에 명시해야 한다. 선언하지 않으면 브리지에서 거부된다(`permission denied: …`).
RAG를 HTTP로 가져오거나, SSO 로그인 흐름을 플러그인이 직접 구동할 때 쓴다.

| API | 권한 | 비고 |
|---|---|---|
| `magi.exec(cmd, {args})` | `exec:<cmd>` | 셸 없이 직접 실행(인젝션 없음), workdir 기준, 60s 타임아웃. `{stdout,stderr,code}` 반환 |
| `magi.open_url(url)` | `exec:open-url` | OS 기본 브라우저로 엶. **http/https만** 허용 |
| `magi.http{url,method,headers,body}` | `net:<host>` | http/https만, 30s 타임아웃, 5MB 응답 cap. `{status,body}` 반환 |
| `magi.serve{port,handler}` | `net:listen` | `127.0.0.1`에 **상주 HTTP 서버**를 인프로세스로 띄움(외부 런타임 불필요 → 단일 바이너리·전 OS 동일). `port=0`은 자유 포트 자동 배정. `{port, stop()}` 반환 |
| `magi.set_base_url(url)` | `net:<host>` | 에이전트의 **LLM 백엔드 base URL을 런타임 변경**(loopback 프록시 또는 로그인 시 알아낸 게이트웨이로). 빈 문자열이면 원복. 언로드 시 자동 원복. http/https만. ⚠️ 에이전트가 **진짜 API 키와 모든 프롬프트를 대상에 보내므로**, `net:<host>` 부여 = 그 호스트로 LLM 트래픽 리다이렉트 허용 — **호스트를 명시적·최소로** 부여하라 |
| `magi.set_model(model)` | `config:write:model` | **현재 세션의 활성 모델을 런타임 변경**(그리고 config에 영속 — `/route` 편집기와 동일). 다음 루프 반복부터 적용. 빈 문자열 거부, 성공 시 `true` / 실패 시 `(nil, err)`. 로그인 후 사용 가능한 백엔드를 알아내 모델을 정하는 SSO 플러그인 등에 유용. `magi.model()`(읽기)도 함께 갱신되어 즉시 새 값을 반환 |
| `magi.reload_config()` | `config:write:model` | **디스크의 config.toml을 다시 읽어 런타임 적용** — 현재는 세션 모델. 파싱 실패면 `(nil, err)`를 반환하고 실행 중 세션은 기존 설정을 유지(잘못된 편집이 모델을 조용히 비우지 못하게). 라우팅·base URL·플러그인 리로드 등 나머지 설정은 재시작 필요. `set_config_key`로 모델을 바꾼 뒤 반영할 때 유용 |
| `magi.clear_transcript()` | (없음 — UI 전용) | **화면의 대사록을 splash로 초기화**(디스크의 세션은 보존). 플러그인 `/logout` 커맨드가 로그아웃 후 깨끗한 시작 화면으로 되돌릴 때 사용. `true` 반환 |
| `magi.get_config_key(key, default?)` | `config:read:<key>` | 사용자 **config.toml**에서 dotted 키(`routing.model`, `plugins.<name>.token`) 읽기. 자기 섹션(`plugins.<name>.*`)은 권한 없이 허용. **키 부재 → `default`; 파일 파싱 실패 → `(nil, err)`**(둘을 구분하니, 깨진 config를 덮어쓰는 악순환을 피하려면 err를 확인하라) |
| `magi.set_config_key(key, value)` | `config:write:<key>` | config.toml에 dotted 키 쓰기(**주석 보존**, `config.SetKey`). 값은 문자열, 빈 문자열이면 키 삭제. 자기 섹션은 권한 없이 허용. top-level 키는 기존 활성 줄을 갱신하고 주석 처리된 템플릿 기본값은 건드리지 않음(중복 키 생성 방지) |

> 🔑 **store_get/store_set vs get/set_config_key**: 앞쪽(`store_get`/`store_set`)은 플러그인 **자체 격리 JSON 저장소**(`config:` 권한 불필요). 뒤쪽(`get/set_config_key`)은 **사용자 config.toml** 직접 접근으로, **권한 게이트**된다. 권한은 `config:read:<key>` / `config:write:<key>`이며 **끝에 `*`로 prefix 와일드카드**(예: `config:write:routing.*`, `config:write:*`). 자기 섹션 `plugins.<name>.*`는 암묵 허용. 키는 `[A-Za-z0-9_-]` dotted segment만 허용(주입 방지). **고정 deny-list**(권한이 있어도 차단): `mcp`·`hooks`·`allow`·`deny`·`permission`·`sandbox`·`profile`·`allow_domains` (명령 실행/보안 포스처 변경 영역).

**예: ADSSO 로그인 → 토큰을 LLM 인증헤더로 (플러그인이 흐름까지 구동)**
```toml
# plugin.toml
name = "adsso"
permissions = ["exec:open-url", "net:sso.corp.example", "fs:write:.magi/"]
```
```lua
-- init.lua: 시작 시 브라우저로 로그인 → 콜백 토큰을 교환해 캐시, 매 요청 주입
local token = ""
local function login()
  magi.open_url("https://sso.corp.example/authorize?...")   -- 브라우저 오픈
  -- (콜백/폴링으로 code 수령 후) 토큰 교환:
  local r = magi.http{ url = "https://sso.corp.example/token",
                         method = "POST", body = "grant_type=..." }
  if r and r.status == 200 then token = r.body end
end
login()
magi.set_llm_headers(function() return { Authorization = "Bearer " .. token } end)
```

> ⚠️ `exec`/`http`는 샌드박스를 넓히는 강력한 권한이다. 신뢰하는 플러그인에만, 최소 host/cmd로
> 좁혀 부여하라. (정적 키만 필요하면 §3.4의 `config.toml [llm].headers`로 충분하다.)

### 3.6 라이프사이클 훅 · 사용자 프롬프트 · 콜백 (SSO 등)

플러그인이 **시작 시점에 사용자와 상호작용**(인증 등)할 수 있는 범용 통로.

- **`magi.on(event, fn)`** — 호스트가 정해진 시점에 호출하는 핸들러 등록.
  이벤트: `startup`(플러그인 로드 후·첫 턴 전, UI 준비됨), `session_start`(세션 생성 후), `shutdown`(종료).
  핸들러는 **동기 실행**되어 블로킹 가능(예: 시작 시 인증 완료까지 대기).
- **`magi.ask{title, fields}`** — 인터랙티브 폼. 필드 `type`: `text`·`password`·`number`·`multiline`·
  `select`·`multiselect`·`confirm`·`note`. 답을 테이블로 반환. **TTY 없으면(헤드리스) 에러** → 폴백 처리.
  필드: `{ name=, type=, label=, options={}, default= }`. (Tab=제출, Esc=취소)
- **`magi.serve`** — `127.0.0.1`에 루프백 HTTP 서버. 두 모드, 둘 다 `net:listen` 필요:
  - **handler 있음 (상주)**: `magi.serve{port, handler=function(req) … end}` → 모든 요청을 `handler(req)`로 라우팅, 반환 테이블이 응답. `port=0`이면 자유 포트 자동 배정. `{port, stop()}` 반환. 언로드/리로드 시 자동 종료.
  - **handler 없음 (일회성 블로킹, OAuth/PKCE 리다이렉트 수신)**: `magi.serve{port, path, timeout}` → 첫 매칭 요청까지 블록 후 `{query={...}, path=}` 반환하고 종료.
  요청: `{ method, path, query={k=v}, headers={k=v}, body }`,
  응답: `{ status=200, headers={k=v}, body }`(또는 문자열만 반환 → 200 본문).
  **인프로세스**라 외부 런타임 없이 단일 정적 바이너리 안에서 동작 — 모든 OS에서 동일.

**예: ADSSO — 시작 시 "브라우저 로그인 / 토큰 붙여넣기" 메뉴 (순수 플러그인, 코어 무수정)**
```toml
# plugin.toml
name = "adsso"
permissions = ["exec:open-url", "net:listen", "net:sso.corp.example", "fs:write:.magi/"]
```
```lua
-- init.lua
magi.on("startup", function()
  if magi.store_get("adsso.token") then return end            -- 이미 있으면 패스
  local a = magi.ask{ title = "ADSSO 인증", fields = {
    { name = "how", type = "select", options = { "브라우저 로그인", "토큰 붙여넣기" } },
  }}
  if not a then return end                                        -- 헤드리스 등 → 폴백
  local token
  if a.how == "브라우저 로그인" then
    magi.open_url("https://sso.corp.example/authorize?redirect_uri=http://127.0.0.1:8765/cb&...")
    local cb = magi.serve{ port = 8765, path = "/cb", timeout = 120 } -- one-shot (no handler)
    local r = magi.http{ url = "https://sso.corp.example/token", method = "POST",
                           body = "grant_type=authorization_code&code=" .. cb.query.code }
    token = parse_token(r.body)
  else
    token = magi.ask{ fields = {{ name = "t", type = "password", label = "토큰" }} }.t
  end
  magi.store_set("adsso.token", token)
end)

-- 매 LLM 요청에 토큰 주입 (저장된 값을 읽어 — 재시작에도 유지)
magi.set_llm_headers(function()
  return { Authorization = "Bearer " .. (magi.store_get("adsso.token") or "") }
end)
```
→ 코어엔 ADSSO 흔적이 전혀 없다. "플러그인이 라이프사이클 시점에 사용자에게 묻고 환경과
상호작용한다"는 범용 기능만 제공한다.

### 3.7 `serve` + `set_base_url` — loopback LLM 프록시 (코어 무수정)

`magi.serve`로 플러그인이 **인프로세스 HTTP 서버**를 띄우고, `magi.set_base_url`로 에이전트의
LLM 트래픽을 그 서버로 돌릴 수 있다. 프롬프트/응답 로깅·요청 변형·모킹·요율 게이트 같은 것을
**외부 프로세스 없이**(= 단일 바이너리·전 OS 동일) 플러그인만으로 구현한다. 서버는 언로드 시 자동 종료.

```toml
# plugin.toml
name = "llm-proxy"
# net:listen=서버 호스팅, net:127.0.0.1=에이전트를 loopback으로 향하게, net:localhost=upstream 포워딩
permissions = ["net:listen", "net:127.0.0.1", "net:localhost"]
```
```lua
-- init.lua: 모든 LLM 요청을 가로채 로깅한 뒤 진짜 백엔드로 포워딩
local upstream = "http://localhost:11434/v1"   -- 원래 백엔드 (이 host엔 net: 권한 필요)
local s = magi.serve{ port = 0, handler = function(req)
  magi.log("LLM " .. req.method .. " " .. req.path .. " (" .. #req.body .. " bytes)")
  local r = magi.http{ url = upstream .. req.path, method = req.method,
                       headers = req.headers, body = req.body }
  return { status = r.status, body = r.body }
end }
magi.set_base_url("http://127.0.0.1:" .. s.port .. "/v1")   -- 에이전트를 프록시로 (loopback)
```
> 🔐 **`set_base_url` 보안**: 에이전트는 `base()`에 **진짜 API 키를 붙여 모든 프롬프트/응답을 보낸다.**
> 따라서 `net:<host>` 권한을 주는 것은 "그 호스트로 에이전트의 자격증명 트래픽을 돌려도 된다"는 명시적
> 승인이다 — 대상 host를 **명시적·최소로** 부여하라(RAG용으로 넓게 준 `net:` 권한이 리다이렉트까지
> 열어줄 수 있으니 주의). loopback 프록시면 `net:127.0.0.1`, 게이트웨이면 그 host를 선언한다. 플러그인
> 언로드/리로드 시 오버라이드는 **자동으로 원복**된다(죽은 대상을 가리킨 채 LLM이 멎지 않게).

> ⚠️ **한계**: ① `serve` 핸들러 응답은 `magi.http`로 받은 **완성된 본문**이라 토큰 단위 SSE
> **스트리밍이 아니다**(상류 완료 후 한 번에). 30s·5MB 캡도 그대로 적용되니, 이 프록시는 **로깅/모킹/짧은
> 완성**에 적합하고 장문 스트리밍 패스스루엔 부적합. ② 고정 포트(`port>0`)로 띄운 `serve` 플러그인은
> 핫리로드 시 이전 인스턴스가 포트를 쥔 채 새 인스턴스가 바인드해 실패할 수 있으니 **`port=0`(자동 배정)을
> 권장**한다.

---

## 더 보기

- 자체 **툴/훅**을 코드 없이 추가 → Lua 플러그인 (MANUAL §9, `plugins/examples/wordcount`)
- 셸 **라이프사이클 훅**(테스트/포맷 게이트) → MANUAL §하네스, `[[hooks]]`
- **포트/어댑터** 구조로 새 백엔드 구현 → ARCHITECTURE §3·§11
