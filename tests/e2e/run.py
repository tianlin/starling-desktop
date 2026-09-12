"""Browser tests against the explicitly synthetic Go harness; never uses a real account.
Requires Python Playwright and Chromium. Set CHROMIUM_PATH to override executable.
"""
import json, os, pathlib, subprocess, time, urllib.request, re, base64
from playwright.sync_api import sync_playwright, expect
ROOT = pathlib.Path(__file__).resolve().parents[2]
OUT = ROOT / 'docs' / 'test-results'
OUT.mkdir(exist_ok=True)
EXE = ROOT / 'build' / ('demo.exe' if os.name == 'nt' else 'demo')
EXE.parent.mkdir(exist_ok=True)
subprocess.run(['go','build','-o',str(EXE),'./cmd/demo'],cwd=ROOT,check=True)
process = subprocess.Popen([str(EXE)],cwd=ROOT,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
results=[]
try:
    for _ in range(100):
        try:
            urllib.request.urlopen('http://127.0.0.1:34115/',timeout=.2).close();break
        except Exception:time.sleep(.1)
    else:raise RuntimeError('Demo server did not start')
    with sync_playwright() as pw:
        browser = pw.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH','/usr/bin/chromium'),headless=True,args=['--no-sandbox'])
        page=browser.new_page(viewport={'width':1280,'height':900},device_scale_factor=1)
        page.set_default_timeout(8000)
        errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
        if os.environ.get('STARLING_E2E_IN_MEMORY') == '1':
            # Policy-restricted browser: same TypeScript compiled to AMD, no network navigation.
            # Only the synthetic harness may use this injected transport and Blob audio.
            subprocess.run([str(ROOT/'frontend/node_modules/.bin/tsc'),'-p',str(ROOT/'frontend/tsconfig.json'),'--module','amd','--moduleResolution','node','--outFile',str(ROOT/'build/ui.amd.js')],check=True)
            cfg=urllib.request.urlopen('http://127.0.0.1:34115/demo-config.js').read().decode()
            token=re.search(r'token:"([a-f0-9]+)"',cfg).group(1)
            def bridge(action,payload):
                req=urllib.request.Request('http://127.0.0.1:34115/api',data=json.dumps({'action':action,'payload':json.loads(payload)}).encode(),headers={'Content-Type':'application/json','X-Starling-Demo':token})
                return urllib.request.urlopen(req).read().decode()
            page.expose_function('__goSyntheticCall',bridge)
            html=(ROOT/'frontend/dist/index.html').read_text()
            html=re.sub(r'<script[^>]*src="/main.js"[^>]*></script>','',html)
            html=html.replace('<link rel="stylesheet" href="/style.css">','')
            page.set_content(html)
            page.add_style_tag(content=(ROOT/'frontend/dist/style.css').read_text())
            wav=urllib.request.urlopen('http://127.0.0.1:34115/fixture.wav').read()
            page.evaluate("""b64=>{const bytes=Uint8Array.from(atob(b64),c=>c.charCodeAt(0));window.__fixtureMediaURL=URL.createObjectURL(new Blob([bytes],{type:'audio/wav'}));
              window.__STARLING_DEMO__={token:'in-memory-test-only'};
              window.go={main:{App:{Call:async(action,payload)=>{const raw=await window.__goSyntheticCall(action,payload);const v=JSON.parse(raw);if(action==='playback.resolve'&&v.ok)v.data.url=window.__fixtureMediaURL;return JSON.stringify(v);}}}};
            }""",base64.b64encode(wav).decode())
            page.add_script_tag(content=r"""
              const mods=new Map(),cache=new Map();
              window.define=(name,deps,fn)=>mods.set(name,{deps,fn});
              window.requireAMD=(name)=>{
                name=name.replace(/^\.\//,'').replace(/\.js$/,'');
                if(cache.has(name))return cache.get(name);
                const m=mods.get(name);if(!m)throw Error('Missing test module '+name);
                const out={};cache.set(name,out);m.fn(...m.deps.map(d=>d==='require'?window.requireAMD:d==='exports'?out:window.requireAMD(d)));return out;
              };
            """)
            page.add_script_tag(content=(ROOT/'build/ui.amd.js').read_text())
            page.evaluate("requireAMD('main')")
        else:
            page.goto('http://127.0.0.1:34115/')
        expect(page.locator('#demo-banner')).to_be_visible()
        expect(page.locator('#page .episode-card')).to_have_count(3)
        assert page.locator('#audio').evaluate('(a)=>a.paused')
        results.append('PASS: explicit synthetic banner and paused startup restore')
        page.locator('#page .episode-card .primary-soft').first.click()
        page.wait_for_function("document.querySelector('#audio').currentTime > 0.1 && !document.querySelector('#audio').paused")
        page.locator('#audio').evaluate('(a)=>window.__audioIdentity=a')
        current_id=page.locator('#player-art').get_attribute('data-id')
        page.locator('[data-route="queue"]').click()
        assert page.locator('#audio').evaluate('(a)=>a === window.__audioIdentity && !a.paused')
        results.append('PASS: actual synthetic WAV playback and one audio instance across routes')
        page.locator('#toggle-play').click()
        page.locator('#audio').evaluate('(a)=>a.currentTime=42')
        page.locator('[data-route="home"]').click()
        expect(page.locator('#page .history-progress').first).to_contain_text('0:42')
        results.append('PASS: local progress persisted and presented')
        page.locator('#top-account').click()
        page.locator('#modal .check-row input').first.check()
        page.get_by_label('手机号',exact=True).fill('00000000000')
        page.get_by_label('短信验证码',exact=True).fill('0000')
        page.get_by_role('button',name='验证并连接',exact=True).click()
        expect(page.locator('#account-name')).to_have_text('合成测试账号')
        expect(page.locator('#page .episode-card')).to_have_count(3)
        page.get_by_role('button',name='加载全部',exact=True).click()
        expect(page.locator('#page .episode-card')).to_have_count(8)
        expect(page.locator('.list-toolbar')).to_contain_text('本轮加载结束')
        results.append('PASS: synthetic login and two-page favorites merge')
        expect(page.get_by_role('button',name='取消加载全部',exact=True)).not_to_be_visible()
        page.screenshot(path=str(ROOT/'docs/screenshots/favorites.png'),full_page=False)
        first=page.locator('#page .episode-card').first
        first.locator('.primary-soft').click()
        page.wait_for_function("!document.querySelector('#audio').paused")
        field=page.get_by_label('筛选已加载内容');field.fill('城市');expect(page.locator('#page .episode-card')).to_have_count(3)
        field.press('Space');assert not page.locator('#audio').evaluate('(a)=>a.paused')
        field.fill('');expect(page.locator('#page .episode-card')).to_have_count(8)
        results.append('PASS: loaded-only filter and space does not hijack text input')
        first.locator('summary').click()
        first.get_by_role('button',name='加入稍后听',exact=True).click()
        first.locator('summary').click();first.get_by_role('button',name='加入稍后听',exact=True).click()
        expect(page.locator('#queue-count')).to_have_text('1')
        results.append('PASS: queue dedup through actual UI and backend')
        first.locator('.episode-title').click()
        expect(page.locator('.shownotes')).to_be_visible()
        assert page.locator('.shownotes script, .shownotes img, .shownotes iframe').count()==0
        assert page.evaluate('window.__xss') is None
        page.get_by_role('button',name='00:45',exact=True).click()
        page.wait_for_function("document.querySelector('#audio').currentTime >= 45 && document.querySelector('#audio').currentTime < 50")
        results.append('PASS: Show Notes sanitization and timestamp seeking')
        page.screenshot(path=str(ROOT/'docs/screenshots/detail.png'),full_page=False)
        page.locator('#toggle-play').click()
        page.locator('#open-link').click()
        page.get_by_label('节目或单集的完整链接').fill('https://www.xiaoyuzhoufm.com.evil.invalid/episode/64db2d493fa4090b744c3100')
        page.get_by_role('button',name='打开内容',exact=True).click()
        expect(page.locator('#modal .form-error')).not_to_have_text('')
        page.locator('#modal .modal-head button').click()
        results.append('PASS: malicious share domain rejected without navigation')
        page.locator('#account-button').click();page.get_by_role('button',name='退出并清除本机账号数据',exact=True).click()
        expect(page.locator('#account-name')).to_have_text('访客模式')
        assert page.locator('#audio').evaluate('(a)=>a.paused')
        page.locator('[data-route="favorites"]').click();expect(page.locator('#page')).to_contain_text('连接账号后查看')
        results.append('PASS: logout stops audio and restores isolated guest scope')
        page.locator('[data-route="home"]').click()
        page.set_viewport_size({'width':900,'height':700});page.screenshot(path=str(ROOT/'docs/screenshots/compact.png'))
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
        results.append('PASS: compact 900×700 layout without horizontal overflow')
        assert not errors,errors
        results.append('PASS: no uncaught browser errors')
        browser.close()
finally:
    process.terminate()
    try:process.wait(timeout=5)
    except subprocess.TimeoutExpired:process.kill();process.wait()
    (OUT/'e2e.json').write_text(json.dumps({'environment':'Chromium synthetic harness; in-memory='+str(os.environ.get('STARLING_E2E_IN_MEMORY')=='1')+'; not Windows WebView2 or a real account','results':results},ensure_ascii=False,indent=2))
for result in results: print(result)
