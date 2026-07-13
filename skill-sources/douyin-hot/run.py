#!/usr/bin/env python3
import json, os, shutil, subprocess, sys
from pathlib import Path
BASE=Path(__file__).resolve().parent

def emit(v): print(json.dumps(v,ensure_ascii=False,separators=(',',':')))
def fail(code,msg,details=None):
    out={'ok':False,'error':{'code':code,'message':msg}}
    if details is not None: out['error']['details']=details
    emit(out); raise SystemExit(1)
def inp():
    raw=sys.stdin.read().strip()
    if not raw:return {}
    try:v=json.loads(raw)
    except Exception as e: fail('INVALID_INPUT','输入不是有效 JSON',str(e))
    if not isinstance(v,dict): fail('INVALID_INPUT','输入必须是 JSON 对象')
    return v

def main():
    args=inp(); op=str(args.pop('skill_action','status')); node=shutil.which('node') or '/opt/homebrew/bin/node'
    script=BASE/'scripts'/'douyin_json.js'
    if op=='status':
        emit({'ok':Path(node).exists(),'node':node,'script':str(script),'script_exists':script.exists(),'skill_version':'1.0.9'}); return
    if op!='hot': fail('UNKNOWN_OPERATION','未知操作',{'operation':op})
    limit=args.get('limit',10)
    if isinstance(limit,bool) or not isinstance(limit,int) or not 1<=limit<=50: fail('INVALID_LIMIT','limit 必须是 1 到 50 的整数')
    try:r=subprocess.run([node,str(script),str(limit)],capture_output=True,text=True,timeout=25)
    except subprocess.TimeoutExpired: fail('TIMEOUT','获取抖音热榜超时')
    if r.returncode!=0: fail('FETCH_FAILED','获取抖音热榜失败',{'stderr':r.stderr[-2000:],'stdout':r.stdout[-2000:]})
    try:items=json.loads(r.stdout)
    except Exception as e: fail('INVALID_RESPONSE','抖音脚本返回无法解析的数据',{'reason':str(e),'stdout':r.stdout[:2000]})
    emit({'ok':True,'count':len(items),'items':items})
if __name__=='__main__': main()
