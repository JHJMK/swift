package markdown

import (
	"regexp"
	"strings"
)

var (
	markdownImagePattern = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]*)\)`)
	htmlImagePattern     = regexp.MustCompile(`(<img\b[^>]*?\bsrc\s*=\s*)(?:"([^"]*)"|'([^']*)')`)
)

// splitImageTarget splits what a Markdown image's parentheses hold into
// its target and its optional title. The target may contain spaces, as
// Typora writes absolute paths to its image folder, and may be enclosed
// in angle brackets.
func splitImageTarget(s string) (target, title string) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "<") {
		if i := strings.Index(s, ">"); i >= 0 {
			target = strings.TrimSpace(s[1:i])
			rest := strings.TrimSpace(s[i+1:])
			if isImageTitle(rest) {
				title = rest
			}
			return target, title
		}
	}
	for _, q := range []string{`"`, `'`} {
		if i := strings.LastIndex(s, " "+q); i >= 0 && len(s) > i+2 && strings.HasSuffix(strings.TrimSpace(s), q) {
			return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
		}
	}
	return s, ""
}

func isImageTitle(s string) bool {
	if len(s) < 2 {
		return false
	}
	q := s[0]
	return (q == '"' || q == '\'') && s[len(s)-1] == q
}

// imageRefs returns the targets of the images a Markdown text shows, in
// Markdown and HTML syntax.
func ImageRefs(md string) []string {
	var refs []string
	for _, m := range markdownImagePattern.FindAllStringSubmatch(md, -1) {
		if target, _ := splitImageTarget(m[2]); target != "" {
			refs = append(refs, target)
		}
	}
	for _, m := range htmlImagePattern.FindAllStringSubmatch(md, -1) {
		if m[2] != "" {
			refs = append(refs, m[2])
		} else if m[3] != "" {
			refs = append(refs, m[3])
		}
	}
	return refs
}

// rewriteImageRefs replaces the targets of the images a Markdown text
// shows, when rewrite has a replacement for them.
func RewriteImageRefs(md string, rewrite func(ref string) (string, bool)) string {
	md = markdownImagePattern.ReplaceAllStringFunc(md, func(m string) string {
		sub := markdownImagePattern.FindStringSubmatch(m)
		if sub == nil {
			return m
		}
		target, title := splitImageTarget(sub[2])
		moved, ok := rewrite(target)
		if !ok {
			return m
		}
		suffix := ""
		if title != "" {
			suffix = " " + title
		}
		return "![" + sub[1] + "](" + moved + suffix + ")"
	})
	md = htmlImagePattern.ReplaceAllStringFunc(md, func(m string) string {
		sub := htmlImagePattern.FindStringSubmatch(m)
		if sub == nil {
			return m
		}
		url, quote := sub[2], `"`
		if url == "" {
			url, quote = sub[3], "'"
		}
		moved, ok := rewrite(url)
		if !ok {
			return m
		}
		return sub[1] + quote + moved + quote
	})
	return md
}
