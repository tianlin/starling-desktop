"""Subscription updates browser checks. Synthetic local account/audio only."""
import json, os, pathlib, shutil, socket, subprocess, time, urllib.request
from playwright.sync_api import sync_playwright, expect

ROOT = pathlib.Path(__file__).resolve().parents[2]
OUT = ROOT / 'docs' / 'test-results'
OUT.mkdir(exist_ok=True)
EXE = ROOT / 'build' / ('demo-updates.exe' if os.name == 'nt' else 'demo-updates')
EXE.parent.mkdir(exist_ok=True)
# Fail before spawning if another test owns the dedicated port.
with socket.socket() as probe: probe.bind(('127.0.0.1', 34119))
subprocess.run([shutil.which('npm.cmd' if os.name == 'nt' else 'npm'), 'run', 'build'], cwd=ROOT/'frontend', check=True)
subprocess.run(['go', 'build', '-o', str(EXE), './cmd/demo'], cwd=ROOT, check=True)
ADDRESS = '127.0.0.1:34119'
process = subprocess.Popen([str(EXE), '-listen', ADDRESS], cwd=ROOT,
    stdout=subprocess.DEVNULL, stderr=subprocess.STDOUT,
    creationflags=subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0)
results = []
try:
    for _ in range(100):
        try: urllib.request.urlopen('http://' + ADDRESS, timeout=.2).close(); break
        except Exception: time.sleep(.1)
    else: raise RuntimeError('Synthetic updates server did not start')
    with sync_playwright() as pw:
        chrome = os.environ.get('CHROMIUM_PATH')
        if not chrome:
            candidate = pathlib.Path('C:/Program Files/Google/Chrome/Application/chrome.exe')
            if candidate.exists(): chrome = str(candidate)
        browser = pw.chromium.launch(executable_path=chrome, headless=True, args=['--mute-audio'])
        page = browser.new_page(viewport={'width':1280,'height':900}, timezone_id='Asia/Shanghai')
        page.set_default_timeout(10000)
        errors, reads = [], []
        page.on('pageerror', lambda error: errors.append(str(error)))
        page.on('request', lambda r: reads.append(r.post_data_json) if r.url.endswith('/api') and r.method == 'POST' else None)
        page.goto('http://' + ADDRESS)
        page.locator('[data-route="updates"]').click()
        assert not any(r['action']=='library' and r['payload'].get('kind')=='updates' for r in reads)
        results.append('PASS: guest updates route makes no account library request')
        page.locator('#top-account').click()
        page.locator('#modal .check-row input').first.check()
        page.get_by_label('手机号', exact=True).fill('00000000000')
        page.get_by_label('短信验证码', exact=True).fill('0000')
        page.get_by_role('button', name='验证并连接', exact=True).click()
        expect(page.locator('#account-name')).to_have_text('合成测试账号')
        page.locator('[data-route="updates"]').click()
        cards = page.locator('.updates-view .episode-card')
        expect(cards).to_have_count(30)
        expect(page.locator('.updates-date')).to_have_count(2)
        expect(cards.first.locator('img')).to_have_count(0)
        page.screenshot(path=str(OUT/'updates-desktop.png'))
        # Exercise native keyboard activation, not only mouse clicks.
        play = cards.first.get_by_role('button', name='▶ 播放', exact=True)
        play.focus(); page.keyboard.press('Enter')
        page.wait_for_function("() => document.querySelector('#audio').currentTime > .1 && !document.querySelector('#audio').paused")
        page.locator('#audio').evaluate('(a)=>window.__updatesAudio=a')
        assert '/fixture.wav' in page.locator('#audio').evaluate('(a)=>a.currentSrc')
        first_id = cards.first.get_attribute('data-id')
        cards.first.locator('summary').click()
        cards.first.get_by_role('button', name='加入稍后听', exact=True).click()
        page.locator('[data-route="queue"]').click()
        expect(page.locator(f'.episode-card[data-id="{first_id}"]')).to_have_count(1)
        page.locator('[data-route="updates"]').click()
        expect(cards).to_have_count(30)
        results.append('PASS: keyboard playback uses local synthetic WAV and queue append works')
        failed = []
        def fail_more(route):
            request = route.request.post_data_json
            if request['action']=='library' and request['payload'].get('kind')=='updates' and request['payload'].get('mode')=='more' and not failed:
                failed.append(True)
                route.fulfill(json={'ok':False,'error':{'code':'NETWORK','message':'合成更新分页失败'}})
            else: route.continue_()
        page.route('**/api', fail_more)
        page.get_by_role('button', name='加载下一页', exact=True).click()
        expect(page.get_by_role('alert')).to_contain_text('合成更新分页失败')
        expect(cards).to_have_count(30)
        page.get_by_role('button', name='加载下一页', exact=True).click()
        expect(cards).to_have_count(60)
        page.unroute('**/api', fail_more)
        page.get_by_role('button', name='加载下一页', exact=True).click()
        expect(page.get_by_text('本轮更新已加载完成', exact=True)).to_be_visible()
        expect(cards).to_have_count(60)
        assert len(set(cards.evaluate_all('(nodes)=>nodes.map(n=>n.dataset.id)'))) == 60
        page.get_by_role('button', name='下一屏', exact=True).click()
        expect(cards).to_have_count(25)
        assert len(set(cards.evaluate_all('(nodes)=>nodes.map(n=>n.dataset.id)'))) == 25
        expect(page.get_by_text('61–85 / 85 条已加载内容', exact=True)).to_be_visible()
        results.append('PASS: failed page retains items, retry deduplicates, 85 items paginate into 60/25 screens')
        cards.nth(8).locator('.episode-title').scroll_into_view_if_needed()
        scroll = page.locator('.main').evaluate('(e)=>e.scrollTop')
        chosen = cards.nth(8).locator('.episode-title').inner_text()
        cards.nth(8).locator('.episode-title').click()
        expect(page.get_by_role('heading', name=chosen, exact=True)).to_be_visible()
        page.get_by_role('button', name='← 返回订阅更新', exact=True).click()
        expect(cards).to_have_count(25)
        assert abs(page.locator('.main').evaluate('(e)=>e.scrollTop')-scroll) < 5
        cards.first.locator('.episode-meta.text-button').click()
        expect(page.get_by_role('button', name='查看单集列表', exact=True)).to_be_visible()
        page.get_by_role('button', name='← 返回订阅更新', exact=True).click()
        expect(cards).to_have_count(25)
        page.get_by_role('button', name='刷新', exact=True).click()
        expect(cards).to_have_count(30)
        assert page.locator('#audio').evaluate('(a)=>a===window.__updatesAudio && !a.paused')
        results.append('PASS: detail and podcast return retain screen/scroll; refresh preserves same playing audio')
        page.set_viewport_size({'width':900,'height':720})
        page.screenshot(path=str(OUT/'updates-narrow.png'))
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
        assert cards.first.locator('.episode-title').evaluate("(e)=>e.getBoundingClientRect().right <= innerWidth && getComputedStyle(e).textOverflow === 'ellipsis'")
        results.append('PASS: desktop/narrow screenshots, long title truncates with no horizontal overflow')
        def fixture_state(status, complete):
            def intercept(route):
                request = route.request.post_data_json
                if request['action']=='library' and request['payload'].get('kind')=='updates' and request['payload'].get('mode')=='refresh':
                    route.fulfill(json={'ok':True,'data':{'items':[],'cursor':'','complete':complete,'status':status,'epoch':request['payload']['epoch'],'pages':1,'revision':1}})
                else: route.continue_()
            return intercept
        unknown = fixture_state('unknown_end', False)
        page.route('**/api', unknown)
        page.get_by_role('button', name='刷新', exact=True).click()
        expect(page.get_by_text('尚未取得更新', exact=True)).to_be_visible()
        # The UI intentionally removed technical pagination notices in 405d9ce.
        # Unknown-end remains distinct from a confirmed empty collection below.
        expect(page.locator('.updates-view .inline-warning')).to_have_count(0)
        expect(page.get_by_text('暂无订阅更新', exact=True)).to_have_count(0)
        page.unroute('**/api', unknown)
        empty = fixture_state('complete', True)
        page.route('**/api', empty)
        page.get_by_role('button', name='刷新', exact=True).click()
        expect(page.get_by_text('暂无订阅更新', exact=True)).to_be_visible()
        page.unroute('**/api', empty)
        page.locator('[data-route="home"]').click()
        before = len([r for r in reads if r['action']=='library' and r['payload'].get('kind')=='updates'])
        page.wait_for_timeout(300)
        assert len([r for r in reads if r['action']=='library' and r['payload'].get('kind')=='updates']) == before
        results.append('PASS: empty vs unknown-end differ; leaving updates stops account library reads')
        page.locator('#top-account').click()
        page.get_by_role('button', name='退出并清除本机账号数据', exact=True).click()
        expect(page.locator('#account-name')).not_to_have_text('合成测试账号')
        before = len([r for r in reads if r['action']=='library' and r['payload'].get('kind')=='updates'])
        page.locator('[data-route="updates"]').click()
        expect(cards).to_have_count(0)
        page.wait_for_timeout(200)
        assert len([r for r in reads if r['action']=='library' and r['payload'].get('kind')=='updates']) == before
        results.append('PASS: logout clears updates and guest re-entry does not leak account requests')
        assert not errors, errors
        browser.close()
finally:
    process.terminate()
    try: process.wait(timeout=10)
    except subprocess.TimeoutExpired: process.kill(); process.wait()
    (OUT/'updates-results.json').write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding='utf-8')
print('\n'.join(results))
