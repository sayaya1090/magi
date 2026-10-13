export function documentConnectionNote(requested, documents = []) {
  const rows = Array.isArray(documents) ? documents : [];
  const matches = requested && rows.some(row => row.document === 'pid-' + requested);
  if (matches) return '이 문서의 편집 연결은 있습니다. 작업창을 닫았다 다시 열어 연결을 갱신해 주세요.';
  const reason = !requested
    ? '이 창의 문서 ID를 확인하지 못했습니다. 미저장 파일이라면 한 번 저장한 뒤 작업창을 다시 열어 주세요.'
    : rows.length
      ? '이 창과 편집 연결의 문서 ID가 다릅니다. 다른 문서로 보내지 않고 전송을 중단했습니다.'
      : '이 문서의 편집 연결이 아직 없습니다. 편집 연결 프로그램이 실행 중인지 확인해 주세요.';
  return reason + '\n연결 진단: 요청=' + (requested || '없음') + '; 등록=' +
    (rows.map(row => String(row.document || '').slice(0,128) + ' [문서=' + String(row.label || '없음').slice(0,80) +
      '; COM 식별버전=' + String(row.identityRevision || '미보고').slice(0,40) + ']').join(', ') || '없음') + '; 작업창 식별버전=shared-tag-v2';
}
