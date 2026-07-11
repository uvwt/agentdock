#!/usr/bin/env python3
import json, os, re, shutil, subprocess, sys
from pathlib import Path

ACTION = sys.argv[1] if len(sys.argv) > 1 else 'status'
SKILL = os.environ.get('SKILL_NAME', 'baidu-netdisk')
HOME = Path.home()
WORKSPACE = Path(os.environ.get('AGENTDOCK_DEFAULT_DIR') or HOME / 'AgentDock').resolve()
ENV = os.environ.copy()
ENV['HOME'] = str(HOME)
ENV['PATH'] = f"{HOME}/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

def load_args():
    raw = os.environ.get('SKILL_ARGS_JSON') or '{}'
    data = json.loads(raw)
    if not isinstance(data, dict):
        raise ValueError('args must be object')
    return data

def clean(s):
    s = s or ''
    s = re.sub(r'\x1b\]8;;.*?\x1b\\', '', s)
    s = re.sub(r'\x1b\[[0-9;?]*[A-Za-z]', '', s)
    return s.strip()

def run(cmd, timeout):
    p = subprocess.run(cmd, env=ENV, text=True, capture_output=True, timeout=timeout)
    return {'returncode': p.returncode, 'stdout': clean(p.stdout), 'stderr': clean(p.stderr)}

def bdpan():
    p = shutil.which('bdpan', path=ENV['PATH'])
    if not p:
        raise RuntimeError('bdpan CLI not found in PATH')
    return p

def safe_remote(path, empty=False, share=False):
    if path is None:
        if empty: return ''
        raise ValueError('missing remote path')
    path = str(path).strip()
    if not path:
        if empty: return ''
        raise ValueError('empty remote path')
    if share and path.startswith('https://pan.baidu.com/s/'):
        return path
    if '\x00' in path or path.startswith('~') or '..' in path.split('/'):
        raise ValueError('unsafe remote path')
    if path.startswith('我的应用数据'):
        raise ValueError('use relative path, not display path')
    if path == '/apps/bdpan':
        return ''
    if path.startswith('/apps/bdpan/'):
        return path[len('/apps/bdpan/'):]
    if path.startswith('/'):
        raise ValueError('absolute remote path outside /apps/bdpan is not allowed')
    return path

def local_path(path, allow=False, must_exist=False):
    p = Path(str(path).strip()).expanduser()
    if not p.is_absolute():
        p = WORKSPACE / p
    p = p.resolve(strict=False)
    if not allow:
        p.relative_to(WORKSPACE)
    if must_exist and not p.exists():
        raise FileNotFoundError(str(p))
    return str(p)

def status(_):
    p = shutil.which('bdpan', path=ENV['PATH'])
    out = {'ok': bool(p), 'skill': SKILL, 'action': 'status', 'bdpan_path': p, 'workspace': str(WORKSPACE)}
    if p:
        out['version'] = run([p, 'version'], 10)
        out['whoami'] = run([p, 'whoami'], 10)
        out['logged_in'] = '已登录' in out['whoami'].get('stdout', '')
    else:
        out['hint'] = 'Install official bdpan-storage skill/CLI first.'
    return out

def act_ls(a):
    p = bdpan(); r = safe_remote(a.get('remote_path', ''), empty=True)
    cmd = [p, 'ls'] + ([r] if r else [])
    if a.get('json'): cmd.append('--json')
    res = run(cmd, 30)
    return {'ok': res['returncode'] == 0, 'skill': SKILL, 'action': 'ls', 'remote_path': r or '.', 'result': res}

def act_upload(a):
    p = bdpan(); src = local_path(a['local_path'], bool(a.get('allow_outside_workspace')), True); dst = safe_remote(a['remote_path'])
    res = run([p, 'upload', src, dst], 600)
    return {'ok': res['returncode'] == 0, 'skill': SKILL, 'action': 'upload', 'local_path': src, 'remote_path': dst, 'result': res}


def act_delete(a):
    if a.get('confirmed') is not True:
        raise ValueError('confirmed=true required')
    paths = [safe_remote(x) for x in a.get('remote_paths', [])]
    if not paths:
        raise ValueError('remote_paths must not be empty')
    if any(not x for x in paths):
        raise ValueError('deleting the bdpan root is forbidden')
    p = bdpan()
    cmd = [p, 'rm', *paths, '--force']
    if a.get('json'):
        cmd.append('--json')
    res = run(cmd, 120)
    return {'ok': res['returncode'] == 0, 'skill': SKILL, 'action': 'delete', 'remote_paths': paths, 'result': res}

def act_download(a):
    p = bdpan(); src = safe_remote(a['remote_path'], share=True); dst = local_path(a['local_path'], bool(a.get('allow_outside_workspace')), False)
    cmd = [p, 'download', src, dst]
    if a.get('pwd'): cmd += ['-p', str(a['pwd'])]
    if a.get('transfer_dir'): cmd += ['-t', safe_remote(a['transfer_dir'])]
    res = run(cmd, 600)
    return {'ok': res['returncode'] == 0, 'skill': SKILL, 'action': 'download', 'remote_path': src, 'local_path': dst, 'result': res}

def act_transfer(a):
    p = bdpan(); url = str(a['share_url']).strip()
    if not url.startswith('https://pan.baidu.com/s/'):
        raise ValueError('bad share_url')
    cmd = [p, 'transfer', url]
    if a.get('pwd'): cmd += ['-p', str(a['pwd'])]
    if a.get('dest_dir'): cmd += ['-d', safe_remote(a['dest_dir'])]
    if a.get('json'): cmd.append('--json')
    res = run(cmd, 600)
    return {'ok': res['returncode'] == 0, 'skill': SKILL, 'action': 'transfer', 'result': res}

def act_share(a):
    p = bdpan(); paths = [safe_remote(x) for x in a['remote_paths']]
    res = run([p, 'share', *paths], 60)
    return {'ok': res['returncode'] == 0, 'skill': SKILL, 'action': 'share', 'remote_paths': paths, 'result': res}

def act_logout(a):
    if a.get('confirmed') is not True:
        raise ValueError('confirmed=true required')
    p = bdpan(); res = run([p, 'logout'], 30)
    return {'ok': res['returncode'] == 0, 'skill': SKILL, 'action': 'logout', 'result': res}


def act_move(a):
    p = bdpan(); src = safe_remote(a['source']); dst = safe_remote(a.get('dest_dir', ''), empty=True) or '.'
    cmd = [p, 'mv', src, dst]
    if a.get('json'): cmd.append('--json')
    res = run(cmd, 120)
    return {'ok': res['returncode'] == 0, 'skill': SKILL, 'action': 'move', 'source': src, 'dest_dir': dst or '.', 'result': res}

def act_copy(a):
    p = bdpan(); src = safe_remote(a['source']); dst = safe_remote(a.get('dest_dir', ''), empty=True) or '.'
    cmd = [p, 'cp', src, dst]
    if a.get('json'): cmd.append('--json')
    res = run(cmd, 120)
    return {'ok': res['returncode'] == 0, 'skill': SKILL, 'action': 'copy', 'source': src, 'dest_dir': dst or '.', 'result': res}

def act_rename(a):
    p = bdpan(); src = safe_remote(a['remote_path']); new_name = str(a['new_name']).strip()
    if not new_name or '/' in new_name:
        raise ValueError('new_name must be a non-empty filename without slashes')
    res = run([p, 'rename', src, new_name], 60)
    return {'ok': res['returncode'] == 0, 'skill': SKILL, 'action': 'rename', 'remote_path': src, 'new_name': new_name, 'result': res}

def act_mkdir(a):
    p = bdpan(); dst = safe_remote(a['remote_path'])
    cmd = [p, 'mkdir', dst]
    if a.get('json'): cmd.append('--json')
    res = run(cmd, 30)
    return {'ok': res['returncode'] == 0, 'skill': SKILL, 'action': 'mkdir', 'remote_path': dst, 'result': res}

ACTIONS = {'status': status, 'ls': act_ls, 'upload': act_upload, 'delete': act_delete, 'download': act_download, 'transfer': act_transfer, 'share': act_share, 'logout': act_logout, 'move': act_move, 'copy': act_copy, 'rename': act_rename, 'mkdir': act_mkdir}
try:
    if ACTION not in ACTIONS: raise ValueError('unknown action: ' + ACTION)
    print(json.dumps(ACTIONS[ACTION](load_args()), ensure_ascii=False))
except Exception as e:
    print(json.dumps({'ok': False, 'skill': SKILL, 'action': ACTION, 'error': {'type': type(e).__name__, 'message': str(e)}}, ensure_ascii=False))
