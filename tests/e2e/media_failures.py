"""FR09: real Chromium media errors and bounded URL re-resolution.

Only an isolated loopback server and checked-in synthetic silence are used.
The test controls the API response boundary, never dispatches audio events or
replaces HTMLMediaElement methods. No desktop, account or external media access.
"""
import http.server
import json
import os
import pathlib
import platform
import re
import threading
import urllib.parse

ROOT = pathlib.Path(__file__).resolve().parents[2]
FIXTURE = pathlib.Path(__file__).with_name('media-fixtures') / 'silence.mp3'
HTML = '''<!doctype html><meta charset="utf-8"><title>Synthetic FR09 media failure</title>
  <audio id="audio" muted preload="metadata"></audio><button id="play">Play expired fixture</button>
  <script type="module">
    import {Player} from '/player.js';
    window.calls = []; window.saved = []; window.mediaErrors = []; window.playResults = [];
    const scenario = new URL(location.href).searchParams.get('scenario');
    window.oldItem = {kind:'episode', id:'640000000000000000000001', title:'Expired fixture', duration:6};
    window.newItem = {kind:'episode', id:'640000000000000000000002', title:'New silent fixture', duration:6};
    let oldResolutions = 0;
    const audio = document.querySelector('audio');
    audio.addEventListener('error', event => mediaErrors.push({trusted:event.isTrusted, code:audio.error?.code}));
    window.player = new Player(audio, async(action,payload) => {
      if (action === 'progress.save') saved.push(payload.progress);
      if (action !== 'playback.resolve') return;
      calls.push({id:payload.id, requestId:payload.requestId});
      if (payload.id === newItem.id) return {item:newItem, url:'/silence.mp3?item=new', epoch:1, position:0};
      oldResolutions++;
      if (oldResolutions === 1) return {item:oldItem, url:'/expired-first.mp3', epoch:1, position:3};
      const result = {item:oldItem, url:scenario === 'twice' ? '/expired-second.mp3' : '/silence.mp3?item=old', epoch:1, position:0};
      if (scenario === 'pause' || scenario === 'switch') {
        return new Promise(resolve => { window.releaseRetry = () => { resolve(result); window.retryReleased = true; }; });
      }
      return result;
    }, () => 1, () => {});
    document.querySelector('button').onclick = () => player.play(oldItem).then(result => playResults.push(result));
  </script>'''


def run_failure_checks(browser):
    requests, results, failures = [], [], []

    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_GET(self):
            path = urllib.parse.urlsplit(self.path).path
            status, headers = 200, {}
            if path == '/':
                data, kind = HTML.encode(), 'text/html; charset=utf-8'
            elif path == '/player.js':
                data, kind = (ROOT / 'frontend/dist/player.js').read_bytes(), 'text/javascript'
            elif path in ('/expired-first.mp3', '/expired-second.mp3'):
                status, data, kind = 403, b'Expired synthetic media URL', 'text/plain'
            elif path == '/silence.mp3':
                data, kind = FIXTURE.read_bytes(), 'audio/mpeg'
                match = re.fullmatch(r'bytes=(\d+)-(\d*)', self.headers.get('Range', ''))
                headers['Accept-Ranges'] = 'bytes'
                if match:
                    start = int(match.group(1))
                    end = min(int(match.group(2)) if match.group(2) else len(data) - 1, len(data) - 1)
                    if start > end:
                        self.send_error(416)
                        return
                    headers['Content-Range'] = f'bytes {start}-{end}/{len(data)}'
                    status, data = 206, data[start:end + 1]
            else:
                self.send_error(404)
                return
            requests.append((self.path, status))
            self.send_response(status)
            self.send_header('Content-Type', kind)
            self.send_header('Content-Length', str(len(data)))
            self.send_header('Cache-Control', 'no-store')
            for key, value in headers.items():
                self.send_header(key, value)
            self.end_headers()
            try:
                self.wfile.write(data)
            except (BrokenPipeError, ConnectionResetError, ConnectionAbortedError):
                pass

    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    server.daemon_threads = True
    worker = threading.Thread(target=server.serve_forever, daemon=True)
    worker.start()
    origin = 'http://127.0.0.1:' + str(server.server_port)
    try:
        for scenario in ('recover', 'twice', 'pause', 'switch'):
            page = browser.new_page()
            errors = []
            page.on('pageerror', lambda error: errors.append(str(error)))
            before = len(requests)
            try:
                page.goto(origin + '/?scenario=' + scenario)
                page.locator('#play').click()
                page.wait_for_function('() => calls.filter(c => c.id === oldItem.id).length >= 2', timeout=2500)
                assert page.evaluate('mediaErrors.length >= 1 && mediaErrors.every(e => e.trusted && [2,4].includes(e.code))'), 'no real browser network/source error observed'
                assert ('/expired-first.mp3', 403) in requests[before:]
                if scenario == 'recover':
                    page.wait_for_function("() => player.state === 'playing' && player.position > 3.1 && player.position < 5", timeout=6000)
                    page.evaluate('async () => { player.pause(); await player.flush(); }')
                    assert page.evaluate('saved.length > 0 && saved.every(p => p.position >= 3)'), 'URL refresh lost the pending resume checkpoint'
                    assert page.evaluate('document.querySelector("audio").error === null && player.error === ""')
                    assert any(path == '/silence.mp3?item=old' and status in (200, 206) for path, status in requests[before:])
                    description = 'one expired URL refresh plays the new URL and preserves the 3s checkpoint'
                elif scenario == 'twice':
                    page.wait_for_function("() => player.state === 'error' && mediaErrors.length >= 2", timeout=6000)
                    page.wait_for_timeout(300)  # Observe quiescence after the second real error event.
                    assert ('/expired-second.mp3', 403) in requests[before:]
                    assert page.evaluate('player.state === "error" && document.querySelector("audio").paused && player.error.length > 0')
                    assert page.evaluate('saved.length === 0'), 'failed media erased the resume checkpoint'
                    description = 'the refreshed URL also fails and stops after one retry'
                else:
                    page.wait_for_function('() => typeof releaseRetry === "function"')
                    assert page.evaluate("player.state === 'resolving'"), 'retry is not waiting at the API boundary'
                    assert page.evaluate('saved.length === 0'), 'queued retry erased the resume checkpoint'
                    if scenario == 'pause':
                        page.evaluate('player.pause()')
                        page.evaluate('releaseRetry()')
                        page.wait_for_timeout(300)  # Drain the released API promise and resulting media tasks.
                        assert page.evaluate("player.state === 'paused' && document.querySelector('audio').paused")
                        assert page.evaluate('saved.length === 0')
                        assert not any('silence.mp3?item=old' in path for path, _ in requests[before:]), 'cancelled retry loaded its stale media URL'
                        description = 'pausing a queued refresh prevents stale media from loading or playing'
                    else:
                        page.evaluate('player.play(newItem)')
                        page.wait_for_function("() => player.item.id === newItem.id && player.state === 'playing' && player.position > 0.1")
                        page.evaluate('releaseRetry()')
                        page.wait_for_timeout(300)
                        assert page.evaluate("player.item.id === newItem.id && player.state === 'playing' && !document.querySelector('audio').paused")
                        assert not any('silence.mp3?item=old' in path for path, _ in requests[before:]), 'old refresh replaced the new selection'
                        assert page.evaluate('saved.every(p => p.item.id === newItem.id)'), 'old refresh saved over the new selection'
                        assert page.evaluate('calls.filter(c => c.id === newItem.id).length === 1')
                        description = 'switching episodes fences a late refresh and keeps the new episode playing'
                assert page.evaluate('calls.filter(c => c.id === oldItem.id).length === 2'), 'URL refresh repeated beyond the one-retry limit'
                assert page.evaluate('new Set(calls.map(c => c.requestId)).size === calls.length'), 'resolution attempt IDs were reused'
                assert not errors, errors
                results.append('PASS: FR09 Chromium ' + description)
            except Exception as error:
                detail = page.evaluate('''() => ({state:window.player?.state, error:window.player?.error,
                  position:window.player?.position, calls:window.calls, mediaErrors:window.mediaErrors,
                  saved:window.saved, paused:document.querySelector('audio').paused})''')
                failures.append({'scenario': scenario, 'error': type(error).__name__ + ': ' + str(error),
                                 'state': detail, 'http': requests[before:], 'pageErrors': errors})
                print('FAIL: FR09', scenario, json.dumps(failures[-1]))
            finally:
                page.close()
    finally:
        server.shutdown()
        server.server_close()
        worker.join(timeout=5)
        output = ROOT / 'docs/test-results/media-failures.json'
        output.parent.mkdir(exist_ok=True)
        output.write_text(json.dumps({'status': 'failed' if failures else 'passed',
            'requirement': 'FR09: one media URL refresh; cancellation; progress preserved',
            'environment': 'Chromium with synthetic loopback HTTP errors; not real CDN or Windows WebView2 acceptance',
            'browser': browser.version, 'platform': platform.platform(), 'results': results, 'failures': failures}, indent=2))
    if failures:
        raise AssertionError('FR09 real media error scenarios failed: ' + ', '.join(failure['scenario'] for failure in failures))
    return results


if __name__ == '__main__':
    from playwright.sync_api import sync_playwright
    with sync_playwright() as pw:
        browser = pw.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH'), headless=True, args=['--mute-audio'])
        try:
            for result in run_failure_checks(browser):
                print(result)
        finally:
            browser.close()
