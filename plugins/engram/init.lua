-- engram — 자기개선 관찰자 플러그인 (관찰 이벤트 기반 교훈/스킬 학습).
-- 캡처: user_message에 결과 신호가 보이면 magi.analyze 사이드카(도구 없음)로
-- 교훈/검증된 스킬을 추출해 SESSION_SUMMARY.md / .claude/skills 에 기록.
-- 회상: register_context_provider 로 최근 교훈을 컨텍스트에 주입.
-- (관찰 이벤트는 호스트가 턴 경로 밖의 워커에서 돌리므로 사이드카가 턴을 막지 않는다.
-- 사이드카가 도는 동안 이 플러그인의 컨텍스트 프로바이더는 그 스텝을 스킵할 수 있다 —
-- 호스트의 바운드 락 획득이 의도한 우아한 퇴화다. 파일 쓰기는 호스트의 단일 관찰
-- 워커가 직렬화하므로 별도 파일락이 필요 없다.)

local SUMMARY = "SESSION_SUMMARY.md"
local SKILLS_DIR = ".claude/skills"

-- 셸 스크립트로 보이는가: 한글 음절이 없고(UTF-8 EA B0 80 ~ ED 9E A3), 첫 비어 있지 않은 줄이
-- shebang 이거나 명령처럼 시작한다. 산문 절차를 .sh 로 쓰지 않기 위한 문지기(2026-09-05).
local function looksLikeShell(s)
  -- (Lua 5.1 에는 \x 이스케이프가 없다 — 10진 바이트로 적는다: EA B0 80 ~ ED 9E A3)
  if s:find("[\234-\237][\128-\191][\128-\191]") then return false end
  local first = s:match("^%s*([^\n]+)") or ""
  return first:match("^#!") ~= nil or first:match("^[%w_%./%-]+[%s$]") ~= nil or first:match("^[%w_%./%-]+$") ~= nil
end
local LEDGER_TITLE = "# 작업 이력 및 교훈 기록 (팀 공유)"
local LEDGER_HEADER = "| 일시 | 사용자 | 분류 | 작업 | 시도한 접근 | 결과 | 교훈 |"
local LEDGER_DIVIDER = "| :--- | :--- | :--- | :--- | :--- | :--- | :--- |"
-- 정정·아카이브 섹션: 교체되거나 밀려난 행이 삭제 대신 이 헤딩 아래로 내려간다(툼스톤).
-- 활성 표를 읽는 모든 경로(회상 주입·중복 판정·다이제스트)는 이 헤딩에서 멈춘다 —
-- 정정으로 밀려난 옛 주장이 계속 주입되고 새 기록을 중복으로 막던 것이 원장의 약점이었다.
local ARCHIVE_TITLE = "## 이전 기록 (정정·아카이브 — 회상에 주입되지 않음)"
local MAX_RECENT = 8        -- 사이드카 입력 윈도우
local MAX_LESSON_ROWS = 50  -- 회상 주입 상한(행)
local MAX_LESSON_CHARS = 6000
local MAX_ACTIVE_ROWS = 80  -- 활성 표 상한 — 초과분은 오래된 행부터 아카이브로 (스킬 prune과 대칭)

-- 세션별 최근 대화 윈도우 / 세션 내 중복 교훈 가드
local recent = {}   -- sid -> { {role=, text=}, ... }
local recorded = {} -- sid -> { [lessonKey]=true }

-- 사이드카 분석기 시스템 프롬프트 (결정 규칙·성공 확정 정의·환각 금지)
local SIDECAR_PROMPT = [[당신은 개발 대화 로그를 분석하는 전용 분석기다. 코딩이나 도구 호출은 절대 하지 말 것.
입력으로 최근 대화 일부가 주어진다. 아래를 판단해 JSON "하나"로만 출력하라. 설명·마크다운·코드펜스 금지.

출력 형태:
{"lesson": <교훈 객체 또는 null>, "skill": <스킬 객체 또는 null>}

[결정 규칙 — 반드시 이대로 분류]
1) "성공이 확정"되고 재사용 가능한 구체적 기법이면 → lesson(outcome:"success")과 skill을 둘 다 채운다.
2) 실패/부분이 확정되면 → lesson(outcome:"fail" 또는 "partial")만 채우고 skill=null. (실패 접근은 스킬로 만들지 말 것)
3) 성패가 아직 확정 안 됐거나, 일반 상식·사소함이라 기록 가치가 없으면 → {"lesson":null,"skill":null}.

[성공 확정의 정의 — 중요]
- "성공 확정"은 사용자가 됐다고 확인했거나(예: '됐다/해결됨/통과') 대화에서 실제로 검증된 경우만 해당한다.
- 에이전트가 방금 해법을 제안·적용한 것만으로는 success가 아니다(아직 미확정). 이 경우 skill=null, lesson은 outcome 미확정이면 null.
- 즉 검증되지 않은 제안을 스킬로 만들지 마라. 스킬은 "이미 먹힌 게 확인된" 기법만.

[근거 규칙 — 환각·과적합 금지]
- 대화에서 실제로 일어나고 확인된 사실에만 근거하라. 시도하지 않은 단계, 확인되지 않은 원인·해결책을 지어내지 마라.
- 확정 안 된 건 outcome에 반영하거나 아예 기록하지 마라.
- 특정 프로젝트·환경에만 해당하면 그 조건을 lesson에 명시하고 일반화하지 마라.
- lesson에는 사용자 지시문의 재진술("~하면 된다"식 요구사항 되풀이)을 넣지 마라 — 검증된 인과(왜 그 결과가 났는가)와 재사용 가능한 결론만.
- 입력 끝에 [기존 교훈] 목록이 주어지면: 실질적으로 같은 교훈이 이미 있을 때 lesson=null (표현만 바뀐 재기록 금지 — 특히 회상된 지식을 확인만 한 턴).

형식:
- lesson: {"task":"어떤 작업","approach":"어떻게 시도","outcome":"success|fail|partial","lesson":"왜 그 결과가 났고 다음에 뭘 해야/피해야 하나","category":"디버깅|설정|구현|리팩터링|분석|기타","replaces":<기존 교훈 행 번호 또는 null>}
  · replaces(정정/병합 — 중요): [기존 교훈] 목록의 행이 (n) 번호를 달고 온다. 이번 교훈이 그중 한 행과 **같은 주제의 정정·구체화·뒤집기**라면 새 행을 추가하지 말고 replaces에 그 번호를 넣어라 — 그 행이 이번 교훈으로 교체되고 옛 행은 아카이브로 내려간다. 특히 결과가 모순되게 뒤집혔으면(전에 ✅였는데 지금 ❌로 판명, 또는 반대) 반드시 replaces를 쓰고 lesson에 왜 뒤집혔는지를 담아라 — 낡은 주장과 새 주장이 나란히 남는 것이 이 원장의 최악의 상태다. 무관한 새 주제일 때만 replaces=null.
- skill:  {"name":"영문 소문자+언더스코어","trigger":"이 스킬이 필요한 상황","technique":"구체적 해결 기법","description":"트리거 문장","verify":"(선택) 적용 후 성공을 확인하는 커맨드나 체크","avoid":"(선택) 이 스킬을 쓰면 안 되는 상황·알려진 함정(관련 ❌실패 교훈이 있으면 여기에 병합)"}
  · description은 무엇을+언제를 한 문장에 담은 3인칭 트리거 문장으로, 키워드·에러명을 앞쪽에 배치한다.
  · trigger/description에 이번 작업 고유의 파일명·경로·프로젝트명을 넣지 마라 — 일반 조건으로 써라(예: "json_output.py 실행 시" ✗ → "python json.dumps 출력에서 한글이 \uXXXX로 이스케이프될 때" ✓). 단 에러 메시지 원문·라이브러리명·옵션명은 그대로 포함하라(트리거 매칭의 핵심).
  · technique에는 실제 실행돼 검증된 커맨드/코드 조각을 패러프레이즈 없이 그대로 담아라 — 요약하면 정확한 식별자(옵션명·플래그)가 유실된다.
  · 병합 우선(진화): 입력 끝의 [기존 스킬] 본문이 그 스킬의 현재 최신 상태다(사람이 직접 고쳤을 수 있다). 같은 주제를 다시 다룬다면 그 name을 재사용하고, 기존 본문을 baseline으로 삼아 이번에 새로 확인된 것만 더하거나 다듬어라 — 기존 본문에 이미 담긴 내용(특히 사람이 추가·수정한 지침)을 삭제하거나 되돌리지 마라. 무관한 새 기법일 때만 새 name.
  · 사용한 스킬 복제 금지(중요): [기존 스킬] 중 **[이번 턴 사용됨]**으로 표시된 것은 이번 작업에서 실제로 로드·적용된 스킬이다. 이번 성공이 그 스킬을 **써서** 난 것이라면, 그것과 사실상 같은 기법을 이름만 바꿔 새 스킬로 만들지 마라 — 그 스킬은 이미 있고 이번에 다시 먹혔음이 확인됐을 뿐이며, 복제는 유사 스킬 난립의 원인이다. 그 스킬에 이번에 새로 다듬을 게 있으면 그 name을 그대로 재사용하고, 딱히 없으면 skill=null. 진짜로 그 스킬과 무관한 별개 기법일 때만 새 스킬을 만들어라.

민감정보 제거(필수, 기록은 git으로 팀 공유됨): 절대경로→상대화, API키/토큰→종류만, IP/도메인/포트→역할, 이메일/실명→역할, 비밀번호/환경변수 값→변수명.]]

-- 결과 게이트: 사용자 발화 키워드 휴리스틱으로 성패를 추측하는 대신, magi 호스트가
-- turn_finished 페이로드에 실어주는 **구조적 판정**을 쓴다 — 호스트는 턴의 성패를
-- 실측으로 안다(카운슬 합의/UNVERIFIED 착지/가드 강제정지/에러). 사용자 발화에서
-- 성패를 추측할 필요가 없다.
--   verified   → 카운슬이 증거를 보고 done에 합의한 완료 (성공 확정의 최상급 근거)
--   unverified → 착지는 했지만 카운슬이 끝내 승인 안 함 (실패/부분 후보)
--   guard      → loop/stall 가드 강제정지 (실패 확정)
--   error      → 에러로 종료 (실패 확정)
--   ungated    → 실작업(툴 사용) 턴인데 카운슬 게이트가 아예 안 돌았다(카운슬 비활성/워크플로/하위깊이)
--                → 완료가 검증되지 않음. 사용자 확인 없이 성공으로 기록하지 마라
--   done       → 카운슬 판정 없는 평이한 종료(잡담 등) → 분석 안 함
local ANALYZE_OUTCOMES = { verified = true, unverified = true, guard = true, error = true, ungated = true }

local function outcome_hint(outcome, reason)
  local desc = {
    verified = "카운슬(3인 합의 게이트)이 실행 증거를 보고 완료를 승인했다 — 성공 확정",
    unverified = "턴은 끝났지만 카운슬이 끝내 승인하지 않았다(UNVERIFIED) — 실패 또는 부분",
    guard = "무진전 가드가 강제 정지시켰다 — 실패 확정",
    error = "에러로 종료됐다 — 실패 확정",
    ungated = "실작업 턴인데 검증 게이트가 돌지 않았다 — 완료 미검증. 사용자가 확인하지 않았다면 성공으로 기록하지 마라",
  }
  local h = "[호스트 판정] outcome=" .. outcome .. " — " .. (desc[outcome] or outcome)
  if reason and reason ~= "" then h = h .. " (사유: " .. reason .. ")" end
  return h .. "\n이 판정은 호스트의 실측이다. lesson.outcome은 이 판정과 모순되게 쓰지 마라."
end

local function sanitize_cell(v)
  v = tostring(v or "")
  v = string.gsub(v, "%z", "")
  v = string.gsub(v, "\1", "") -- ledger_cells의 이스케이프 마스크 바이트 — 셀이 위조하면 안 된다
  v = string.gsub(v, "|", "\\|")
  v = string.gsub(v, "\r?\n", " ")
  return (string.gsub(v, "^%s*(.-)%s*$", "%1"))
end

local function outcome_label(o)
  if o == "success" then return "✅ 성공" end
  if o == "fail" then return "❌ 실패" end
  if o == "partial" then return "⚠️ 부분" end
  return o or "-"
end

local function slugify(name)
  local s = string.lower(tostring(name or ""))
  s = string.gsub(s, "[^a-z0-9_%-]+", "_")
  s = string.gsub(s, "^[_%-]+", "")
  s = string.gsub(s, "[_%-]+$", "")
  if s == "" then s = "skill" end
  return string.sub(s, 1, 64)
end

local function lesson_key(task, approach)
  local k = string.lower((task or "") .. "|" .. (approach or ""))
  return (string.gsub(k, "%s+", " "))
end

-- LLM 출력에서 JSON 오브젝트 추출(코드펜스/잡담 허용: 첫 { ~ 마지막 })
local function parse_cortex_json(raw)
  if not raw then return nil end
  local s = string.find(raw, "{", 1, true)
  local e = nil
  for i = #raw, 1, -1 do
    if string.sub(raw, i, i) == "}" then e = i break end
  end
  if not s or not e or e <= s then return nil end
  local obj = magi.json_decode(string.sub(raw, s, e))
  return obj
end

local function push_recent(sid, role, text)
  if not text or text == "" then return end
  local r = recent[sid]
  if not r then r = {} recent[sid] = r end
  r[#r + 1] = { role = role, text = string.sub(text, 1, 4000) }
  while #r > MAX_RECENT do table.remove(r, 1) end
end

local function render_window(r)
  local parts = {}
  for _, m in ipairs(r) do
    parts[#parts + 1] = "[" .. m.role .. "]\n" .. m.text
  end
  return table.concat(parts, "\n\n---\n\n")
end

-- 스킬 인덱스(병합 판단용 다이제스트)는 플러그인 스토어에 name<TAB>description 행으로 유지
-- (샌드박스에 디렉토리 나열이 없어 파일 시스템 스캔 대신 자체 인덱스를 쓴다)
-- SKILL_BODY_CAP bounds one skill's body in the digest. Skills are authored SHORT
-- (save_skill keeps them terse; long verify procedures go to a side script), so the
-- FULL current SKILL.md fits — the sidecar must see the file's actual current text
-- (including a HUMAN edit) as the baseline to evolve from, not a stale one-line index.
local SKILL_BODY_CAP = 2000

-- used_now is a set {slug=true} of skills LOADED THIS TURN (from turn_finished ev.skills), so
-- the digest can mark them [이번 턴 사용됨]. That closes the duplicate-skill loop the user hit:
-- a turn that SUCCEEDED BY USING skill X is confirmation X works, not grounds to extract an
-- X-prime under a new name — the sidecar must see which skills the success actually leaned on.
-- 디스크가 진실이다. 이 목록은 오래도록 engram 자신의 장부(skill_index)에서만 나왔고, 그
-- 장부는 engram이 직접 쓴 스킬만 담는다 — 사람이 만든 스킬, 다른 컴패니언이 만들어 공유
-- 스토어로 넘어온 스킬, 장부가 생기기 전의 스킬은 디스크에 있으면서 사이드카 눈에는 없었다.
-- 없으니 "이미 있다"를 판단할 수 없고, 모델은 같은 기법을 새 이름으로 다시 만든다. 유사 스킬
-- 난립의 원인 중 프롬프트로는 절대 못 고치는 절반이 이것이었다.
--
-- 이제 SKILLS_DIR을 직접 나열하고, 장부는 설명 문구의 폴백으로만 남는다(디렉토리는 이름만
-- 주고 한 줄 설명은 안 주므로). 나열이 실패하면 예전처럼 장부로 돌아간다 — 목록이 없는 것보다
-- 불완전한 목록이 낫다.
local function skill_slugs()
  local seen, out = {}, {}
  for _, name in ipairs(magi.list_files(SKILLS_DIR) or {}) do
    local slug = string.match(name, "^(.+)/$") -- 디렉토리만: 스킬 하나 = 폴더 하나
    if slug and slug ~= ".archive" and not seen[slug] then
      seen[slug] = true
      out[#out + 1] = slug
    end
  end
  return out, seen
end

local function skills_digest(used_now)
  local idx = magi.store_get("skill_index") or ""
  -- 장부의 한 줄 설명을 슬러그로 색인해 둔다(본문을 못 읽을 때의 폴백).
  local desc_of = {}
  for line in string.gmatch(idx, "[^\n]+") do
    local n, d = string.match(line, "^([^\t]*)\t(.*)$")
    if n then desc_of[n] = d end
  end
  local slugs, on_disk = skill_slugs()
  -- 디스크에서 못 읽었으면 장부라도 쓴다.
  if #slugs == 0 then
    for n in pairs(desc_of) do
      if not on_disk[n] then slugs[#slugs + 1] = n end
    end
  end
  if #slugs == 0 then return "" end
  -- 사용 실적을 병합 판단 근거로 함께 노출(로드/성공/실패).
  local usage = {}
  for line in string.gmatch(magi.store_get("skill_usage") or "", "[^\n]+") do
    local n, l, o, b = string.match(line, "^([^\t]*)\t(%d+)\t(%d+)\t(%d+)$")
    if n then usage[n] = " (로드 " .. l .. "·성공 " .. o .. "·실패 " .. b .. ")" end
  end
  local blocks = {}
  for _, name in ipairs(slugs) do
    do
      local desc = desc_of[name] or ""
      -- The CURRENT on-disk body is the baseline: a human edit to SKILL.md becomes the
      -- starting point of the next extraction, so refinement builds on it rather than
      -- reverting it. Fall back to the one-line index if the file is unreadable.
      local body = magi.read_file(SKILLS_DIR .. "/" .. name .. "/SKILL.md")
      local mark = ""
      if used_now and used_now[name] then mark = " **[이번 턴 사용됨]**" end
      local header = "### " .. name .. (usage[name] or "") .. mark
      if body and body ~= "" then
        if #body > SKILL_BODY_CAP then body = string.sub(body, 1, SKILL_BODY_CAP) .. "\n…(생략)" end
        blocks[#blocks + 1] = header .. "\n" .. body
      else
        blocks[#blocks + 1] = header .. ": " .. desc
      end
    end
  end
  if #blocks == 0 then return "" end
  return "[기존 스킬] (아래 본문이 현재 최신 상태다 — 이를 baseline으로 개선/병합하되, 이미 담긴 내용을 되돌리지 마라)\n\n"
    .. table.concat(blocks, "\n\n")
end

local function index_skill(slug, desc)
  local idx = magi.store_get("skill_index") or ""
  local out = {}
  for line in string.gmatch(idx, "[^\n]+") do
    local name = string.match(line, "^([^\t]*)\t")
    if name ~= slug then out[#out + 1] = line end
  end
  out[#out + 1] = slug .. "\t" .. string.gsub(desc or "", "\n", " ")
  magi.store_set("skill_index", table.concat(out, "\n"))
end

-- 원장을 활성부/아카이브부로 가른다. 아카이브 헤딩 아래는 툼스톤 — 어떤 읽기 경로에도
-- 실리지 않고, git과 사람에게만 남는 기록이다.
local function split_ledger(raw)
  raw = raw or ""
  local i = string.find(raw, ARCHIVE_TITLE, 1, true)
  if not i then return raw, "" end
  return string.sub(raw, 1, i - 1), string.sub(raw, i)
end

local function is_data_row(line)
  local t = string.gsub(line, "^%s+", "")
  return string.sub(t, 1, 1) == "|" and not string.find(t, "일시 | 사용자", 1, true)
    and not string.match(t, "^|%s*:?%-%-")
end

-- 원장 행의 판정과 분해는 이 한 곳이다: 다이제스트 번호·정정(replaces) 대상·상한 정리가
-- 전부 같은 행 집합을 봐야 한다(어긋나면 번호가 밀려 엉뚱한 행이 아카이브된다). "≥7컬럼
-- |행"만으로는 부족했다 — 사람이 산문 구역에 둔 7컬럼 표(주간 일정표)가 헤더째 원장으로
-- 읽혀 사이드카에 "기존 교훈"으로 나가고 정정·상한의 표적이 됐다(4라운드 검수, 실측).
-- 원장 행의 제1컬럼은 magi.time()의 RFC3339 일시다: 날짜꼴 첫 컬럼을 요구하면 사람의 표는
-- 형태로 갈라지고, 날짜 없는 행은 원장이 아니라 사람 몫으로 취급돼 어느 경로도 손대지
-- 않는다. 들여쓴 행의 선행 공백이 세그먼트 하나로 세리며 컬럼을 밀던 것도 트림으로 봉한다.
-- (Lua 패턴에 lazy 빈 캡처 함정이 있어 비지 않은 세그먼트만 뽑는다.)
local function ledger_cells(line)
  if not is_data_row(line) then return nil end
  -- 작성기(sanitize_cell)가 셀 안의 |를 \|로 이스케이프한다 — 분해도 같은 규약을 지킨다:
  -- \|에서 쪼개면 파이프를 품은 셀(셸 파이프라인이 흔한 내용이다)이 한 칸씩 밀려,
  -- 다이제스트가 엉뚱한 열을 교훈이라 소개하고 중복 판정이 엉뚱한 셀을 읽었다(5라운드).
  -- 자리표시 바이트로 가렸다가 셀 단위로 복원한다.
  local masked = string.gsub(string.gsub(line, "^%s+", ""), "\\|", "\1")
  local cols = {}
  for c in string.gmatch(masked, "[^|]+") do
    c = string.gsub(c, "^%s*(.-)%s*$", "%1")
    cols[#cols + 1] = string.gsub(c, "\1", "\\|")
  end
  if #cols < 7 then return nil end
  if not string.match(cols[1], "^%d%d%d%d%-%d%d%-%d%d") then return nil end
  return cols
end

-- 원장 파싱: | 일시 | 사용자 | 분류 | 작업 | 접근 | 결과 | 교훈 |
-- 활성부만 읽는다 — 정정으로 밀려난 옛 주장이 새 기록을 중복으로 막으면 안 된다.
local function ledger_rows()
  local active = split_ledger(magi.read_file(SUMMARY))
  if active == "" then return {} end
  local rows = {}
  for line in string.gmatch(active, "[^\n]+") do
    local cols = ledger_cells(line)
    if cols then rows[#rows + 1] = cols end
  end
  return rows
end

-- 기존 교훈 다이제스트(최근 N행의 작업/접근/교훈 요약) — 사이드카가 "이미 기록된
-- 교훈"을 알고 재기록을 거부하거나(같으면 null) 정정을 지목하게 한다(replaces=행 번호).
-- (n)은 활성 표에서의 진짜 행 번호다 — replaces가 이 번호로 교체 대상을 찾는다.
local function lessons_digest()
  local rows = ledger_rows()
  if #rows == 0 then return "" end
  local out = {}
  for i = math.max(1, #rows - 9), #rows do
    local c = rows[i]
    out[#out + 1] = "- (" .. i .. ") " .. c[4] .. " / " .. c[5] .. " [" .. c[6] .. "] → " .. string.sub(c[7], 1, 120)
  end
  return "[기존 교훈] (같은 주제의 정정·뒤집기면 replaces에 그 (n)을 넣어라)\n" .. table.concat(out, "\n")
end

-- 결정론적 유사-중복 백스톱: LLM의 "동일하면 null" 규칙은 소프트해서 뚫린다(실측).
-- 새 교훈의 토큰 집합이 기존 어느 행의 교훈과 과반 이상 겹치면 중복으로 스킵한다.
local function tokenize(s)
  local set, n = {}, 0
  for w in string.gmatch(string.lower(tostring(s or "")), "[%w가-힣_%-=]+") do
    if #w >= 2 and not set[w] then set[w] = true n = n + 1 end
  end
  return set, n
end

local function lesson_is_duplicate(lesson_text)
  local a, na = tokenize(lesson_text)
  if na == 0 then return false end
  for _, c in ipairs(ledger_rows()) do
    local b, nb = tokenize(c[7])
    if nb > 0 then
      local inter = 0
      for w in pairs(a) do
        if b[w] then inter = inter + 1 end
      end
      if inter / math.min(na, nb) >= 0.6 then return true end
    end
  end
  return false
end

-- append_lesson lands one lesson row. replaces(행 번호)가 유효하면 그 행을 **제자리 교체**한다:
-- 옛 행은 삭제가 아니라 아카이브 섹션으로 내려간다(사유 표기) — 정정된 주장과 새 주장이
-- 나란히 남는 것도, 정정이 흔적 없이 사라지는 것도 원장의 실패이기 때문이다. 활성 표가
-- 상한을 넘으면 오래된 행부터 같은 곳으로 내려간다.
local function append_lesson(entry, replaces)
  local active, archive = split_ledger(magi.read_file(SUMMARY))
  local row = {
    sanitize_cell(entry.timestamp), sanitize_cell(entry.username),
    sanitize_cell(entry.category), sanitize_cell(entry.task),
    sanitize_cell(entry.approach), sanitize_cell(outcome_label(entry.outcome)),
    sanitize_cell(entry.lesson),
  }
  -- 디스크 중복 가드: 작업+접근+결과 시그니처가 활성 표에 이미 있으면 스킵.
  -- (정정 경로는 예외 — 정정은 옛 행과 겹치는 것이 정상이다.)
  local signature = row[4] .. " | " .. row[5] .. " | " .. row[6]
  if not replaces and string.find(active, signature, 1, true) then return false end

  -- 활성부를 줄 단위로 해체해 데이터 행 위치를 잡는다. 빈 줄을 **보존**하는 분해다 —
  -- "[^\n]+"는 빈 줄을 삼켜, 수제 산문의 문단 나눔과 헤딩 앞 공백을 첫 기록에 전부
  -- 뭉개버렸다. 행 판정은 원장 파싱(ledger_rows)과 같은 ledger_cells 하나다 — 여기서 다른
  -- 기준을 쓰면 다이제스트의 (n)과 번호가 어긋나 replaces가 옆 행을 아카이브한다.
  local lines, data_at = {}, {}
  local rest = active
  while rest ~= "" do
    local nl = string.find(rest, "\n", 1, true)
    local line
    if nl then
      line = string.sub(rest, 1, nl - 1)
      rest = string.sub(rest, nl + 1)
    else
      line = rest
      rest = ""
    end
    lines[#lines + 1] = line
    if ledger_cells(line) then data_at[#data_at + 1] = #lines end
  end
  while #lines > 0 and lines[#lines] == "" do table.remove(lines) end
  if #data_at == 0 and not string.find(active, LEDGER_HEADER, 1, true) then
    -- 표가 없으면 표 골격을 **덧붙인다** — 통째 교체는 사람이 손으로 써 둔 산문(표 없는
    -- SESSION_SUMMARY.md)을 첫 교훈 기록에 통째로 날려버렸다.
    if #lines > 0 then lines[#lines + 1] = "" end
    lines[#lines + 1] = LEDGER_TITLE
    lines[#lines + 1] = ""
    lines[#lines + 1] = LEDGER_HEADER
    lines[#lines + 1] = LEDGER_DIVIDER
  end

  local function to_archive(line, why)
    if archive == "" then
      archive = ARCHIVE_TITLE .. "\n" .. LEDGER_HEADER .. "\n" .. LEDGER_DIVIDER .. "\n"
    end
    archive = string.gsub(archive, "%s*$", "") .. "\n" .. line .. " ← " .. why .. "\n"
  end

  local anchored = false
  if replaces and data_at[replaces] then
    -- 내용 앵커: 번호는 사이드카 호출 **전**의 다이제스트에서 왔고, 느린 호출 동안 다른
    -- 턴이나 사람이 원장을 고치면 번호가 밀린다(위치 주소의 숙명). 대상 행이 새 교훈과
    -- 토큰을 실제로 공유할 때만 정정으로 믿고, 아니면 일반 추가로 강등한다 — 엉뚱한 행을
    -- 아카이브하는 것보다 중복 한 줄이 낫다.
    local target = lines[data_at[replaces]]
    local a = tokenize(sanitize_cell(entry.task) .. " " .. sanitize_cell(entry.lesson))
    local inter = 0
    for w in pairs(tokenize(target)) do
      if a[w] then inter = inter + 1 end
    end
    if inter >= 2 then
      to_archive(target, "정정됨(" .. row[1] .. ")")
      table.remove(lines, data_at[replaces])
      anchored = true
    end
  end
  -- 정정이 일반 추가로 강등되는 순간, 정정이라서 면제했던 중복 가드 **둘 다** 다시 선다:
  -- 이 행은 어느 중복 검사도 통과한 적이 없다. 시그니처(작업|접근|결과)는 사이드카가 접근을
  -- 패러프레이즈하면 못 잡으므로, 교훈 텍스트의 토큰 유사도 백스톱(lesson_is_duplicate)까지
  -- 함께 세운다 — 강등된 채 그냥 덧붙이면 옛 주장과 미정정 새 주장이 나란히 남는다.
  if replaces and not anchored
    and (string.find(active, signature, 1, true) or lesson_is_duplicate(entry.lesson)) then
    -- 무음이면 안 된다: 정정이 여기서 죽으면 낡은 주장이 원장의 현재로 남는데, 로그가 없으면
    -- 그 손실을 아무도 못 본다. 다음 검증 턴이 번호를 제대로 짚으면 정정은 여전히 착지한다.
    magi.log("engram: 정정 강등+중복으로 미기록(대상 행 불일치) — " .. string.sub(sanitize_cell(entry.lesson), 1, 80))
    return false
  end
  lines[#lines + 1] = "| " .. table.concat(row, " | ") .. " |"

  -- 상한: 오래된 데이터 행부터 아카이브로. (원장 행만 — 사람이 쓴 다른 표는 건드리지 않는다)
  local n = 0
  for _, l in ipairs(lines) do
    if ledger_cells(l) then n = n + 1 end
  end
  local i = 1
  while n > MAX_ACTIVE_ROWS and i <= #lines do
    if ledger_cells(lines[i]) then
      to_archive(lines[i], "오래됨(상한 " .. MAX_ACTIVE_ROWS .. "행)")
      table.remove(lines, i)
      n = n - 1
    else
      i = i + 1
    end
  end

  local out = table.concat(lines, "\n") .. "\n"
  if archive ~= "" then out = out .. "\n" .. string.gsub(archive, "%s+$", "\n") end
  magi.write_file(SUMMARY, out)
  return true
end

-- YAML 큰따옴표 스칼라 이스케이프: 백슬래시를 먼저(이중 이스케이프 방지), 그 다음
-- 따옴표. "\uXXXX" 같은 원문이 진짜 YAML 파서(다른 호스트)에서 유니코드 이스케이프로
-- 오해석되지 않게 한다.
local function yaml_quote(s)
  s = string.gsub(tostring(s or ""), "\\", "\\\\")
  s = string.gsub(s, '"', '\\"')
  s = string.gsub(s, "\r?\n", " ")
  return '"' .. s .. '"'
end

local function load_lastused()
  local m = {}
  for line in string.gmatch(magi.store_get("skill_lastused") or "", "[^\n]+") do
    local n, d = string.match(line, "^([^\t]*)\t(.*)$")
    if n then m[n] = d end
  end
  return m
end

local function save_lastused(m)
  local out = {}
  for n, d in pairs(m) do out[#out + 1] = n .. "\t" .. d end
  magi.store_set("skill_lastused", table.concat(out, "\n"))
end

-- 결정론적 유사-스킬 백스톱. 사이드카의 "같은 주제면 name 재사용" 규칙은 소프트해서 뚫리고,
-- 뚫릴 때마다 유사 스킬이 하나 늘었다(실측 — 난립의 주범). 새 이름의 설명이 기존 스킬 설명과
-- 토큰 과반 이상 겹치면 생성을 막는다(save_skill이 nil 반환 — 쌍둥이 이름을 돌려주면
-- 취소 창(undo)이 멀쩡한 기존 스킬을 지울 수 있어 일부러 안 돌려준다).
local function similar_skill(slug, desc)
  local a, na = tokenize(desc)
  if na < 3 then return nil end
  for line in string.gmatch(magi.store_get("skill_index") or "", "[^\n]+") do
    local name, d = string.match(line, "^([^\t]*)\t(.*)$")
    if name and name ~= slug then
      local b, nb = tokenize(d)
      if nb > 0 then
        local inter = 0
        for w in pairs(a) do
          if b[w] then inter = inter + 1 end
        end
        if inter / math.min(na, nb) >= 0.7 then return name end
      end
    end
  end
  return nil
end

local function skill_indexed(slug)
  for line in string.gmatch(magi.store_get("skill_index") or "", "[^\n]+") do
    if string.match(line, "^([^\t]*)\t") == slug then return true end
  end
  return false
end

local function save_skill(skill)
  local slug = slugify(skill.name)
  local desc = tostring(skill.description or skill.technique or "")
  if not skill_indexed(slug) then
    local twin = similar_skill(slug, desc)
    if twin then
      magi.log("engram: 유사 스킬 '" .. twin .. "' 존재 — '" .. slug .. "' 생성 스킵(난립 방지)")
      return nil
    end
  end
  local body = {
    "---",
    "name: " .. slug,
    "description: " .. yaml_quote(desc),
    "---",
    "",
    "# " .. tostring(skill.name),
    "",
  }
  local when = tostring(skill.trigger or "")
  if when ~= "" then
    body[#body + 1] = "이 스킬은 **" .. when .. "** 상황에 적용합니다."
    body[#body + 1] = ""
  end
  body[#body + 1] = "## 적용 방법"
  body[#body + 1] = tostring(skill.technique or "")
  body[#body + 1] = ""
  local verify = tostring(skill.verify or "")
  -- **산문은 스크립트가 아니다.** 앞 판본은 200자를 넘는 verify 를 내용을 안 보고 scripts/verify.sh 로
  -- 썼다. 실물 2026-09-05(PPT 컴패니언): 사이드카가 「read_notes 로 고지문을 확인하고 read_slide 로
  -- 실측한다」는 한국어 문장을 verify 에 담았고, 그것이 verify.sh 가 되어 스킬 본문이 「실행해 확인한다」고
  -- 적혔다. 다음 런의 모델은 bash 로 그 파일을 돌리려 했고(exit 127), 카운슬은 실행 증거를 요구했고,
  -- 기억은 「검증 스크립트 실행 증거 없음」을 교훈으로 굳혔다 — 세 런에 걸쳐 되풀이됐다.
  -- 스크립트로 쓰는 것은 셸로 보이는 것뿐이다: 한글이 없고, 첫 줄이 shebang 이거나 명령처럼 시작한다.
  local asScript = verify ~= "" and #verify > 200 and looksLikeShell(verify)
  if verify ~= "" then
    body[#body + 1] = "## 검증"
    if asScript then
      -- 긴 검증 스크립트는 부속 파일로 분리 — magi가 번들 리소스 매니페스트로
      -- 스킬 디렉토리 파일 목록을 본문에 노출하므로 상대 참조가 해석된다.
      magi.write_file(SKILLS_DIR .. "/" .. slug .. "/scripts/verify.sh", verify)
      body[#body + 1] = "scripts/verify.sh 를 실행해 확인한다(셸 스크립트)."
    else
      body[#body + 1] = verify
    end
    body[#body + 1] = ""
  end
  if not asScript then
    -- 머지 저장이 부속 스크립트에서 인라인(또는 무검증)으로 돌아오면, 이전 판의 verify.sh는
    -- 본문이 더는 가리키지 않는 고아다 — 지운다. 취소 스냅샷은 저장 전에 떠 있으므로
    -- undo가 되살린다.
    local vpath = SKILLS_DIR .. "/" .. slug .. "/scripts/verify.sh"
    if magi.read_file(vpath) then magi.remove_file(vpath) end
  end
  local avoid = tostring(skill.avoid or "")
  if avoid ~= "" then
    body[#body + 1] = "## 주의 (쓰면 안 되는 경우 / 알려진 함정)"
    body[#body + 1] = avoid
    body[#body + 1] = ""
  end
  body[#body + 1] = "<!-- engram: 작업 이력에서 자동 추출된 스킬 -->"
  magi.write_file(SKILLS_DIR .. "/" .. slug .. "/SKILL.md", table.concat(body, "\n"))
  index_skill(slug, desc)
  -- 생성 시각이 망각 시계의 시작점이다. last-used가 로드 시에만 찍히던 시절, 정리의 표적인
  -- "한 번도 안 쓰인 스킬"이 바로 그 이유로 날짜가 없어 영원히 스킵됐다 — 망각 기전이
  -- 존재하는데 작동하는 걸 아무도 못 본 이유가 이것이었다.
  local lastused = load_lastused()
  if not lastused[slug] then
    lastused[slug] = string.sub(magi.time(), 1, 10)
    save_lastused(lastused)
  end
  return slug
end

-- 캡처: 결과가 확정된 턴 → 사이드카 분석 → 교훈 기록 + 검증된 스킬 저장.
-- 이 핸들러는 호스트의 관찰 워커에서 돌므로 느린 사이드카가 턴을 막지 않는다.
local function analyze_and_record(sid, hint, user, used_csv)
  local r = recent[sid]
  if not r or #r < 2 then return end -- 직전 시도가 될 맥락 부족
  local input = render_window(r)
  if hint and hint ~= "" then input = input .. "\n\n" .. hint end
  local ld = lessons_digest()
  if ld ~= "" then input = input .. "\n\n" .. ld end
  -- Skills loaded THIS turn → mark them in the digest so the sidecar won't clone a skill the
  -- success actually came from using (the duplicate-skill loop).
  local used = nil
  if used_csv and used_csv ~= "" then
    used = {}
    for name in string.gmatch(used_csv, "[^,]+") do used[name] = true end
  end
  local digest = skills_digest(used)
  if digest ~= "" then input = input .. "\n\n" .. digest end
  local model = magi.store_get("sidecar_model") -- [plugins.engram] sidecar_model 오버라이드
  local raw, err = magi.analyze{ system = SIDECAR_PROMPT, text = input, model = model }
  if not raw then
    magi.log("engram: 사이드카 실패(무시): " .. tostring(err))
    return
  end
  local result = parse_cortex_json(raw)
  if not result then
    magi.log("engram: 사이드카 응답 파싱 실패(무시)")
    return
  end
  if not result.lesson and not result.skill then
    magi.log("engram: 기록할 교훈/스킬 없음(사소하거나 미확정)")
    return
  end

  local lesson = result.lesson
  if lesson and lesson.task and lesson.lesson then
    local outcome = tostring(lesson.outcome or "none")
    if outcome ~= "none" then
      local key = lesson_key(lesson.task, lesson.approach)
      local seen = recorded[sid]
      if not seen then seen = {} recorded[sid] = seen end
      if not seen[key] then
        seen[key] = true
        -- 정정(replaces)은 옛 행과 겹치는 것이 정상이라 유사-중복 백스톱을 타지 않는다 —
        -- 백스톱이 정정을 막으면 낡은 주장이 원장의 현재로 영영 남는다. 단 검증은 한다:
        -- 범위 밖이거나 정수가 아닌 번호는 정정이 아니라 오지정이고, 그대로 믿으면 두 중복
        -- 가드를 다 우회한 채 엉뚱한 행을 아카이브할 수 있다 → 일반 append 경로로 강등.
        local replaces = tonumber(lesson.replaces)
        if replaces then
          replaces = math.floor(replaces)
          if replaces < 1 or replaces > #ledger_rows() or replaces ~= tonumber(lesson.replaces) then
            replaces = nil
          end
        end
        if not replaces and lesson_is_duplicate(lesson.lesson) then
          magi.log("engram: 유사 교훈 이미 기록됨 — 스킵: " .. tostring(lesson.task))
        else
          local ok = append_lesson({
            timestamp = magi.time(),
            username = magi.store_get("username") or user or "user",
            category = tostring(lesson.category or "기타"),
            task = tostring(lesson.task), approach = tostring(lesson.approach or ""),
            outcome = outcome, lesson = tostring(lesson.lesson),
          }, replaces)
          if ok then
            magi.log("engram: 교훈 기록 — " .. tostring(lesson.task))
            -- D13 공유 경험 스토어에도 제안(리뷰 큐 → 팀 tier). 스토어가 없거나
            -- 실패해도 무시 — 원장이 1차 저장소다.
            local _, perr = magi.propose_experience{
              memories = { {
                text = "[" .. tostring(lesson.category or "기타") .. "] " .. tostring(lesson.task)
                  .. " — " .. tostring(lesson.approach or "") .. " → " .. outcome .. ": " .. tostring(lesson.lesson),
                tags = { "engram", tostring(lesson.category or "기타") },
              } },
            }
            if perr then magi.log("engram: D13 제안 실패(무시): " .. tostring(perr)) end
          end
        end
      end
    end
  end

  -- 검증된 성공의 스킬만 저장 (사이드카 프롬프트의 결정 규칙이 1차 게이트)
  local skill = result.skill
  if skill and skill.name and skill.technique then
    -- 되돌리기 스냅샷은 저장 **전**에 뜬다: 같은 name 재사용(머지·진화) 저장의 취소는
    -- "삭제"가 아니라 "이전 판 복원"이어야 한다 — 삭제는 이번 턴이 만든 것이 아니라
    -- 이전에 승인받고 사람이 다듬었을 수도 있는 스킬 전체를 지운다(4라운드 검수, 실측).
    local pslug = slugify(skill.name)
    local prev_body = magi.read_file(SKILLS_DIR .. "/" .. pslug .. "/SKILL.md") or ""
    local prev_verify = magi.read_file(SKILLS_DIR .. "/" .. pslug .. "/scripts/verify.sh") or ""
    local slug = save_skill(skill)
    -- nil = 유사-스킬 백스톱이 생성을 막았다. 여기서 그대로 이어가면 nil 연결(concat)이
    -- 핸들러 전체를 죽여 이 턴의 prune까지 건너뛴다 — 백스톱이 발동할 때마다.
    if not slug then return end
    magi.log("engram: 스킬 저장 — " .. slug)
    -- 세션 키와 함께: 취소 창이 스토어 전역이면, 다른 세션의 무관한 질문에 "아니요"라
    -- 답한 것이 이 스킬을 지웠다. 창은 저장을 목격한 그 대화의 것이다.
    -- 창 키는 세션별이다: 한 칸짜리 전역 창은 다른 세션의 저장이 이 세션의 창을 덮어,
    -- 방금 "N이라고 하면 되돌립니다"라고 들은 사용자의 N이 조용히 무시됐다.
    magi.store_set("last_skill:" .. sid, sid .. "\t" .. slug)
    magi.store_set("last_skill_prev:" .. sid, prev_body)
    magi.store_set("last_skill_prev_verify:" .. sid, prev_verify)
    magi.notify(sid, "engram: 검증된 성공에서 스킬 '" .. slug
      .. "' 을(를) 자동 저장했습니다. 잘못 저장됐으면 'N' 또는 '스킬 취소'라고 입력하면 되돌립니다.")
    local _, perr = magi.propose_experience{
      skills = { {
        name = slug,
        description = tostring(skill.description or ""),
        body = tostring(skill.technique or ""),
      } },
    }
    if perr then magi.log("engram: D13 스킬 제안 실패(무시): " .. tostring(perr)) end
  end
end

-- 짧은 부정 응답(자동 저장된 스킬 취소) 판정.
local DENIALS = {
  "^%s*[nN][oO]?%s*[.!]?%s*$", "^%s*아니요?%s*[.!]?%s*$", "^%s*아냐%s*[.!]?%s*$",
  "^%s*취소%s*[.!]?%s*$", "^%s*스킬%s*취소%s*[.!]?%s*$", "^%s*하지%s*마%s*[.!]?%s*$",
  "^%s*[pP][aA][sS][sS]%s*[.!]?%s*$", "^%s*[sS][kK][iI][pP]%s*[.!]?%s*$", "^%s*스킵%s*[.!]?%s*$",
}
local function is_denial(text)
  for _, pat in ipairs(DENIALS) do
    if string.match(tostring(text or ""), pat) then return true end
  end
  return false
end

-- 취소 창의 스냅샷 키 정리 — 창을 닫는 모든 경로(취소 실행·비부정 응답)가 같이 부른다.
local function clear_undo_window(sid)
  magi.store_set("last_skill:" .. sid, "")
  magi.store_set("last_skill_prev:" .. sid, "")
  magi.store_set("last_skill_prev_verify:" .. sid, "")
end

-- 마지막 자동 저장 스킬의 취소(undo): 저장 직후 "다음 사용자 메시지"까지가 취소 창.
-- 두 모드: 이번 턴이 스킬을 **만든** 것이면 삭제, 기존 스킬을 **덮어쓴** 것이면(머지 저장)
-- 저장 전 스냅샷으로 복원 — 삭제는 이전에 승인받은 스킬 전체를 날리는 데이터 손실이었다.
local function undo_last_skill(sid)
  local rec = magi.store_get("last_skill:" .. sid)
  if not rec or rec == "" then return false end
  local owner, slug = string.match(rec, "^([^\t]*)\t(.*)$")
  if not slug or slug == "" or owner ~= sid then return false end
  local prev = magi.store_get("last_skill_prev:" .. sid) or ""
  if prev ~= "" then
    magi.write_file(SKILLS_DIR .. "/" .. slug .. "/SKILL.md", prev)
    local vpath = SKILLS_DIR .. "/" .. slug .. "/scripts/verify.sh"
    local pv = magi.store_get("last_skill_prev_verify:" .. sid) or ""
    if pv ~= "" then
      magi.write_file(vpath, pv)
    elseif magi.read_file(vpath) then
      magi.remove_file(vpath) -- 이번 저장이 새로 만든 부속 — 이전 판엔 없었다
    end
    -- 인덱스 설명도 이전 판의 frontmatter에서 되살린다. yaml_quote 역파싱 대신 원문 그대로 —
    -- 인덱스는 유사도 토큰용이라 따옴표가 남아도 무해하다. (사용실적·망각시계는 안 건드린다:
    -- 이 스킬은 이번 턴 전부터 있었고 시계도 그때부터 돌고 있었다.)
    local pdesc = string.match(prev, "\ndescription:%s*([^\n]*)") or ""
    index_skill(slug, pdesc)
    clear_undo_window(sid)
    magi.notify(sid, "engram: 스킬 '" .. slug .. "' 을(를) 이번 턴 저장 전의 판으로 되돌렸습니다.")
    magi.log("engram: 스킬 되돌림(이전 판 복원) — " .. slug)
    return true
  end
  magi.remove_file(SKILLS_DIR .. "/" .. slug)
  -- 인덱스·사용실적·망각시계에서도 제거 — 시계를 남기면 같은 이름으로 재생성된 스킬이
  -- 취소된 전임자의 날짜를 물려받아 조기 아카이브된다.
  for _, key in ipairs({ "skill_index", "skill_usage", "skill_lastused" }) do
    local out = {}
    for line in string.gmatch(magi.store_get(key) or "", "[^\n]+") do
      if string.match(line, "^([^\t]*)\t") ~= slug then out[#out + 1] = line end
    end
    magi.store_set(key, table.concat(out, "\n"))
  end
  clear_undo_window(sid)
  magi.notify(sid, "engram: 스킬 '" .. slug .. "' 저장을 취소(삭제)했습니다.")
  magi.log("engram: 스킬 취소 — " .. slug)
  return true
end

magi.on("user_message", function(ev)
  -- 취소 창: 직전에 자동 저장된 스킬이 있으면, 이 메시지가 짧은 부정이면 되돌리고
  -- 아니면 창을 닫는다(one-shot — 다음-턴 취소 시맨틱).
  local last = magi.store_get("last_skill:" .. ev.session)
  if last and last ~= "" then
    local owner = string.match(last, "^([^\t]*)\t") or ""
    if owner == ev.session then
      if is_denial(ev.text) then
        undo_last_skill(ev.session)
      else
        clear_undo_window(ev.session)
      end
    end
  end
  push_recent(ev.session, "user", ev.text)
end)

-- 스킬 사용 실적 원장(플러그인 스토어): slug\t로드수\t성공수\t실패수.
-- 호스트가 turn_finished 페이로드에 실어주는 "이번 턴에 로드된 스킬" ×
-- 구조적 outcome으로 결정론적으로 계측한다 — 큐레이션(병합/정리)의 실측 근거.
-- Last-used dates (YYYY-MM-DD per skill slug) drive date-based pruning. Stored as a
-- newline-joined "slug\tdate" map alongside skill_usage; the load/save helpers live above
-- save_skill, which stamps a skill's creation date as the forgetting clock's start.

-- days_between returns the whole-day difference between two YYYY-MM-DD strings
-- (a - b) using a proleptic-Gregorian day count. Returns nil on a malformed date.
local function to_days(ymd)
  local y, m, d = string.match(ymd or "", "^(%d+)-(%d+)-(%d+)$")
  if not y then return nil end
  y, m, d = tonumber(y), tonumber(m), tonumber(d)
  -- days since a fixed epoch (Howard Hinnant's civil algorithm)
  y = (m <= 2) and (y - 1) or y
  local era = math.floor(((y >= 0) and y or (y - 399)) / 400)
  local yoe = y - era * 400
  local doy = math.floor((153 * ((m > 2) and (m - 3) or (m + 9)) + 2) / 5) + d - 1
  local doe = yoe * 365 + math.floor(yoe / 4) - math.floor(yoe / 100) + doy
  return era * 146097 + doe - 719468
end

-- PRUNE_DAYS default; overridable per-user with /engram-prune-days (stored).
local PRUNE_DAYS_DEFAULT = 7

local function prune_days()
  local v = tonumber(magi.store_get("prune_days") or "")
  if v and v > 0 then return math.floor(v) end
  return PRUNE_DAYS_DEFAULT
end

-- prune_stale archives engram-authored skills unused for >= prune_days days: it MOVES
-- .claude/skills/<slug>/SKILL.md to .claude/skills/.archive/<slug>/SKILL.md (read →
-- write → remove — recoverable, git-tracked) and drops the slug from the index/usage/
-- lastused. HUMAN-edited skills are protected: a skill whose SKILL.md no longer carries
-- the engram auto-generated marker is never archived. Pure count/date logic — no LLM.
local ENGRAM_MARKER = "engram: 작업 이력에서 자동 추출된 스킬"

local function prune_stale()
  local today = to_days(string.sub(magi.time(), 1, 10))
  if not today then return end
  local cutoff = prune_days()
  local lastused = load_lastused()
  local idx = magi.store_get("skill_index") or ""
  local archived = {}
  local dated = false
  for line in string.gmatch(idx, "[^\n]+") do
    local slug = string.match(line, "^([^\t]*)\t")
    if slug then
      local path = SKILLS_DIR .. "/" .. slug .. "/SKILL.md"
      local body = magi.read_file(path)
      if body and string.find(body, ENGRAM_MARKER, 1, true) then -- engram-owned only
        local ld = to_days(lastused[slug])
        if not ld then
          -- 시계 없는 스킬(이 수정 이전에 만들어졌거나 기록이 유실됨)은 스킵이 아니라
          -- **오늘부터 시계를 시작**한다. 스킵은 "한 번도 안 쓰인 스킬은 영원히 산다"였고,
          -- 그것은 정리가 정확히 잡으라던 대상이다.
          lastused[slug] = string.sub(magi.time(), 1, 10)
          dated = true
        elseif (today - ld) >= cutoff then
          magi.write_file(SKILLS_DIR .. "/.archive/" .. slug .. "/SKILL.md", body)
          -- 부속 스크립트도 함께 — 본문만 옮기면 verify.sh가 고아로 남는다.
          local verify = magi.read_file(SKILLS_DIR .. "/" .. slug .. "/scripts/verify.sh")
          if verify and verify ~= "" then
            magi.write_file(SKILLS_DIR .. "/.archive/" .. slug .. "/scripts/verify.sh", verify)
            magi.remove_file(SKILLS_DIR .. "/" .. slug .. "/scripts/verify.sh")
          end
          magi.remove_file(path)
          archived[slug] = true
        end
      end
    end
  end
  if dated then save_lastused(lastused) end
  if next(archived) == nil then return end
  -- Drop archived slugs from the index, usage, and lastused maps.
  local function filter_lines(key, slugof)
    local kept = {}
    for line in string.gmatch(magi.store_get(key) or "", "[^\n]+") do
      local s = slugof(line)
      if s and not archived[s] then kept[#kept + 1] = line end
    end
    magi.store_set(key, table.concat(kept, "\n"))
  end
  filter_lines("skill_index", function(l) return string.match(l, "^([^\t]*)\t") end)
  filter_lines("skill_usage", function(l) return string.match(l, "^([^\t]*)\t") end)
  filter_lines("skill_lastused", function(l) return string.match(l, "^([^\t]*)\t") end)
  local names = {}
  for s in pairs(archived) do names[#names + 1] = s end
  magi.log("engram: " .. #names .. "개 미사용 스킬 아카이브(>" .. cutoff .. "일): " .. table.concat(names, ", "))
end

local function update_usage(skills_csv, outcome)
  if not skills_csv or skills_csv == "" then return end
  local map, order = {}, {}
  for line in string.gmatch(magi.store_get("skill_usage") or "", "[^\n]+") do
    local n, l, o, b = string.match(line, "^([^\t]*)\t(%d+)\t(%d+)\t(%d+)$")
    if n then map[n] = { tonumber(l), tonumber(o), tonumber(b) } order[#order + 1] = n end
  end
  local today = string.sub(magi.time(), 1, 10) -- YYYY-MM-DD (UTC), for last-used pruning
  local lastused = load_lastused()
  for name in string.gmatch(skills_csv, "[^,]+") do
    local m = map[name]
    if not m then m = { 0, 0, 0 } map[name] = m order[#order + 1] = name end
    m[1] = m[1] + 1
    if outcome == "verified" then m[2] = m[2] + 1
    elseif outcome == "unverified" or outcome == "guard" or outcome == "error" then m[3] = m[3] + 1 end
    lastused[name] = today -- loaded this turn → refresh its last-used date
  end
  save_lastused(lastused)
  local out = {}
  for _, n in ipairs(order) do
    local m = map[n]
    out[#out + 1] = n .. "\t" .. m[1] .. "\t" .. m[2] .. "\t" .. m[3]
  end
  magi.store_set("skill_usage", table.concat(out, "\n"))
end

magi.on("turn_finished", function(ev)
  push_recent(ev.session, "assistant", ev.text)
  local outcome = ev.outcome or "done"
  update_usage(ev.skills, outcome)
  if ANALYZE_OUTCOMES[outcome] then
    local user = ev.user
    if user == "" then user = nil end
    analyze_and_record(ev.session, outcome_hint(outcome, ev.reason), user, ev.skills)
  end
  prune_stale() -- archive engram-owned skills unused past the cutoff (count/date only)
end)

-- /engram-prune-days [N] — show or set the unused-skill archive cutoff (days). Human-
-- edited skills are never archived regardless of the cutoff.
magi.register_command{
  name = "engram-prune-days",
  description = "미사용 스킬 아카이브 기준 일수 조회/설정 (예: /engram-prune-days 14)",
  execute = function(args)
    local n = tonumber((args or ""):match("%d+"))
    if n and n > 0 then
      magi.store_set("prune_days", tostring(math.floor(n)))
      return "engram: 미사용 스킬 아카이브 기준을 " .. math.floor(n) .. "일로 설정했습니다."
    end
    return "engram: 현재 아카이브 기준 " .. prune_days() .. "일. 변경하려면 `/engram-prune-days <일수>`."
  end,
}

-- 회상: 최근 교훈을 컨텍스트로 주입(원장이 없으면 침묵). 행/문자 상한으로 바운드.
magi.register_context_provider{
  name = "engram-lessons",
  provide = function(q)
    -- 활성부만: 아카이브(정정으로 밀려난 옛 주장·상한 초과분)는 주입되지 않는다.
    local raw = split_ledger(magi.read_file(SUMMARY))
    if not raw or raw == "" then return {} end
    -- 관련성 게이트: 현재 질의와 토큰이 겹치는 교훈 행만 주입한다. 무관한 행(예:
    -- 세션 첫 턴의 교훈)이 매 스텝 실리면 모델 추론이 그 옛 요청을 계속 되뇐다
    -- (실측 버그 리포트). 질의가 짧아 겹침 판단이 무의미하면 최근 행으로 폴백.
    local qset, qn = tokenize(q.prompt or "")
    local rows = {}
    for line in string.gmatch(raw, "[^\n]+") do
      local t = string.gsub(line, "^%s+", "")
      -- 행 판정은 원장의 단일 판정(ledger_cells)이다 — 4라운드가 사이드카 다이제스트에서
      -- 막은 사람 표가 리콜 주입으로는 그대로 새고 있었다(5라운드).
      if ledger_cells(line) then
        if qn >= 3 then
          -- 행 토큰이 질의 문자열의 부분문자열로 나타나는지로 겹침을 센다:
          -- 한국어 조사("포트가" vs "포트") 때문에 토큰 동등 비교는 오탐 필터링한다.
          local qtext = string.lower(q.prompt or "")
          local rset = tokenize(t)
          local inter = 0
          for w in pairs(rset) do
            if string.find(qtext, w, 1, true) then inter = inter + 1 end
          end
          if inter >= 2 then rows[#rows + 1] = t end
        else
          rows[#rows + 1] = t
        end
      end
    end
    if #rows == 0 then return {} end
    local from = math.max(1, #rows - MAX_LESSON_ROWS + 1)
    local body = table.concat(rows, "\n", from)
    if #body > MAX_LESSON_CHARS then
      body = string.sub(body, #body - MAX_LESSON_CHARS + 1)
      local nl = string.find(body, "\n", 1, true)
      if nl then body = string.sub(body, nl + 1) end
    end
    local block = table.concat({
      "[ENGRAM — 이 저장소에 축적된 과거 작업 교훈입니다.]",
      "- 현재 작업과 상황이 실제로 일치할 때만 참고하세요. 무관하면 답변·사고 과정에서 언급 자체를 하지 마세요(이 기록을 요약하거나 되뇌지 말 것).",
      "- 일치하는 교훈을 근거로 쓸 때는 누가·언제·어떤 결과(❌/✅)였는지를 함께 밝히세요.",
      "- 동일 상황에서 ❌실패로 기록된 접근은 피하세요.",
      "",
      LEDGER_HEADER, LEDGER_DIVIDER, body,
    }, "\n")
    return { { text = block, source = "engram" } }
  end,
}

magi.log("engram: 플러그인 로드 완료 (관찰 이벤트 + 사이드카 분석 + 회상)")
