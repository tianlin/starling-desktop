package security

import "testing"

func TestParseShare(t *testing.T) {
	valid := []string{"https://www.xiaoyuzhoufm.com/episode/64db2d493fa4090b744c313c", "来听这一期 https://www.xiaoyuzhoufm.com/podcast/64db2d493fa4090b744c313c?source=share ，很好"}
	for _, s := range valid {
		if _, err := ParseShare(s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	invalid := []string{"http://www.xiaoyuzhoufm.com/episode/64db2d493fa4090b744c313c", "https://www.xiaoyuzhoufm.com.evil.test/episode/64db2d493fa4090b744c313c", "https://user@www.xiaoyuzhoufm.com/episode/64db2d493fa4090b744c313c", "https://www.xiaoyuzhoufm.com:8443/episode/64db2d493fa4090b744c313c", "file:///etc/passwd", "https://www.xiaoyuzhoufm.com/episode/../x", "https://www.xiaoyuzhoufm.com/episode/not-an-id", "https://www.xiaoyuzhoufm.com/episode/64db2d493fa4090b744c313c/extra", "https://www.xiaoyuzhoufm.com/%65pisode/64db2d493fa4090b744c313c"}
	for _, s := range invalid {
		if _, err := ParseShare(s); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
}
func TestMediaAllowlist(t *testing.T) {
	for _, s := range []string{"https://media.xyzcdn.net/a.m4a", "https://media.xyzcdn.net/a.mp3?signature=abc"} {
		if ValidateMediaURL(s) != nil {
			t.Errorf("rejected %s", s)
		}
	}
	for _, s := range []string{"http://media.xyzcdn.net/a.mp3", "https://media.xyzcdn.net.evil.test/a", "https://127.0.0.1/a", "https://169.254.169.254/", "https://media.xyzcdn.net:99/a", "https://a:b@media.xyzcdn.net/a", "file:///a", "javascript:alert(1)", "https://evil.test/a"} {
		if ValidateMediaURL(s) == nil {
			t.Errorf("accepted %s", s)
		}
	}
}
func TestExternalURL(t *testing.T) {
	for _, s := range []string{"javascript:alert(1)", "https://127.0.0.1/", "https://localhost/a", "https://[::1]/", "https://a:b@example.org/"} {
		if ValidateExternalURL(s) == nil {
			t.Error(s)
		}
	}
	if ValidateExternalURL("https://example.org/notes") != nil {
		t.Fatal("valid https")
	}
}
