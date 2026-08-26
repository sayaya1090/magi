# Terminal-Bench 2.1로 magi 벤치마킹하기

무엇을 재는지, 직접 돌리는 법, 그리고 나온 숫자.

[Terminal-Bench](https://www.tbench.ai)는 도커 컨테이너 안의 진짜 터미널과 과제를 에이전트에게 주고
해냈는지를 채점합니다. 에이전트 전체를 재는 벤치입니다 — 루프, 도구, 회복, 모델이 함께 시험대에 오르고,
한 번의 실행은 그중 무엇이 결과를 만들었는지 말해주지 않습니다. 아래 표가 판정 옆에 토큰과 호출 수를
같이 싣는 이유가 그것입니다. 둘 다 통과했더라도 한쪽이 다섯 배 일했다면 같은 결과가 아닙니다.

## 무엇이 시험대에 오르나

- **루프**: magi의 도구, 가드, 카운슬, 회복. 고정된 바이너리에서.
- **백엔드**: `MAGI_BASE_URL`이 서빙하는 것.
- **데이터셋**: `terminal-bench/terminal-bench-2-1`, 89개 과제.

## 준비물

- **Docker**. 요청할 병렬도만큼의 여유와 함께 돌아가고 있어야 합니다. Terminal-Bench는 trial마다
  컨테이너를 띄우고 그것들이 VM이 가진 것을 나눠 씁니다. 무엇을 가졌는지는 `docker info`가 말해줍니다.
- **[Harbor](https://www.tbench.ai)**, Terminal-Bench 러너(`harbor run`).
- **컨테이너용 magi 바이너리** — `magi-arm64` 또는 `magi-amd64`를 한 디렉토리에. 어댑터가 컨테이너마다
  올려서 거기에 설치합니다. 안에서 빌드하는 것은 없습니다.
- **컨테이너가 닿을 수 있는 백엔드.** 호스트에서 돌든 다른 곳에서 돌든 상관없습니다. 컨테이너는
  HTTP만 말하므로 이미지에 무언가를 더 넣을 필요가 없습니다. `BASE_URL`에는 **컨테이너가 보는** 주소를
  주십시오(`localhost`가 아니라 `host.docker.internal`).

`host.docker.internal`은 **컨테이너 안에서** 풀리는 이름이지 호스트에서 풀리지 않고, 그것도 Docker
Desktop에서만입니다 — 리눅스의 맨 Docker에서는 컨테이너가 브리지 주소(`172.17.0.1`)나 직접 맞춰 둔
`--add-host`로 호스트에 닿습니다. 그러니 백엔드가 답하는지는 **호스트 쪽 주소**로 확인하십시오. 89개짜리
실행을 시작하기 전에, 끝난 뒤가 아니라:

```sh
curl -s http://localhost:11434/v1/models | head -c 200   # 같은 백엔드를 호스트 시점에서
```

## 돌리는 법

```sh
# 컨테이너가 실행할 magi 바이너리 (magi-arm64 / magi-amd64).
export BINARY=/tmp/magi-serve
export BASE_URL=http://host.docker.internal:11434/v1

# 89개 전부. 인자는 동시에 몇 개를 돌릴지입니다.
MODEL=qwen3-coder:30b bench/harbor/run.sh 1
```

실행이 끝나면 표를 찍습니다. 언제든 다시 물어볼 수 있습니다:

```sh
python3 bench/harbor/report.py --jobs-glob 'jobs/*tb21*'
python3 bench/harbor/report.py --jobs-glob 'jobs/*tb21*' --markdown   # 문서에 붙일 때
```

## 표 읽는 법

| 컬럼 | 무엇인가 |
|---|---|
| `min` | trial의 벽시계. 컨테이너 셋업과 검증 포함 |
| `turns` | magi가 답한 사용자 프롬프트 수 — 과제 하나는 보통 **하나**입니다 |
| `calls` | 백엔드가 실제로 서빙한 LLM 요청 수. 규모를 따라 늘어나는 건 이 숫자입니다 |
| `in` / `cached` / `out` | magi 자신의 토큰 집계. trial의 `turn.finished`에서 |
| `council` | 그 턴의 완료 선언에 대한 집계, `done 3-0`. `— none`은 완료 선언이 없었다는 뜻 |
| `usd` | 백엔드가 값을 매기는 경우 그 청구액. `~`는 여러 trial이 나눠 쓴 수치 |

`turns`와 `calls`는 자릿수가 다르고 둘 다 사실입니다 — 과제 하나, 프롬프트 하나, 그 안에서 모델 호출
마흔 번. 에이전트 타임아웃으로 잘린 trial은 `turn.finished`에 도달하지 못하므로 입력이 `0`이 아니라
`?`로 나옵니다. 0은 주장이니까요.

## 발등을 찍을 하나

**병렬은 점수를 움직일 수 있고, 아래쪽으로만 움직입니다.** trial들이 도커 VM의 CPU와 메모리를 나눠 씁니다.
7 CPU · 8GB VM에서 네 개를 돌리면 각자 1.75코어와 2GB를 받고, 컴파일하거나 학습시키는 과제는 에이전트와
아무 상관없는 이유로 타임아웃 납니다. 왜곡은 한 방향입니다 — 굶주림은 통과를 타임아웃으로 바꿀 수 있지
그 반대는 못 합니다. 올리기 전에 `docker info`를 확인하고, 점수는 같은 여유에서 돌린 점수하고만
비교하십시오.

## 격리, 그리고 치팅이 없었음을 확인한 방법

과제가 GitHub에 공개된 벤치마크는 두 가지로 틀릴 수 있습니다. trial이 앞선 trial에게서 배우거나,
에이전트가 답을 찾아보거나. 어느 쪽도 바란다고 막히지 않습니다.

**trial 사이에 넘어가는 것이 없습니다.** 과제마다 자기 이미지에서 컨테이너를 새로 띄웁니다. 호스트에서
안으로 들어가는 것은 정확히 하나 — magi 바이너리이고, 업로드 후 `install`합니다. 네트워크를 쓰지 않으므로
컨테이너 안에서 curl/wget을 망가뜨려도 설치를 실패시킬 수 없습니다. 마운트된 호스트 디렉토리는 없습니다.
`MAGI_DATA_DIR`는 컨테이너 안을 가리키고, 어댑터는 경험·메모리·스킬 디렉토리를 **하나도 설정하지
않습니다.** magi의 스토어는 그 컨테이너와 함께 살고 죽습니다. 어느 과제의 무엇도 다음 과제의 프롬프트에
들어가지 않습니다 — trial마다 세션을 새로 열고 히스토리를 처음부터 렌더합니다.

**컨테이너에는 네트워크가 있습니다.** 많은 과제가 그것을 필요로 합니다 — 패키지 설치, 소스 다운로드,
데이터셋 내려받기. 그러므로 정답 조회는 원리적으로 가능하고, 없다고 가정할 게 아니라 확인해야 합니다.
magi가 하는 모든 도구 호출은 웹 호출까지 이벤트 로그에 남으므로 검사는 기계적입니다:

```sh
set -- jobs/*tb21*/*__*/agent/magi-stdout.txt   # 감사할 실행으로 범위를 좁히십시오
grep -ohE '⚙ (websearch|webfetch)' "$@" | sort | uniq -c
grep -ohE '⚙ (websearch|webfetch) \{[^}]{0,200}' "$@"        # 하나하나 다 읽어보십시오
grep -ohE '⚙ bash \{"command": "[^"]{0,160}' "$@" | grep -iE 'curl|wget|git clone'
grep -oih 'terminal-bench' "$@" | wc -l
grep -ohE '⚙ (read|grep|bash) \{[^}]{0,200}' "$@" | grep -E 'test_outputs\.py|correct_output|_gtruth'
```

범위가 중요합니다. 범위를 안 좁힌 glob은 이 저장소가 수집한 모든 job을 훑고, 다른 실행의 호출을 세는
감사는 이 실행의 감사가 아닙니다.


**데이터셋은 이름이 둘이고, 하나만 아는 패턴은 눈이 먼 것입니다.** GitHub 조직명은 하이픈이 있는
`harbor-framework`이고, 허브와 태스크 레지스트리는 하이픈이 없는 `harborframework.com`입니다. 저장소
URL만 보고 쓴 감사 패턴은 그래서 `registry.harborframework.com/tasks/...`를 영영 못 봅니다 — 다른 이름으로
서비스되는 같은 과제 페이지입니다. 이 구멍은 2026-08-26에 눈으로 찾았습니다. 이미 한 번 격리된 과제의
재실행에서였고, 구멍을 메우자(`harbor-?framework`) 앞선 훑기가 통과시켰던 trial이 셋 더 나왔습니다.
데이터셋에 닿을 수 있는 이름은 전부 패턴에 넣으십시오. 기억나는 URL 한 가지 모양이 아니라, 실제로 내용을
내려준 호스트에서 이름을 뽑아야 합니다.

**정답을 찾아 나선 trial은 따로 다시 돌립니다.** 위 검사는 장식이 아닙니다 — trial은 데이터셋 자신의
과제 페이지를 GitHub에서 찾아낼 수 있고 실제로 찾아냅니다. 일단 찾아낸 뒤의 판정은 에이전트에 대한
증거가 아닙니다. 그래서 웹 호출이 데이터셋의 과제 페이지·정답 파일·채점 테스트에 닿은 trial은
격리합니다: 그 결과는 어떤 표에도 혼자 서지 않고, 그 과제는 89개가 모두 끝난 뒤 다시 큐에 들어가며,
**재실행 결과가 채택되는 값입니다.** 어느 과제였는지는 총계에 조용히 접어넣지 않고 결과와 함께 이름을
댑니다 — 표가 언급하지 않는 것을 독자가 감사할 수는 없기 때문입니다.

**웹만 보는 것은 검사의 절반입니다.** 컨테이너 안에 이미 있는 채점기는 `read` 한 번이면 닿고, 웹 호출을
아무리 읽어도 보이지 않습니다. 2026-08-26에 검사를 로컬 읽기까지 넓히자(`test_outputs.py`,
`correct_output`, `ground_truth`, `/tests/test.sh`) 123개 런 중 정확히 하나가 걸렸습니다.
`break-filter-js-from-html`이 `/app/test_outputs.py`를 읽고 실행했습니다. 이건 격리 대상이 아닙니다.
그 태스크의 Dockerfile에 `COPY tests/test_outputs.py /app`이 있고 지시문이 *"You can run
/app/test_outputs.py to verify"*로 끝납니다. 거기서 채점기는 답안지가 아니라 명세이고, 읽는 것이 시킨
대로 하는 것입니다. 89개 중 채점기를 내주는 태스크는 이것 하나뿐입니다.

이름 목록을 좁게 잡은 것이 검사를 읽을 만하게 만듭니다. 고치라고 준 저장소는 자기 테스트를 들고 오는데,
caffe의 `src/caffe/test/test_io.cpp`와 `fix-code-vulnerability`의 `/app/test/test_environ.py`는 둘 다
걸리지 않았습니다.

**채점 테스트는 정답 파일보다 나쁘므로 똑같이 격리합니다.** 2026-08-23에 `headless-terminal` trial 둘이
데이터셋 저장소에서 `tests/test_outputs.py`를 페치했습니다. 정답 파일은 답에 이르는 한 가지 길을
보여주지만, 채점 테스트는 판정이 계산되는 단언문 자체를 알려줍니다. 그게 답안지입니다. 둘 다 격리했습니다.

**재실행이 같은 곳에 닿으면 규칙은 순환합니다 — 그때는 결과를 그대로 쓰고 이름을 댑니다.**
`mteb-leaderboard`는 이 저장소가 지켜본 다섯 번의 trial에서 다섯 번 모두 자기 과제의 README를
페치했습니다(2026-08-23 두 번, 08-24, 08-26 두 번). 에이전트의 버릇이 아니라 과제의 성질입니다.
지시문이 *"2025년 8월 기준 Scandinavian MTEB 리더보드에서 가장 좋은 임베딩 모델"*을 묻기 때문에
웹을 봐야만 풀리고, 그 검색어를 넣으면 데이터셋의 과제 페이지가 상위에 뜹니다. 격리 → 재실행 →
다시 닿음이 무한히 반복되므로, 이 한 건은 **재실행 결과를 그대로 표에 싣고 여기에 사유를 적습니다.**
읽는 사람이 그 행을 다른 행과 같은 무게로 읽지 않도록 하는 것이 격리가 원래 하려던 일이고,
그것은 이름을 대는 것으로도 됩니다.

**격리는 그 trial의 앞선 것들까지 데려갑니다.** 격리된 trial 하나만 빼는 것으로는 모자랍니다. 그 과제의
바로 앞 trial이 자리를 대신 채우고, 규칙이 채택한 적 없는 숫자가 표에 남습니다. 2026-08-26에 격리를
배선하면서 이게 한 번에 드러났습니다 — trial 셋을 뺐는데 총계가 그대로 50개였고, 더 오래된 셋이 대신
올라섰기 때문입니다. 그래서 격리는 그 과제의 해당 trial과 그 이전 trial 전부에 걸리고, 재실행이 들어올
때까지 그 과제는 미측정입니다.

**아래 실행에 대한 감사.** 89 trial에서 에이전트는 웹 페치 41회, 웹 검색 18회를 했고 전부 읽었습니다.
그중 둘이 데이터셋 자신의 GitHub 페이지에 닿았습니다 — `mteb-leaderboard`가 그 과제의 README를,
`qemu-alpine-ssh`가 번역 오류를 진단하다 이슈 스레드를 받았습니다. 둘 다 정답 파일이 아니고 판정을
바꾸지도 않았습니다(앞은 통과, 뒤는 위에 적은 이유로 실패).

앞선 두 trial은 실제로 정답 파일에 닿았고, 그래서 격리 규칙이 가정이 아니라 실물입니다. `extract-elf`는
`solution/solve.sh`를, `regex-chess`는 데이터셋을 복제한 제3자 저장소의 정답 blob을 디코딩했습니다. 둘 다
재실행했습니다. `extract-elf`의 재실행은 그런 페치가 없었고 격리된 시도보다 적은 콜로 다시
통과했습니다(29회 대 20회). `regex-chess`는 그때는 도움 없이 통과했지만 그것을 되풀이하지는 못했습니다.
2026-08-26 연속된 두 trial이 각각 데이터셋의 `tasks/regex-chess/solution/solve.sh`를 받아왔고, 두 번째는
`webfetch` 두 번에 `curl` 한 번이었습니다. 둘 다 격리했고, 그 trial들이 돌려준 1.00 대신 **측정되지 않음**으로
셉니다 — 정답 파일을 읽고 낸 통과는 아무것도 재지 않습니다. 이것은 `mteb-leaderboard`와 정반대의 결말입니다.
그쪽은 닿은 페이지가 과제 자신의 README이고 지시문이 웹 없이는 답할 수 없는 것이라 사유를 밝힌 채 행이
남지만, 이쪽은 닿은 파일이 답 자체라 행이 아예 남지 않습니다.

**반대편도 감사하지 않으면 비교는 반쪽입니다.** 리더보드의 행은 그 trial들을 읽기 전에는 숫자일 뿐이고,
"답을 찾아온 것인가 풀어낸 것인가"라는 같은 질문이 비교 대상에도 걸립니다. Harbor Hub는 각 trial의 전체
트라젝토리를 로그인 없이 JSON으로 내줍니다 —
`/api/trials/{trialId}/trajectory?jobId={jobId}&trajectory_path=trials%2F{trialId}%2Ftrajectory.json`.
trial id는 행의 결과 페이지에서 나오고 그 페이지는 서버 렌더링이라, 전량을 `curl`로 받아 오프라인에서
훑을 수 있습니다. 2026-08-26에 `d7540f21` 행(claude-code 2.1.205 / claude-sonnet-5, 445 trial)에 그렇게 한
결과 트라젝토리 440개를 받았고 그중 439개가 비어 있지 않았으며, 같은 패턴 셋으로 데이터셋의 과제 페이지·정답
파일·채점 테스트에 닿은 trial은 **0개**였습니다.

**그 훑기가 같이 정리한 것은 둘 다 웹을 썼다는 사실입니다.** 비교 페이지의 이전 판은 Claude Code에게 웹
접근이 없다고 적었습니다. 근거는 표본으로 열어본 트라젝토리 몇 개에 Bash·Read·Edit만 있었다는 것이었습니다.
Bash면 충분합니다. 439개 중 54개 trial(22개 태스크)이 `curl`·`wget`으로 외부 호스트를 때렸고, 그중 30개
(13개 태스크)는 패키지·배포 미러가 아닌 곳에 닿았습니다. 태스크 넷에 걸친 trial 여섯 —
`build-pov-ray`·`crack-7z-hash`·`dna-assembly`·`regex-chess` — 은 검색엔진을 직접 쳤습니다. 검색 도구가
없을 뿐 검색은 했습니다. 이 숫자들은 총계가 아니라 하한입니다. `curl`과 `wget`에 맞춘 훑기는 나가는 다른
길을 못 봅니다 — `git clone`, 그리고 파이썬 자신의 `urllib.request`(한 trial이 이걸로
`api.github.com/search/repositories`를 쳤습니다). 트라젝토리에 나타나는 URL을 전부 세면 **439개 중 165개
trial, 89개 중 45개 태스크**로 올라갑니다. 절반 가까이가 밖으로 나갔습니다. 기억나는 동사로 grep하면
기억나는 동사만 재게 됩니다. URL로 grep해야 컨테이너를 실제로 떠난 것이 잡힙니다. 양쪽이 다 밖으로 나간 자리에서는 가는 곳까지 자주 같았습니다.
`dna-assembly`는 양쪽 다 `www.neb.com`, `build-pov-ray`는 양쪽 다 `povray.org`와 검색엔진,
`caffe-cifar-10`은 비교 대상 다섯 trial 전부가 `cs.toronto.edu`였습니다. `torch-pipeline-parallelism`은
비교 대상의 한 trial이 `api.github.com/repos/huggingface/picotron`과 그 README를 받았는데, 이 저장소가 같은
과제의 자기 trial을 격리한 바로 그 참조 구현입니다. 도구 차이를 우위라고 부르기 전에 반대편을 읽으십시오.

**데이터셋에 닿는 것만이 답에 닿는 길은 아닙니다.** 답이 널리 알려진 공개 프로젝트인 과제는 그 이름만으로
닿을 수 있고, 위의 감사 패턴 — 데이터셋·정답 파일·채점 테스트 — 은 그것을 못 봅니다. `regex-chess`가 그
사례의 전부입니다. 과제는 정규표현식만으로 만든 체스 수 생성기를 요구하고, 저자는 Nicholas Carlini이며,
`github.com/carlini/regex-chess`가 바로 그것을 구현한 저자의 공개 저장소입니다. 이 저장소의 2026-08-26
시행 셋은 각각 다른 경로로 거기 닿았습니다 — 데이터셋 자신의 `solution/solve.sh`가 두 번, 그다음에는
데이터셋을 복제한 제3자 미러와 저자의 저장소, 저자의 해설 글, 그리고 제3자가 모아둔 풀이 트라젝토리입니다.
비교 대상도 여기서 깨끗하지는 않습니다. 다섯 trial 중 하나가 `urllib`로 GitHub를 검색해 저장소를 찾아
클론했습니다. 다만 의미 있는 비대칭이 있습니다 — 나머지 넷은 아무 데도 안 가고도 통과했으므로, 이 과제는
보지 않고도 풀리는 과제입니다. 그런 과제의 판정기는 데이터셋과 나란히 원본 프로젝트의 이름을 대야 하고,
`scratchpad/rc_clean.py`가 그 일을 합니다.

**오염된 시행은 끝까지 갈 필요가 없습니다.** 판정은 그 시행의 웹 호출에서 나오고 그것은 첫 몇 분에
떨어집니다. 남은 30분을 기다려 봐야 얻는 것이 없습니다. 그래서 재실행 루프는 전사를 폴링하다가 답에 닿는
순간 그 시행을 죽이고 다음을 시작합니다. 30분짜리 시도가 20초가 되고, 비교 대상과 같은 k=5를 채우는 것이
감당 가능해집니다. 죽일 때는 PID로 죽이십시오. `pkill -f <패턴>`은 루프 자신의 명령줄에도 매치해서, 이
저장소에서 루프가 스스로를 죽인 적이 여러 번 있습니다.

이 검사는 반쪽이 아니라 전부이고, 그것도 잴 수 있습니다: 이 실행은 `spawn`·`meeting`·`hand_off`를
**0회** 호출했으므로 자기 세션 스토어를 따로 가진 위임 서브에이전트가 없고, 부모 전사가 에이전트가 한 일의
전부입니다.

**백엔드 자신의 설정은 모든 trial에 동일하게 적용됩니다.** 추론 에포트 같은 것은 엔드포인트의 성질이지
어느 한 과제의 성질이 아닙니다. 그러므로 그것은 **런의 성질**이고, 숫자에서 추론하게 두는 대신 아래
결과와 함께 명시합니다.

## 결과

**맞대결 리포트는 따로 한 페이지입니다:**
[magi vs Claude Code on Terminal-Bench 2.1](https://claude.ai/code/artifact/b8bbb95a-24f0-4ed5-a2c2-4d27b3981a0e). 과제별 표를 싣습니다 — magi의 판정과 콜·토큰 수를
비교 대상의 다섯 trial 및 비용과 나란히 놓은 것입니다. 여기에 magi가 실패한 과제와 magi만 푼 과제마다
쓴 카드, 각 trial을 왜 격리했는지 적은 목록, 그리고 양쪽의 웹 사용 감사가 붙습니다. job 디렉토리에서
`bench/harbor/compare/build_page.py`가 다시 만들어 내므로, 실행을 옮겨 적은 것이 아니라 실행을 비추는
화면입니다. 이 페이지는 자체 공유 메뉴에서 공유하기 전까지는 비공개입니다.


`claude-sonnet-5` 위의 magi, 추론 에포트 HIGH(magi 플래그가 아니라 백엔드 쪽 설정입니다),
반복 축약 끔(당시엔 env 스위치였고, 그 기전은 이후 제거됐습니다), 과제당 1회 시도,
동시 1~2 trial.

| | |
|---|---|
| 패스율 | **65 / 89 = 73.0%** |
| 통과하지 못한 24건 | 검증 실패 11, 에이전트 타임아웃 13 |
| 벽시계 | 21.2시간, 과제당 14.3분 |
| 모델 호출 | 총 3,081, 과제당 35 |
| 입력 토큰 | 85.5M, 그중 64.8M이 캐시 읽기 |
| 출력 토큰 | 3.3M |

다시 만드는 명령:

```sh
python3 bench/harbor/report.py --jobs-glob 'jobs/*tb21*' --markdown
```

**과제당 1회 시도**입니다. 공개 리더보드 항목들은 5회이므로, 그쪽이 말하는 ±1.6%보다 훨씬 넓은 오차를
안고 있습니다.

### 과제별

데이터셋이 이름 붙인 순서 그대로 전부. `in`은 캐시 읽기를 포함하므로 `in − cached`가 새로 쓴 양입니다. 에이전트 타임아웃으로
끊긴 trial은 `turn.finished`에 도달하지 못해 `0`이 아니라 `?`입니다(0은 주장이 되니까요).
`council`은 그 턴의 완료 선언에 대한 집계이고, `— none`은 거기까지 못 갔다는 뜻입니다. 이 표는
`report.py`에 `usd` 열이 생기기 전에 기록돼서 지금 돌린 리포트보다 열이 하나 적습니다. 나머지는
그대로입니다.

| task | | min | turns | calls | in | cached | out | council |
|---|---|---:|---:|---:|---:|---:|---:|---|
| `adaptive-rejection-sampler` | ✅ PASS | 15 | 3 | 38 | 1,440,668 | 1,105,646 | 68,128 | done 3-0 |
| `bn-fit-modify` | ✅ PASS | 7 | 2 | 35 | 897,651 | 688,096 | 19,943 | done 3-0 |
| `break-filter-js-from-html` | ✅ PASS | 5 | 2 | 20 | 311,187 | 225,487 | 15,868 | done 3-0 |
| `build-cython-ext` | ✅ PASS | 10 | 3 | 68 | 3,046,657 | 2,553,973 | 22,803 | done 3-0 |
| `build-pmars` | ✅ PASS | 12 | 2 | 87 | 4,147,391 | 3,319,594 | 39,728 | continue 1-2 |
| `build-pov-ray` | ✅ PASS | 7 | 2 | 47 | 1,522,215 | 1,220,568 | 10,805 | done 3-0 |
| `caffe-cifar-10` | ✅ PASS | 40 | 2 | 64 | 2,442,396 | 1,993,433 | 22,136 | done 3-0 |
| `cancel-async-tasks` | ✅ PASS | 4 | 3 | 25 | 428,024 | 316,250 | 15,145 | done 3-0 |
| `chess-best-move` | ⏱ TIME | 16 | 2 | 38 | ? | ? | 56,307 | — none |
| `circuit-fibsqrt` | ✅ PASS | 26 | 4 | 33 | 1,148,930 | 818,913 | 99,700 | done 3-0 |
| `cobol-modernization` | ✅ PASS | 13 | 5 | 55 | 1,891,579 | 1,505,162 | 58,807 | done 3-0 |
| `code-from-image` | ✅ PASS | 3 | 1 | 19 | 257,221 | 178,090 | 6,446 | done 3-0 |
| `compile-compcert` | ✅ PASS | 20 | 1 | 35 | 830,665 | 643,978 | 12,347 | done 3-0 |
| `configure-git-webserver` | ✅ PASS | 8 | 2 | 36 | 696,797 | 484,981 | 24,970 | done 3-0 |
| `constraints-scheduling` | ✅ PASS | 6 | 2 | 14 | 225,016 | 142,827 | 31,867 | done 3-0 |
| `count-dataset-tokens` | ✅ PASS | 3 | 2 | 16 | 241,814 | 145,960 | 4,266 | done 3-0 |
| `crack-7z-hash` | ✅ PASS | 15 | 1 | 32 | 989,766 | 805,681 | 6,431 | done 3-0 |
| `custom-memory-heap-crash` | ✅ PASS | 18 | 2 | 46 | 1,560,459 | 1,216,969 | 71,367 | done 3-0 |
| `db-wal-recovery` | ❌ FAIL | 16 | 4 | 43 | 923,238 | 553,646 | 64,458 | done 3-0 |
| `distribution-search` | ✅ PASS | 5 | 3 | 18 | 367,615 | 268,256 | 22,374 | done 3-0 |
| `dna-assembly` | ✅ PASS | 23 | 5 | 53 | 2,339,706 | 1,802,281 | 112,882 | done 3-0 |
| `dna-insert` | ❌ FAIL | 12 | 1 | 32 | 902,799 | 714,020 | 48,978 | done 3-0 |
| `extract-elf` | ✅ PASS | 6 | 2 | 20 | 367,858 | 259,271 | 26,768 | done 3-0 |
| `extract-moves-from-video` | ⏱ TIME | 32 | 1 | 30 | ? | ? | 10,010 | — none |
| `feal-differential-cryptanalysis` | ✅ PASS | 10 | 3 | 18 | 468,530 | 330,455 | 45,073 | done 3-0 |
| `feal-linear-cryptanalysis` | ✅ PASS | 20 | 2 | 30 | 1,101,435 | 870,283 | 94,922 | done 3-0 |
| `filter-js-from-html` | ❌ FAIL | 23 | 6 | 40 | 1,170,484 | 812,129 | 65,480 | done 3-0 |
| `financial-document-processor` | ✅ PASS | 5 | 1 | 16 | 268,092 | 138,996 | 16,624 | done 3-0 |
| `fix-code-vulnerability` | ✅ PASS | 3 | 1 | 31 | 669,854 | 524,008 | 6,386 | done 3-0 |
| `fix-git` | ✅ PASS | 3 | 2 | 15 | 239,694 | 140,023 | 10,869 | done 3-0 |
| `fix-ocaml-gc` | ✅ PASS | 45 | 1 | 59 | 1,874,773 | 1,475,071 | 16,687 | done 3-0 |
| `gcode-to-text` | ⏱ TIME | 15 | 3 | 52 | ? | ? | 58,369 | — none |
| `git-leak-recovery` | ✅ PASS | 2 | 1 | 13 | 141,033 | 87,760 | 3,462 | done 3-0 |
| `git-multibranch` | ✅ PASS | 5 | 1 | 36 | 683,876 | 510,832 | 10,951 | done 3-0 |
| `gpt2-codegolf` | ⏱ TIME | 16 | 3 | 27 | ? | ? | 71,833 | — none |
| `headless-terminal` | ✅ PASS | 5 | 4 | 22 | 363,307 | 261,228 | 18,591 | done 3-0 |
| `hf-model-inference` | ✅ PASS | 4 | 3 | 13 | 443,407 | 312,411 | 8,531 | done 3-0 |
| `install-windows-3.11` | ❌ FAIL | 57 | 5 | 116 | 7,187,422 | 5,747,391 | 131,504 | done 2-1 |
| `kv-store-grpc` | ✅ PASS | 3 | 1 | 21 | 304,773 | 224,158 | 5,651 | done 3-0 |
| `large-scale-text-editing` | ✅ PASS | 6 | 2 | 17 | 284,771 | 189,384 | 18,637 | done 3-0 |
| `largest-eigenval` | ✅ PASS | 5 | 4 | 23 | 380,108 | 285,554 | 13,042 | done 3-0 |
| `llm-inference-batching-scheduler` | ✅ PASS | 14 | 5 | 44 | 1,849,604 | 1,427,495 | 68,297 | done 3-0 |
| `log-summary-date-ranges` | ✅ PASS | 2 | 3 | 18 | 277,563 | 188,576 | 6,698 | done 3-0 |
| `mailman` | ✅ PASS | 20 | 4 | 77 | 3,972,780 | 3,163,747 | 65,203 | done 3-0 |
| `make-doom-for-mips` | ⏱ TIME | 16 | 2 | 46 | ? | ? | 66,226 | — none |
| `make-mips-interpreter` | ❌ FAIL | 30 | 5 | 53 | 3,626,465 | 2,777,225 | 147,035 | done 3-0 |
| `mcmc-sampling-stan` | ✅ PASS | 18 | 4 | 73 | 5,624,196 | 4,489,664 | 22,807 | done 3-0 |
| `merge-diff-arc-agi-task` | ✅ PASS | 5 | 2 | 32 | 698,206 | 553,270 | 13,707 | done 3-0 |
| `model-extraction-relu-logits` | ✅ PASS | 5 | 1 | 15 | 291,140 | 181,564 | 22,567 | done 3-0 |
| `modernize-scientific-stack` | ✅ PASS | 3 | 2 | 20 | 291,293 | 210,861 | 9,013 | done 3-0 |
| `mteb-leaderboard` | ✅ PASS | 21 | 5 | 66 | 3,208,550 | 2,655,831 | 20,609 | done 3-0 |
| `mteb-retrieve` | ✅ PASS | 7 | 5 | 26 | 423,831 | 296,069 | 8,599 | done 3-0 |
| `multi-source-data-merger` | ✅ PASS | 4 | 2 | 23 | 306,685 | 112,815 | 12,158 | done 3-0 |
| `nginx-request-logging` | ✅ PASS | 5 | 1 | 23 | 391,873 | 261,208 | 15,843 | done 2-1 |
| `openssl-selfsigned-cert` | ❌ FAIL | 3 | 3 | 24 | 388,691 | 267,546 | 6,743 | done 3-0 |
| `overfull-hbox` | ✅ PASS | 5 | 1 | 20 | 354,784 | 242,842 | 14,927 | done 3-0 |
| `password-recovery` | ✅ PASS | 6 | 1 | 29 | 577,451 | 436,767 | 24,587 | done 3-0 |
| `path-tracing` | ⏱ TIME | 30 | 6 | 62 | ? | ? | 135,337 | — none |
| `path-tracing-reverse` | ⏱ TIME | 31 | 8 | 43 | ? | ? | 139,512 | — none |
| `polyglot-c-py` | ✅ PASS | 5 | 1 | 16 | 260,952 | 166,832 | 12,925 | done 3-0 |
| `polyglot-rust-c` | ✅ PASS | 7 | 2 | 15 | 372,933 | 277,544 | 28,579 | done 3-0 |
| `portfolio-optimization` | ✅ PASS | 5 | 2 | 23 | 393,289 | 261,708 | 11,381 | done 3-0 |
| `protein-assembly` | ❌ FAIL | 28 | 4 | 64 | 6,304,097 | 3,864,733 | 81,182 | done 3-0 |
| `prove-plus-comm` | ✅ PASS | 2 | 3 | 13 | 153,488 | 105,200 | 4,994 | done 3-0 |
| `pypi-server` | ✅ PASS | 4 | 2 | 24 | 364,535 | 255,976 | 4,973 | done 3-0 |
| `pytorch-model-cli` | ✅ PASS | 6 | 2 | 34 | 610,251 | 463,591 | 11,308 | done 3-0 |
| `pytorch-model-recovery` | ✅ PASS | 5 | 2 | 17 | 276,851 | 177,817 | 10,624 | done 3-0 |
| `qemu-alpine-ssh` | ❌ FAIL | 11 | 4 | 70 | 2,083,920 | 1,719,255 | 25,630 | done 3-0 |
| `qemu-startup` | ✅ PASS | 9 | 5 | 44 | 1,113,744 | 888,035 | 26,269 | done 3-0 |
| `query-optimize` | ✅ PASS | 19 | 2 | 16 | 234,979 | 157,869 | 13,641 | done 3-0 |
| `raman-fitting` | ❌ FAIL | 9 | 6 | 38 | 832,768 | 596,260 | 35,366 | done 3-0 |
| `regex-chess` | ✅ PASS | 40 | 6 | 48 | 2,086,417 | 1,685,916 | 130,722 | done 3-0 |
| `regex-log` | ✅ PASS | 7 | 1 | 12 | 148,460 | 76,937 | 15,855 | done 3-0 |
| `reshard-c4-data` | ✅ PASS | 23 | 4 | 40 | 1,044,314 | 822,756 | 80,736 | done 3-0 |
| `rstan-to-pystan` | ⏱ TIME | 31 | 7 | 66 | ? | ? | 39,027 | continue 1-2 |
| `sam-cell-seg` | ✅ PASS | 13 | 5 | 35 | 1,052,136 | 784,669 | 39,538 | done 3-0 |
| `sanitize-git-repo` | ⏱ TIME | 16 | 3 | 54 | ? | ? | 20,489 | continue 1-2 |
| `schemelike-metacircular-eval` | ⏱ TIME | 41 | 1 | 15 | ? | ? | 45,534 | — none |
| `sparql-university` | ✅ PASS | 6 | 2 | 18 | 338,046 | 229,593 | 19,903 | done 3-0 |
| `sqlite-db-truncate` | ✅ PASS | 3 | 1 | 13 | 172,093 | 108,351 | 9,134 | done 3-0 |
| `sqlite-with-gcov` | ✅ PASS | 5 | 1 | 30 | 592,914 | 452,392 | 7,803 | done 3-0 |
| `torch-pipeline-parallelism` | ❌ FAIL | 12 | 2 | 24 | 504,037 | 341,632 | 29,230 | done 3-0 |
| `torch-tensor-parallelism` | ✅ PASS | 11 | 1 | 17 | 329,882 | 214,404 | 24,074 | done 3-0 |
| `train-fasttext` | ⏱ TIME | 70 | 4 | 50 | ? | ? | 22,049 | — none |
| `tune-mjcf` | ✅ PASS | 10 | 2 | 27 | 572,953 | 439,049 | 17,864 | done 3-0 |
| `video-processing` | ❌ FAIL | 8 | 4 | 21 | 510,815 | 406,257 | 29,850 | — none |
| `vulnerable-secret` | ✅ PASS | 13 | 1 | 20 | 289,763 | 193,780 | 6,782 | done 3-0 |
| `winning-avg-corewars` | ⏱ TIME | 61 | 3 | 68 | ? | ? | 259,604 | continue 0-3 |
| `write-compressor` | ⏱ TIME | 16 | 1 | 2 | ? | ? | 5,344 | — none |

검증 실패 11건 중 **일곱이 `done 3-0`** 입니다 — 카운슬이 만장일치로 끝났다고 한 작업을 검증이
물렸습니다. 반대로 카운슬이 막아서 시간이 끝난 trial은 둘인데(`continue 1-2`, `continue 0-3`), 그 둘은
검증도 카운슬 편이었습니다. 이 백엔드에서 카운슬은 **통과시키는 쪽으로 기울어** 있고, 그게 이번 실행이
드러낸 가장 큰 단일 결함입니다 — 토큰에 관한 어떤 것보다 큽니다. 요구사항 훑기와 닫는 호출
(ARCHITECTURE §5)은 바로 이 실측을 겨냥해 붙였고, 이 실행 이후의 것입니다.

### 어떤 기계에서 돌았고, 어떤 과제가 손해를 봤나

| | |
|---|---|
| 호스트 | Mac mini(Mac16,11), Apple M4 Pro, 14코어(성능 10 + 효율 4), 64GB, macOS 26.5.2, arm64 |
| 도커 VM | **7 CPU, 7.7GiB** — trial들이 실제로 나눠 쓰는 것은 위의 64GB가 아니라 이쪽입니다 |
| 과제 이미지 | amd64를 arm64에서 **번역 실행** |

중요한 숫자는 VM 할당입니다. 동시 2 trial이면 각자 약 3.5코어·3.8GiB를 받고, 컴파일하거나 학습시키는
과제는 14코어 기계가 아니라 그 안에서 일합니다. 병렬도를 더 올리려면 먼저 VM 몫을 키워야 합니다.

결과에 실제로 드러난 결과 넷:

- **`qemu-alpine-ssh`는 이 아키텍처가 만든 핸디캡을 안고 시작합니다.** 과제 자신의 일이 시작되기도 전에
  컨테이너 로그에 `rosetta error: Unimplemented syscall number 282`가 찍힙니다 — x86 바이너리가 애플
  번역 계층의 구멍에 부딪힌 것입니다. 네이티브 amd64 호스트에는 없는 구멍이므로, 이 과제 900초 예산의
  일부가 과제가 아니라 호스트를 무는 데 쓰입니다. 이 절의 이전 판은 이 과제가 여기서 통과할 수 없다고
  적었습니다. 2026-08-26의 두 trial이 그 단정이 틀렸음을 보였습니다 — 핸디캡은 벽이 아니라 비용입니다.
- **구멍을 지나는 길을 두 trial이 각각 다르게 찾았습니다.** `qemu-startup`은 오류 문자열을 그대로 검색해
  `docker/for-mac#7475`에 닿은 뒤 amd64 컨테이너 안에 **arm64 네이티브 QEMU**를 깔았습니다
  (`dpkg --add-architecture arm64` 다음 `apt-get install qemu-system-x86:arm64`). 번역 계층을 아예
  거치지 않는 길이고, 그 trial은 통과했습니다. `qemu-alpine-ssh`는 같은 멀티아치 길이 gstreamer 의존성
  충돌로 막히자 **웹을 한 번도 쓰지 않고** 한 층 아래로 내려갔습니다. `objdump -T`로 `qemu_signalfd`를
  찾아내고, 282(`signalfd`)와 289(`signalfd4`)에만 `ENOSYS`를 돌려주는 32줄짜리 `LD_PRELOAD`
  인터포저를 짜서 QEMU가 자기 파이프 기반 폴백으로 내려가게 만들었습니다. SeaBIOS가 떴고 Alpine 3.19가
  VM 시간 3분 2초 만에 `localhost login:`에 닿았지만, 그 직후 15분 예산이 끝나 sshd는 손도 대지
  못했습니다. 그 trial을 이긴 것은 아키텍처가 아니라 예산입니다.
- **`install-windows-3.11`은 KVM 없이 QEMU를 돌렸습니다**(`/dev/kvm` 부재). 게스트가 순수 소프트웨어
  에뮬레이션으로 57분을 쓰고 졌습니다. 리눅스 호스트라도 `--device /dev/kvm`을 넘겨주지 않으면 같으므로
  순수한 macOS 페널티는 아니지만, 페널티인 것은 맞습니다.
- **계산이 무거운 타임아웃들이 번역 오버헤드를 안고 있었습니다** — `path-tracing`,
  `path-tracing-reverse`, `make-doom-for-mips`, `gpt2-codegolf`. 각 타임아웃에서 번역 몫과 에이전트 몫이
  얼마인지는 여기서 분리하지 않았고, 짐작해서 적지 않겠습니다.

이 기계의 점수는 같은 여유를 준 기계의 점수와만 비교됩니다.

비교를 위해: 같은 루프를 로컬 `qwen3.8:27b-mlx`로 14개 부분집합에 돌렸을 때는 시도한 모든 추론 에포트에서
9/14였습니다.
