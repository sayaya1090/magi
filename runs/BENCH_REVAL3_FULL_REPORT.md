# 벤치마크 재검증 #3 — 최종 보고서

> 🏁 **완주: 11/80 resolved (13.75%)** — 2026-07-03 07:2x~19:23, 첫 크래시 없는 완주(reval1·reval2는 tb 내부 레이스로 중도 크래시).
> **런 목적이었던 fabrication 하드닝 검증: 달성** — 위양성(가짜 done) 0건, 게이트 반려 후 실제 풀이 전환 성공 사례 실측.

## 1. 런 정보

| 항목 | 값 |
|---|---|
| Run ID | `2026-07-03__reval3-41e645e` |
| 바이너리 | HEAD `41e645e` = `44c30dd`(bash self-verify) + fabrication 하드닝 3커밋(`946fcd5`·`d32f460`·`41e645e`) |
| 데이터셋 / 하네스 | terminal-bench-core==0.1.1 (80태스크) / terminal-bench 0.2.18 |
| 모델 / 실행 | qwen3-coder:30b (로컬 Ollama) / `--n-concurrent 1`, MaxSteps 40 |
| 환경 | Podman machine (2GiB RAM), 런 전 이미지 전체 클린 |

## 2. 최종 스코어보드

**resolved 11**: blind-maze-explorer-algorithm.hard · fix-pandas-version · fix-permissions · git-workflow-hack · heterogeneous-dates · incompatible-python-fasttext.base_with_hint · organization-json-generator · sqlite-with-gcov · swe-bench-fsspec · swe-bench-langcodes · tmux-advanced-workflow

**실패 69 분포**:

| 클래스 | 건수 | 성격 |
|---|---|---|
| agent_timeout | 34 | 지배적 실패. MaxSteps 40 소진 + wall-clock(360~1000s) 소진 혼합 |
| 테스트 실패 (unset) | 23 | 산출물은 냈으나 오답 — 다수가 council 만장일치 done 후 실패(오검증) |
| unknown_agent_error | 7 | `docker compose build/up` 실패 — Podman VM 환경성 |
| parse_error | 3 | 채점 파싱 실패 (post-test 요약 부재 계열) |
| test_timeout / install_failed | 1 / 1 | jupyter-notebook-server / cron-broken-network(의도적 curl 사보타주 태스크) |

## 3. 런 간 비교

| 런 | 날짜 | 바이너리 | 결과 | 상태 |
|---|---|---|---|---|
| 배치 | 07-01 | fe0936e | **14/80** | 완주 (환경 위양성 ~10건 포함 시점) |
| reval1 | 07-02 | daca701 | 10/48 | 48/80에서 크래시 — 최종치 아님 |
| reval2 | 07-02 | 44c30dd | 3/44 | 45/80에서 크래시 (tb tmux 레이스) |
| **reval3** | 07-03 | **41e645e** | **11/80** | **완주** |

**reval1 승리 10개의 재현** — 유지 5 / 회귀 5:
- 유지 ✅: fix-pandas-version, git-workflow-hack, heterogeneous-dates, sqlite-with-gcov, swe-bench-fsspec
- 회귀 ❌: blind-maze-5x5, crack-7z-hash, crack-7z-hash.easy, prove-plus-comm (전원 agent_timeout), qemu-startup (parse_error)
- **판정: 로직 회귀 아님.** 회귀 5건 전원이 타임아웃 계열이고, 원인 분해 결과 모델 변동성(blind-5x5: 이번엔 가짜 경로로 빠짐 — 게이트가 없었어도 테스트는 실패했을 내용), 수렴 실패+echo 스텝 낭비(prove-plus-comm: coqc 10회 반복 후 소진), brute-force 시간 소요(crack-7z 계열)였다.

**reval3 신규 승리 6개**: blind-maze-algorithm.hard(아래 §4), incompatible-fasttext.hint(환경 회복), tmux-advanced-workflow, fix-permissions, organization-json-generator, swe-bench-langcodes.

**배치 14 대비 -3에 대해**: 배치의 14에는 이번 런이 환경/타임아웃으로 잃은 태스크가 섞여 있고, 커밋 기인 회귀로 지목할 단일 사례는 없음. 채점 페이스는 6.9/h(reval1)→5.5/h로 느려졌는데, 게이트·council 라운드의 wall-clock 비용 가설과 부합(태스크 믹스 차이로 단정은 불가).

## 4. 런 목적 판정 — fabrication 하드닝 (3커밋)

**위양성 0건. 목표 동작 실측됨.**

- **blind-maze-explorer-algorithm.hard ✅**: 에이전트가 시뮬레이션(가짜) 산출물로 done을 시도 → 게이트 반려 → **실제 풀이로 전환해 테스트 통과**. 하드닝이 의도한 최선의 경로.
- blind-maze 5x5·algorithm·easy: 게이트 반려 후 전환 실패 → 정직한 agent_timeout (이전 런들의 false-done 소멸).
- gpt2-codegolf·write-compressor: 동일 패턴(반려 → 타임아웃). 게이트가 없었어도 가짜는 테스트에서 떨어졌으므로 스코어 손실 아님.
- 비용: 반려된 에이전트가 placeholder를 "다듬으며" 잔여 예산을 태우는 패턴 — 런 이후 2회 에스컬레이션 상한(`2e10f06`)으로 완화.

## 5. 실패 클래스 심층 분석 (런 중 발견 1~12 통합)

### 5a. agent_timeout 34 (49%) — 지배적
- MaxSteps 40 소진(blind-maze류, swe-astropy, prove-plus-comm 등)과 wall-clock 소진(대형 설치·부팅: pytorch/train-fasttext/qemu 계열)의 혼합.
- 실측 낭비 요인: ① 서사용 echo가 스텝 소비(prove-plus-comm 마지막 스텝들), ② 반려 후 placeholder 재작업, ③ council 라운드(멤버 3 LLM 호출)의 wall-clock 잠식(타임아웃 태스크들에서 라운드 로그 6~13줄).
- **이 런은 MaxSteps 40 시대의 마지막 런** — 이후 기본 240(`d2e6a1e`)·스톨 백스톱(`0ce6f36`)·echo 금지·소프트 추정(`6082e78`)이 정확히 이 클래스를 겨냥한다.

### 5b. 오검증 (unset 23의 다수) — 최대 단일 "품질" 원인군
council 만장일치 done 후 테스트 실패가 반복 실측됨:
- **합리화된 done**: play-zork("문서 기반으로 엔딩은 맞다"), run-pdp11-code("불가능하므로 이것이 완료"), sqlite-db-truncate("손상 때문에 빈 배열이 정답"), chess-best-move(**fabrication 신호가 제시된 상태에서도** 서사로 3:0 통과).
- **확신에 찬 오작업**: hello-world — `write "Hello, world!"`(개행 누락, "wrote 13 bytes")를 하고 "개행 포함 확인"이라 서술, council 통과, 테스트는 `\n` 하나로 실패.
- **미검증 산출물**: password-recovery·security-vulhub-minio·new-encrypt-command·create-bucket — 산출물 존재만으로 done.
- 런 이후 대응: 합리화 반려(`7521cab`), 검증 실행 요구 — "존재≠정확성"(`8aad495`), 교착 UNVERIFIED 표기(`81fea75`), 무변경 재제출 재심의 스킵(`5fb0dd4`).

### 5c. 환경성 10 (unknown 7 + parse 3)
- unknown 7 전원이 `docker compose build/up` exit 1 (conda-env, build-initramfs/tcc-qemu, oom, path-tracing, simple-sheets-put, simple-web-scraper). **클린 디스크로 시작한 당일에도 발생** → 디스크 고갈만이 아니라 **Podman VM RAM 2GiB 부족**이 유력. 다음 벤치 전 `podman machine set --memory 8192` 필수.
- parse 3(qemu-startup, pytorch-model-cli.hard, cartpole-rl-training): 타임아웃 후 post-test 요약 부재 — tb 0.2.x 채점기 계열, harbor(TB 2.1) 이관으로 소멸 기대.
- cron-broken-network(install_failed): 태스크가 /usr/bin/curl을 1초마다 사보타주 → 우리 설치 스크립트의 curl 다운로드가 사망. **network-free 설치(`fac076b`, docker cp)로 해결 완료** — 재실행 시 스모크 확인.

## 6. 이 런이 촉발한 수정 (전부 main에 커밋됨)

| 영역 | 커밋 | 내용 |
|---|---|---|
| 예산 | `d2e6a1e` | MaxSteps 기본 40→240 (상한은 백스톱, 페이싱은 가드가) |
| 가드 | `0ce6f36` | 스톨 넛지 소진 후 force-stop(`stall_guard`) + bash 쓰기의 진전 인정 |
| 게이트 | `2e10f06` | 서사 echo 스텝 금지 + fabrication 반려 2회 에스컬레이션 후 양보 |
| council | `7521cab`·`8aad495`·`81fea75` | 합리화된 done 반려 · 검증 실행 요구 · 교착 UNVERIFIED |
| council | `5fb0dd4`·`f15637c` | 무변경 재제출 재심의 스킵 + TUI 중복 답변 접기 |
| 벤치 인프라 | `fac076b`·`b741c25` | network-free 설치(binary_path) + TB 2.1 harbor 어댑터 |

## 7. 다음 벤치

- **TB 2.1 (harbor)** 로 전환: `harbor run --agent-import-path bench.harbor.magi_agent:MagiAgent --dataset terminal-bench/terminal-bench-2-1` + `MAGI_BENCH_BINARY_PATH=/tmp/magi-serve`
- 사전: **podman machine 리사이즈(RAM 8GiB+)** + 이미지 클린 → §5c 환경성 소멸 확인
- MaxSteps 240 재조정 기준: `max_steps` 종료 중 "직전 윈도우에 실제 mutation이 있던"(진행 중 잘림) 사례 발생 시 400~500으로 상향

---

## 부록 A. 진행 로그 (요약)

> 틱 1~5 시각은 mtime 역산 근사. 초기 틱의 "0 resolved"는 잘못된 grep 필드(`resolved` vs `is_resolved`)로 인한 과소집계였음(11:4x 정정).

| 틱 | 시각 | 채점 | resolved | 비고 |
|---|---|---|---|---|
| 1~5 | ~07:43–11:00 | 3→18 | (실제 0→3) | 최난도 초반 구간; conda-env compose 실패, astropy-1 1000s 타임아웃 |
| 6~9 | 11:33–13:03 | 20→27 | 3 | qemu-startup·pytorch.hard 파싱 실패, fasttext류 타임아웃 |
| 10~12 | 13:33–14:33 | 33→39 | 4 | +tmux-advanced-workflow (reval2 실패였던 태스크) |
| 13~15 | 15:03–16:03 | 43→49 | 6→8 | reval1 승리 구간 진입: heterogeneous·sqlite-gcov 유지, fix-permissions·org-json 신규 |
| 16~19 | 16:33–18:31 | 54→67 | 8→11 | git-workflow-hack·fix-pandas 유지, swe-langcodes 신규 |
| 20 | 19:23 | **80** | **11** | tb 정상 종료, run-level results.json 확정 |

## 부록 B. 런 중 심층 분석 아카이브

상세 원문(발견 1~12, 중간 비교표)은 git 이력의 이 파일 이전 리비전 참조 (`git log --follow runs/BENCH_REVAL3_FULL_REPORT.md`). 핵심은 §4·§5에 통합됨.
