package library

import "testing"

func TestNormalizeFanha(t *testing.T) {
	cases := []struct {
		stem      string
		canonical string
		ok        bool
	}{
		{"IPZZ-001", "ipzz-001", true},
		{"ipzz-001", "ipzz-001", true},
		{"  IPZZ-001  ", "ipzz-001", true},
		// 修饰后缀共享同一份元数据，必须归一到同一个番号
		{"IPZZ-001-c", "ipzz-001", true},
		{"IPZZ-001-4K", "ipzz-001", true},
		{"IPZZ-001_1", "ipzz-001", true},
		{"1pondo-123456", "1pondo-123456", true},
		{"SIRO-4321-C", "siro-4321", true},
		// 提取不出番号的应被标记失败，而不是猜一个出来
		{"演员名字", "", false},
		{"", "", false},
		{"IPZZ", "", false},
		{"-001", "", false},
	}

	for _, tc := range cases {
		canonical, ok := NormalizeFanha(tc.stem)
		if ok != tc.ok || canonical != tc.canonical {
			t.Errorf("NormalizeFanha(%q) = (%q, %v)，期望 (%q, %v)",
				tc.stem, canonical, ok, tc.canonical, tc.ok)
		}
	}
}

// TestNormalizeFanhaCollapsesModifiers 保证同一部作品的不同后缀文件归一到同一个番号。
//
// 既有实现是 mp4.stem.lower().replace("-c", "")：那不是归一规则，而是一次字符串
// 替换 —— 它只认 -c 这一种修饰，-4k、-1 会原样留在番号里。结果是同一部作品
// 产生多个匹配键，被重复刮削、重复入库，而界面上看起来是两个不同的条目。
func TestNormalizeFanhaCollapsesModifiers(t *testing.T) {
	const want = "ipzz-001"
	for _, stem := range []string{
		"IPZZ-001",
		"IPZZ-001-c",
		"IPZZ-001-4K",
		"IPZZ-001-1",
		"ipzz-001-c-2",
	} {
		canonical, ok := NormalizeFanha(stem)
		if !ok {
			t.Errorf("%s 应当能被解析", stem)
			continue
		}
		if canonical != want {
			t.Errorf("%s 归一为 %q，期望 %q", stem, canonical, want)
		}
	}
}

func TestIsVideoFile(t *testing.T) {
	for _, name := range []string{"a.mp4", "A.MP4", "b.mkv", "c.wmv"} {
		if !IsVideoFile(name) {
			t.Errorf("%s 应被识别为视频", name)
		}
	}
	for _, name := range []string{"a.jpg", "a.srt", "a.mp4.txt", "mp4"} {
		if IsVideoFile(name) {
			t.Errorf("%s 不应被识别为视频", name)
		}
	}
}

func TestArtRelPrefixMatchesLayout(t *testing.T) {
	// 图片相对路径以演员目录为基准，前缀必须是 meta/<stem>，
	// 因为边车 JSON 里的 cover 字段用的是这个基准。
	if got := ArtRelPrefix("IPZZ-001"); got != "meta/IPZZ-001" {
		t.Fatalf("ArtRelPrefix = %q，期望 meta/IPZZ-001", got)
	}
}
