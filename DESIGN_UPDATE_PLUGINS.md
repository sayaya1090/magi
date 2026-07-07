# 설계: 플러그인 자동 업데이트 + 스코프별 업데이트 커맨드 + TUI 한정 자동 체크

상태: **구현 완료(①②③ + 강제정책)**. 사용자 확정: git 체크아웃 + flag 3종 + 알림-only,
그리고 "패치=알림 / 마이너 이상=강제"(E-2). 아래 설계 중 실제 구현이 갈린 지점은 인라인 주석으로 표기.

## 구현 요약(코드 대응)
- **Layer ①** `internal/update/plugin/`: `Discover`/`UpdateOne`(fetch+`merge --ff-only`, 비-ff 거부)/`Install`
  (`git clone --depth 1`, plugin.toml 없으면 거부+정리). git remote=출처(매니페스트 필드 불필요).
  `internal/update.UpdatePolicy`(patch=Notify, minor/major=Force) 추가. 실 임시 git repo 테스트.
- **Layer ②** `cmd/magi`: `-update`(본체+플러그인)/`-update-core`/`-update-plugins`/`-plugin-install`/
  `-plugin-pin`. `runUpdateCmd` 스코프 디스패치(함수-변수 seam으로 라우팅 테스트). 테마 프로브 앞에서 조기종료.
- **Layer ③** `cmd/magi/autoupdate.go`: 인터랙티브 TUI 기동 직전 **동기** 체크(비동기 배너 대신 —
  force는 실행 중 바이너리 교체를 피하려면 어차피 launch 전 종료해야 하므로 동기가 더 단순·안전).
  게이트 `shouldCheckUpdates(!headless && isTTY && !optOut)`, 24h 스탬프 캐시, 3s 타임아웃,
  Notify=배너 후 계속 / Force=고지+취소창 후 설치+재시작 안내(설치 성공 시 launch 없이 종료).
  `-no-update-check`(+`MAGI_NO_UPDATE_CHECK`) 옵트아웃. **벤치 불변식 테스트로 잠금.**

## 배경(현행 사실)

- **본체 self-update**: `internal/update` — `update.Run(src, curVer, exe)`가 최신 GitHub 릴리스를
  받아 exe 교체. `src=NewGitHubSource("sayaya1090","magi")`. **오직 `magi -update`로만** 발화.
  공개 API: `Source.Latest(ctx) → Release{Version,...}`, `IsNewer(cur,latest) bool`,
  `Run(...) Result{Updated,From,To,Skipped}`, `Apply(bin,target)`.
- **매 실행 자동 체크 없음**: 평상시 기동엔 버전 조회 자체가 없음.
- **플러그인**: `<ConfigDir>/plugins/`, `<wd>/.magi/plugins/`, `-plugins <dir>`에서 `plugin.toml`
  가진 하위 디렉터리를 로드. 매니페스트 필드 = `name/version/description/entry/capabilities/permissions`.
  **출처(repo/url) 필드 없음**, 설치 커맨드 없음. fsnotify 핫리로드만 존재(`Host.LoadDir`, `Host.Watch`).

→ 병목: 플러그인을 갱신하려면 **각 플러그인의 출처를 먼저 기록**해야 함.

## 결정(권장)

- **① 플러그인 출처 = git 체크아웃 모델.** `plugin install`=`git clone`, `update`=`git pull --ff-only`.
  `.git` 있는 관리형 디렉터리만 갱신, 수동 배치 디렉터리는 안전하게 무시. 본체 self-update의
  GitHub 정합, 태그/브랜치 핀 자연 지원, 신규코드 최소. (git PATH 필요 — 없으면 명확히 스킵/경고.)
- **② CLI = flag 3종.** `-update`(본체+관리형 플러그인 전부), `-update-core`(바이너리만=기존),
  `-update-plugins`(플러그인만; 핫리로드라 재시작 불필요). 현행 `flag` 파서에 그대로 적합.
- **③ TUI 한정 자동 체크 = 알림-only.** `!headless && isTTY(stdout)`일 때만 비동기 체크→새 버전이면
  배너. 놀람 설치 없음(실행 중 바이너리 교체 위험 회피). ConfigDir 타임스탬프로 N시간 1회 캐시.
  **벤치는 headless라 어느 경로로도 미발화.**

## 변경 설계

### A. 플러그인 매니페스트 출처 필드
`internal/adapter/plugin/lua/manifest.go`의 `Manifest`에 추가:
```toml
# plugin.toml
source = "https://github.com/user/magi-plugin-foo"   # optional; git remote
pin    = "v1.2.0"                                      # optional; tag/branch/commit
```
- `Source string \`toml:"source"\``, `Pin string \`toml:"pin"\``. 둘 다 옵셔널(하위호환).
- 로드 동작엔 무영향(표시/갱신 메타데이터일 뿐). `source` 없으면 "갱신 불가(수동 관리)"로 표시.

### B. 플러그인 관리 패키지 `internal/update/plugin` (신규, 순수 Go + git 셸아웃)
프로덕션 app이 builtin을 import하지 않는 원칙과 동일하게, cmd 계층에서만 호출.
```go
type Managed struct{ Name, Dir, Source, Pin, Version string; Git bool } // Git=.git 존재

func Discover(dirs []string) []Managed           // plugin.toml + .git 스캔
func UpdateOne(ctx, m Managed) (changed bool, from,to string, err error)  // git fetch+pull --ff-only (pin이면 checkout)
func Install(ctx, url, pin, destRoot string) (Managed, error)            // git clone --depth 1 [--branch pin]
```
- `UpdateOne`: `git -C dir remote get-url origin`로 출처 확인 → `git fetch` → `pin`이면
  `git checkout <pin>` + `git pull --ff-only`, 아니면 현재 브랜치 `pull --ff-only`. 충돌/비-ff는
  에러로 보고(강제 안 함). 갱신 전후 `plugin.toml`의 `version`을 from/to로 사용.
- git 미설치/`.git` 없음 → `changed=false`, 사유 문자열(스킵).

### C. cmd/magi 업데이트 디스패치 (`update.go`)
`runUpdate()`를 스코프 인자로 일반화:
```go
type updateScope int
const ( scopeAll updateScope = iota; scopeCore; scopePlugins )

func runUpdate(scope updateScope, pluginRoots []string) int {
    if scope != scopePlugins { /* 기존 본체 self-update */ }
    if scope != scopeCore {
        for _, m := range pluginupd.Discover(pluginRoots) {
            changed, from, to, err := pluginupd.UpdateOne(ctx, m)
            // 결과 라인 출력(updated foo v1.0→v1.1 / up to date / skipped: no source)
        }
    }
    return rc
}
```
플래그(main.go `run()`):
```go
doUpdate        = flag.Bool("update", false, "update magi core and managed plugins, then exit")
doUpdateCore    = flag.Bool("update-core", false, "update only the magi binary, then exit")
doUpdatePlugins = flag.Bool("update-plugins", false, "update only managed plugins, then exit")
```
- 조기 종료 처리는 **테마 프로브보다 앞**에 배치(현행 lipgloss v2는 비-TTY에서 프로브를 이미
  스킵하므로 행 위험은 없으나, 의미상·프로브 왕복 절약을 위해 앞에 두는 게 깔끔).
- `pluginRoots = pluginDirs(plat, wd, *pluginsDir)` 재사용. 단 self-update만 하는 `-update-core`는
  plat 준비 불필요(기존 경로).

### D. 플러그인 설치 커맨드 (①의 짝)
```go
pluginInstall = flag.String("plugin-install", "", "git URL of a plugin to install into the user plugins dir, then exit")
pluginPin     = flag.String("plugin-pin", "", "optional tag/branch/commit for -plugin-install")
```
`<ConfigDir>/plugins/<repo-name>`로 clone. `source`/`pin`을 `plugin.toml`에 없으면 주입(또는
사이드카 `.magi-plugin.lock` — 매니페스트 불변 존중). **권장: 사이드카 lock** 파일로 출처 기록
(사용자가 만든 `plugin.toml`을 magi가 재기록하지 않도록). Discover는 lock 우선, 없으면 git remote.

### E. TUI 한정 자동 체크(알림-only)
`run()`의 인터랙티브 분기(=`!headless`)에서, TUI 기동 직전:
```go
if !headless && term.IsTerminal(int(os.Stdout.Fd())) && !*noUpdateCheck {
    go maybeNotifyUpdate(ctx, plat.ConfigDir(), llmSrc, version.Version, host, pluginRoots)
}
```
`maybeNotifyUpdate`:
1. `<ConfigDir>/.update-check`의 mtime이 N시간(기본 24h) 이내면 즉시 반환(캐시).
2. 짧은 타임아웃(예: 3s) 컨텍스트로 `src.Latest()` 조회, `IsNewer`면 TUI에 배너 이벤트 전송
   ("magi vX.Y 사용 가능 — `magi -update`"). 실패는 조용히 무시(오프라인 등).
3. 타임스탬프 파일 touch.
- 절대 자동 설치 안 함. 배너는 비침투적(스낵/상단 1줄). `-no-update-check` 플래그 + config 키로 옵트아웃.
- 플러그인 알림도 동일하게 확장 가능(관리형 플러그인 origin `git fetch --dry-run`) — v1은 본체만.

### E-2. 버전 델타 기반 업데이트 정책(patch=알림, minor↑=강제)
강제/알림 결정은 **semver 컴포넌트 델타로 결정론적**으로 계산(릴리스 마커 불필요):
```go
// internal/update: 순수 함수, 단위테스트로 잠금.
type Policy int
const ( PolicyNone Policy = iota; PolicyNotify; PolicyForce )

// current/latest 파싱(parseSemver 재사용). latest<=current면 None.
// 메이저(x) 또는 마이너(y) 증가 → Force. 패치(z)만 증가 → Notify.
func UpdatePolicy(current, latest string) Policy
//   1.2.3 → 1.2.4  : Notify (패치)
//   1.2.3 → 1.3.0  : Force  (마이너, "중간")
//   1.2.3 → 2.0.0  : Force  (메이저)
//   parse 불가/동일/구버전 : None (안전 기본 — 강제하지 않음)
```
- 동작(**TUI 인터랙티브 경로에서만**):
  - `PolicyForce` → 짧은 고지 + 취소창("필수 업데이트 vX 설치… ctrl-c로 취소", ~3s) 후
    `update.Run` 자동 설치, 완료 시 재시작 안내.
  - `PolicyNotify` → 기존 비침투 배너("vX 사용 가능 — `magi -update`").
  - `PolicyNone` → 아무 것도 안 함.
- **벤치/headless 불변식 유지**: 게이트가 `!headless && isTTY`이므로 마이너 범프(강제)여도 벤치·CI·
  `-p`·파이프엔 미발화. 옵트아웃 `-no-update-check`는 강제 경로도 함께 끈다(탈출구 보장).
- 파싱 불가 버전(예: 개발 빌드 `dev`)은 `PolicyNone` → 강제 안 함(안전 실패).
- 스코프: v1은 **본체**만 이 정책 적용. 플러그인은 알림/수동(`-update-plugins`)이 기본.

### F. 벤치 안전 불변식
- 자동 체크 게이트 = `!headless && isTTY`. 벤치/CI/`-p`/파이프/`-version`/`-doctor`는 headless 또는
  비-TTY라 **어느 경로로도 미발화**. 네트워크 접근 0, 기동 지연 0(비동기+캐시).
- 회귀 테스트로 이 불변식 잠금(아래).

## 테스트
- `internal/update/plugin`: fake git(또는 실제 임시 repo)로 `Discover`(source 유/무·`.git` 유/무),
  `UpdateOne`(ff 성공/비-ff 거부/미설치 스킵), `Install`(clone→매니페스트 파싱).
- `cmd/magi`: `runUpdate(scope)` 스코프별 호출이 본체/플러그인 경로를 올바로 타는지(주입 fake).
- **벤치 불변식**: `maybeNotifyUpdate` 게이트가 headless=true 또는 비-TTY에서 **호출 0회**임을 단언
  (자동 체크가 벤치에 새는 것 방지).
- TUI 배너: `IsNewer` 참일 때만 배너 이벤트 발생, 캐시 신선하면 미조회.

## 단계 구현안(레이어드 커밋 순서)
1. `manifest.go` source/pin 필드 + `internal/update/plugin` (Discover/UpdateOne/Install) + 테스트.
2. `cmd/magi` 스코프 디스패치 + `-update`/`-update-core`/`-update-plugins` + `-plugin-install` + 조기종료 재배치.
3. TUI 알림-only 자동 체크(게이트+캐시+배너) + 벤치 불변식 테스트 + `-no-update-check` 옵트아웃.

## 미확정(사용자 확정 대기)
- ① 출처 메커니즘(git 체크아웃 vs manifest+tarball vs 레지스트리) — 문서는 **git** 가정.
- ② CLI 형태(flag 3종 vs 서브커맨드) — 문서는 **flag 3종** 가정.
- ③ 자동 수위(알림-only vs 플러그인만 자동설치 vs 전부 자동) — 문서는 **알림-only** 가정.
- 자동 체크 주기 N(기본 24h), 배너 UX(스낵 vs 상단 고정), 옵트아웃 키 이름.
