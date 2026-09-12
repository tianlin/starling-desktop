"""Offline fixture generator, not a test-time dependency.

Uses already-installed soundfile/numpy for MP3 and local Chromium's MediaRecorder
for AAC in MP4. Only a silent Web Audio buffer enters the recorder; no microphone,
network media, or speaker output is used. Generated fixtures contain no user data.
"""
import base64
import os
import pathlib
import struct

import numpy
import soundfile
from playwright.sync_api import sync_playwright

OUT = pathlib.Path(__file__).with_name('media-fixtures')
OUT.mkdir(exist_ok=True)
soundfile.write(OUT / 'silence.mp3', numpy.zeros(44100 * 6), 44100, format='MP3', subtype='MPEG_LAYER_III')
with sync_playwright() as pw:
    browser = pw.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH'), headless=True, args=['--mute-audio'])
    page = browser.new_page()
    encoded = page.evaluate('''async () => {
      const type = 'audio/mp4;codecs=mp4a.40.2';
      if (!MediaRecorder.isTypeSupported(type)) throw Error('Local AAC encoder unavailable');
      const context = new AudioContext({sampleRate:44100});
      const destination = context.createMediaStreamDestination();
      const source = context.createBufferSource();
      source.buffer = context.createBuffer(1, 44100 * 6, 44100);
      source.connect(destination);
      const chunks = [];
      const recorder = new MediaRecorder(destination.stream, {mimeType:type, audioBitsPerSecond:64000});
      const stopped = new Promise((resolve,reject) => {
        recorder.ondataavailable = event => chunks.push(event.data);
        recorder.onerror = reject;
        recorder.onstop = resolve;
      });
      source.onended = () => recorder.stop();
      await context.resume();
      recorder.start(); source.start(); await stopped;
      destination.stream.getTracks().forEach(track => track.stop());
      await context.close();
      const bytes = new Uint8Array(await new Blob(chunks).arrayBuffer());
      return btoa(Array.from(bytes, byte => String.fromCharCode(byte)).join(''));
    }''')
    browser.close()
mp4 = base64.b64decode(encoded)
(OUT / 'silence.m4a').write_bytes(mp4)
# Guard the ADTS constants below against a local encoder choosing a new format.
entry = mp4.index(b'mp4a')
if int.from_bytes(mp4[entry + 20:entry + 22], 'big') != 2 or int.from_bytes(mp4[entry + 28:entry + 32], 'big') != 44100 << 16:
    raise ValueError('Generated AAC is not stereo at 44100 Hz')
descriptor = mp4[mp4.index(b'esds'):]
if b'\x05\x80\x80\x80\x02\x12\x10' not in descriptor[:80]:
    raise ValueError('Generated AAC AudioSpecificConfig is not AAC LC at 44100 Hz stereo')


def boxes(data, offset=0):
    while offset < len(data):
        size, kind = struct.unpack_from('>I4s', data, offset)
        if size < 8 or offset + size > len(data):
            raise ValueError('Invalid generated MP4 box')
        yield kind, data[offset + 8:offset + size]
        offset += size


# Chromium's generated fragmented MP4 stores each AAC packet size in trun.
# Extract only the samples described by those tables and wrap them in ADTS.
packet_sizes = []
media = bytearray()
for kind, payload in boxes(mp4):
    if kind == b'moof':
        for subkind, subdata in boxes(payload):
            if subkind != b'traf':
                continue
            for runkind, run in boxes(subdata):
                if runkind != b'trun':
                    continue
                flags = int.from_bytes(run[1:4], 'big')
                if not flags & 0x200:
                    raise ValueError('Generated MP4 omits per-sample sizes')
                count = int.from_bytes(run[4:8], 'big')
                offset = 8 + (4 if flags & 1 else 0) + (4 if flags & 4 else 0)
                for _ in range(count):
                    if flags & 0x100:
                        offset += 4
                    packet_sizes.append(int.from_bytes(run[offset:offset + 4], 'big'))
                    offset += 4
                    offset += 4 if flags & 0x400 else 0
                    offset += 4 if flags & 0x800 else 0
    elif kind == b'mdat':
        media.extend(payload)
if not packet_sizes or sum(packet_sizes) != len(media):
    raise ValueError('Generated MP4 sample tables do not match media bytes')
adts = bytearray()
offset = 0
for size in packet_sizes:
    length = size + 7
    if length >= 8192:
        raise ValueError('AAC packet exceeds ADTS limit')
    # AAC LC, 44100 Hz, stereo: MediaStreamDestination always outputs two channels.
    adts.extend([0xff, 0xf1, 0x50, 0x80 | (length >> 11), (length >> 3) & 0xff, ((length & 7) << 5) | 0x1f, 0xfc])
    adts.extend(media[offset:offset + size])
    offset += size
(OUT / 'silence.aac').write_bytes(adts)
for path in sorted(OUT.glob('silence.*')):
    print(path.name, path.stat().st_size, 'bytes; generated silence')
