# -*- coding: utf-8 -*-
"""48개 도구 전수 스윕 — 헬퍼(https://127.0.0.1:3000)에 붙은 **첫 덱**에 순서대로 다 불러 보고 표로 낸다.

  python3 clients/powerpoint/tools/sweep.py [--deck <pid-deck-…>] [--image <png>]

읽기·쓰기 전부 실제로 부른다(장 3~4개를 만들고 끝에 지운다 — 1장만 남는다). 토큰은 헬퍼 페이지에서,
덱은 /api/documents 에서 얻는다. Windows 의 tools-sweep.ps1 과 같은 일을 Mac/Linux 에서 한다.
실측 2026-09-05: 48/48 · 57호출 · 오류 0 · 약 1초.
"""
import json, ssl, urllib.request, os, base64, sys, time, re, argparse
ap=argparse.ArgumentParser(); ap.add_argument('--deck', default=''); ap.add_argument('--image', default=''); ap.add_argument('--origin', default='https://127.0.0.1:3000')
opt=ap.parse_args()
S=os.path.dirname(os.path.abspath(__file__))
ctx=ssl.create_default_context(); ctx.check_hostname=False; ctx.verify_mode=ssl.CERT_NONE
def get(path, tok=None):
    req=urllib.request.Request(opt.origin+path, headers={'authorization':'Bearer '+tok} if tok else {})
    with urllib.request.urlopen(req, context=ctx, timeout=20) as r: return r.read().decode('utf-8','replace')
page=get('/taskpane.html'); m=re.search(r'token[^a-zA-Z0-9]{1,6}([A-Za-z0-9_-]{16,})', page)
if not m: sys.exit('헬퍼 페이지에서 토큰을 못 찾았다 — 헬퍼가 떠 있나?')
TOK=m.group(1)
docs=json.loads(get('/api/documents', TOK)).get('documents') or []
DECK=opt.deck or (docs[0]['document'] if docs else '')
if not DECK: sys.exit('붙은 작업창이 없다 — PowerPoint 에서 magi 작업창을 열어라')
IMG=opt.image or os.path.join(S, 'sweep-image.png')
if not os.path.exists(IMG):
    # 1×1 PNG — add_image·배경 그림용
    open(IMG,'wb').write(base64.b64decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=='))
rows=[]; done=set()
def call(name, args, note=''):
    body=json.dumps({"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":name,"arguments":args}}).encode()
    req=urllib.request.Request(f'https://127.0.0.1:3000/mcp?deck={DECK}', data=body, headers={'authorization':'Bearer '+TOK,'content-type':'application/json'}, method='POST')
    t0=time.time()
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=120) as r: resp=json.loads(r.read())
    except Exception as e:
        rows.append((name,'ERR',f'transport: {e}'[:120],note)); return None
    res=resp.get('result') or {}; err=bool(res.get('isError')); txt=''.join(b.get('text','') for b in res.get('content',[]) if b.get('type')=='text')
    img=[b for b in res.get('content',[]) if b.get('type')=='image']
    try: x=json.loads(txt)
    except Exception: x={'_raw':txt}
    if img: open(os.path.join(S, f'sweep_{name}.png'),'wb').write(base64.b64decode(img[0]['data']))
    summary=(' | '.join(x.get('changed',[])) if isinstance(x,dict) and x.get('changed') else txt)[:110].replace('\n',' ')
    if img: summary=f'image {len(img[0]["data"])//1024}KB ' + summary
    rows.append((name, 'ERR' if err else 'ok', summary, note, round(time.time()-t0,1))); done.add(name)
    return None if err else x
def shapes(slide):
    x=call('read_slide',{'slide':slide},'되읽기') or {}
    return x.get('shapes',[])
def first(shs, pred):
    for s in shs:
        if pred(s): return s
    return None

lay=call('list_layouts',{},'') or {}
names=[l['layout'] for m in lay.get('masters',[]) for l in m.get('layouts',[])]
title_layout=next((n for n in names if '제목 슬라이드' in n or 'Title Slide' in n), names[0] if names else None)
call('describe_style',{},'')
call('read_theme_colors',{'scope':'master'},'master')
call('add_slides',{'slides':[{'title':'스윕 2','body':'첫째\n둘째','bullet':True},{'title':'스윕 3','body':'차트와 그림','bullet':False}]},'2장')
call('add_slide',{'title':'스윕 4','body':'레이아웃 바꿀 장'},'')
call('list_slides',{},'')
sh=shapes(2)
title=first(sh, lambda s: 'title' in str(s.get('placeholder','')).lower()); body=first(sh, lambda s: str(s.get('placeholder','')).lower() in ('content','body'))
call('find_shapes',{'text':'스윕'},'text=스윕')
call('export_slide_ooxml',{'slide':2,'part':'list'},'part=list')
call('set_text',{'slide':2,'placeholder':'title','text':'스윕 2 — 제목'},'placeholder=title')
if title: call('format_shape',{'slide':2,'shape_id':title['shape_id'],'bold':True,'color':'#1E3A8A','valign':'Middle','underline':'Single'},'제목')
if title: call('format_text',{'slide':2,'shape_id':title['shape_id'],'find':'스윕','color':'#DC2626','bold':True},'find=스윕')
A=call('add_shape',{'slide':2,'kind':'textbox','text':'상자 A','left':60,'top':330,'width':150,'height':50,'fill':'#EEF2FF','line':'#3B82F6'},'textbox') or {}
B=call('add_shape',{'slide':2,'kind':'rectangle','text':'B','left':260,'top':340,'width':150,'height':50,'fill':'#FECACA','transparency':0.3},'rectangle') or {}
a,b=A.get('shape_id'),B.get('shape_id')
if b: call('move_shape',{'slide':2,'shape_id':b,'top':330,'z_order':'SendToBack'},'top+z_order')
if a and b: call('align_shapes',{'slide':2,'shape_ids':[a,b],'how':'top'},'how=top')
G=call('group_shapes',{'slide':2,'shape_ids':[a,b]},'') if a and b else None
if G: call('ungroup_shapes',{'slide':2,'shape_id':G['shape_id']},'')
sh=shapes(2); tb=first(sh, lambda s: s.get('type')=='TextBox')
if tb: call('set_hyperlink',{'slide':2,'shape_id':tb['shape_id'],'url':'https://example.com','screen_tip':'예'},'textbox')
if tb: call('render_shape',{'slide':2,'shape_id':tb['shape_id'],'max_width':300},'')
T=call('add_table',{'slide':3,'rows':3,'columns':3,'values':[['구분','전','후'],['a','1','2'],['b','3','4']],'left':60,'top':300,'width':400,'height':110,'table_style':'MediumStyle2Accent1','header_row':True,'column_widths':[160,120,120]},'style+widths') or {}
tid=T.get('shape_id')
if tid:
    call('set_table_cells',{'slide':3,'shape_id':tid,'cells':[{'row':1,'column':1,'text':'값'}]},'')
    call('format_table_cells',{'slide':3,'shape_id':tid,'row':0,'bold':True,'fill':'#1E3A8A','color':'#FFFFFF','valign':'Middle'},'머리행')
    call('edit_table',{'slide':3,'shape_id':tid,'add_rows':1,'merge':[{'row':0,'column':1,'columns':2}]},'add_rows+merge')
    call('replace_table',{'slide':3,'shape_id':tid,'rows':2,'columns':2},'3x3→2x2')
call('add_chart',{'slide':3,'kind':'column','title':'스윕','categories':['a','b'],'series':[{'name':'s','values':[1,2]}],'left':480,'top':120,'width':400,'height':160},'')
call('add_image',{'slide':3,'path':IMG,'left':60,'top':120,'width':200,'alt':'스윕 그림'},'png')
call('set_notes',{'slide':2,'text':'스윕 노트'},''); call('read_notes',{'slide':2},'')
call('set_tag',{'slide':2,'key':'sweep','value':'1'},''); call('read_tags',{'slide':2},'')
sh=shapes(2); body=first(sh, lambda s: str(s.get('placeholder','')).lower() in ('content','body'))
if body: call('animate_slide',{'slide':2,'steps':[{'shape_id':body['shape_id'],'effect':'fade'}]},'body fade')
call('read_animation',{'slide':2},'')
call('suggest',{'slide':2,'what':'제목을 짧게','why':'두 줄이다'},''); sg=call('read_suggestions',{'slide':2},'') or {}
key=None
for k in ('suggestions','items'):
    if isinstance(sg.get(k),list) and sg[k]: key=sg[k][0].get('key'); break
if key: call('drop_suggestion',{'slide':2,'key':key},'')
call('advise',{'items':[{'message':'표지 대비 확인','why':'배경이 어둡다','slide_id':None}]},''); call('clear_advice',{},'')
call('set_background',{'slide':2,'color':'#0F172A'},'dark → 대비 경고?')
call('set_theme_colors',{'colors':{'accent1':'#0284C7'},'scope':'master'},'master accent1')
call('apply_style',{'title':{'bold':True},'slides':[2]},'slide 2 title bold')
if title_layout: call('apply_layout',{'slide':4,'layout':title_layout},title_layout)
call('reorder_slide',{'slide':4,'to':2},'4→2')
call('duplicate_slide',{'slide':3},'')
snap=call('snapshot_slide',{'slide':3},'') or {}
if snap.get('snapshot'): call('restore_slide',{'slide':3,'snapshot':snap['snapshot']},'')
call('render_slide',{'slide':3,'max_width':640},'')
sh=shapes(3); tb=first(sh, lambda s: s.get('type') in ('TextBox','GeometricShape'))
if tb: call('delete_shape',{'slide':3,'shape_id':tb['shape_id']},'')
else:
    x=call('add_shape',{'slide':3,'kind':'rectangle','left':10,'top':10,'width':20,'height':20},'삭제용') or {}
    if x.get('shape_id'): call('delete_shape',{'slide':3,'shape_id':x['shape_id']},'')
ls=call('list_slides',{},'정리 전') or {}
n=len(ls.get('slides',[]))
for i in range(n,1,-1): call('delete_slide',{'slide':i},f'{i}')
ls=call('list_slides',{},'정리 후') or {}
lst=json.dumps({"jsonrpc":"2.0","id":1,"method":"tools/list"}).encode()
req=urllib.request.Request(f'{opt.origin}/mcp?deck={DECK}', data=lst, headers={'authorization':'Bearer '+TOK,'content-type':'application/json'}, method='POST')
with urllib.request.urlopen(req, context=ctx, timeout=20) as r: all48=[t['name'] for t in json.loads(r.read())['result']['tools']]
missing=[t for t in all48 if t not in done]
print(f"호출 {len(rows)} · 도구 {len(done)}/{len(all48)} · 안 부른 것: {missing}")
errs=[r for r in rows if r[1]=='ERR']
print(f"오류 {len(errs)}")
for r in rows: print(f"{r[1]:<3} {r[0]:<20} {r[4] if len(r)>4 else '':>5}s  {r[3]:<14} {r[2]}")
sys.exit(1 if errs or missing else 0)
