#!/usr/bin/env python3
"""Deterministic Codex app-server v2 peer; never contacts a model."""
import json
import sys

if '--version' in sys.argv:
    print('codex-cli fake')
    raise SystemExit(0)
if 'debug' in sys.argv:
    print('{"models":[{"slug":"test","context_window":100000}]}')
    raise SystemExit(0)
if 'exec' in sys.argv:
    print('unexpected exec fallback', file=sys.stderr)
    raise SystemExit(1)

thread_count = 0
turn_count = 0
sessions = {}

def emit(value):
    print(json.dumps(value), flush=True)

def notify(method, params):
    emit({'method': method, 'params': params})

for line in sys.stdin:
    request = json.loads(line)
    method = request.get('method')
    params = request.get('params', {})
    request_id = request.get('id')
    if method == 'initialize':
        emit({'id': request_id, 'result': {'userAgent': 'fake-codex'}})
    elif method == 'thread/start':
        assert params['sandbox'] == 'read-only'
        assert params['approvalPolicy'] == 'never'
        thread_count += 1
        thread = 'thread-' + str(thread_count)
        sessions[thread] = 0
        emit({'id': request_id, 'result': {'thread': {'id': thread}}})
    elif method == 'turn/start':
        thread = params['threadId']
        assert thread in sessions
        sessions[thread] += 1
        turn_count += 1
        turn = 'turn-' + str(turn_count)
        text = params['input'][0]['text']
        base = {'threadId': thread, 'turnId': turn}
        n = sessions[thread]
        total = {'inputTokens': n * 1000, 'cachedInputTokens': max(0, (n-1) * 900),
                 'outputTokens': n * 10, 'reasoningOutputTokens': n * 3}
        # Duplicate cumulative notifications must not inflate usage.
        for _ in range(2):
            notify('thread/tokenUsage/updated', dict(base, tokenUsage={'total': total, 'last': total}))
        # Notifications can arrive before the turn/start response.
        emit({'id': request_id, 'result': {'turn': {'id': turn}}})
        notify('item/completed', dict(base, item={'type': 'agentMessage',
               'text': thread + '/' + turn + ':' + text}))
        notify('turn/completed', {'threadId': thread, 'turn': {'id': turn,
               'status': 'failed' if 'FAIL' in text else 'completed'}})
