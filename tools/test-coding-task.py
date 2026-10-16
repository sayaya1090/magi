#!/usr/bin/env python3
"""Run a real local-model coding task in disposable directories.

Usage: python3 tools/test-coding-task.py --binary /path/to/magi --model MODEL
The model endpoint defaults to http://localhost:11434/v1. No existing project,
configuration, session or repository is modified. Evidence remains in --output.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--binary', required=True)
parser.add_argument('--model', required=True)
parser.add_argument('--base-url', default='http://localhost:11434/v1')
parser.add_argument('--output', default=None)
parser.add_argument('--timeout', default='15m')
args = parser.parse_args()
binary = str(Path(args.binary).resolve())
root = Path(args.output) if args.output else Path(tempfile.mkdtemp(prefix='magi-task-qa-'))
root = root.resolve()
root.mkdir(parents=True, exist_ok=True)
source, config = root / 'source', root / 'config'
# Refuse reused QA directories rather than overwriting evidence.
source.mkdir()
config.mkdir()
before = 'def add(a, b):\n    return a - b\n'
(source / 'calculator.py').write_text(before)
(source / 'test_calculator.py').write_text(
    'import unittest\nfrom calculator import add\nclass Tests(unittest.TestCase):\n'
    '    def test_add(self):\n        self.assertEqual(add(2, 3), 5)\n'
    '        self.assertEqual(add(-2, 3), 1)\n')
(config / 'config.toml').write_text('[council]\nenabled = false\n')
common = ['--source', str(source), '--store', str(root / 'tasks')]


def task(operation, *options, expect=0):
    result = subprocess.run([binary, 'coding-task', operation] + common + list(options),
                            capture_output=True, text=True)
    assert result.returncode == expect, result.stderr
    return json.loads(result.stdout)


record = task('prepare', '--mode', 'copy', '--request', 'real-model-qa')
(root / 'record.json').write_text(json.dumps(record, indent=2))
task_id = record['taskId']
env = os.environ.copy()
env.update(MAGI_CONFIG_DIR=str(config), MAGI_DATA_DIR=str(root / 'data'),
           MAGI_BASE_URL=args.base_url, MAGI_MODEL=args.model,
           MAGI_PERMISSION='allow', MAGI_PROFILE='standard', MAGI_WORKFLOW='false')
print('Evidence directory:', root, flush=True)
with (root / 'run.log').open('w') as log:
    subprocess.run(
        [binary, 'coding-task', 'run'] + common + ['--id', task_id,
         '--timeout', args.timeout, '--prompt',
         'Fix add in calculator.py to add its arguments. Run python3 -m unittest -v '
         'and report its result. Work only in the current isolated workspace. '
         'Do not create another workspace or use git.'],
        env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
assert (source / 'calculator.py').read_text() == before, 'source changed during run'
review = task('review', '--id', task_id, '--path', 'calculator.py')
assert review['beforeText'] == before
assert 'a + b' in review['afterText']
assert task('apply', '--id', task_id, 'calculator.py')['state'] == 'applied'
result = subprocess.run(['python3', '-m', 'unittest', '-v'], cwd=source,
                        capture_output=True, text=True)
(root / 'source-test.log').write_text(result.stdout + result.stderr)
assert result.returncode == 0, 'applied code failed tests'
user_edit = '# later user edit\ndef add(a, b):\n    return a + b + 1\n'
(source / 'calculator.py').write_text(user_edit)
assert task('restore', '--id', task_id, expect=1)['state'] == 'partial'
assert (source / 'calculator.py').read_text() == user_edit
# This explicit choice is confined to the disposable test source.
(source / 'calculator.py').write_text(review['afterText'])
assert task('restore', '--id', task_id)['state'] == 'restored'
assert (source / 'calculator.py').read_text() == before
result = {'taskId': task_id, 'model': args.model,
          'sourceUnchangedDuringRun': True, 'sourceTestsAfterApply': 'passed',
          'userEditPreservedOnRestore': True, 'baselineRestore': 'passed'}
(root / 'acceptance.json').write_text(json.dumps(result, indent=2))
print(json.dumps(result, indent=2))
