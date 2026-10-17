#!/usr/bin/env python3
"""Update the optional, locally installed Codex plugin without replacing its settings.

Usage: python3 tools/update-codex-transport.py /path/to/plugins/codex/init.lua
The original file is backed up once per content digest. Running again is a no-op.
"""
import hashlib
import os
from pathlib import Path
import sys
import tempfile


def update(path: Path) -> bool:
    source = path.read_text(encoding='utf-8')
    marker = '-- Inlined into the optional local Codex plugin by update-codex-transport.py.'
    start = source.find(marker)
    if start < 0:
        start = source.find('local function liveOpen()')
    end = source.find('-- liveTurn answers', start)
    if start < 0 or end < 0:
        raise ValueError('Expected persistent-thread plugin layout was not found; file unchanged')
    transport = Path(__file__).with_name('codex-app-server-transport.lua').read_text(encoding='utf-8')
    updated = source[:start] + transport + source[end:]
    updated = updated.replace(
        'completion = (tonumber(u.output_tokens) or 0) + (tonumber(u.reasoning_output_tokens) or 0),',
        'completion = tonumber(u.output_tokens) or 0,')
    updated = updated.replace('live thread dropped (', 'app-server connection closed (')
    updated = updated.replace('); falling back to codex exec', ')')
    updated = updated.replace("' → codex exec'", "' → codex app-server (exec fallback before submission only)'")
    # Once a turn was submitted, a failed response must not silently replay the
    # whole request through exec (and pay for a second model run).
    call = '  local live = liveTurn(req, model, renderHead(req), blocks, volatileBlock, msgs)'
    if call not in updated:
        raise ValueError('Expected liveTurn caller was not found; file unchanged')
    if '  appTurnSubmitted = false\n' + call not in updated:
        updated = updated.replace(call, '  appTurnSubmitted = false\n' + call, 1)
    success = '  if live then return replyBody(live.text, live.usage) end'
    if success not in updated:
        raise ValueError('Expected liveTurn result handler was not found; file unchanged')
    if '  if appTurnSubmitted then' not in updated:
        updated = updated.replace(success, success + '\n  if appTurnSubmitted then\n' +
            '''    return { status = 409, body = '{"error":{"message":"Codex 요청이 전송된 뒤 정상 완료되지 않았습니다. 중복 실행을 피하려고 자동 재요청하지 않았습니다. 상태를 확인한 뒤 다시 시도해 주세요."}}' }\n''' +
            '  end', 1)
    # 409 is deliberately non-retryable by Magi's HTTP client; a 5xx here
    # would replay the already-submitted request at the next layer.
    if updated == source:
        return False
    digest = hashlib.sha256(source.encode()).hexdigest()[:12]
    backup = path.with_name(path.name + '.before-app-server-' + digest)
    if not backup.exists():
        backup.write_text(source, encoding='utf-8')
        backup.chmod(path.stat().st_mode & 0o777)
    with tempfile.NamedTemporaryFile(mode='w', encoding='utf-8', dir=path.parent, delete=False) as temp:
        staged = Path(temp.name)
        try:
            temp.write(updated)
            temp.flush()
            os.fsync(temp.fileno())
            staged.chmod(path.stat().st_mode & 0o777)
            os.replace(staged, path)
        finally:
            staged.unlink(missing_ok=True)
    return True


if __name__ == '__main__':
    if len(sys.argv) != 2:
        raise SystemExit(__doc__)
    try:
        print('updated' if update(Path(sys.argv[1])) else 'already current')
    except (OSError, ValueError) as error:
        raise SystemExit(str(error))
