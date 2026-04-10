// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package transform

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Relative URL resolution
// ---------------------------------------------------------------------------

func TestHTMLToMD_RelativeLinks_WithBase(t *testing.T) {
	result := htmlToMDWithBase(
		`<a href="user?id=alice">alice</a> | <a href="/about">About</a> | <a href="https://example.com">Absolute</a>`,
		"https://news.ycombinator.com",
	)
	if !strings.Contains(result, "[alice](https://news.ycombinator.com/user?id=alice)") {
		t.Errorf("relative query link not resolved, got %q", result)
	}
	if !strings.Contains(result, "[About](https://news.ycombinator.com/about)") {
		t.Errorf("root-relative link not resolved, got %q", result)
	}
	if !strings.Contains(result, "[Absolute](https://example.com)") {
		t.Errorf("absolute link should remain unchanged, got %q", result)
	}
}

func TestHTMLToMD_RelativeImages_WithBase(t *testing.T) {
	result := htmlToMDWithBase(
		`<img src="/images/photo.jpg" alt="Photo">`,
		"https://example.com",
	)
	if !strings.Contains(result, "![Photo](https://example.com/images/photo.jpg)") {
		t.Errorf("relative image not resolved, got %q", result)
	}
}

func TestHTMLToMD_NoBase_KeepsRelative(t *testing.T) {
	result := htmlToMD(`<a href="user?id=alice">alice</a>`)
	if !strings.Contains(result, "[alice](user?id=alice)") {
		t.Errorf("without base URL, relative links should stay as-is, got %q", result)
	}
}

func TestHTMLToMD_MailtoNotResolved(t *testing.T) {
	result := htmlToMDWithBase(
		`<a href="mailto:hello@example.com">Email</a>`,
		"https://example.com",
	)
	if !strings.Contains(result, "[Email](mailto:hello@example.com)") {
		t.Errorf("mailto should not be resolved, got %q", result)
	}
}

func TestHTMLToMD_BaseWithPath(t *testing.T) {
	result := htmlToMDWithBase(
		`<a href="details">Details</a>`,
		"https://example.com/items/123",
	)
	if !strings.Contains(result, "https://example.com/items/details") {
		t.Errorf("relative link should resolve against base path, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// Visibility: display:none, visibility:hidden, aria-hidden, hidden attr
// ---------------------------------------------------------------------------

func TestHTMLToMD_DisplayNone(t *testing.T) {
	result := htmlToMD(`<p>visible</p><div style="display:none">hidden content</div><p>also visible</p>`)
	if strings.Contains(result, "hidden content") {
		t.Errorf("display:none content should be stripped, got %q", result)
	}
	if !strings.Contains(result, "visible") {
		t.Errorf("visible content missing, got %q", result)
	}
}

func TestHTMLToMD_DisplayNoneCaseInsensitive(t *testing.T) {
	result := htmlToMD(`<div style="Display: None;">hidden</div><p>visible</p>`)
	if strings.Contains(result, "hidden") {
		t.Errorf("Display:None (mixed case) should be stripped, got %q", result)
	}
}

func TestHTMLToMD_VisibilityHidden(t *testing.T) {
	result := htmlToMD(`<span style="visibility: hidden">invisible</span><p>visible</p>`)
	if strings.Contains(result, "invisible") {
		t.Errorf("visibility:hidden content should be stripped, got %q", result)
	}
}

func TestHTMLToMD_AriaHidden(t *testing.T) {
	result := htmlToMD(`<span aria-hidden="true">icon-glyph</span> Settings`)
	if strings.Contains(result, "icon-glyph") {
		t.Errorf("aria-hidden content should be stripped, got %q", result)
	}
	if !strings.Contains(result, "Settings") {
		t.Errorf("visible text missing, got %q", result)
	}
}

func TestHTMLToMD_HiddenAttribute(t *testing.T) {
	result := htmlToMD(`<div hidden>secret</div><p>public</p>`)
	if strings.Contains(result, "secret") {
		t.Errorf("hidden attribute content should be stripped, got %q", result)
	}
	if !strings.Contains(result, "public") {
		t.Errorf("visible content missing, got %q", result)
	}
}

func TestHTMLToMD_CookieBanner(t *testing.T) {
	html := `<div class="content"><p>Article text here.</p></div>
	<div style="display:none" id="cookie-banner"><p>We use cookies. Accept?</p><button>Accept</button></div>`
	result := htmlToMD(html)
	if strings.Contains(result, "cookies") {
		t.Errorf("cookie banner should be hidden, got %q", result)
	}
	if !strings.Contains(result, "Article text") {
		t.Errorf("main content missing, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// Lazy-loaded images
// ---------------------------------------------------------------------------

func TestHTMLToMD_LazyImage_DataSrc(t *testing.T) {
	result := htmlToMD(`<img data-src="real-photo.jpg" src="placeholder.gif" alt="Photo">`)
	// Should use data-src since src looks like a placeholder
	if !strings.Contains(result, "real-photo.jpg") {
		t.Errorf("expected data-src to be used for lazy image, got %q", result)
	}
}

func TestHTMLToMD_LazyImage_DataURI(t *testing.T) {
	result := htmlToMD(`<img src="data:image/gif;base64,R0lGODlhAQABAIAAAP" data-src="actual.png" alt="Test">`)
	if !strings.Contains(result, "actual.png") {
		t.Errorf("expected data-src fallback for data URI placeholder, got %q", result)
	}
}

func TestHTMLToMD_LazyImage_NoDataSrc(t *testing.T) {
	// Normal image without lazy loading should work as before
	result := htmlToMD(`<img src="normal.jpg" alt="Normal">`)
	if !strings.Contains(result, "![Normal](normal.jpg)") {
		t.Errorf("expected normal image rendering, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// Semantic HTML5
// ---------------------------------------------------------------------------

func TestHTMLToMD_Del_Strikethrough(t *testing.T) {
	result := htmlToMD(`<p>Price: <del>$50</del> $30</p>`)
	if !strings.Contains(result, "~~$50~~") {
		t.Errorf("expected strikethrough, got %q", result)
	}
}

func TestHTMLToMD_Mark_Highlight(t *testing.T) {
	result := htmlToMD(`<p>This is <mark>highlighted</mark> text.</p>`)
	if !strings.Contains(result, "==highlighted==") {
		t.Errorf("expected highlight marks, got %q", result)
	}
}

func TestHTMLToMD_Abbr(t *testing.T) {
	result := htmlToMD(`<p>The <abbr title="World Wide Web">WWW</abbr> is great.</p>`)
	if !strings.Contains(result, "WWW (World Wide Web)") {
		t.Errorf("expected abbreviation expansion, got %q", result)
	}
}

func TestHTMLToMD_FigureFigcaption(t *testing.T) {
	result := htmlToMD(`<figure><img src="chart.png" alt="Chart"><figcaption>Sales data for Q4</figcaption></figure>`)
	if !strings.Contains(result, "![Chart](chart.png)") {
		t.Errorf("expected image in figure, got %q", result)
	}
	if !strings.Contains(result, "*Sales data for Q4*") {
		t.Errorf("expected italic figcaption, got %q", result)
	}
}

func TestHTMLToMD_DetailsSummary(t *testing.T) {
	result := htmlToMD(`<details><summary>Click to expand</summary><p>Hidden details content.</p></details>`)
	if !strings.Contains(result, "**Click to expand**") {
		t.Errorf("expected bold summary, got %q", result)
	}
	if !strings.Contains(result, "Hidden details content") {
		t.Errorf("expected details content, got %q", result)
	}
}

func TestHTMLToMD_Sup(t *testing.T) {
	result := htmlToMD(`<p>E = mc<sup>2</sup></p>`)
	if !strings.Contains(result, "^(2)") {
		t.Errorf("expected superscript, got %q", result)
	}
}

func TestHTMLToMD_Sub(t *testing.T) {
	result := htmlToMD(`<p>H<sub>2</sub>O</p>`)
	if !strings.Contains(result, "~(2)") {
		t.Errorf("expected subscript, got %q", result)
	}
}

func TestHTMLToMD_Button(t *testing.T) {
	result := htmlToMD(`<button>Subscribe Now</button>`)
	if !strings.Contains(result, "[Subscribe Now]") {
		t.Errorf("expected button text in brackets, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// Embedded content: iframe, SVG, form elements
// ---------------------------------------------------------------------------

func TestHTMLToMD_Iframe_Stripped(t *testing.T) {
	result := htmlToMD(`<p>Watch:</p><iframe src="https://youtube.com/embed/abc"></iframe><p>End.</p>`)
	if strings.Contains(result, "youtube") {
		t.Errorf("iframe should be stripped, got %q", result)
	}
	if !strings.Contains(result, "Watch:") || !strings.Contains(result, "End.") {
		t.Errorf("surrounding content missing, got %q", result)
	}
}

func TestHTMLToMD_SVG_Stripped(t *testing.T) {
	result := htmlToMD(`<p>Icon: <svg viewBox="0 0 24 24"><path d="M12 2L2 22h20z"/></svg> text</p>`)
	if strings.Contains(result, "viewBox") || strings.Contains(result, "path") {
		t.Errorf("SVG content should be stripped, got %q", result)
	}
	if !strings.Contains(result, "Icon:") {
		t.Errorf("surrounding text missing, got %q", result)
	}
}

func TestHTMLToMD_FormInputs_Stripped(t *testing.T) {
	result := htmlToMD(`<form><input type="text" placeholder="Name"><textarea>Content</textarea><select><option>A</option></select></form>`)
	if strings.Contains(result, "placeholder") || strings.Contains(result, "Content") {
		t.Errorf("form elements should be stripped, got %q", result)
	}
}

func TestHTMLToMD_HTMLComments(t *testing.T) {
	result := htmlToMD(`<p>visible</p><!-- This is a comment --><p>also visible</p>`)
	if strings.Contains(result, "comment") {
		t.Errorf("HTML comments should be stripped, got %q", result)
	}
	if !strings.Contains(result, "visible") {
		t.Errorf("visible content missing, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// Character encoding edge cases
// ---------------------------------------------------------------------------

func TestHTMLToMD_Emoji(t *testing.T) {
	result := htmlToMD(`<p>Hello 🌍 World 🎉</p>`)
	if !strings.Contains(result, "🌍") || !strings.Contains(result, "🎉") {
		t.Errorf("emoji should be preserved, got %q", result)
	}
}

func TestHTMLToMD_Unicode(t *testing.T) {
	result := htmlToMD(`<p>Héllo Wörld — café résumé</p>`)
	if !strings.Contains(result, "Héllo") || !strings.Contains(result, "café") {
		t.Errorf("unicode characters should be preserved, got %q", result)
	}
}

func TestHTMLToMD_ChineseJapaneseKorean(t *testing.T) {
	result := htmlToMD(`<h1>你好世界</h1><p>こんにちは 안녕하세요</p>`)
	if !strings.Contains(result, "# 你好世界") {
		t.Errorf("Chinese text should be preserved in heading, got %q", result)
	}
	if !strings.Contains(result, "こんにちは") {
		t.Errorf("Japanese text should be preserved, got %q", result)
	}
}

func TestHTMLToMD_NumericEntities(t *testing.T) {
	result := htmlToMD(`<p>&#8220;quoted&#8221; &#169; 2024</p>`)
	if !strings.Contains(result, "\u201c") || !strings.Contains(result, "\u201d") {
		t.Errorf("numeric entities should be decoded, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// Whitespace handling
// ---------------------------------------------------------------------------

func TestHTMLToMD_ExcessiveWhitespace(t *testing.T) {
	result := htmlToMD(`<p>  lots   of    spaces   here  </p>`)
	if strings.Contains(result, "   ") {
		t.Errorf("excessive spaces should be collapsed, got %q", result)
	}
	if !strings.Contains(result, "lots of spaces here") {
		t.Errorf("expected collapsed whitespace, got %q", result)
	}
}

func TestHTMLToMD_PreservesPreWhitespace(t *testing.T) {
	result := htmlToMD(`<pre>  line 1
  line 2
    indented</pre>`)
	if !strings.Contains(result, "  line 1") {
		t.Errorf("pre whitespace should be preserved, got %q", result)
	}
	if !strings.Contains(result, "    indented") {
		t.Errorf("pre indentation should be preserved, got %q", result)
	}
}

func TestHTMLToMD_NestedDivsNoExcessNewlines(t *testing.T) {
	result := htmlToMD(`<div><div><div><div><p>Deep content</p></div></div></div></div>`)
	// Should not produce more than 2 consecutive newlines
	if strings.Contains(result, "\n\n\n") {
		t.Errorf("nested divs should not produce excessive newlines, got %q", result)
	}
	if !strings.Contains(result, "Deep content") {
		t.Errorf("nested content missing, got %q", result)
	}
}

func TestHTMLToMD_NBSPChain(t *testing.T) {
	result := htmlToMD(`<p>word1&nbsp;&nbsp;&nbsp;word2</p>`)
	// Multiple nbsp should produce spaces, but not break
	if !strings.Contains(result, "word1") || !strings.Contains(result, "word2") {
		t.Errorf("expected both words, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// Structural edge cases
// ---------------------------------------------------------------------------

func TestHTMLToMD_MalformedUnclosedTags(t *testing.T) {
	// golang.org/x/net/html auto-closes tags
	result := htmlToMD(`<p>First paragraph<p>Second paragraph<p>Third`)
	if !strings.Contains(result, "First paragraph") {
		t.Errorf("first paragraph missing, got %q", result)
	}
	if !strings.Contains(result, "Second paragraph") {
		t.Errorf("second paragraph missing, got %q", result)
	}
	if !strings.Contains(result, "Third") {
		t.Errorf("third paragraph missing, got %q", result)
	}
}

func TestHTMLToMD_SelfClosingTags(t *testing.T) {
	result := htmlToMD(`<p>Before<br/>After</p><hr/><p>End</p>`)
	if !strings.Contains(result, "Before") || !strings.Contains(result, "After") {
		t.Errorf("content around br missing, got %q", result)
	}
	if !strings.Contains(result, "---") {
		t.Errorf("hr should produce ---, got %q", result)
	}
}

func TestHTMLToMD_EmptyDocument(t *testing.T) {
	result := htmlToMD("")
	if strings.TrimSpace(result) != "" {
		t.Errorf("empty input should produce empty output, got %q", result)
	}
}

func TestHTMLToMD_PlainText(t *testing.T) {
	result := htmlToMD("Just plain text, no HTML")
	if !strings.Contains(result, "Just plain text, no HTML") {
		t.Errorf("plain text should pass through, got %q", result)
	}
}

func TestHTMLToMD_NestedLists(t *testing.T) {
	result := htmlToMD(`<ul><li>Top level<ul><li>Nested item</li><li>Another nested</li></ul></li><li>Back to top</li></ul>`)
	if !strings.Contains(result, "- Top level") {
		t.Errorf("top level item missing, got %q", result)
	}
	if !strings.Contains(result, "  - Nested item") {
		t.Errorf("nested item should be indented, got %q", result)
	}
}

func TestHTMLToMD_MixedListTypes(t *testing.T) {
	result := htmlToMD(`<ul><li>Unordered</li></ul><ol><li>Ordered</li></ol>`)
	if !strings.Contains(result, "- Unordered") {
		t.Errorf("unordered item missing, got %q", result)
	}
	if !strings.Contains(result, "1. Ordered") {
		t.Errorf("ordered item missing, got %q", result)
	}
}

func TestHTMLToMD_Blockquote_Nested(t *testing.T) {
	result := htmlToMD(`<blockquote><p>First quote</p><blockquote><p>Nested quote</p></blockquote></blockquote>`)
	if !strings.Contains(result, "> ") {
		t.Errorf("expected blockquote marker, got %q", result)
	}
	if !strings.Contains(result, "First quote") {
		t.Errorf("first quote missing, got %q", result)
	}
}

func TestHTMLToMD_LinkWithinHeading(t *testing.T) {
	result := htmlToMD(`<h2><a href="/docs">Documentation</a></h2>`)
	if !strings.Contains(result, "## [Documentation](/docs)") {
		t.Errorf("expected linked heading, got %q", result)
	}
}

func TestHTMLToMD_MultipleCodeBlocks(t *testing.T) {
	result := htmlToMD(`<pre><code>block1</code></pre><p>middle</p><pre><code>block2</code></pre>`)
	count := strings.Count(result, "```")
	if count != 4 { // 2 fences x 2 blocks
		t.Errorf("expected 4 code fences, got %d in %q", count, result)
	}
}

// ---------------------------------------------------------------------------
// Framework-specific patterns
// ---------------------------------------------------------------------------

func TestHTMLToMD_ReactWrapper(t *testing.T) {
	html := `<div id="__next" data-reactroot=""><div class="container"><h1>Title</h1><p>Content</p></div></div>`
	result := htmlToMD(html)
	if !strings.Contains(result, "# Title") {
		t.Errorf("expected heading through React wrappers, got %q", result)
	}
	if !strings.Contains(result, "Content") {
		t.Errorf("expected content through React wrappers, got %q", result)
	}
}

func TestHTMLToMD_VueWrapper(t *testing.T) {
	html := `<div id="app" data-v-abc123><div data-v-abc123 class="main"><h1 data-v-abc123>Title</h1></div></div>`
	result := htmlToMD(html)
	if !strings.Contains(result, "# Title") {
		t.Errorf("expected heading through Vue wrappers, got %q", result)
	}
}

func TestHTMLToMD_WordPressPost(t *testing.T) {
	html := `<article class="post type-post">
		<header class="entry-header"><h1 class="entry-title">Blog Post Title</h1></header>
		<div class="entry-content">
			<p>First paragraph of the post.</p>
			<p>Second paragraph with <strong>bold</strong> and <a href="/link">a link</a>.</p>
		</div>
		<footer class="entry-footer"><span class="posted-on">Published 2024-01-01</span></footer>
	</article>`
	result := htmlToMD(html)
	if !strings.Contains(result, "# Blog Post Title") {
		t.Errorf("expected post title, got %q", result)
	}
	if !strings.Contains(result, "**bold**") {
		t.Errorf("expected bold text, got %q", result)
	}
	if !strings.Contains(result, "[a link](/link)") {
		t.Errorf("expected link, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// Real-world site patterns
// ---------------------------------------------------------------------------

func TestHTMLToMD_NewsArticle(t *testing.T) {
	html := `<html><body>
		<nav><a href="/">Home</a> | <a href="/news">News</a></nav>
		<article>
			<h1>Breaking: Important Event</h1>
			<p class="byline">By <strong>John Doe</strong> | <time datetime="2024-01-01">Jan 1, 2024</time></p>
			<p>The first paragraph of the article with important details.</p>
			<figure>
				<img src="photo.jpg" alt="Event photo">
				<figcaption>Photo credit: Reuters</figcaption>
			</figure>
			<p>The second paragraph continues the story.</p>
			<blockquote><p>"This is a direct quote from a source."</p></blockquote>
		</article>
		<footer><p>&copy; 2024 News Corp</p></footer>
	</body></html>`
	result := htmlToMD(html)
	if !strings.Contains(result, "# Breaking: Important Event") {
		t.Errorf("headline missing, got %q", result)
	}
	if !strings.Contains(result, "**John Doe**") {
		t.Errorf("byline missing, got %q", result)
	}
	if !strings.Contains(result, "![Event photo](photo.jpg)") {
		t.Errorf("article image missing, got %q", result)
	}
	if !strings.Contains(result, "*Photo credit: Reuters*") {
		t.Errorf("figcaption missing, got %q", result)
	}
	if !strings.Contains(result, "> ") {
		t.Errorf("blockquote missing, got %q", result)
	}
}

func TestHTMLToMD_DocumentationPage(t *testing.T) {
	html := `<div class="docs">
		<h1>API Reference</h1>
		<h2>Authentication</h2>
		<p>All requests require an API key passed via the <code>Authorization</code> header.</p>
		<pre><code>curl -H "Authorization: Bearer YOUR_KEY" https://api.example.com/v1/data</code></pre>
		<h3>Parameters</h3>
		<dl>
			<dt>api_key</dt>
			<dd>Your API key (required)</dd>
		</dl>
		<details>
			<summary>Rate Limits</summary>
			<p>100 requests per minute for free tier.</p>
		</details>
	</div>`
	result := htmlToMD(html)
	if !strings.Contains(result, "# API Reference") {
		t.Errorf("expected h1, got %q", result)
	}
	if !strings.Contains(result, "## Authentication") {
		t.Errorf("expected h2, got %q", result)
	}
	if !strings.Contains(result, "`Authorization`") {
		t.Errorf("expected inline code, got %q", result)
	}
	if !strings.Contains(result, "```") {
		t.Errorf("expected code block, got %q", result)
	}
	if !strings.Contains(result, "**Rate Limits**") {
		t.Errorf("expected details/summary, got %q", result)
	}
}

func TestHTMLToMD_ForumPost(t *testing.T) {
	html := `<div class="post">
		<div class="post-header">
			<span class="username"><a href="/u/alice">alice</a></span>
			<span class="timestamp">2 hours ago</span>
		</div>
		<div class="post-body">
			<p>Has anyone tried the new framework? I found it has these benefits:</p>
			<ul>
				<li>Fast compilation</li>
				<li>Good <em>error messages</em></li>
				<li>Great documentation</li>
			</ul>
			<p>The only downside is <del>poor Windows support</del> (fixed in v2.1).</p>
		</div>
	</div>`
	result := htmlToMD(html)
	if !strings.Contains(result, "[alice](/u/alice)") {
		t.Errorf("expected username link, got %q", result)
	}
	if !strings.Contains(result, "- Fast compilation") {
		t.Errorf("expected list items, got %q", result)
	}
	if !strings.Contains(result, "*error messages*") {
		t.Errorf("expected italic, got %q", result)
	}
	if !strings.Contains(result, "~~poor Windows support~~") {
		t.Errorf("expected strikethrough, got %q", result)
	}
}

func TestHTMLToMD_EcommercePage(t *testing.T) {
	html := `<div class="product">
		<h1>Wireless Headphones</h1>
		<div class="price">
			<span class="original"><del>$99.99</del></span>
			<span class="sale">$59.99</span>
		</div>
		<div class="description">
			<p>Premium wireless headphones with <strong>40-hour battery</strong> life.</p>
			<h3>Features</h3>
			<ul>
				<li>Active noise cancellation</li>
				<li>Bluetooth 5.0</li>
				<li>USB-C charging</li>
			</ul>
		</div>
		<button>Add to Cart</button>
		<div style="display:none" class="modal"><p>Added to cart!</p></div>
	</div>`
	result := htmlToMD(html)
	if !strings.Contains(result, "# Wireless Headphones") {
		t.Errorf("expected product title, got %q", result)
	}
	if !strings.Contains(result, "~~$99.99~~") {
		t.Errorf("expected strikethrough price, got %q", result)
	}
	if !strings.Contains(result, "**40-hour battery**") {
		t.Errorf("expected bold feature, got %q", result)
	}
	if strings.Contains(result, "Added to cart") {
		t.Errorf("hidden modal should be stripped, got %q", result)
	}
	if !strings.Contains(result, "[Add to Cart]") {
		t.Errorf("expected button text, got %q", result)
	}
}

func TestHTMLToMD_BlogWithMedia(t *testing.T) {
	html := `<article>
		<h1>Travel Report</h1>
		<p>Today we visited the ancient ruins.</p>
		<figure>
			<img src="ruins.jpg" alt="Ancient ruins at sunset">
			<figcaption>The ruins were breathtaking at sunset.</figcaption>
		</figure>
		<p>The local guide told us about the history.</p>
		<blockquote>
			<p>These ruins date back to the 3rd century BC.</p>
		</blockquote>
	</article>`
	result := htmlToMD(html)
	if !strings.Contains(result, "# Travel Report") {
		t.Errorf("expected title, got %q", result)
	}
	if !strings.Contains(result, "![Ancient ruins at sunset](ruins.jpg)") {
		t.Errorf("expected image, got %q", result)
	}
	if !strings.Contains(result, "*The ruins were breathtaking at sunset.*") {
		t.Errorf("expected figcaption, got %q", result)
	}
}

func TestHTMLToMD_WikiArticle(t *testing.T) {
	html := `<div class="mw-parser-output">
		<p><b>Go</b> (also known as <b>Golang</b>) is a <a href="/wiki/Programming_language">programming language</a>
		designed at <a href="/wiki/Google">Google</a><sup><a href="#ref1">[1]</a></sup>.</p>
		<h2>History</h2>
		<p>Go was designed in 2007 by Robert Griesemer, Rob Pike, and Ken Thompson.</p>
		<h3>Version 1.0</h3>
		<p>Released in March 2012, marking the language as stable.</p>
	</div>`
	result := htmlToMD(html)
	if !strings.Contains(result, "**Go**") {
		t.Errorf("expected bold term, got %q", result)
	}
	if !strings.Contains(result, "[programming language](/wiki/Programming_language)") {
		t.Errorf("expected wiki link, got %q", result)
	}
	if !strings.Contains(result, "## History") {
		t.Errorf("expected h2, got %q", result)
	}
	if !strings.Contains(result, "### Version 1.0") {
		t.Errorf("expected h3, got %q", result)
	}
	if !strings.Contains(result, "^(") {
		t.Errorf("expected superscript notation, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// Complex integration scenarios
// ---------------------------------------------------------------------------

func TestHTMLToMD_FullPageWithChromeAndContent(t *testing.T) {
	html := `<!DOCTYPE html>
	<html lang="en">
	<head><title>Page Title</title><style>.nav{color:blue}</style></head>
	<body>
		<header>
			<nav>
				<a href="/">Home</a>
				<a href="/about">About</a>
				<a href="/contact">Contact</a>
			</nav>
		</header>
		<main>
			<article>
				<h1>Main Article Title</h1>
				<p>This is the main content that matters.</p>
				<p>It has <strong>formatting</strong> and <a href="/info">links</a>.</p>
			</article>
		</main>
		<aside>
			<h3>Related Posts</h3>
			<ul><li><a href="/other">Other post</a></li></ul>
		</aside>
		<footer>
			<p>&copy; 2024 Example Corp. All rights reserved.</p>
		</footer>
		<script>console.log("tracking")</script>
	</body>
	</html>`
	result := htmlToMD(html)

	// Must NOT contain script, style
	if strings.Contains(result, "tracking") {
		t.Error("script content should be stripped")
	}
	if strings.Contains(result, ".nav{color") {
		t.Error("style content should be stripped")
	}

	// Must contain content
	if !strings.Contains(result, "# Main Article Title") {
		t.Errorf("expected main heading, got %q", result)
	}
	if !strings.Contains(result, "**formatting**") {
		t.Errorf("expected bold, got %q", result)
	}
	if !strings.Contains(result, "[links](/info)") {
		t.Errorf("expected links, got %q", result)
	}
}

func TestHTMLToMD_PageWithHiddenElements(t *testing.T) {
	html := `<body>
		<div style="display:none" class="popup">Subscribe to our newsletter!</div>
		<div aria-hidden="true" class="overlay"></div>
		<div hidden class="draft">Draft content not ready</div>
		<article>
			<h1>Visible Article</h1>
			<p>This content is visible.</p>
		</article>
		<div style="visibility:hidden">Invisible tracking text</div>
	</body>`
	result := htmlToMD(html)
	if strings.Contains(result, "newsletter") {
		t.Error("display:none popup should be stripped")
	}
	if strings.Contains(result, "Draft content") {
		t.Error("hidden attr content should be stripped")
	}
	if strings.Contains(result, "Invisible tracking") {
		t.Error("visibility:hidden content should be stripped")
	}
	if !strings.Contains(result, "# Visible Article") {
		t.Errorf("main content missing, got %q", result)
	}
}
