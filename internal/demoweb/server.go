// Package demoweb serves an explicitly synthetic UI test harness. Never imported by desktop.
package demoweb

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"starling/internal/app"
	"starling/internal/model"
	"starling/internal/store"
	"starling/internal/testkit"
	"strings"
	"time"
)

type Server struct {
	app                    *app.Service
	db                     *store.Store
	temp, dir, host, token string
	audio                  []byte
}

func New(dir, host string) (*Server, error) {
	temp, e := os.MkdirTemp("", "starling-demo-")
	if e != nil {
		return nil, e
	}
	db, e := store.Open(filepath.Join(temp, "demo.db"))
	if e != nil {
		os.RemoveAll(temp)
		return nil, e
	}
	token := make([]byte, 32)
	if _, e = rand.Read(token); e != nil {
		db.Close()
		os.RemoveAll(temp)
		return nil, e
	}
	f := &testkit.Fake{}
	installCommentFixtures(f)
	f.ListFunc = func(ctx context.Context, token, kind, pid, cursor string) (model.Page, error) {
		if kind == "updates" {
			return updatesPage(cursor)
		}
		var items []model.Item
		if kind == "subscriptions" {
			for i := 0; i < 4; i++ {
				it := sample(i)
				it.Kind = "podcast"
				it.ID = podcastID(i)
				it.Title = programs[i]
				it.SourceURL = "https://www.xiaoyuzhoufm.com/podcast/" + it.ID
				items = append(items, it)
			}
		} else {
			for i := 0; i < 8; i++ {
				items = append(items, sample(i))
			}
		}
		if cursor == "" {
			return model.Page{Items: items[:3], Cursor: "demo-page-2", Complete: false}, nil
		}
		if cursor != "demo-page-2" {
			return model.Page{}, model.Err("PAGINATION", "演示游标无效。")
		}
		return model.Page{Items: items[3:], Complete: true}, nil
	}
	f.DetailFunc = func(ctx context.Context, token, kind, id string) (model.Item, error) {
		if kind == "episode" {
			for i := 0; i < 85; i++ {
				if it := updateSample(i); it.ID == id {
					return it, nil
				}
			}
		}
		for i := 0; i < 8; i++ {
			it := sample(i)
			if id == it.ID && kind == "episode" {
				return it, nil
			}
			if id == podcastID(i%4) && kind == "podcast" {
				it.Kind = "podcast"
				it.ID = id
				it.Title = programs[i%4]
				it.SourceURL = "https://www.xiaoyuzhoufm.com/podcast/" + id
				return it, nil
			}
		}
		return model.Item{}, model.Err("NOT_FOUND", "演示仅包含合成内容，请从当前列表选择。")
	}
	a := app.New(f, db, &testkit.MemoryVault{})
	epoch := a.Session().Epoch
	for i := 0; i < 3; i++ {
		_ = a.SaveProgress(epoch, model.Progress{Item: sample(i), Position: float64(12 + i*17), Duration: 120})
	}
	_, _ = a.Bookmarks(epoch, "append", sample(1))
	_, _ = a.Queue(epoch, "append", sample(2))
	return &Server{app: a, db: db, temp: temp, dir: dir, host: host, token: hex.EncodeToString(token), audio: wave()}, nil
}

var programs = []string{"慢慢生活", "城市切片", "山间来信", "未完成的对话"}
var titles = []string{"001｜把周末还给自己：在城市里找回慢节奏", "002｜你听见了吗？街角那些被忽略的声音", "003｜在山里住了七天，我们重新认识了时间", "004｜工作之外，如何保留一件真正喜欢的事", "005｜一杯咖啡的时间，和朋友聊聊最近", "006｜走一条没走过的路，遇见城市的另一面", "007｜允许自己留白，也是一种认真生活", "008｜书页里的远方：读书、旅行与新的开始"}

func podcastID(i int) string { return fmt.Sprintf("64db2d493fa4090b744c41%02d", i) }
func sample(i int) model.Item {
	id := fmt.Sprintf("64db2d493fa4090b744c31%02d", i)
	return model.Item{Kind: "episode", ID: id, Title: titles[i], PodcastID: podcastID(i % 4), PodcastTitle: programs[i%4], Description: "合成演示内容 · 一段关于日常、好奇心与生活节奏的虚构对话，不是真实播客。", ShowNotes: `<p>这是用于界面和播放测试的合成内容，不连接小宇宙。</p><h3>本期时间点</h3><p>00:10 开场：把时间留给自己</p><p>00:45 城市里的慢生活</p><p>01:20 认真听见日常</p><p><strong>测试音频为 120 秒低音量合成声，不含真实播客。</strong></p><img src="https://evil.invalid/image" onerror="window.__xss=1"><script>window.__xss=1</script><a href="javascript:window.__xss=1">危险链接应当被清洗</a>`, Duration: 120, Published: fmt.Sprintf("2026-09-%02dT08:00:00Z", 10-i), SourceURL: "https://www.xiaoyuzhoufm.com/episode/" + id, MediaURL: "https://media.xyzcdn.net/fixture.wav"}
}
func (s *Server) Close() { s.app.Close(); s.db.Close(); os.RemoveAll(s.temp) }
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	if r.Host != s.host {
		http.Error(w, "invalid host", 403)
		return
	}
	if r.URL.Path == "/api" || r.URL.Path == "/demo-config.js" {
		site := r.Header.Get("Sec-Fetch-Site")
		if site != "" && site != "same-origin" && site != "none" {
			http.Error(w, "cross origin blocked", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+s.host {
			http.Error(w, "cross origin blocked", 403)
			return
		}
	}
	switch r.URL.Path {
	case "/api":
		if r.Method != "POST" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Starling-Demo")), []byte(s.token)) != 1 {
			http.Error(w, "capability required", 403)
			return
		}
		var req struct {
			Action  string          `json:"action"`
			Payload json.RawMessage `json:"payload"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		d.DisallowUnknownFields()
		if d.Decode(&req) != nil {
			http.Error(w, "bad request", 400)
			return
		}
		var trailing any
		if d.Decode(&trailing) != io.EOF {
			http.Error(w, "trailing input", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if strings.HasPrefix(req.Action, "desktop.") {
			switch req.Action {
			case "desktop.info":
				io.WriteString(w, `{"ok":true,"data":{"tray":false,"mediaKey":false,"demo":true}}`)
			default:
				io.WriteString(w, `{"ok":false,"error":{"code":"DEMO_ONLY","message":"演示不执行外部打开、文件导出或桌面系统操作。"}}`)
			}
			return
		}
		raw := s.app.Dispatch(r.Context(), req.Action, string(req.Payload))
		// Only this test harness replaces the approved fixture URL with its fixed local test audio.
		if req.Action == "playback.resolve" {
			var v struct {
				OK    bool            `json:"ok"`
				Data  model.Playback  `json:"data"`
				Error *model.AppError `json:"error,omitempty"`
			}
			if json.Unmarshal([]byte(raw), &v) == nil && v.OK {
				v.Data.URL = "/fixture.wav"
				b, _ := json.Marshal(v)
				raw = string(b)
			}
		}
		io.WriteString(w, raw)
	case "/demo-config.js":
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprintf(w, "window.__STARLING_DEMO__={token:%q};", s.token)
	case "/fixture.wav":
		w.Header().Set("Content-Type", "audio/wav")
		http.ServeContent(w, r, "fixture.wav", time.Time{}, bytes.NewReader(s.audio))
	case "/", "/index.html":
		b, e := os.ReadFile(filepath.Join(s.dir, "index.html"))
		if e != nil {
			http.Error(w, "Build frontend first: cd frontend && npm run build", 503)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; media-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		io.WriteString(w, strings.Replace(string(b), `<script type="module"`, `<script src="/demo-config.js"></script><script type="module"`, 1))
	default:
		if r.Method != "GET" || strings.Contains(r.URL.Path, "..") || (filepath.Ext(r.URL.Path) != ".js" && filepath.Ext(r.URL.Path) != ".css") {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(s.dir, filepath.Base(r.URL.Path)))
	}
}
func wave() []byte {
	const rate = 8000
	const duration = 120
	n := rate * duration
	b := make([]byte, 44+n*2)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], rate)
	binary.LittleEndian.PutUint32(b[28:], rate*2)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(n*2))
	for i := 0; i < n; i++ {
		v := int16(110 * math.Sin(2*math.Pi*220*float64(i)/rate))
		binary.LittleEndian.PutUint16(b[44+i*2:], uint16(v))
	}
	return b
}
