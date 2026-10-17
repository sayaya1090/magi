const en = {
  connected: 'Daemon connected', working: 'Daemon connected · Working', waiting: 'Waiting for your response',
  disconnected: 'Daemon disconnected', unknown: 'Connection status unavailable', conversation: 'Chat',
  session: 'Current chat', newSession: 'New chat', provider: 'Provider', defaultProvider: 'Default provider', model: 'Model',
  permission: 'Approval mode', permissions: { ask: 'Ask before changes', auto: 'Allow file edits; ask for commands', allow: 'Allow all', deny: 'Deny all' },
  council: 'Council', choose: 'Select…', applying: 'Applying…', unavailable: 'Unavailable', agentSettings: 'Agent settings…', connectionSettings: 'Connection settings…',
  plan: 'Plan', jobs: 'Background work', handed: 'Delegated work', scheduled: 'Scheduled work', editorTools: 'Editor tools', fleet: 'Companions',
  estimated: 'Shares, estimated', context: 'Context', compact: 'Compact context', compacting: 'Requesting compaction…', noUsage: 'Usage unavailable', unsupported: 'Not supported by this companion', empty: 'None',
  confirm: 'Change', changeFailed: 'Could not apply the change.',
  councilEnableConfirm: 'Enable council for subsequent requests? Changing instructions may rebuild part of the context cache.',
  councilDisableConfirm: 'Disable council for subsequent requests? Changing instructions may rebuild part of the context cache.',
  companionInfo: 'Show companion information and settings', copy: 'Copy', openOutput: 'Open in editor (read-only)',
};
const ko: typeof en = {
  connected: '데몬 연결됨', working: '데몬 연결됨 · 작업 중', waiting: '사용자 응답 대기',
  disconnected: '데몬 연결 끊김', unknown: '연결 확인 불가', conversation: '대화',
  session: '현재 대화', newSession: '새 대화', provider: '프로바이더', defaultProvider: '기본 프로바이더', model: '모델',
  permission: '승인 방식', permissions: { ask: '변경 전 확인', auto: '파일 편집 허용 · 명령은 확인', allow: '모두 허용', deny: '모두 거절' },
  council: '카운슬', choose: '선택…', applying: '적용 중…', unavailable: '목록을 불러올 수 없음', agentSettings: '에이전트 설정…', connectionSettings: '연결 설정…',
  plan: '계획', jobs: '백그라운드 작업', handed: '위임한 작업', scheduled: '예약 작업', editorTools: '편집기 도구', fleet: '컴패니언',
  estimated: '구성 비율 추정치', context: '컨텍스트', compact: '컨텍스트 압축', compacting: '압축 요청 중…', noUsage: '사용량 정보 없음', unsupported: '이 컴패니언에서 지원하지 않음', empty: '없음',
  confirm: '변경', changeFailed: '변경을 적용하지 못했습니다.',
  councilEnableConfirm: '카운슬을 켜시겠습니까? 다음 요청부터 적용되며, 지침 변경으로 컨텍스트 캐시 일부가 다시 생성될 수 있습니다.',
  councilDisableConfirm: '카운슬을 끄시겠습니까? 다음 요청부터 적용되며, 지침 변경으로 컨텍스트 캐시 일부가 다시 생성될 수 있습니다.',
  companionInfo: '컴패니언 정보 및 설정 보기', copy: '복사', openOutput: '편집창에서 열기 (읽기 전용)',
};

/** VS Code language, with English fallback for languages without a translation. */
export function panelText(language?: string): typeof en {
  return language?.toLowerCase().startsWith('ko') ? ko : en;
}
