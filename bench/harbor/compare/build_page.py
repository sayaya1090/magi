#!/usr/bin/env python3
"""Rebuild the magi-vs-Claude-Code artifact from whatever has been measured so far.

Reads the job directories directly rather than a tally kept by hand, so a run that lands while
the page is being written is in the page. Two conventions matter and both are the leaderboard's:

  * a trial killed by quota is not a result -- it is dropped, and the task falls back to its
    newest run that was not, or goes unmeasured;
  * a trial killed by the wall clock is billed at zero, because the leaderboard leaves the cost
    column blank on its own timeout rows and the two columns have to mean the same thing.
"""
import json, os, re, glob, html, subprocess, sys

# 데이터(리더보드 tsv, 격리 대장, 템플릿)는 이 스크립트 옆에 있고, 스캔 대상인 job 디렉토리는
# 저장소 뿌리에 있다. 둘을 따로 잡아야 어디서 실행하든 같은 페이지가 나온다.
HERE = os.path.dirname(os.path.abspath(__file__))
ARCDIR = os.path.join(HERE, "..", "runs", "tb21-2026-08")
REPO = os.path.dirname(os.path.dirname(os.path.dirname(HERE)))
OUT = sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, "bench.html")
CONTAMINATED = {"NonZeroAgentExitCodeError", "NetworkConnectionError", "CancelledError"}

# What the failure actually was, where a run has been read line by line. Anything not named here
# is described from its own record instead of guessed at.
NOTES = {
 "path-tracing-reverse": ("30분 내내 분석만 하고 <b>mystery.c를 한 번도 쓰지 않음</b>. "
   "다 알아낸 뒤 쓰려다 벽시계에 잘려 빈손."),
 "gcode-to-text": ("G-code의 M486 오브젝트 <b>이름표를 내용으로 오독</b>. "
   "스스로 의심해 두 번 검색하고도 채택. 카운슬 3-0 done."),
 "path-tracing": "정상 종료하고도 0점. 시계가 아니라 <b>틀린 산출물로 done을 선언</b>했다.",
 "regex-chess": ("<b>정답 파일 원문</b> — <code>carlini/regex-chess</code>의 "
   "<code>chess_engine.py</code>. 리더보드 규칙에 따라 이 시행은 <b>0으로 재산정</b>했고, "
   "경위는 실패 카드에 적었다"),
 "qemu-alpine-ssh": ("<b>15분·1 CPU·4GB·KVM 없음</b>. 컨테이너는 amd64인데 호스트가 "
   "aarch64라 <code>qemu-system-x86_64</code>가 뜨자마자 "
   "<code>rosetta error: Unimplemented syscall number 282</code>로 코어덤프했다 — 문서가 "
   "이 기계에서 통과 불가라고 적어둔 그 벽이다. 이번 시행은 <b>웹을 한 번도 쓰지 않고</b> 그 벽을 넘었다. "
   "먼저 <code>dpkg --add-architecture arm64</code>로 arm64 네이티브 QEMU를 깔려 했으나 "
   "gstreamer 의존성이 풀리지 않아 <b>그 길은 실패했다</b>. 그러자 방향을 바꿔 gcc·binutils를 깔고 "
   "<code>objdump -T</code>로 <code>qemu_signalfd</code> 심볼을 찾아 0x8bc6e0을 역어셈블한 뒤, "
   "<code>syscall()</code>을 가로채 282(signalfd)와 289(signalfd4)에만 <code>ENOSYS</code>를 "
   "돌려주는 <b>32줄짜리 LD_PRELOAD 인터포저를 직접 짰다</b> — QEMU가 자기 파이프 기반 폴백으로 "
   "내려가게 만드는 수다. 첫 컴파일은 <code>NULL undeclared</code>로 깨졌고 "
   "<code>&lt;stddef.h&gt;</code>를 넣어 고쳤다. 그러자 SeaBIOS가 떴다.<br><br>"
   "남은 시간은 시리얼 콘솔에 갔다. 커널 로그가 안 보여 ISO를 마운트하려다 권한이 없어 막히자 "
   "<code>genisoimage</code>를 깔고 <code>isoinfo</code>로 "
   "<code>/boot/syslinux/syslinux.cfg</code>를 꺼내 <code>console=ttyS0</code>가 없다는 걸 확인했다. "
   "부트 프롬프트에 직접 타이핑해 봤지만 <code>TIMEOUT</code>이 짧아 자동 부팅에 밀렸고, "
   "결국 ISO에서 vmlinuz·initramfs를 뽑아 <code>-kernel</code>/<code>-initrd</code>로 "
   "isolinux를 건너뛰었다. Alpine 3.19가 <code>localhost login:</code>을 내놓기까지 "
   "VM 시간만 <b>3분 2초</b>. <code>root</code>를 보낸 직후 15분이 끝났다. "
   "sshd는 손도 못 댔고, 카운슬은 한 번도 열리지 않았다 — 스스로 done을 선언한 게 아니라 "
   "하네스가 끊었다. <b>막힌 것은 방법이 아니라 예산이다.</b>"),
 "feal-linear-cryptanalysis": "단일 <code>wait_for</code>로 30분을 대기. 산출물 없음.",
 "db-wal-recovery": ("<b>7콜 · 3분 17초 · $0.29</b>로 통과했다. WAL 프레임을 직접 읽어 "
   "복구 경로를 찾았고 헤매지 않았다. Claude Code는 다섯 번 다 실패했다($0.53/회)."),
 "build-pov-ray": ("<b>격리가 점수를 바꾼 자리다.</b> 1차(08-25)는 데이터셋의 "
   "<code>solution/solve.sh</code>를 읽고 통과했고, 그래서 격리됐다. "
   "재실행은 데이터셋에 한 번도 닿지 않았고 — 검색으로 미러를 찾아 POV-Ray 2.2를 직접 빌드해 "
   "<code>/usr/local/bin/povray</code>를 세우고, <code>illum1.pov</code>를 렌더해 "
   "30,018바이트 TGA가 정확히 일치하는 것까지 확인했다. 세 검사 중 둘을 통과했다: "
   "<b>렌더링 일치와 버전 2.2 확인</b>. 걸린 것은 "
   "<code>test_povray_built_from_correct_source</code> 하나로, 소스 트리에 "
   "<code>file_id.diz</code>가 없다 — 정답이 지목한 <b>그 배포 아카이브</b>에만 들어 있는 파일이다. "
   "같은 버전을 다른 패키징으로 받은 것이다. Claude Code도 0/5."),

 "sanitize-git-repo": ("여섯 검사 중 <b>시크릿 제거와 치환은 둘 다 통과</b>했다. 걸린 것은 "
   "<code>test_no_other_files_changed</code>로, 채점기가 원래 커밋을 찾다 "
   "<code>SHA d6987af0… missing</code>으로 죽었다 — <code>git-filter-repo</code>로 히스토리를 "
   "통째로 재작성해 모든 해시가 바뀐 탓이다. "
   "그리고 <b>magi 자신의 되돌릴 수 없는-명령 게이트가 두 번 막았다</b>: "
   "<code>git push --force</code>(옳은 거부)와, 에이전트가 안전을 위해 만든 "
   "<code>/app/dclm_backup_…</code>의 삭제(오발 — 그 백업에 지우라던 시크릿이 그대로 남았다). "
   "워크스페이스가 git 루트인 <code>/app/dclm</code>로 좁아져 형제 경로가 '밖'이 된 것이다. "
   "카운슬은 네 번 반려하고 다섯 번째에 done을 냈고, 그 왕복이 15분 예산을 넘겼다."),
 "openssl-selfsigned-cert": ("여섯 검사 중 <b>다섯을 통과</b>했다 — 디렉토리·키(600)·인증서·PEM·"
   "verification.txt까지 전부 맞았다. 걸린 것은 하나, "
   "<code>check_cert.py</code>를 <code>python3</code>으로 시험했는데 채점기는 "
   "<code>python</code>으로 부른다는 점이다. 그 둘이 다른 인터프리터라 "
   "<code>ModuleNotFoundError: No module named 'cryptography'</code>로 죽었다. "
   "지시문은 어느 쪽으로 부를지 말하지 않으니 확인할 근거는 없었지만, 양쪽으로 돌려보는 것은 "
   "할 수 있는 일이었다. <code>sam-cell-seg</code>와 같은 계열 — <b>채점기가 어떻게 부를지에 대한 "
   "가정</b>이 틀렸다. 카운슬 3-0 done."),
 "make-doom-for-mips": ("MIPS 크로스 툴체인 없이 doom을 freestanding으로 빌드해 ELF를 만드는 과제. "
   "예산 15분에 <code>write</code>를 25번 했는데, 그 대부분이 <b>없는 libc 헤더를 손으로 채우는 일</b>이었다 — "
   "마지막 행동이 <code>/app/build/inc/limits.h</code>에 <code>CHAR_BIT</code>부터 적어 넣고 "
   "다시 빌드를 거는 것이다. 헤더 하나를 채우면 다음 컴파일 오류가 나오는 구조라 15분에 닿을 수 있는 "
   "끝이 아니다. <b>CC도 0/5</b>이고 다섯 시행 모두 16분대에 잘렸다 — 이 런(16분 34초)과 같은 벽이다."),
 "install-windows-3.11": ("지시문이 <i>\"known to be compatible with QEMU 5.2.0\"</i>이라 못 박아, "
   "컨테이너의 버전이 다르자 <b>QEMU를 소스에서 빌드</b>하기로 했다. 마지막 두 콜이 그 진행이다: "
   "8분에 905/2289 오브젝트, <i>\"steady but slow, continuing to wait\"</i>. "
   "예산은 어느 에이전트에게도 주어지지 않으니(리더보드의 Claude Code도 프롬프트에 시간이 없다) "
   "탓할 것은 남은 시간을 몰랐다는 점이 아니라 <b>기다리는 방식</b>이다: "
   "<code>bash_output</code>을 두 번 써놓고도 <code>sleep 180</code> 반복으로 돌아갔다. "
   "<code>wait_for</code>로 조건을 걸었다면 빌드가 끝나는 순간 돌려받았을 시간을 통으로 잤다. "
   "완료를 선언하지 않아 UNVERIFIED로 착지했다."),
 "extract-moves-from-video": ("zork 플레이 영상에서 입력한 명령을 받아 적는 과제. 예산은 30분, "
   "컨테이너는 1코어다. ffmpeg이 없어 설치부터 했고, <code>fps=2</code>로 뽑아 380프레임이 나왔다. "
   "명령은 화면에 몇 초씩 머무니 <code>fps=1</code> 이하로 충분했을 자리다. "
   "tesseract를 <code>-P 4</code>로 돌렸지만 1코어에서는 병렬도가 늘지 않는다 — "
   "148프레임을 마친 시점에 30초에 8개씩이었고, 남은 232장에 15분이 더 필요했다. "
   "환경 탓은 아니다: 1코어·2GB는 태스크가 선언한 값이고 host load는 2.12였다. "
   "CC도 1/5이며 그 실패 중 하나는 31분 13초로 이 런(31분 43초)과 같은 벽이다."),
 "sam-cell-seg": ("MobileSAM 변환기는 제대로 만들었다 — 임베딩 1회 재사용, box+중심점 프롬프트, "
   "겹침 제거와 최대 연결 성분 유지, 단순화 후 재래스터화까지. 데모 48개(19 rect + 29 polyline)를 "
   "전부 polyline으로 바꾸고 겹침 0 · 다중 연결 성분 0을 확인했다. 그러고는 <b>정리하면서 "
   "<code>/app/mobile_sam.pt</code>를 지웠다</b> — 채점기가 <code>--weights_path "
   "/app/mobile_sam.pt</code>로 넘기는 바로 그 파일이다. "
   "\"숨겨진 테스트가 자체 경로를 넘길 것\"이라고 적어두었는데, 그건 추측이었다. 카운슬 3-0 done."),
 "train-fasttext": ("62콜 · 1시간을 꽉 채우고 잘렸다. 학습 자체는 세 번 방향을 바꿔가며 됐다 — "
   "autotune(5 trial, 28MB, 정확도 0.609)이 크기 제약을 <b>작게 만들라는 뜻으로 읽어</b> 용량을 "
   "버린 것을 알아채고, 손튜닝을 거쳐 fastText 공식 저장소의 yelp 하이퍼파라미터로 재현했다. "
   "막힌 곳은 학습이 아니라 <b>검증</b>이었다: numpy 2.x에서 fastText 바인딩의 "
   "<code>predict()</code>가 <code>np.array(probs, copy=False)</code>로 터진다. "
   "그 원인을 <code>remember</code>에 적은 것이 마지막 행동이다."),
 "gpt2-codegolf": ("15분 예산 안에 <b>동작하는 GPT-2 추론기를 처음부터 썼다</b> — BPE 토크나이저를 "
   "공개된 토큰 ID로 검증하고(<code>\"Hello, my name is\"</code> → 15496 11 616 1438 318), "
   "체크포인트의 텐서 순서 가설을 세워 반증하고(자연스러운 순서 → 출력 붕괴 → TF의 알파벳순이 정답), "
   "<code>\"Hello, my name is John. I'm a writer, and I'm\"</code>까지 뽑았다. "
   "걸린 것은 크기 제약뿐이다: 7,648바이트를 5,835로 줄이던 중 벽시계가 끊었고 목표는 5,000이었다."),
 "filter-js-from-html": ("정규식 새니타이저를 쓰고 <b>자기가 만든 공격 벡터로만</b> 검증했다. "
   "자기 테스트에서 괄호 균형과 <code>srcdoc</code> 처리 순서 버그를 잡아 고칠 만큼 성실했지만, "
   "막은 것은 스스로 생각해낸 공격뿐이었다. 채점기는 selenium으로 실제 브라우저에서 벡터를 실행한다. "
   "카운슬 3-0 done — 증거가 구체적이되 자기가 고른 입력이라 반대할 근거가 없었다."),
 "video-processing": ("허들 점프 감지 휴리스틱이 끝내 수렴하지 않았다. 후보 프레임을 "
   "valley-prominence 방식으로 다시 설계하던 중 턴이 끝났고, <b>완료를 선언하지 않았다</b> — "
   "틀린 걸 알고 있었으므로 UNVERIFIED로 착지했고 카운슬도 열리지 않았다. "
   "<code>jump_analyzer.py</code>는 세워져 있다."),
 "raman-fitting": ("카운슬이 정합성 함정(두 피크가 같은 2.37× 어긋남)을 <b>정확히 짚었으나</b>, "
   "요구한 보정 확인이 이 샌드박스에선 충족 불가."),
}

# What each web call actually went after, read from the run rather than inferred from the outcome.
WEBWHAT = {
 "caffe-cifar-10": "<code>cifar-10-binary.tar.gz</code>의 다른 미러 — 내려받을 곳",
 "compile-compcert": "<code>compcert.org</code>의 download/release 페이지 — 소스 tarball 위치",
 "build-pov-ray": ("POV-Ray 2.2 소스 아카이브를 어디서 받나 — 검색 둘, 페치 둘(ibiblio 색인과 "
   "제3자가 이 과제를 두고 쓴 메모). 내려받을 곳을 찾은 것이고, 데이터셋에는 닿지 않았다. "
   "<b>그러고도 정답이 지목한 아카이브가 아니어서 실패했다</b>"),
 "count-dataset-tokens": "세라고 한 그 HuggingFace 데이터셋의 카드와 README",
 "dna-assembly": "NEB의 BsaI-HF v2 가이드 — 절단 부위가 조각 끝에 가까울 때 필요한 여분 염기",
 "break-filter-js-from-html": "BeautifulSoup의 <code>html.parser</code>가 놓치지만 브라우저는 실행하는 태그",
 "gcode-to-text": "PrusaSlicer의 M486 오브젝트 기본 이름 — <b>찾아보고도 틀렸다</b>",
 "train-fasttext": "fastText 공식 저장소의 <code>classification-results.sh</code> — yelp 하이퍼파라미터",
 "protein-assembly": ("<code>antibody.fasta</code>에서 읽은 경쇄 서열 38자를 그대로 검색해 "
   "그 항체가 무엇인지 식별 — 주어진 데이터를 알아보는 일이지 답을 찾는 일이 아니다"),
 "mteb-leaderboard": ("<b>데이터셋 자신의 과제 README</b> — 그리고 이 저장소가 지켜본 다섯 번의 "
   "trial에서 다섯 번 다 그랬다. 지시문이 <i>2025년 8월 기준 Scandinavian MTEB 리더보드</i>를 물어 "
   "웹을 봐야만 풀리는데, 그 검색어를 넣으면 과제 페이지가 상위에 뜬다. "
   "격리→재실행→다시 닿음이 순환하므로 이 행은 재실행 결과를 그대로 싣고 사유를 여기 적는다 "
   "(<code>docs/BENCHMARK.md</code> 같은 절). <b>다른 행과 같은 무게로 읽지 말 것</b>"),
 "mailman": ("Mailman3 공식 문서의 MTA 연동 페이지(<code>docs.mailman3.org .../mta.html</code>) — "
   "postfix의 <code>local_recipient_maps</code>·<code>transport_maps</code>를 같은 도메인에서 "
   "어떻게 맞추는지. 도구가 자기 설정에 대해 써둔 문서다"),
 "torch-pipeline-parallelism": ("과제가 <b>이름까지 지정한 함수</b> "
   "<code>train_step_pipeline_afab</code>을 그대로 검색해 huggingface/picotron_tutorial의 "
   "참조 구현을 읽었다. 데이터셋 저장소도 정답 파일도 아닌 공개 튜토리얼이지만 <b>이 과제의 원본</b>이다. "
   "레지스트리의 과제 페이지도 한 번 받았는데 SPA라 껍데기만 돌아왔다. 1차 시행이 같은 이유로 격리됐고 "
   "재실행이 같은 길을 다시 걸었으므로, <code>mteb-leaderboard</code>와 같이 재실행 결과를 싣고 "
   "사유를 여기 적는다. 쓴 코드는 베낀 것이 아니라 <code>_partition_layers</code>·"
   "<code>batch_isend_irecv</code>로 직접 짠 것이다. <b>다른 행과 같은 무게로 읽지 말 것</b>"),
 "regex-chess": ("<b>이 행은 다른 행과 같은 무게로 읽으면 안 된다.</b> 2026-08-26에 여덟 번 돌렸고 "
   "<b>여덟 번 다 정답 파일을 받아왔다</b> — 데이터셋의 <code>solution/solve.sh</code> 원문, "
   "또는 저자 원본 <code>carlini/regex-chess</code>의 <code>chess_engine.py</code>. "
   "경로는 매번 같았다: <code>check.py</code>를 읽고 → 검색하고 → 미러나 공식 저장소를 찾고 → "
   "<code>solution/</code>을 열고 → raw 원문을 받는다. 미러만 넷을 거쳤고(<code>moogician</code>, "
   "<code>marginlab</code>, <code>dafinadjombalic-cmd</code>, 공식) 한 번은 11분을 생각한 뒤 갔다. "
   "깨끗한 시행이 없으므로 재실행 규칙이 채택할 값을 못 만든다. 그래서 <b>마지막으로 완주한 시행을 "
   "그대로 싣고 한계를 여기 적는다</b>. 비교 대상은 다섯 중 넷이 아무 데도 가지 않고 통과했고 "
   "나머지 하나가 <code>carlini/regex-chess</code>를 클론했다 — <b>이 과제는 보지 않고도 풀리는 "
   "과제이며, 그 점에서 이 1.00은 magi에게 불리한 사실을 가리고 있다</b>"),
 "qemu-startup": ("자기가 받은 오류 문자열 <code>rosetta error: unimplemented syscall 282</code> — "
   "x86_64 QEMU가 Rosetta 아래서 막히는 그 문제이고, 우회를 찾아 통과했다"),
}

def read_runs():
    """job 디렉토리를 훑는다. 없으면(클론) 커밋된 아카이브에서 같은 것을 읽는다.

    아카이브는 채택된 시행만 담으므로 격리·ADOPT 로직이 걸릴 대상이 애초에 없다 — 그래서
    같은 페이지가 나온다. 원본 jobs/ 가 있으면 그쪽이 우선이다(재실행을 반영해야 하므로).
    """
    runs = [r for r in _runs_from_jobs()]
    return runs or _runs_from_archives()

def _runs_from_archives():
    import tarfile
    runs = []
    man = os.path.join(ARCDIR, "manifest.json")
    if not os.path.exists(man):
        return runs
    for task, meta in json.load(open(man)).items():
        day, hhmm = meta["run"].rsplit("-", 1)
        tgz = os.path.join(ARCDIR, task + ".tar.gz")
        if not os.path.exists(tgz):
            continue
        with tarfile.open(tgz) as tf:
            def grab(suffix):
                for m in tf.getmembers():
                    if m.name.endswith(suffix):
                        f = tf.extractfile(m)
                        return f.read().decode("utf-8", "replace") if f else ""
                return ""
            try:
                r = json.loads(grab("/result.json"))
            except Exception:
                continue
            rw = ((r.get("verifier_result") or {}).get("rewards") or {}).get("reward")
            if rw is None:
                continue
            txt = grab("agent/magi-stdout.txt")
            runs.append(dict(task=task, day=day, hhmm=hhmm, ok=rw == 1.0,
                             exc=(r.get("exception_info") or {}).get("exception_type") or "",
                             unverified="landed UNVERIFIED" in txt,
                             calls=txt.count("\n⚙ "), cn=txt.count("⚖ council:")))
    return runs

def _runs_from_jobs():
    runs = []
    for rj in glob.glob(os.path.join(REPO, "jobs", "*-sonnet-one-*", "*", "result.json")):
        d = os.path.dirname(rj)
        m = re.match(r"(\d{4}-\d{2}-\d{2})-(\d{4})-sonnet-one-(.+)$", os.path.basename(os.path.dirname(d)))
        if not m:
            continue
        day, hhmm, task = m.groups()
        try:
            r = json.load(open(rj))
        except Exception:
            continue
        rw = ((r.get("verifier_result") or {}).get("rewards") or {}).get("reward")
        if rw is None:
            continue
        exc = (r.get("exception_info") or {}).get("exception_type") or ""
        s = os.path.join(d, "agent/magi-stdout.txt")
        txt = open(s, errors="ignore").read() if os.path.exists(s) else ""
        runs.append(dict(task=task, day=day, hhmm=hhmm, ok=rw == 1.0, exc=exc,
                         unverified="landed UNVERIFIED" in txt,
                         calls=txt.count("\n⚙ "), cn=txt.count("⚖ council:")))
    return runs

def ledger():
    """(task, hhmm) -> (usd, secs).

    실행 로그가 있으면 거기서 읽고, 없으면 굳혀 둔 ledger.tsv 를 쓴다. 로그는 저장소에 없으므로
    클론에서 페이지를 다시 만들 때 쓰이는 것은 늘 후자다.
    """
    logs = sorted(glob.glob(os.path.join(REPO, "scratchpad", "*.log")))
    if logs:
        return _ledger_from_logs(logs)
    out = {}
    for ln in open(os.path.join(HERE, "ledger.tsv")):
        if ln.startswith("#") or not ln.strip():
            continue
        task, hhmm, usd, secs = ln.rstrip("\n").split("\t")
        out[(task, hhmm)] = (float(usd), int(secs))
    return out

def _ledger_from_logs(logs):
    """(task, hhmm) -> (usd, secs), from the per-run logs the harness wrote as it went."""
    out, cur = {}, None
    for f in logs:
        for ln in open(f, errors="ignore"):
            m = re.search(r"\[(\d\d):(\d\d):\d\d\] === sonnet .* (\S+) · loop ([0-9a-f]+)", ln)
            if m:
                cur = dict(hhmm=m.group(1) + m.group(2), task=m.group(3), before=None, t0=None)
                continue
            m = re.search(r"\[(\d\d):(\d\d):(\d\d)\] ledger (before|after)\s*:"
                          r"\s*(?:calls in out cache_read cache_write usd =)?\s*([\d. ]+)$", ln)
            if m and cur:
                usd = float(m.group(5).split()[-1])
                sec = int(m.group(1)) * 3600 + int(m.group(2)) * 60 + int(m.group(3))
                if m.group(4) == "before":
                    cur["before"], cur["t0"] = usd, sec
                else:
                    if cur["before"] is not None:
                        d = sec - cur["t0"]
                        out[(cur["task"], cur["hhmm"])] = (round(usd - cur["before"], 4),
                                                          d + 86400 if d < 0 else d)
                    cur = None
    return out

def quarantined():
    """Trials whose web calls reached the dataset's own task or solution pages.

    A verdict reached after finding the answer is not evidence about the agent, so the trial is
    held out of every table and the task is queued again -- and it is the re-run that counts.
    The hold is on the one trial, not the task, so a clean re-run is picked up on its own.
    """
    out = {}
    f = os.path.join(HERE, "quarantine.tsv")
    if not os.path.exists(f):
        return out
    for ln in open(f):
        if ln.startswith("#") or not ln.strip():
            continue
        task, day, hhmm, why = ln.rstrip("\n").split("\t")
        out[(task, day, hhmm)] = why
    return out

# A task whose answer is a famous public repository can defeat the re-run rule outright: every
# attempt reaches it, so there is never a clean trial to adopt and the row would vanish. Naming the
# run the table uses keeps the row -- and the caveat beside it does the work the hold cannot.
# regex-chess: eight attempts on 2026-08-26 each fetched the answer file; 1310 is the last one that
# ran to completion, so it is what the table carries, with its limitation stated in WEBWHAT.
ADOPT = {"regex-chess": ("2026-08-26", "1310")}

# 리더보드 자신의 규칙을 이쪽에도 적용한다. tbench.ai 의 leaderboard integrity update 는 리워드 해킹을
# "모델이 과제가 재려던 능력을 보이지 않고 구멍을 이용해 해결하는 것"으로 정의하고 그 결과를
# "시행 보상 0"으로 못박는다(예로 든 것이 인터넷에서 정답을 찾는 경우이고, 선례는 웹에서 정답을 curl 로
# 받아 쓴 ForgeCode 시행들의 0점 재산정이다). 비교 대상의 숫자도 같은 규칙으로 3건이 실격돼
# 75.28% -> 74.61% 가 된 값을 쓰므로, 이쪽만 원시 보상을 쓰면 비교가 성립하지 않는다.
REWARD_HACK = {
    "regex-chess": "carlini/regex-chess 의 chess_engine.py 원문을 받아 풀었다",
}

def pick(runs, led):
    # A held trial takes everything before it with it. Falling back to an older run would put a
    # number in the table that the rule never adopted -- only a re-run, which is later, counts.
    held = quarantined()
    cut = {}
    for (task, day, hhmm) in held:
        k = day + hhmm
        cut[task] = max(cut.get(task, ""), k)
    runs = [r for r in runs
            if r["task"] in ADOPT or r["day"] + r["hhmm"] > cut.get(r["task"], "")]
    # A pinned task keeps exactly the named run, so neither a later abandoned attempt nor a hold
    # on an earlier one can move it.
    runs = [r for r in runs
            if r["task"] not in ADOPT or (r["day"], r["hhmm"]) == ADOPT[r["task"]]]
    by = {}
    for r in runs:
        by.setdefault(r["task"], []).append(r)
    sel = []
    for t, rs in by.items():
        rs.sort(key=lambda x: (x["day"], x["hhmm"]))
        ok = [r for r in rs if r["exc"] not in CONTAMINATED]
        if not ok:
            continue
        r = ok[-1]
        r["usd"], r["secs"] = led.get((t, r["hhmm"]), (None, None))
        if r["usd"] is None:
            continue
        sel.append(r)
    return sorted(sel, key=lambda r: r["task"])

# 표의 각 행은 그 시행의 job 디렉토리 아카이브로 링크한다. 아티팩트는 자기 페이지에서 시작한 다운로드가
# 막혀 있으므로, 링크는 저장소의 파일을 가리킨다(푸시 이후에 열린다).
RUNBASE = "https://github.com/sayaya1090/magi/raw/main/bench/harbor/runs/tb21-2026-08/"

def archive_sizes():
    """태스크 -> "57 KB". 아카이브가 아직 없으면 빈 문자열."""
    out = {}
    for name in os.listdir(ARCDIR) if os.path.isdir(ARCDIR) else []:
        if name.endswith(".tar.gz"):
            n = os.path.getsize(os.path.join(ARCDIR, name))
            out[name[:-7]] = f"{n/1024:.0f} KB" if n < 1024 * 1024 else f"{n/1024/1024:.1f} MB"
    return out

def main():
    cc = {}
    for ln in open(os.path.join(HERE, "cc89.tsv")):
        t, p, n, usd, b = ln.rstrip("\n").split("\t")
        cc[t] = dict(p=int(p), n=int(n), usd=float(usd), b=int(b))
    sel = [r for r in pick(read_runs(), ledger()) if r["task"] in cc]
    rows = [dict(t=r["task"], ok=r["ok"] and r["task"] not in REWARD_HACK,
                hack=REWARD_HACK.get(r["task"], ""), calls=r["calls"], cn=r["cn"], usd=round(r["usd"], 2),
                 secs=r["secs"], exc=r["exc"], head="f4c94d00",
                 ccp=cc[r["task"]]["p"], ccn=cc[r["task"]]["n"],
                 ccusd=round(cc[r["task"]]["usd"] / cc[r["task"]]["n"], 2),
                 unverified=r["unverified"],
                 ccblank=cc[r["task"]]["b"]) for r in sel]

    N = len(rows)
    bill = lambda r: 0.0 if r["exc"] == "AgentTimeoutError" else r["usd"]
    mp = sum(1 for r in rows if r["ok"])
    mu, mdrop = sum(bill(r) for r in rows), sum(r["usd"] - bill(r) for r in rows)
    ccp = sum(r["ccp"] for r in rows); ccn = sum(r["ccn"] for r in rows)
    ccu = sum(r["ccusd"] for r in rows); ccb = sum(r["ccblank"] for r in rows)
    hrs = sum(r["secs"] for r in rows) / 3600

    def card(r, good=False):
        note = NOTES.get(r["t"])
        if not note and good:
            # A task only magi solved is not a failure, and must not borrow a failure's wording.
            note = ("%d콜 · 카운슬 %d회 · %d분 %d초 · $%.2f로 통과했다. "
                    "Claude Code는 다섯 번 다 실패했다."
                    % (r["calls"], r["cn"], r["secs"] // 60, r["secs"] % 60, r["usd"]))
        if not note:
            if r.get("unverified"):
                why = ("완료를 <b>선언하지 않고</b> UNVERIFIED로 착지했다 — 자기 결과가 "
                       "틀린 걸 알고 있었으므로 카운슬도 열리지 않았다")
            elif r["exc"] == "AgentTimeoutError":
                why = "벽시계에 잘림"
            else:
                why = "정상 종료하고도 0점 — 틀린 산출물로 done을 선언했다"
            note = (f"{why}. {r['calls']}콜 · 카운슬 {r['cn']}회 · "
                    f"{r['secs']//60}분 {r['secs']%60}초. <b>아직 로그를 읽지 않았다.</b>")
        style = ' style="border-left-color:var(--pass)"' if good else ''
        return ('<div class="fc"%s><div class="n">%s</div><div class="w">%s</div>'
                '<div class="cc">CC %d/%d</div></div>'
                % (style, html.escape(r["t"]), note, r["ccp"], r["ccn"]))

    # Name the held tasks rather than folding them into the total -- a reader cannot audit what
    # the table does not mention.
    held = sorted({t for (t, _, _) in quarantined()})
    waiting = sorted(t for t in held if t not in {r["t"] for r in rows})
    qline = ""
    if waiting:
        names = " · ".join("<code>%s</code>" % t for t in waiting)
        qline = ('<li><b>정답을 찾아 나선 trial은 격리하고 다시 돌린다.</b> 웹 호출이 데이터셋 자신의 '
                 '과제·정답 페이지에 닿은 trial은 어떤 표에도 서지 않는다 — 답을 찾은 뒤의 판정은 '
                 '에이전트에 대한 증거가 아니기 때문이다. 지금 재실행을 기다리는 것은 %s. '
                 '채택되는 값은 재실행 결과다.</li>' % names)
    # magi has websearch/webfetch; the leaderboard's Claude Code has neither (sampled trials show
    # Bash/Read/Edit only, with curl going to localhost). That is a real difference in what the two
    # could do, so the tasks where magi actually used the web are named rather than left implicit.
    used = []
    for r in sel:
        g = glob.glob(os.path.join(REPO, "jobs",
                                   "%s-%s-sonnet-one-%s" % (r["day"], r["hhmm"], r["task"]),
                                   "*", "agent", "magi-stdout.txt"))
        if not g:
            continue
        txt = open(g[0], errors="ignore").read()
        n = txt.count("⚙ webfetch") + txt.count("⚙ websearch")
        if n:
            used.append((r["task"], n, cc[r["task"]]["p"], cc[r["task"]]["n"]))
    webline = ""
    if used:
        items = "".join(
            "<li><code>%s</code> — %s</li>" % (t, WEBWHAT.get(t, "<b>아직 무엇을 찾았는지 읽지 않았다</b>"))
            for t, _, _, _ in sorted(used))
        ccw = json.load(open(os.path.join(HERE, "cc_web.json")))
        ccper = ccw["per"]
        both = sorted(t for t, _, _, _ in used if ccper.get(t))
        items = "".join(
            "<li><code>%s</code>%s — %s</li>" % (
                t, (" <span class='tag'>CC도 %d/5</span>" % ccper[t]) if ccper.get(t) else "",
                WEBWHAT.get(t, "<b>아직 무엇을 찾았는지 읽지 않았다</b>"))
            for t, _, _, _ in sorted(used))
        webline = ('<li><b>둘 다 웹을 썼다 — 한쪽만 쓴 것이 아니다.</b> '
                   'magi에게는 <code>websearch</code>·<code>webfetch</code>가 도구로 있고, '
                   'Claude Code에게는 그런 도구가 없는 대신 Bash가 있다. 리더보드 job의 trial '
                   '439개를 내려받아 세어 보니 <b>54개 trial(22개 태스크)이 curl·wget으로 외부 '
                   '호스트를 때렸고</b>, 그중 30개(13개 태스크)는 패키지·배포 미러가 아닌 곳에 닿았다. '
                   '태스크 넷에 걸친 trial 여섯은 검색엔진을 직접 쳤다 — 도구가 없을 뿐 검색은 했다. '
                   '집계된 %d개 중 magi가 웹을 쓴 것은 %d개이고, 그중 %d개는 CC도 같은 태스크에서 '
                   '외부를 때렸다. 무엇을 찾았는지는 이렇다:'
                   '<ul style="margin:8px 0 0;padding-left:18px">%s</ul>'
                   '<div style="margin-top:8px">겹치는 자리에서는 <b>가는 곳까지 같았다</b> — '
                   '<code>dna-assembly</code>는 양쪽 다 <code>www.neb.com</code>, '
                   '<code>build-pov-ray</code>는 양쪽 다 <code>povray.org</code>와 검색엔진, '
                   '<code>torch-pipeline-parallelism</code>은 CC의 한 trial이 '
                   '<code>api.github.com/repos/huggingface/picotron</code>과 그 README를 받았다 — '
                   'magi를 격리 대상으로 올렸던 바로 그 참조 구현이다. '
                   '같은 감사기를 CC의 439개에 돌린 결과 <b>데이터셋·정답·채점기 URL에 닿은 trial은 '
                   '0개</b>였다. magi 쪽도 답을 받아온 호출은 없다: 둘은 내려받을 주소였고 나머지는 '
                   '도구·벤더가 자기 물건에 대해 써놓은 문서다. 의미를 가리려고 검색한 유일한 건 '
                   '<code>gcode-to-text</code>인데, 찾아보고도 틀렸다.</div></li>'
                   % (len(rows), len(used), len(both), items))
    arc = archive_sizes()
    for r in rows:
        r["arc"] = arc.get(r["t"], "아카이브 없음")
    fails = [r for r in rows if not r["ok"]]
    only = [r for r in rows if r["ok"] and r["ccp"] == 0]
    tpl = open(os.path.join(HERE, "page.html")).read()
    page = (tpl
      .replace("{{ROWS}}", json.dumps(rows, ensure_ascii=False, separators=(",", ":")))
      .replace("{{N}}", str(N))
      .replace("{{MPASS}}", str(mp)).replace("{{MPCT}}", f"{mp/N*100:.1f}")
      .replace("{{MUSD}}", f"{mu:,.2f}").replace("{{MDROP}}", f"{mdrop:,.2f}")
      .replace("{{NTO}}", str(sum(1 for r in rows if r["exc"] == "AgentTimeoutError")))
      .replace("{{CCPASS}}", str(ccp)).replace("{{CCN}}", str(ccn))
      .replace("{{CCPCT}}", f"{ccp/ccn*100:.1f}").replace("{{CCUSD}}", f"{ccu:,.2f}")
      .replace("{{CCBLANK}}", str(ccb))
      .replace("{{RATIO}}", f"{mu/ccu:.2f}" if ccu else "—")
      .replace("{{COVPCT}}", f"{N/89*100:.0f}").replace("{{HRS}}", f"{hrs:.1f}")
      .replace("{{FAILCARDS}}", "\n    ".join(card(r) for r in fails))
      .replace("{{NFAIL}}", str(len(fails)))
      .replace("{{ONLYCARDS}}", "\n    ".join(card(r, True) for r in only))
      .replace("{{NONLY}}", str(len(only)))
      .replace("{{QUARANTINE}}", qline)
      .replace("{{WEBTOOLS}}", webline)
      .replace("{{RUNBASE}}", RUNBASE)
      .replace("{{STAMP}}", subprocess.run(["date", "+%m-%d %H:%M"], capture_output=True,
                                           text=True).stdout.strip()))
    open(OUT, "w").write(page)
    print(f"{N} tasks · magi {mp}/{N} = {mp/N*100:.1f}% ${mu:.2f} "
          f"(타임아웃 {mdrop:.2f} 제외) · CC {ccp}/{ccn} = {ccp/ccn*100:.1f}% ${ccu:.2f} · {mu/ccu:.2f}×")


if __name__ == "__main__":
    main()
