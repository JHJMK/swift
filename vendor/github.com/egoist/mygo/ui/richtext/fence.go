package richtext

import (
	"strconv"
	"strings"
)

// CodeBlockHeaderHeight is the room above the first line of a block of
// code for its header: the title, the language and the copy control.
const CodeBlockHeaderHeight float32 = 38

// parseFenceInfo splits a fenced code block's info string into its
// language and its title, which this app keeps as title="...". Other
// words after the language, as Markdown tools write, are left out.
func parseFenceInfo(info string) (language, title string) {
	info = strings.TrimSpace(info)
	if info == "" {
		return "", ""
	}
	if strings.HasPrefix(info, "title=") {
		return "", parseFenceTitle(info[len("title="):])
	}
	language = info
	if i := strings.IndexAny(info, " \t"); i >= 0 {
		language = info[:i]
		rest := strings.TrimSpace(info[i+1:])
		if strings.HasPrefix(rest, "title=") {
			title = parseFenceTitle(rest[len("title="):])
		}
	}
	return strings.TrimSpace(language), title
}

func parseFenceTitle(s string) string {
	s = strings.TrimSpace(s)
	if value, err := strconv.Unquote(s); err == nil {
		return value
	}
	return strings.Trim(s, `"'`)
}

// fenceInfo writes the info string of a block of code: its language and,
// when set, its title.
func fenceInfo(language, title string) string {
	info := strings.TrimSpace(language)
	if title = strings.TrimSpace(title); title != "" {
		if info != "" {
			info += " "
		}
		info += "title=" + strconv.Quote(title)
	}
	return info
}
