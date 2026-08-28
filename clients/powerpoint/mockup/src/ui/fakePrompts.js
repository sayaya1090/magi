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

export function mountFakePrompts(status, root) {
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

  root.append(box);
}
