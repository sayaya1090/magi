# 개선안: 유효 concern이 N라운드 미해결일 때 수단 주입·전략 전환

상태: 제안(uncommitted). 근거: 2026-07-07 wt-20 벤치 `pypi-server` trial 로그.

## 1. 문제 (로그로 확증)

`pypi-server` 태스크는 900s 벽시계(`AgentTimeoutError`)로 죽었다. 채점 테스트 `test_api`는
`python -m pip install --index-url http://localhost:8080/simple vectorops==0.1.0` 를 실제로 실행하며,
:8080에서 **살아있는 PEP 503 인덱스 서버**를 요구한다.

로그(`agent/magi-stdout.txt`)가 보여준 진행:

- council done-gate가 round 1→2→3에 걸쳐 계속 CONTINUE. 매 라운드 **문구는 다르지만 본질은 같은 concern**:
  - r1: "PyPI 서버가 실제로 돌고 있다는 증거가 없다"
  - r2: "포트 8080에서 pip 설치되는 실증을 내라"
  - r3: "Missing evidence that the PyPI server is running"
- 에이전트는 서버를 detached로 못 띄우고(`http.server &` → bash 툴 셸 종료 시 프로세스 사망, `timeout 5` → 5초 후 사망),
  결국 `README_SERVER_SETUP.md`·`verify_installation.py` 같은 **설명/시뮬레이션 텍스트**만 새로 만들며
  "I can't actually run it in this environment but it's properly configured"를 반복.

즉 **concern은 정당**(채점 스펙과 일치)했고 council은 거짓완료를 올바르게 거부했다. 문제는 concern이
유효한데 에이전트에게 만족시킬 수단이 없을 때, 루프가 수렴도 조기중단도 못 하고 벽시계까지 헛돈 것이다.

## 2b. 코드 재확인으로 정정된 사실 (착수 전 검증)

구현 착수 전 실제 코드를 읽어 제안서 초안의 전제 두 가지를 정정한다:

1. **council done-gate의 텍스트 피드백은 concern 원장에 key화되지 않는다.** `council_gate.go`가 raise하는
   keyed concern은 `self-check/unverified-premise`(N14 조작방지) **하나뿐**(L26-29, L412-431). 라운드별
   피드백은 `councilRounds`(카운트)와 `lastCouncilFeedback`(텍스트)로 **턴-로컬**로만 추적된다
   (loop.go:134-135). ⇒ 트리거를 "concern key 지속성"이 아니라 **턴 내 council CONTINUE 횟수
   (`councilRounds`)** 로 잡는다(언어무관, 이미 존재).
2. **Rung 3(deadlock→위임 재분해)는 이미 구현돼 있다.** loop.go:393-407이 `councilDeadlock` 시
   `redecomposeStuck`로 fresh 자식을 depth+1 스폰한다. **재구현 금지.** 다만 pypi 로그엔 redecompose/
   deadlock 마커가 전혀 없이 912줄에서 harbor 900s로 잘렸다 — 즉 **발동조차 못 했다.** 이유: 게이트는
   에이전트가 "done"을 주장할 때만 돌고, 라운드 캡(카운트)은 실행 중간을 못 끊는다. round 3 재주입 후
   에이전트가 다음 "done"에 도달하기 전에 벽시계가 먼저 소진됐다. 설령 발동했어도 fresh 자식이 같은
   능력 공백(detached PEP503 서버)에 재차 막힌다 — **redecompose는 컨텍스트를 바꿀 뿐 결핍된 수단을
   주지 않는다.**

⇒ **비중복·최고가치 조각은 Rung 1(수단 주입).** 다음 시도의 궤적을 바꿔 애초에 벽시계 churn에 빠지지
않게 한다. Rung 3은 그대로 두고, Rung 1을 첫 슬라이스로 착수한다.

## 2. 루프 메커니즘상의 근본 원인 (코드)

`internal/app/council_gate.go`:

- CONTINUE 시 피드백을 council-authored 프롬프트로 주입(L559-565), 다음 턴에서 루프 재실행.
- **no-progress 가드는 피드백 텍스트 동일성만 검사**(L552-556: `fb == "" || fb == *lastFeedback`).
  council이 같은 concern을 매번 다른 문장으로 표현하면 이 가드를 우회한다 — pypi가 정확히 이 경우.
- 안전장치는 라운드 캡(`CouncilMaxRounds` 기본 3, L308-327 → UNVERIFIED 종료 + `deadlocked=true`)과
  비용 캡(L332-340)뿐. 그러나 **캡은 "council 게이트 호출 횟수"만 센다.** 게이트는 에이전트가 "done"을
  주장할 때만 돈다. pypi는 게이트 사이사이 에이전트가 대량의 bash/write로 헛돌아 **캡에 닿기 전에 harbor
  900s가 먼저** 발동했다.
- `deadlocked *bool` out-param은 주석(L287-291)에 "stuck-recovery hook이 '매 라운드 미충족'을
  구분하도록"이라고 명시돼 있으나 **아직 이를 소비하는 복구 로직이 없다.** 배선만 돼 있는 훅 지점.
- concern 원장(`internal/app/concern.go`)은 concern을 **key 단위**로 open/resolved 추적한다. 즉
  "같은 concern key가 몇 council 라운드째 열려 있나"는 이미 계산 가능하다(텍스트 동일성보다 견고한 신호).

핵심 간극 두 가지:
1. **정체 판정이 약하다** — 텍스트 동일성(우회 쉬움) 대신 *concern key 지속성*으로 봐야 한다.
2. **정체 시 행동이 없다** — 같은 반대만 재주입한다. "어떻게 충족하는지"도, 전략 전환도, 조기 손절도 없다.

## 3. 제안 — concern-지속성 기반 3단 에스컬레이션 사다리

트리거는 **턴 내 council CONTINUE 횟수(`councilRounds`, 이미 존재)** 다(§2b 정정: 피드백은 key화되지
않으므로 concern-key 지속성이 아님). 단계적으로 대응하며, 각 단은 게이트를 *약화*하지 않는다 — 자동 승인은 없다.
**첫 착수 = Rung 1** (비중복·최고가치). Rung 3은 이미 구현됨(loop.go:393-407), 재구현하지 않는다.

### Rung 1 — 수단 주입 (means injection)
같은 concern이 `K1`(기본 2)라운드 지속되면, 재주입 프롬프트에 반대만 넣지 말고 **"이것을 충족하는 구체적
수단"**을 함께 붙인다. 태스크별 하드코딩이 아니라 concern 범주에서 파생되는 **일반 휴리스틱 레시피**:

- concern이 "프로세스/서버/데몬이 상시 실행" 부류일 때 → 백그라운드 상시화 레시피:
  `setsid <cmd> >/tmp/x.log 2>&1 < /dev/null &` 또는 `nohup <cmd> & disown`; 이어서
  `sleep 1 && curl -sf <health-url>` 로 **실제 기동을 자기검증**하라는 지시.
- concern이 "산출물이 실제로 동작/설치/빌드됨을 실증" 부류일 때 → 채점이 하는 것과 동일한 end-to-end
  명령을 직접 돌려 통과를 보이라는 지시(예: pip index면 PEP 503 `/simple/` 서버 필요 — 단순 파일서버
  `http.server`로는 불충분함을 명시).

레시피 매칭은 concern 텍스트/kind에 대한 소수의 키워드 규칙으로 시작(과설계 금지). 목적은 "정답을
떠먹여주기"가 아니라 **에이전트가 반복해 놓치는 실행 수단의 존재를 알려주는 것.**

### Rung 2 — 결정적 신호로 승격 (promote to deterministic signal)
`K2`(기본 3)라운드까지도 열려 있으면, concern을 **검증 명령으로 승격**한다(기존 D16 `port.Signal`
경로 재사용). 오케스트레이터가 concern에서 검증 커맨드를 도출해 컨테이너에서 실행하고:

- 실패 → 그 **실제 오류 출력**을 에이전트에 준다(모호한 반대 대신 구체적 스택/exit code). 동시에 council에
  결정적 fail 신호로 투입 → 텍스트 설득만으로는 done 못 됨.
- 통과 → concern을 **자동 resolve**(`resolveconcern` 경로) → 헛된 라운드 종료.

pypi라면 검증 커맨드 ≈ 채점의 `pip install --index-url :8080/simple <pkg>`; 실패 시 에이전트는 "설정은
됐다"가 아니라 실제 connection-refused/404를 보고 서버 부재를 직시하게 된다.

도출 방식은 보수적으로: (a) 태스크가 제공한 예시 명령이 있으면 그것, (b) concern kind별 템플릿(health
curl 등), (c) 도출 불가하면 이 단은 건너뛰고 Rung 3로.

### Rung 3 — 전략 전환 / 조기 손절 (**이미 존재 — 재구현 금지**)
`councilDeadlock`(라운드 캡 소진) 시 loop.go:393-407이 `redecomposeStuck`로 fresh 자식을 depth+1
스폰하는 로직이 이미 있다. 남은 개선 여지(별건, 이번 착수 범위 아님):

- redecompose 자식에 **Rung 1에서 축적된 means-recipe를 전달**해, fresh 컨텍스트가 같은 능력 공백에
  다시 막히지 않게 한다(현재는 컨텍스트만 바꾸고 결핍 수단은 안 줌).
- deadlock이 **벽시계 소진 전에** 당겨지도록: pypi처럼 게이트 재도달 전에 죽는 경우를 위해 Rung 1이
  애초에 churn을 줄이는 것이 1차 방어. (라운드 캡을 시간 기반으로 바꾸는 것은 리스크가 커 비목표.)

## 4. 코드 훅 지점

- `internal/app/council_gate.go` L557 부근(CONTINUE 재주입 직전) — 정체 판정 후 Rung 분기 삽입.
- `internal/app/concern.go` — "concern key가 최근 몇 council 라운드 연속 open인지" 카운터 추가(원장이
  이미 key/open을 추적하므로 소규모).
- 기존 `deadlocked *bool`(L286-291) — Rung 3 조기 손절이 소비.
- 기존 `port.Signal`/D16 배선(L389-399 부근) — Rung 2가 재사용.
- 위임은 기존 SpawnRequest/CloneContext 경로 재사용(refine 자식과 동일 메커니즘).

## 5. 가드레일

- **게이트를 약화하지 않는다**: 어떤 Rung도 concern을 자동 승인하지 않는다. Rung 1은 힌트, Rung 2는 더
  엄격한(결정적) 판정, Rung 3은 위임 또는 손절.
- 레시피/커맨드 도출은 **일반 휴리스틱**이어야 하며 특정 태스크명·정답 하드코딩 금지(벤치 오염 방지).
- 전 기능 **opt-in 플래그**(`[council] stuck_escalation` 또는 `MAGI_COUNCIL_ESCALATE`) 뒤에 두고,
  off일 때 현행 동작과 바이트 동일.
- 모든 단은 **유한**: `K1<K2<K3`로 단조 진행, Rung 2 커맨드는 타임아웃 bounded, 위임은 1회.

## 6. 검증 계획

- 단위: fakeCouncil을 "매 라운드 다른 문구·같은 concern key"로 스크립트 → (a) 텍스트가 달라도 정체가
  탐지되는지, (b) Rung 1/2/3가 `K` 경계에서 정확히 발동하는지, (c) off 플래그에서 현행과 동일한지.
  기존 `council_test.go` 픽스처 재사용.
- 통합(오프라인): concern 원장 카운터가 council 라운드에 걸쳐 올바르게 증가/리셋되는지.
- 라이브(벤치): `pypi-server` 재실행 → Rung 2에서 실제 pip 실패를 에이전트가 받고 `pypiserver`(PEP503)로
  전환하는지, 또는 Rung 3에서 벽시계 전에 손절되는지. off 대조군과 per-test 비교.

## 7. 비목표

- council을 느슨하게 만들기(회귀 위험 — pypi에서 council은 채점과 정확히 일치했다).
- 태스크별 정답 주입/하드코딩.
- 백그라운드 프로세스 실행 자체를 에이전트 대신 오케스트레이터가 대행(에이전트 능력 공백은 Rung 1/2가
  *유도*할 뿐, 대신 실행하지 않는다).
