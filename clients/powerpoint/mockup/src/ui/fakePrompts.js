// 데몬이 묻는 상황을 손으로 만드는 자리. **목업 전용이고 애드인에는 안 들어간다.**
//
// 있는 이유는 하나다 — 이 머신에 데몬 문이 없어서 물음을 만들 곳이 필요하다. 그리고 이 칸이
// 없으면 §5.7이 세운 물음 창은 **아무도 못 본 채로** 착지한다. 못 본 화면은 안 만든 화면이다.

const CASES = [
  {
    label: '권한 물음',
    ask: {
      id: 'call_perm_1', kind: 'permission', what: 'bash',
      args: { command: 'rm -rf build/ && make', cwd: '/Users/me/deck' },
      reason: '허용 규칙에 없는 명령입니다',
    },
  },
  {
    label: '질문 (근거 있음)',
    ask: {
      id: 'call_ask_1#1', kind: 'question',
      what: '표지의 부제를 어느 쪽으로 맞출까요?',
      options: ['왼쪽 정렬', '가운데 정렬'],
      report: [
        { key: 'tried', text: '2·5·9쪽 부제는 왼쪽, 표지만 가운데입니다.' },
        { key: 'costs', text: '왼쪽으로 맞추면 표지가 본문과 같아지고, 가운데로 두면 표지만 다릅니다.' },
        { key: 'leaning', text: '왼쪽으로 기웁니다 — 어긋난 쪽이 하나뿐이라.' },
      ],
      index: 1, total: 2,
    },
  },
  {
    // 코어의 `Waiting.Event` 가 `default:` 로 권한 물음을 만들어 내는 그 자리(§5.7).
    label: '모르는 종류',
    ask: { id: 'call_x_1', kind: 'confirm', what: '무언가를 확인해 주십시오' },
  },
];

export function mountFakePrompts(status, root, { stream, readTranscript, sessionId } = {}) {
  const box = document.createElement('div');
  box.className = 'fake-prompts';

  const title = document.createElement('span');
  title.textContent = '데몬 흉내:';
  box.append(title);

  for (const c of CASES) {
    const b = document.createElement('button');
    b.textContent = c.label;
    b.addEventListener('click', () => status.ask(c.ask));
    box.append(b);
  }

  const clear = document.createElement('button');
  clear.textContent = '남이 답함';
  clear.title = '물음만 내려간다 — 이 창은 무엇으로 답했는지 모른다';
  clear.addEventListener('click', () => status.clear());
  box.append(clear);

  const cut = document.createElement('button');
  cut.textContent = '문 끊기 / 잇기';
  cut.addEventListener('click', () => { status.reachable = !status.reachable; });
  box.append(cut);

  // **연결이 둘이라는 것을 손으로 겪게 하는 자리.** 요청 쪽은 멀쩡한데 전사 스트림만 죽는
  // 경우가 진짜로 있고(§5.7), 그때 화면이 어떻게 되는지는 눌러 봐야 안다.
  if (stream && readTranscript) {
    const drop = document.createElement('button');
    drop.textContent = '스트림 끊기';
    drop.addEventListener('click', () => stream.drop());
    const back = document.createElement('button');
    back.textContent = '스트림 잇기';
    back.addEventListener('click', () => readTranscript.attach(sessionId));
    box.append(drop, back);

    // 두 화면은 **프레임이 있어야만 뜬다.** 눌러 볼 자리가 없으면 CSS 한 줄이 틀려도 아무도
    // 모른 채 착지한다 — 목업이 있는 이유가 바로 그것이다.
    const unver = document.createElement('button');
    unver.textContent = '검증 못 한 끝';
    unver.title = '`TurnFinishedData.Unverified` — 「고쳤다」와 「고쳤다는데 아무도 못 봤다」';
    unver.addEventListener('click', () => stream.push({ type: 'turn.finished',
      data: { unverified: true, reason: '독립 실행으로 확인된 것이 없습니다' } }));

    const stray = document.createElement('button');
    stray.textContent = '남의 서버 안내';
    stray.title = 'MCP 서버 이름이 설정값이라, 다르면 포스트잇이 한 장도 안 붙는다';
    stray.addEventListener('click', () => stream.push({ type: 'part.appended',
      data: { messageId: 'stray', part: { kind: 'tool-call', toolCall: {
        name: 'mcp__powerpoint__advise', callId: 'c-stray',
        args: { items: [{ message: '이건 안 붙어야 한다', slideId: 's4f2a1' }] } } } } }));

    box.append(unver, stray);
  }

  root.append(box);
}
