"""Exercise the shipped Player with offline encoded silence over real HTTP.

This is Chromium evidence only, not WebView2/device/long-session acceptance.
The temporary server listens only on an OS-assigned loopback port.
"""
import http.server
import pathlib
import re
import threading
import urllib.error
import urllib.parse
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parents[2]
FIXTURES = pathlib.Path(__file__).with_name('media-fixtures')
TYPES = {'mp3': 'audio/mpeg', 'm4a': 'audio/mp4', 'aac': 'audio/aac'}


def run_media_checks(browser):
    requests = []
    # Load only fixed, checked-in fixtures before accepting HTTP requests.
    # Request data selects bytes, never a filesystem path.
    fixtures = {
        'mp3': (FIXTURES / 'silence.mp3').read_bytes(),
        'm4a': (FIXTURES / 'silence.m4a').read_bytes(),
        'aac': (FIXTURES / 'silence.aac').read_bytes(),
    }

    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_GET(self):
            path = urllib.parse.urlsplit(self.path).path
            media = re.fullmatch(r'/media/(range|plain)/silence\.(mp3|m4a|aac)', path)
            status, headers = 200, {}
            if path == '/player.js':
                data = (ROOT / 'frontend/dist/player.js').read_bytes()
                kind = 'text/javascript'
            elif path == '/':
                query = urllib.parse.parse_qs(urllib.parse.urlsplit(self.path).query)
                ext, mode = query.get('format', [''])[0], query.get('mode', [''])[0]
                if ext not in TYPES or mode not in ('range', 'plain'):
                    self.send_error(400)
                    return
                kind = 'text/html; charset=utf-8'
                data = ('''<!doctype html><meta charset="utf-8"><title>Synthetic media check</title>
                  <audio id="audio" muted preload="metadata"></audio><button id="play">Play fixture</button>
                  <script type="module">
                    import {Player} from '/player.js';
                    window.saved = [];
                    window.resumePosition = RESUME_POSITION;
                    window.item = {kind:'episode', id:'640000000000000000000001', title:'Generated silence', duration:6};
                    window.player = new Player(document.querySelector('audio'), async(action,payload) => {
                      if (action === 'playback.resolve') return {item, url:'MEDIA_URL', epoch:1, position:resumePosition};
                      if (action === 'progress.save') saved.push(payload.progress);
                    }, () => 1, () => {});
                    document.querySelector('button').onclick = () => player.play(item);
                  </script>'''.replace('MEDIA_URL', '/media/' + mode + '/silence.' + ext)
                  .replace('RESUME_POSITION', '3' if mode == 'range' else '0')).encode()
            elif media:
                mode, ext = media.groups()
                data = fixtures[ext]
                kind = TYPES[ext]
                requested = self.headers.get('Range', '')
                if mode == 'range':
                    headers['Accept-Ranges'] = 'bytes'
                    match = re.fullmatch(r'bytes=(\d+)-(\d*)', requested)
                    if requested and not match:
                        self.send_error(416)
                        return
                    if match:
                        start = int(match.group(1))
                        end = min(int(match.group(2)) if match.group(2) else len(data) - 1, len(data) - 1)
                        if start > end:
                            self.send_error(416)
                            return
                        headers['Content-Range'] = f'bytes {start}-{end}/{len(data)}'
                        data = data[start:end + 1]
                        status = 206
                # "plain" deliberately ignores Range and returns the complete body.
                requests.append((mode, ext, requested, status))
            else:
                self.send_error(404)
                return
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
                pass  # The browser may cancel an obsolete media read after seeking.

    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    server.daemon_threads = True
    worker = threading.Thread(target=server.serve_forever, daemon=True)
    worker.start()
    origin = 'http://127.0.0.1:' + str(server.server_port)
    results = []
    try:
        for path in (
            '/media/range/../media.py',
            '/media/plain/%2e%2e%2fmedia.py',
            '/media/range/silence.mp3/../../media.py',
            '/media/plain/silence.wav',
        ):
            try:
                urllib.request.urlopen(origin + path, timeout=5).close()
            except urllib.error.HTTPError as error:
                assert error.code == 404, (path, error.code)
                error.close()
            else:
                raise AssertionError('Unexpected media route accepted: ' + path)
        results.append('media server rejects traversal and unsupported fixture paths')
        for ext in TYPES:
            for mode in ('range', 'plain'):
                req = urllib.request.Request(origin + '/media/' + mode + '/silence.' + ext, headers={'Range': 'bytes=10-29'})
                with urllib.request.urlopen(req, timeout=5) as response:
                    body = response.read()
                    expected = (FIXTURES / ('silence.' + ext)).read_bytes()
                    if mode == 'range':
                        assert response.status == 206 and body == expected[10:30]
                        assert response.headers['Content-Range'] == f'bytes 10-29/{len(expected)}'
                    else:
                        assert response.status == 200 and body == expected
                        assert response.headers.get('Content-Range') is None
                # Keep control probes separate from requests actually issued by audio.
                before = len(requests)
                page = browser.new_page()
                errors = []
                page.on('pageerror', lambda error: errors.append(str(error)))
                try:
                    page.goto(origin + '/?format=' + ext + '&mode=' + mode)
                    page.locator('#play').click()
                    if mode == 'range':
                        page.wait_for_function("() => player.state === 'playing' && player.position > 3.1 && player.position < 5", timeout=12000)
                    else:
                        page.wait_for_function("() => player.state === 'playing' && player.position > 0.1 && player.position < 2", timeout=12000)
                    assert page.evaluate('player.duration > 5 && player.duration < 8')
                    page.evaluate('async () => { player.pause(); await player.flush(); }')
                    if mode == 'range':
                        assert page.evaluate('saved.length > 0 && saved.every(p => p.position >= 3)'), 'resume checkpoint was overwritten before seeking'
                    else:
                        assert page.evaluate('saved.length > 0 && saved.at(-1).position > 0')
                    supports_seek = page.evaluate('''() => { const a = document.querySelector('audio');
                      return Array.from({length:a.seekable.length}, (_,i) => [a.seekable.start(i),a.seekable.end(i)])
                        .some(([start,end]) => start <= 1 && end >= 1); }''')
                    assert page.evaluate('player.seek(1)') == supports_seek, 'seek result does not match actual seekable ranges'
                    if mode == 'range':
                        assert supports_seek, 'range fixture is not seekable'
                    if supports_seek:
                        page.wait_for_function('() => player.position >= 1 && player.position < 1.2')
                    page.evaluate('player.toggle()')
                    page.wait_for_function("() => player.state === 'playing' && player.position > 1.1 && player.position < 3")
                    page.evaluate('async () => { player.pause(); await player.flush(); }')
                    assert page.evaluate('saved.at(-1).position > 1 && saved.at(-1).position < 2')
                    assert page.evaluate('document.querySelector("audio").error === null')
                    assert not errors, errors
                    audio_requests = requests[before:]
                    assert audio_requests, 'audio did not request the HTTP fixture'
                    assert all(entry[3] == (206 if mode == 'range' and entry[2] else 200) for entry in audio_requests)
                    if mode == 'range':
                        assert any(entry[2] and entry[3] == 206 for entry in audio_requests), 'no browser Range request was observed'
                    if mode == 'range':
                        behavior = 'decode, resume at 3s, seek to 1s, progress save'
                    else:
                        behavior = 'decode from start, progress save; ' + ('buffered seek works' if supports_seek else 'unavailable seek correctly rejected')
                        if not supports_seek:
                            page.evaluate('() => { player.clear(); saved.length = 0; window.resumePosition = 3; }')
                            page.locator('#play').click()
                            page.wait_for_function("() => player.state === 'playing' && document.querySelector('audio').currentTime > 0.2")
                            assert page.evaluate('player.position > 0.1 && player.position < 2'), 'position displays an unreachable checkpoint'
                            assert page.evaluate('player.notice.length > 0'), 'unreachable resume checkpoint has no explanation'
                            pending_notice = page.evaluate('player.notice')
                            assert page.evaluate('!player.seekable'), 'zero-width seekable range enables seeking'
                            page.evaluate('player.persist()')
                            assert page.evaluate('saved.length === 0'), 'unreachable resume checkpoint was overwritten'
                            page.wait_for_function("() => document.querySelector('audio').currentTime > 3.2", timeout=6000)
                            page.evaluate('async () => { player.pause(); await player.flush(); }')
                            assert page.evaluate('saved.length > 0 && saved.every(p => p.position >= 3)'), 'progress did not recover after reaching the checkpoint'
                            assert page.evaluate('player.notice') != pending_notice, 'stale resume notice remains after recovery'
                            behavior += '; unreachable resume explained and preserved until natural playback reaches it'
                    results.append(f'PASS: Chromium Player {ext.upper()} over HTTP {mode}: {behavior}')
                except Exception:
                    print('FAIL:', ext, mode, page.evaluate('''() => ({state:window.player?.state, error:window.player?.error,
                      position:window.player?.position, duration:window.player?.duration,
                      mediaError:document.querySelector('audio')?.error?.message,
                      seekable:Array.from({length:document.querySelector('audio').seekable.length}, (_,i) =>
                        [document.querySelector('audio').seekable.start(i),document.querySelector('audio').seekable.end(i)])})'''), requests[before:], errors)
                    raise
                finally:
                    page.close()
    finally:
        server.shutdown()
        server.server_close()
        worker.join(timeout=5)
    return results


if __name__ == '__main__':
    import json
    import os
    import platform
    from playwright.sync_api import sync_playwright
    with sync_playwright() as pw:
        browser = pw.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH'), headless=True, args=['--mute-audio'])
        results, failure = [], None
        try:
            results = run_media_checks(browser)
            for result in results:
                print(result)
        except Exception as error:
            failure = type(error).__name__ + ': ' + str(error)
            raise
        finally:
            output = ROOT / 'docs/test-results/media.json'
            output.parent.mkdir(exist_ok=True)
            output.write_text(json.dumps({'status': 'passed' if failure is None else 'failed',
                'environment': 'Chromium; offline synthetic silence; not Windows WebView2, real CDN, or long-session acceptance',
                'browser': browser.version, 'platform': platform.platform(), 'results': results, 'failure': failure}, indent=2))
            browser.close()
