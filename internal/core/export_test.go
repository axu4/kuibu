package core

import "testing"

func TestMarkdownToHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{"## 本周完成\n### 产品线A", "<h2>本周完成</h2><h3>产品线A</h3>"},
		{"- a\n- b", "<ul><li>a</li><li>b</li></ul>"},
		{"- a\n  - a1\n  - a2\n- b", "<ul><li>a<ul><li>a1</li><li>a2</li></ul></li><li>b</li></ul>"},
		{"- a\n    - 跳级\n- b", "<ul><li>a<ul><li>跳级</li></ul></li><li>b</li></ul>"},
		{"1. 一\n2. 二", "<ol><li>一</li><li>二</li></ol>"},
		{"**加粗** 和 *斜体* 和 `x<y`", "<p><strong>加粗</strong> 和 <em>斜体</em> 和 <code>x&lt;y</code></p>"},
		{"见 [PR](https://github.com/a/b/pull/1)", `<p>见 <a href="https://github.com/a/b/pull/1">PR</a></p>`},
		{"> 未纳入：王五\n\n段落一\n段落二", "<blockquote>未纳入：王五</blockquote><p>段落一<br>段落二</p>"},
		{"- 项\n## 标题", "<ul><li>项</li></ul><h2>标题</h2>"},
		{"<script>", "<p>&lt;script&gt;</p>"},
	}
	for _, c := range cases {
		if got := MarkdownToHTML(c.in); got != c.want {
			t.Errorf("MarkdownToHTML(%q)\n got  %s\n want %s", c.in, got, c.want)
		}
	}
}

func TestMarkdownToText(t *testing.T) {
	in := "> 未纳入：王五\n\n## 本周完成\n### 产品线A\n- **接口**联调（张三）\n  - 见 [文档](https://x.y)\n\n## 风险\n- 无"
	want := "未纳入：王五\n\n本周完成\n产品线A\n• 接口联调（张三）\n  • 见 文档（https://x.y）\n\n风险\n• 无\n"
	if got := MarkdownToText(in); got != want {
		t.Errorf("MarkdownToText\n got  %q\n want %q", got, want)
	}
}
