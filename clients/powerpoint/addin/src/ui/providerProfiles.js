/** Saved profiles are global; secrets are write-only and cleared after every request. */
export function mountProviderProfiles({ api, root = document, onSaved = () => {} } = {}) {
  const advanced = root.querySelector('#advanced');
  if (!advanced || !api) return;
  const button = root.createElement('button');
  button.type = 'button'; button.className = 'ghost'; button.id = 'manage-providers'; button.textContent = '프로바이더 관리';
  const picker = root.querySelector('#pick-model');
  if (picker) picker.insertBefore(button, picker.querySelector('#model-note'));
  else advanced.append(button);
  const dialog = root.createElement('dialog');
  dialog.id = 'provider-profiles-dialog'; dialog.className = 'provider-profiles-dialog';
  dialog.setAttribute('aria-labelledby', 'provider-profiles-title');
  dialog.innerHTML = `<h2 id="provider-profiles-title">프로바이더 관리</h2>
    <p>이 컴퓨터의 모든 Magi 클라이언트가 공유하는 설정입니다. 현재 대화에서 선택한 프로바이더를 수정하면 저장 후 다음 요청부터 반영됩니다. 저장한 프로바이더는 설정의 프로바이더 목록에서 선택할 수 있습니다.</p>
    <label>저장된 프로바이더<select id="profile-list"><option value="">새 프로바이더</option></select></label>
    <form id="profile-form" autocomplete="off" autocapitalize="off" autocorrect="off" spellcheck="false">
    <label>이름<input id="profile-name" required pattern="[A-Za-z0-9_-]+" maxlength="100" autocomplete="off" autocapitalize="off" autocorrect="off" spellcheck="false"><small>영문·숫자·밑줄·하이픈으로 입력하세요.</small></label>
    <label>엔드포인트<input id="profile-endpoint" type="url" required placeholder="https://api.example.com/v1" autocomplete="off" autocapitalize="off" autocorrect="off" spellcheck="false"></label>
    <label>기본 모델<input id="profile-model" required autocomplete="off" autocapitalize="off" autocorrect="off" spellcheck="false"></label>
    <label>API 키<input id="profile-key" type="password" autocomplete="new-password" autocapitalize="off" autocorrect="off" spellcheck="false"><small id="profile-key-note">빈칸으로 두면 저장된 키를 유지합니다.</small></label>
    <label class="profile-clear"><input id="profile-clear-key" type="checkbox">저장된 API 키 삭제</label>
    <div class="profile-actions"><button type="submit" id="profile-save" class="primary">저장</button><button type="button" id="profile-delete" class="ghost" disabled>삭제</button><button type="button" id="profile-close" class="ghost">닫기</button></div>
    </form><p id="profile-status" role="status" aria-live="polite"></p>`;
  root.body.append(dialog);
  const el = id => dialog.querySelector('#' + id);
  let profiles = []; let busy = false;
  const status = text => { el('profile-status').textContent = text; };
  const clearSecret = () => { el('profile-key').value = ''; el('profile-clear-key').checked = false; };
  const select = () => {
    const p = profiles.find(p => p.name === el('profile-list').value);
    el('profile-name').value = p?.name ?? ''; el('profile-name').readOnly = Boolean(p);
    el('profile-endpoint').value = p?.baseUrl ?? ''; el('profile-model').value = p?.model ?? '';
    clearSecret(); el('profile-delete').disabled = !p;
    el('profile-key-note').textContent = p?.hasKey ? '키가 저장되어 있습니다. 빈칸으로 두면 유지합니다.' : '키가 저장되어 있지 않습니다.';
  };
  const fill = (out, selected = '') => {
    profiles = out?.profiles ?? [];
    el('profile-list').replaceChildren(new Option('새 프로바이더', ''));
    for (const p of profiles) el('profile-list').append(new Option(p.name, p.name));
    el('profile-list').value = selected; select();
  };
  const lock = value => {
    busy = value;
    for (const item of dialog.querySelectorAll('input,select,button')) item.disabled = value;
    if (!value) { el('profile-delete').disabled = !el('profile-list').value; }
    el('profile-form').setAttribute('aria-busy', String(value));
  };
  button.addEventListener('click', async () => {
    dialog.showModal(); status('설정을 불러오는 중입니다…'); lock(true);
    try { fill(await api.providerProfiles()); status(''); }
    catch { status('설정을 불러오지 못했습니다. 헬퍼 연결을 확인하고 다시 열어 주세요.'); }
    finally { lock(false); }
  });
  el('profile-list').addEventListener('change', select);
  el('profile-close').addEventListener('click', () => { clearSecret(); dialog.close(); });
  dialog.addEventListener('cancel', event => { if (busy) event.preventDefault(); else clearSecret(); });
  dialog.addEventListener('close', () => { clearSecret(); button.focus(); });
  el('profile-clear-key').addEventListener('change', () => { el('profile-key').disabled = el('profile-clear-key').checked; if (el('profile-clear-key').checked) el('profile-key').value = ''; });
  const change = async op => {
    if (busy) return;
    const name = el('profile-name').value.trim();
    if (op === 'delete' && !globalThis.confirm(`프로바이더 “${name}”를 삭제할까요? 다른 클라이언트에서도 사용할 수 없게 됩니다.`)) return;
    const body = { op, name, baseUrl: el('profile-endpoint').value.trim(), model: el('profile-model').value.trim(), apiKey: el('profile-key').value, clearKey: el('profile-clear-key').checked };
    lock(true); status('저장하는 중입니다…');
    try { const out = await api.setProviderProfile(body); fill(out, op === 'delete' ? '' : name); status(op === 'delete' ? '삭제했습니다. 이미 적용한 대화는 유지됩니다.' : '저장했습니다. 프로바이더 목록에서 선택할 수 있습니다.');
      if (op === 'save') dialog.close();
      try { await onSaved(out); } catch { status('설정은 저장했습니다. 목록을 새로 불러오려면 설정 화면을 다시 열어 주세요.'); } }
    catch { status('저장하지 못했습니다. 입력값과 헬퍼 연결을 확인해 주세요.'); }
    finally { body.apiKey = ''; clearSecret(); lock(false); }
  };
  el('profile-form').addEventListener('submit', event => { event.preventDefault(); void change('save'); });
  el('profile-delete').addEventListener('click', () => void change('delete'));
}
