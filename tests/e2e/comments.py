"""Comment UI against the synthetic Go provider. Never accesses a real account."""
import json, os, pathlib, subprocess, time, urllib.request
from playwright.sync_api import sync_playwright, expect

ROOT = pathlib.Path(__file__).resolve().parents[2]
OUT = ROOT / 'docs' / 'test-results'
OUT.mkdir(exist_ok=True)
EXE = ROOT / 'build' / ('demo-comments.exe' if os.name == 'nt' else 'demo-comments')
EXE.parent.mkdir(exist_ok=True)
subprocess.run(['go', 'build', '-o', str(EXE), './cmd/demo'], cwd=ROOT, check=True)
ADDRESS = '127.0.0.1:34117'
process = subprocess.Popen([str(EXE), '-listen', ADDRESS], cwd=ROOT, stdout=subprocess.PIPE,
                           stderr=subprocess.STDOUT, creationflags=subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0)
results = []
try:
    for _ in range(100):
        try:
            urllib.request.urlopen('http://' + ADDRESS, timeout=.2).close(); break
        except Exception: time.sleep(.1)
    else: raise RuntimeError('Synthetic comments server did not start')
    with sync_playwright() as pw:
        browser = pw.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH'), headless=True, args=['--mute-audio'])
        page = browser.new_page(viewport={'width':1280,'height':900})
        page.set_default_timeout(8000)
        errors, reads = [], []
        page.on('pageerror', lambda e: errors.append(str(e)))
        page.on('request', lambda r: reads.append(r.post_data_json) if r.url.endswith('/api') and r.method == 'POST' else None)
        page.goto('http://' + ADDRESS)
        page.locator('.episode-title').first.click()
        expect(page.get_by_role('tab', name='节目说明', exact=True)).to_have_attribute('aria-selected','true')
        page.get_by_role('tab', name='评论', exact=True).click()
        expect(page.get_by_role('heading', name='连接账号后查看评论')).to_be_visible()
        assert not any(r['action'].startswith('comments.') for r in reads)
        results.append('PASS: notes default, guest prompt, no background comment requests')
        page.locator('#top-account').click()
        page.locator('#modal .check-row input').first.check()
        page.get_by_label('手机号', exact=True).fill('00000000000')
        page.get_by_label('短信验证码', exact=True).fill('0000')
        page.get_by_role('button', name='验证并连接', exact=True).click()
        expect(page.locator('#account-name')).to_have_text('合成测试账号')
        page.locator('.episode-title').first.click()
        assert not any(r['action'].startswith('comments.') for r in reads)
        page.get_by_role('button', name='▶  播放单集', exact=True).click()
        page.wait_for_function("() => document.querySelector('#audio').currentTime > 0.1 && !document.querySelector('#audio').paused")
        page.locator('#audio').evaluate('(a)=>window.__commentAudio=a')
        page.get_by_role('tab', name='评论', exact=True).click()
        cards = page.locator('.comments-panel > .comment')
        expect(cards).to_have_count(2)
        expect(cards.nth(1)).to_contain_text('<img src=x onerror=window.__commentXSS=1>')
        assert cards.nth(1).locator('img').count() == 0
        assert page.evaluate('window.__commentXSS === undefined')
        results.append('PASS: lazy logged-in reading and safe multiline plain text')
        failed = []
        def fail_page(route):
            payload = route.request.post_data_json
            if payload['action'] == 'comments.list' and payload['payload'].get('cursor') and not failed:
                failed.append(True)
                route.fulfill(json={'ok':False,'error':{'code':'NETWORK','message':'合成分页失败'}})
            else: route.continue_()
        page.route('**/api', fail_page)
        page.get_by_role('button', name='加载更多评论', exact=True).click()
        expect(page.get_by_role('alert')).to_contain_text('合成分页失败')
        expect(cards).to_have_count(2)
        page.get_by_role('button', name='重试', exact=True).click()
        expect(cards).to_have_count(3)
        page.unroute('**/api', fail_page)
        assert len(set(cards.evaluate_all('(nodes)=>nodes.map(n=>n.dataset.commentId)'))) == 3
        results.append('PASS: page error preserves content, retry merges and deduplicates')
        cards.first.locator('.comment-replies-toggle').click()
        expect(page.locator('.comment-replies .comment')).to_have_count(1)
        expect(page.locator('.comment-replies')).to_contain_text('合成回复')
        assert page.locator('#audio').evaluate('(a)=>a===window.__commentAudio && !a.paused')
        page.get_by_role('tab', name='节目说明', exact=True).click()
        page.get_by_role('tab', name='评论', exact=True).click()
        expect(cards).to_have_count(3)
        results.append('PASS: replies expand, tabs preserve comments and continuous playback')

        draft = page.get_by_role('textbox', name='评论内容', exact=True)
        send = page.get_by_role('button', name='发表评论', exact=True)
        draft.fill(' \n\t ')
        expect(send).to_be_disabled()
        draft.fill('合成浏览器发表：保留换行。\n第二行。')
        expect(send).to_be_enabled()
        held = []
        def hold_created(route):
            if route.request.post_data_json['action'] == 'comments.create':
                response = route.fetch()
                assert response.json()['ok']
                held.append((route, response))
            else: route.continue_()
        page.route('**/api', hold_created)
        send.click()
        for _ in range(100):
            if held: break
            page.wait_for_timeout(20)
        assert len(held) == 1
        expect(page.get_by_role('button', name='正在发表…', exact=True)).to_be_disabled()
        expect(draft).to_have_value('合成浏览器发表：保留换行。\n第二行。')
        held[0][0].fulfill(response=held[0][1])
        page.unroute('**/api', hold_created)
        expect(page.get_by_role('status').filter(has_text='评论已发表')).to_be_visible()
        expect(draft).to_have_value('')
        expect(cards).to_have_count(4)
        assert len([r for r in reads if r['action'] == 'comments.create']) == 1
        results.append('PASS: whitespace blocked, one in-flight write, confirmed result clears draft')

        draft.fill('跨页面草稿')
        page.get_by_role('tab', name='节目说明', exact=True).click()
        page.get_by_role('tab', name='评论', exact=True).click()
        expect(draft).to_have_value('跨页面草稿')
        page.locator('[data-route="favorites"]').click()
        page.locator('.episode-title').nth(1).click()
        page.get_by_role('tab', name='评论', exact=True).click()
        expect(draft).to_have_value('')
        page.locator('[data-route="favorites"]').click()
        page.locator('.episode-title').first.click()
        page.get_by_role('tab', name='评论', exact=True).click()
        expect(draft).to_have_value('跨页面草稿')
        results.append('PASS: drafts persist across tabs/routes and stay isolated by episode')

        def lost_created_response(route):
            if route.request.post_data_json['action'] == 'comments.create':
                response = route.fetch()
                assert response.json()['ok']
                route.fulfill(json={'ok':False,'error':{'code':'COMMENT_UNCERTAIN','message':'合成发表响应丢失'}})
            else: route.continue_()
        page.route('**/api', lost_created_response)
        draft.fill('合成已发表但响应丢失')
        send.click()
        expect(page.locator('.comment-send-error')).to_contain_text('合成发表响应丢失')
        expect(draft).to_have_value('合成已发表但响应丢失')
        expect(send).to_be_disabled()
        page.unroute('**/api', lost_created_response)
        page.get_by_role('button', name='刷新评论', exact=True).click()
        expect(cards.filter(has_text='合成已发表但响应丢失')).to_have_count(1)
        sent_count = len([r for r in reads if r['action'] == 'comments.create'])
        page.get_by_role('button', name='已确认发表，清除草稿', exact=True).click()
        expect(draft).to_have_value('')
        assert len([r for r in reads if r['action'] == 'comments.create']) == sent_count
        results.append('PASS: lost success response stays uncertain; refresh/acknowledgment never republishes')

        def uncertain_without_send(route):
            if route.request.post_data_json['action'] == 'comments.create':
                route.fulfill(json={'ok':False,'error':{'code':'COMMENT_UNCERTAIN','message':'合成网络中断'}})
            else: route.continue_()
        page.route('**/api', uncertain_without_send)
        draft.fill('合成明确重试')
        send.click()
        expect(page.locator('.comment-send-error')).to_contain_text('合成网络中断')
        expect(send).to_be_disabled()
        page.unroute('**/api', uncertain_without_send)
        page.get_by_role('button', name='刷新评论', exact=True).click()
        expect(page.get_by_role('button', name='刷新评论', exact=True)).to_be_enabled()
        expect(cards.filter(has_text='合成明确重试')).to_have_count(0)
        page.get_by_role('button', name='确认未发表，重新发送', exact=True).click()
        expect(draft).to_have_value('')
        expect(cards.filter(has_text='合成明确重试')).to_have_count(1)
        writes = [r['payload'] for r in reads if r['action'] == 'comments.create']
        assert len(writes) == 4 and len(set(w['requestId'] for w in writes)) == 4
        assert page.locator('#audio').evaluate('(a)=>a===window.__commentAudio && !a.paused')
        results.append('PASS: uncertain failure needs explicit manual retry with new request ID, playback unaffected')

        delayed_read = []
        def hold_refresh(route):
            if route.request.post_data_json['action'] == 'comments.list':
                response = route.fetch()
                delayed_read.append((route, response))
            else: route.continue_()
        page.route('**/api', hold_refresh)
        page.get_by_role('button', name='刷新评论', exact=True).click()
        for _ in range(100):
            if delayed_read: break
            page.wait_for_timeout(20)
        assert delayed_read
        draft.fill('刷新时继续编辑的草稿')
        draft.focus()
        draft.evaluate('(e)=>e.setSelectionRange(2,5)')
        delayed_read[0][0].fulfill(response=delayed_read[0][1])
        page.unroute('**/api', hold_refresh)
        expect(page.get_by_role('button', name='刷新评论', exact=True)).to_be_enabled()
        expect(draft).to_be_focused()
        expect(draft).to_have_value('刷新时继续编辑的草稿')
        assert draft.evaluate('(e)=>[e.selectionStart,e.selectionEnd]') == [2,5]
        results.append('PASS: delayed comment refresh preserves textarea focus, caret and draft')
        draft.fill('退出前的合成草稿')
        page.screenshot(path=str(OUT/'comments.png'))
        page.set_viewport_size({'width':900,'height':700})
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
        page.screenshot(path=str(OUT/'comments-compact.png'))
        results.append('PASS: compact comments layout without horizontal overflow')
        page.locator('#account-button').click()
        page.get_by_role('button', name='退出并清除本机账号数据', exact=True).click()
        expect(page.locator('#account-name')).to_have_text('访客模式')
        assert page.locator('.comments-panel').count() == 0
        assert not errors, errors
        results.append('PASS: logout removes comment view and no uncaught browser errors')
        browser.close()
finally:
    process.terminate()
    try: process.wait(timeout=5)
    except subprocess.TimeoutExpired: process.kill(); process.wait()
    (OUT/'comments.json').write_text(json.dumps({'environment':'Synthetic Go provider + headless Chromium; no real comment publication','results':results}, ensure_ascii=False, indent=2), encoding='utf-8')
for result in results: print(result)
