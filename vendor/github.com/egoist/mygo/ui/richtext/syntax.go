package richtext

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// The colors below are GitHub's light syntax colors; code blocks have a
// light background in this app.
var (
	synKeyword  = ui.Hex("#cf222e")
	synString   = ui.Hex("#0a3069")
	synNumber   = ui.Hex("#0550ae")
	synComment  = ui.Hex("#6e7781")
	synFunction = ui.Hex("#8250df")
	synType     = ui.Hex("#953800")
	synPunct    = ui.Hex("#57606a")
)

type synKind uint8

const (
	synPlain synKind = iota
	synKeywordKind
	synStringKind
	synNumberKind
	synCommentKind
	synFunctionKind
	synTypeKind
	synPunctKind
)

type synToken struct {
	text string
	kind synKind
}

func (k synKind) color() (ui.Color, bool) {
	switch k {
	case synKeywordKind:
		return synKeyword, true
	case synStringKind:
		return synString, true
	case synNumberKind:
		return synNumber, true
	case synCommentKind:
		return synComment, true
	case synFunctionKind:
		return synFunction, true
	case synTypeKind:
		return synType, true
	case synPunctKind:
		return synPunct, true
	}
	return ui.Color{}, false
}

var synLanguages = map[string]string{
	"golang": "go", "go": "go",
	"js": "js", "javascript": "js", "jsx": "js", "node": "js",
	"ts": "ts", "typescript": "ts", "tsx": "ts",
	"py": "py", "python": "py", "python3": "py",
	"sh": "sh", "bash": "sh", "shell": "sh", "zsh": "sh", "console": "sh",
	"sql": "sql", "mysql": "sql", "postgres": "sql", "postgresql": "sql", "sqlite": "sql",
	"json": "json",
	"yaml": "yaml", "yml": "yaml",
	"rust": "rust", "rs": "rust",
	"java": "java",
	"c":    "c", "cpp": "c", "c++": "c", "cs": "c", "csharp": "c",
}

func syntaxLanguage(name string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(name)))
	if len(fields) == 0 {
		return ""
	}
	return synLanguages[fields[0]]
}

// syntaxSpans colorizes a code paragraph's spans, keeping their fonts and
// sizes exactly as they are.
func syntaxSpans(spans []ui.Span, language string) []ui.Span {
	lang := syntaxLanguage(language)
	if lang == "" {
		return spans
	}
	out := make([]ui.Span, 0, len(spans))
	for _, sp := range spans {
		if sp.Text == "" {
			continue
		}
		for _, tok := range lexCode(sp.Text, lang) {
			ns := sp
			ns.Text = tok.text
			if c, ok := tok.kind.color(); ok {
				ns.Color = c
			}
			out = append(out, ns)
		}
	}
	return out
}

func lexCode(s, lang string) []synToken {
	var out []synToken
	add := func(text string, kind synKind) {
		if text != "" {
			out = append(out, synToken{text: text, kind: kind})
		}
	}
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if unicode.IsSpace(r) {
			j := i + size
			for j < len(s) {
				r2, n := utf8.DecodeRuneInString(s[j:])
				if !unicode.IsSpace(r2) {
					break
				}
				j += n
			}
			add(s[i:j], synPlain)
			i = j
			continue
		}
		// Comments.
		if strings.HasPrefix(s[i:], "//") && lang != "sql" && lang != "py" && lang != "sh" && lang != "yaml" {
			add(s[i:], synCommentKind)
			return out
		}
		if lang == "sql" && strings.HasPrefix(s[i:], "--") {
			add(s[i:], synCommentKind)
			return out
		}
		if (lang == "py" || lang == "sh" || lang == "yaml") && s[i] == '#' {
			add(s[i:], synCommentKind)
			return out
		}
		if strings.HasPrefix(s[i:], "/*") {
			if j := strings.Index(s[i+2:], "*/"); j >= 0 {
				j += i + 2 + 2
				add(s[i:j], synCommentKind)
				i = j
				continue
			}
			add(s[i:], synCommentKind)
			return out
		}
		// Strings.
		if s[i] == '"' || s[i] == '\'' || s[i] == '`' {
			quote := s[i]
			j := i + 1
			for j < len(s) {
				if s[j] == '\\' && quote != '`' && j+1 < len(s) {
					j += 2
					continue
				}
				if s[j] == quote {
					j++
					break
				}
				_, n := utf8.DecodeRuneInString(s[j:])
				j += n
			}
			add(s[i:j], synStringKind)
			i = j
			continue
		}
		// Numbers.
		if s[i] >= '0' && s[i] <= '9' {
			j := i + 1
			for j < len(s) {
				c := s[j]
				if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') ||
					c == 'x' || c == 'X' || c == '.' || c == '_' || c == 'e' || c == 'E' || c == '+' || c == '-' {
					j++
					continue
				}
				break
			}
			add(s[i:j], synNumberKind)
			i = j
			continue
		}
		// Identifiers, keywords, types and function names.
		if isIdentStart(r) {
			j := i + size
			for j < len(s) {
				r2, n := utf8.DecodeRuneInString(s[j:])
				if !isIdentPart(r2) {
					break
				}
				j += n
			}
			word := s[i:j]
			kind := classifyWord(word, lang)
			if kind == synPlain {
				k := j
				for k < len(s) && (s[k] == ' ' || s[k] == '\t') {
					k++
				}
				if k < len(s) && s[k] == '(' {
					kind = synFunctionKind
				}
			}
			add(word, kind)
			i = j
			continue
		}
		add(s[i:i+size], synPunctKind)
		i += size
	}
	return out
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func classifyWord(word, lang string) synKind {
	key := word
	if lang == "sql" {
		key = strings.ToUpper(word)
	}
	if syntaxTypes[lang][key] {
		return synTypeKind
	}
	if syntaxKeywords[lang][key] {
		return synKeywordKind
	}
	return synPlain
}

var syntaxKeywords = map[string]map[string]bool{
	"sql": setOf(`SELECT FROM WHERE JOIN LEFT RIGHT INNER OUTER FULL ON AS AND OR NOT NULL IS IN
		GROUP ORDER BY LIMIT OFFSET HAVING INSERT INTO VALUES UPDATE SET DELETE CREATE TABLE INDEX
		DROP ALTER ADD COLUMN PRIMARY KEY FOREIGN REFERENCES DEFAULT UNIQUE UNION ALL DISTINCT CASE
		WHEN THEN ELSE END EXISTS BETWEEN LIKE ASC DESC WITH RECURSIVE VIEW TRUNCATE BEGIN COMMIT
		ROLLBACK GRANT REVOKE USING CROSS NATURAL INNER OUTER TRUE FALSE`),
	"go": setOf(`break case chan const continue default defer else fallthrough for func go goto if import
		interface map package range return select struct switch type var`),
	"js": setOf(`break case catch class const continue debugger default delete do else export extends
		finally for function if import in instanceof let new return super switch this throw try typeof
		var void while with yield async await of from as`),
	"ts": setOf(`break case catch class const continue debugger default delete do else export extends
		finally for function if import in instanceof let new return super switch this throw try typeof
		var void while with yield async await of from as interface type enum implements namespace public
		private protected readonly declare abstract`),
	"py": setOf(`and as assert async await break class continue def del elif else except finally for from
		global if import in is lambda nonlocal not or pass raise return try while with yield match case`),
	"sh": setOf(`if then else elif fi for while in do done case esac function return break continue
		local export readonly unset shift source alias echo cd pwd exit`),
	"json": setOf(`true false null`),
	"yaml": setOf(`true false null yes no on off`),
	"rust": setOf(`as break const continue crate else enum extern false fn for if impl in let loop match
		mod move mut pub ref return self Self static struct super trait true type unsafe use where while
		async await dyn`),
	"java": setOf(`abstract assert boolean break byte case catch char class const continue default do
		double else enum extends final finally float for goto if implements import instanceof int interface
		long native new package private protected public return short static strictfp super switch
		synchronized this throw throws transient try void volatile while var record sealed permits`),
	"c": setOf(`auto break case char const continue default do double else enum extern float for goto if
		inline int long register restrict return short signed sizeof static struct switch typedef union
		unsigned void volatile while class public private protected namespace using template typename new
		delete true false nullptr`),
}

var syntaxTypes = map[string]map[string]bool{
	"go": setOf(`bool byte complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune
		string uint uint8 uint16 uint32 uint64 uintptr true false nil iota`),
	"js": setOf(`Array Boolean Date Error Function JSON Map Math Number Object Promise RegExp Set String
		Symbol WeakMap WeakSet console window document`),
	"ts": setOf(`Array Boolean Date Error Function JSON Map Math Number Object Promise RegExp Set String
		Symbol WeakMap WeakSet any unknown never void object string number boolean bigint symbol`),
	"py": setOf(`True False None int float str bytes list dict set tuple object type Exception`),
	"sql": setOf(`INT INTEGER BIGINT SMALLINT TINYINT VARCHAR CHAR TEXT BLOB DATE DATETIME TIMESTAMP TIME
		BOOLEAN BOOL DECIMAL NUMERIC FLOAT DOUBLE REAL JSON UUID`),
	"rust": setOf(`bool char str i8 i16 i32 i64 i128 isize u8 u16 u32 u64 u128 usize f32 f64 String Vec
		Option Result Box`),
	"java": setOf(`boolean byte char double float int long short void String Object Integer Long Double
		Boolean List Map Set`),
	"c":    setOf(`bool char double float int long short signed unsigned void size_t string`),
	"json": setOf(``),
	"sh":   setOf(``),
	"yaml": setOf(``),
}

func setOf(words string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(words) {
		m[w] = true
	}
	return m
}
