const en = {
  nodeRemove:'Remove diagram element', nodeAbout:'About these diagram elements:', nodeExplain:'Explain these diagram elements:',
  retryWait: 'Retrying in {seconds}s · attempt {attempt}', reconnecting: 'Reconnecting…', ready: 'Ready', stop: 'Stop', retryNetwork: 'Connection interrupted', retryRate: 'Provider rate limit', retryBusy: 'Provider temporarily unavailable',
  sessionChanged: 'Conversation changed before sending. Your message was saved as a draft.', closeRecovery: 'Close', resumeTask: 'Resume task', resumeReady: 'The interrupted task is ready to resume.',
  recoveryFailed: 'Recovery could not settle this conversation. Retry before starting new work.',
  recoveryRetry: 'Retry recovery', checkCalls: 'Check uncertain operations before resuming',
  checkTip: 'Inspect the command or file and its saved result before choosing. A verdict records your decision; it does not execute anything.',
  verdictRan: 'Already ran', verdictNotRan: 'Did not run', verdictRerun: 'Allow rerun', verdictDeferred: 'Decide later',
  task: 'Task', lastCompleted: 'Last completed operation', nextStep: 'Next: continue from saved progress', inspect: 'Show saved operation',
  latest: 'Jump to latest', loadingSession: 'Loading conversation…', reconnected: 'Connection restored',

  connected: 'Daemon connected', working: 'Daemon connected · Working', waiting: 'Waiting for your response',
  disconnected: 'Daemon disconnected', unknown: 'Connection status unavailable', conversation: 'Chat',
  session: 'Current chat', newSession: 'New chat', provider: 'Provider', defaultProvider: 'Default provider', model: 'Model',
  permission: 'Approval mode', permissions: { ask: 'Ask before changes', auto: 'Allow file edits; ask for commands', allow: 'Allow all', deny: 'Deny all' },
  council: 'Council', choose: 'Select…', applying: 'Applying…', unavailable: 'Unavailable', agentSettings: 'Agent settings…', connectionSettings: 'Connection settings…',
  approvals: 'Pending approvals and questions', plan: 'Plan', jobs: 'Background work', handoffWaiting: 'Awaiting response', handed: 'Delegated work', scheduled: 'Scheduled work', editorTools: 'Editor tools', fleet: 'Companions',
  estimated: 'Shares, estimated', context: 'Context', compact: 'Compact context', compacting: 'Requesting compaction…', noUsage: 'Usage unavailable', unsupported: 'Not supported by this companion', empty: 'None',
  confirm: 'Change', changeFailed: 'Could not apply the change.',
  councilEnableConfirm: 'Enable council for subsequent requests? Changing instructions may rebuild part of the context cache.',
  councilDisableConfirm: 'Disable council for subsequent requests? Changing instructions may rebuild part of the context cache.',
  collapse: 'Collapse', collapseArgs: 'Collapse arguments', expandArgs: 'Expand all arguments',
  companionInfo: 'Show companion information and settings', copy: 'Copy', openOutput: 'Open in editor (read-only)',
};
const ko: typeof en = {
  nodeRemove:'다이어그램 요소 제거', nodeAbout:'이 다이어그램 요소에 대해:', nodeExplain:'이 다이어그램 요소를 설명해 주세요:',
  retryWait: '{seconds}초 후 재시도 · {attempt}회차', reconnecting: '재연결 중…', ready: '준비됨', stop: '중단', retryNetwork: '연결이 중단됨', retryRate: '제공자 요청 한도', retryBusy: '제공자 일시적 오류',
  sessionChanged: '전송 전에 대화가 바뀌었습니다. 메시지는 초안으로 보관했습니다.', closeRecovery: '닫기', resumeTask: '작업 재개', resumeReady: '중단된 작업을 이어갈 수 있습니다.',
  recoveryFailed: '대화 복구를 마치지 못했습니다. 새 작업 전에 복구를 다시 시도해 주세요.',
  recoveryRetry: '복구 다시 시도', checkCalls: '재개 전에 실행 여부가 불확실한 작업을 확인하세요',
  checkTip: '명령·파일과 저장된 결과를 확인한 뒤 판단해 주세요. 판정은 기록만 하며 작업을 실행하지 않습니다.',
  verdictRan: '실행됨', verdictNotRan: '실행 안 됨', verdictRerun: '다시 실행 허용', verdictDeferred: '나중에 판단',
  task: '작업', lastCompleted: '마지막 완료 작업', nextStep: '다음: 저장된 진행 상태에서 계속', inspect: '저장된 작업 보기',
  latest: '최신 위치로', loadingSession: '대화 불러오는 중…', reconnected: '연결 복구됨',

  connected: '데몬 연결됨', working: '데몬 연결됨 · 작업 중', waiting: '사용자 응답 대기',
  disconnected: '데몬 연결 끊김', unknown: '연결 확인 불가', conversation: '대화',
  session: '현재 대화', newSession: '새 대화', provider: '프로바이더', defaultProvider: '기본 프로바이더', model: '모델',
  permission: '승인 방식', permissions: { ask: '변경 전 확인', auto: '파일 편집 허용 · 명령은 확인', allow: '모두 허용', deny: '모두 거절' },
  council: '카운슬', choose: '선택…', applying: '적용 중…', unavailable: '목록을 불러올 수 없음', agentSettings: '에이전트 설정…', connectionSettings: '연결 설정…',
  approvals: '승인·질문 대기', plan: '계획', jobs: '백그라운드 작업', handoffWaiting: '응답 대기', handed: '위임한 작업', scheduled: '예약 작업', editorTools: '편집기 도구', fleet: '컴패니언',
  estimated: '구성 비율 추정치', context: '컨텍스트', compact: '컨텍스트 압축', compacting: '압축 요청 중…', noUsage: '사용량 정보 없음', unsupported: '이 컴패니언에서 지원하지 않음', empty: '없음',
  confirm: '변경', changeFailed: '변경을 적용하지 못했습니다.',
  councilEnableConfirm: '카운슬을 켜시겠습니까? 다음 요청부터 적용되며, 지침 변경으로 컨텍스트 캐시 일부가 다시 생성될 수 있습니다.',
  councilDisableConfirm: '카운슬을 끄시겠습니까? 다음 요청부터 적용되며, 지침 변경으로 컨텍스트 캐시 일부가 다시 생성될 수 있습니다.',
  collapse: '접기', collapseArgs: '인자 접기', expandArgs: '인자 전체 펼치기',
  companionInfo: '컴패니언 정보 및 설정 보기', copy: '복사', openOutput: '편집창에서 열기 (읽기 전용)',
};

/** VS Code language, with English fallback for languages without a translation. */
export function panelText(language?: string): typeof en {
  return language?.toLowerCase().startsWith('ko') ? ko : en;
}
