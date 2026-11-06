package richtext

import (
	"runtime"

	"github.com/egoist/mygo/internal/rope"
	"github.com/egoist/mygo/internal/text"
	"github.com/egoist/mygo/ui"
)

const scrollbarWidth = 10

// input takes the editor's input as it comes: keys, text, edit commands,
// the pointer and the wheel.
func (v *view) input(ev ui.InputEvent) bool {
	v.sync()
	defer v.publish()
	switch ev.Kind {
	case ui.InputKeyDown:
		if v.key(ev.Key, ev.Mods) {
			v.blinkStart = ev.Time
			return true
		}
		return false
	case ui.InputText:
		if v.opts.ReadOnly || ev.Text == "" {
			return false
		}
		v.compose = ""
		v.insert(ev.Text)
		v.typed(ev.Text)
		v.blinkStart = ev.Time
		return true
	case ui.InputCommand:
		return v.editCommand(ev.Text)
	case ui.InputPointerDown:
		return v.press(ev)
	case ui.InputPointerMove:
		return v.move(ev)
	case ui.InputPointerUp:
		return v.release(ev)
	case ui.InputSelect:
		n := v.shown.Len()
		v.selAnchor, v.caret = max(0, min(ev.Start, n)), max(0, min(ev.End, n))
		v.affinity = ui.TextDownstream
		v.reveal = true
		return true
	case ui.InputScroll:
		before := v.scroll
		content := v.hs.top(len(v.paras)) + float64(v.opts.BottomPadding)
		v.setScroll(max(0, min(v.scroll+float64(ev.DY), content-float64(v.viewH))))
		// At an end, the wheel goes on to the containers around.
		return v.scroll != before
	}
	return false
}

// selectAll selects the block of code the caret is in, or the whole text
// otherwise.
func (v *view) selectAll() {
	n := v.shown.Len()
	p := v.shown.paraAt(v.caret)
	if p >= 0 && p < len(v.paras) && v.para(p).style.CodeBlock {
		first, last := p, p
		for first > 0 && v.sameCodeGroup(first-1, p) {
			first--
		}
		for last+1 < len(v.paras) && v.sameCodeGroup(last, last+1) {
			last++
		}
		v.selAnchor = v.shown.paraStart(first)
		v.caret = v.shown.paraStart(last) + v.para(last).n
		v.affinity = ui.TextDownstream
		return
	}
	v.selAnchor, v.caret = 0, n
	v.affinity = ui.TextDownstream
}

// moveTo puts the caret at i, extending the selection or not.
func (v *view) moveTo(i int, affinity ui.TextAffinity, extend bool) {
	i = max(0, min(i, v.shown.Len()))
	v.caret, v.affinity = i, affinity
	if !extend {
		v.selAnchor = i
	}
	v.coalesce = false
	v.reveal = true
}

// runeAt returns rune i of the text shown.
func (v *view) runeAt(i int) rune {
	r, _ := rope.TextOf(v.shown.text).RuneAt(rope.ByteOfRune(v.shown.text, i))
	return r
}

// nextWord returns the end of the word after i; prevWord the start of the
// word before it. Each ideograph and kana is a word, as
// text.WordBoundary has it.
func (v *view) nextWord(i int) int {
	n, word, prev := v.shown.Len(), false, rune(0)
	for ; i < n; i++ {
		r := v.runeAt(i)
		if word && text.WordBoundary(prev, r) {
			break
		}
		word, prev = word || text.IsWordRune(r), r
	}
	return i
}

func (v *view) prevWord(i int) int {
	word, next := false, rune(0)
	for ; i > 0; i-- {
		r := v.runeAt(i - 1)
		if word && text.WordBoundary(r, next) {
			break
		}
		word, next = word || text.IsWordRune(r), r
	}
	return i
}

// wordAt returns the word, or the run of other runes, around i, within its
// paragraph: an ideograph or a kana is a word alone.
func (v *view) wordAt(i int) (int, int) {
	n := v.shown.Len()
	if n == 0 {
		return 0, 0
	}
	i = max(0, min(i, n-1))
	at := v.runeAt(i)
	a, z := i, i
	for next := at; a > 0; a-- {
		r := v.runeAt(a - 1)
		if r == '\n' || text.WordBoundary(r, next) {
			break
		}
		next = r
	}
	for prev := at; z < n; z++ {
		r := v.runeAt(z)
		if r == '\n' || z > i && text.WordBoundary(prev, r) {
			break
		}
		prev = r
	}
	return a, z
}

// grapheme returns the edge of the grapheme before i (dir < 0) or after
// it: a newline is one.
func (v *view) grapheme(i, dir int) int {
	n := v.shown.Len()
	p := v.shown.paraAt(i)
	ps := v.shown.paraStart(p)
	pe := ps + v.para(p).n
	if dir < 0 && i == ps {
		return max(i-1, 0)
	}
	if dir > 0 && i >= pe {
		return min(i+1, n)
	}
	pv := v.paraLayout(p)
	u := pv.units[min(v.local(p, i), len(pv.units)-1)]
	if dir < 0 {
		u = pv.layout.PreviousBoundary(u)
	} else {
		u = pv.layout.NextBoundary(u)
	}
	return v.global(p, pv.runeOfUnit(u))
}

// visual returns the caret one grapheme left (dir < 0) or right on screen,
// across bidi runs, and to the next paragraph at the edge of one.
func (v *view) visual(dir int) (int, ui.TextAffinity) {
	p := v.shown.paraAt(v.caret)
	pv := v.paraLayout(p)
	l := min(v.local(p, v.caret), len(pv.units)-1)
	pos := ui.TextCaretPosition{Index: pv.units[l], Affinity: v.affinity}
	next := pv.layout.MoveCaret(pos, dir)
	if next.Index == pos.Index {
		if dir < 0 {
			return max(v.shown.paraStart(p)-1, 0), ui.TextDownstream
		}
		return min(v.shown.paraStart(p)+v.para(p).n+1, v.shown.Len()), ui.TextDownstream
	}
	return v.global(p, pv.runeOfUnit(next.Index)), next.Affinity
}

// leaveBlockFocus moves up out of a focused code block's outer edge.
func (v *view) leaveBlockFocus() {
	first := v.codeBlockFirst(v.doc.blockFocus)
	v.doc.blockFocus = 0
	if first > 0 {
		p := first - 1
		v.moveTo(v.shown.paraStart(p)+v.para(p).n, ui.TextUpstream, false)
		v.reveal = true
	}
}

// enterBlockFocus returns from a code block's outer edge into its code.
func (v *view) enterBlockFocus() {
	first := v.codeBlockFirst(v.doc.blockFocus)
	v.doc.blockFocus = 0
	if first >= 0 {
		v.moveTo(v.shown.paraStart(first), ui.TextDownstream, false)
		v.reveal = true
	}
}

// deleteEmptyCodeBlock removes an empty block of code when Delete or
// Backspace is pressed at its first line's start, leaving the surrounding
// paragraphs as they were.
func (v *view) deleteEmptyCodeBlock(p int) bool {
	if p < 0 || p >= len(v.paras) || v.caret != v.shown.paraStart(p) {
		return false
	}
	p0 := p
	for p0 > 0 && v.sameCodeGroup(p0-1, p) {
		p0--
	}
	p1 := p
	for p1+1 < len(v.paras) && v.sameCodeGroup(p1, p1+1) {
		p1++
	}
	if p != p0 {
		return false
	}
	for i := p0; i <= p1; i++ {
		if v.para(i).n != 0 {
			return false
		}
	}
	v.doc.DeleteCodeBlockAt(v.caret)
	v.doc.blockFocus = 0
	v.reveal = true
	return true
}

// escapeCodeBlock moves the caret out of a block of code when Up or Down
// is pressed on its first or last visual line. Without this, the padding
// around a block can keep the caret inside it at its edges.
func (v *view) escapeCodeBlock(key ui.Key, extend bool) bool {
	p := v.shown.paraAt(v.caret)
	if p < 0 || !v.para(p).style.CodeBlock {
		return false
	}
	ps := v.shown.paraStart(p)
	pe := ps + v.para(p).n
	s, e := v.lineEdges(v.caret)
	switch key {
	case ui.KeyUp:
		if s != ps {
			return false
		}
		if p == 0 {
			v.doc.blockFocus = v.para(p).style.CodeBlockID
			v.reveal = true
			return true
		}
		if v.para(p - 1).style.CodeBlock {
			if !v.sameCodeGroup(p-1, p) {
				v.doc.blockFocus = v.para(p).style.CodeBlockID
				v.reveal = true
				return true
			}
			return false
		}
		v.moveTo(max(ps-1, 0), ui.TextUpstream, extend)
		return true
	case ui.KeyDown:
		if e != pe {
			return false
		}
		if p+1 >= len(v.paras) {
			v.addParagraphAroundCode(p, false)
			return true
		}
		if v.para(p + 1).style.CodeBlock {
			return false
		}
		v.moveTo(v.shown.paraStart(p+1), ui.TextDownstream, extend)
		return true
	}
	return false
}

// addParagraphAroundCode makes a normal paragraph before or after the block
// of code at p, so the arrows can leave a block at the document's edge.
func (v *view) addParagraphAroundCode(p int, before bool) {
	ps := v.shown.paraStart(p)
	pe := ps + v.para(p).n
	if before {
		// Insert a new, ordinary paragraph before the code line. Doing this
		// through Enter would put the caret in the code line, and unstyling
		// it would turn the code's first line into body text.
		v.edit(func(e *Edit) {
			e.ReplaceString(ps, ps, "\n", Attributes{})
			e.FormatParagraphs(ps, ps, func(s *ParagraphStyle) {
				s.CodeBlock, s.Language, s.CodeBlockID, s.CodeBlockTitle = false, "", 0, ""
			})
			e.Select(ps, ps)
		})
		return
	}
	v.moveTo(pe, ui.TextDownstream, false)
	v.enter()
	v.command(func(d *Document) {
		d.FormatParagraphs(func(s *ParagraphStyle) {
			s.CodeBlock, s.Language, s.CodeBlockID, s.CodeBlockTitle = false, "", 0, ""
		})
	})
}

// vertical moves the caret up or down lines, keeping its x.
func (v *view) vertical(lines int, extend bool) {
	x, y, h := v.caretAt(v.caret, 0, v.affinity)
	if !v.hasDesired {
		v.desiredX, v.hasDesired = x, true
	}
	target := y + float64(h)/2 + float64(lines)*float64(h)
	switch {
	case target < 0:
		v.moveTo(0, ui.TextDownstream, extend)
	case target > v.hs.top(len(v.paras)):
		v.moveTo(v.shown.Len(), ui.TextDownstream, extend)
	default:
		i, a := v.positionAt(v.desiredX, target)
		v.moveTo(i, a, extend)
	}
	v.hasDesired = true
}

// emacsKeys are the Control keys of macOS text views that act as other
// keys.
var emacsKeys = map[ui.Key]ui.Key{ui.KeyB: ui.KeyLeft, ui.KeyF: ui.KeyRight, ui.KeyP: ui.KeyUp, ui.KeyN: ui.KeyDown, ui.KeyH: ui.KeyBackspace, ui.KeyD: ui.KeyDelete}

// key handles a key pressed, and reports whether it took it.
func (v *view) key(key ui.Key, mods ui.Modifiers) bool {
	if cmd, ok := shortcut(key, mods); ok {
		return v.run(cmd)
	}
	mac := runtime.GOOS == "darwin"
	shift := mods&ui.Shift != 0
	m := mods &^ ui.Shift
	if mac && m == ui.Ctrl {
		if to, ok := emacsKeys[key]; ok {
			key, m = to, 0
		} else {
			p := v.shown.paraAt(v.caret)
			ps := v.shown.paraStart(p)
			pe := ps + v.para(p).n
			switch key {
			case ui.KeyA:
				v.moveTo(ps, ui.TextDownstream, shift)
				return true
			case ui.KeyE:
				v.moveTo(pe, ui.TextDownstream, shift)
				return true
			case ui.KeyK:
				if v.opts.ReadOnly {
					return true
				}
				if pe == v.caret && pe < v.shown.Len() {
					pe++
				}
				v.deleteRange(v.caret, pe, false)
				return true
			}
		}
	}
	word := (!mac && m == ui.Ctrl) || (mac && m == ui.Alt)
	readOnly := v.opts.ReadOnly
	if key == ui.KeyV && shift && (m == ui.Cmd || m == ui.Cmd|ui.Alt) {
		// Paste and Match Style: ⇧⌥⌘V, and ⇧⌘V as Notes has it.
		return v.editCommand("pasteAndMatchStyle")
	}
	a, z := v.selection()
	n := v.shown.Len()
	if v.doc.blockFocus != 0 && !readOnly {
		switch {
		case key == ui.KeyUp && m == 0:
			v.leaveBlockFocus()
			return true
		case key == ui.KeyDown && m == 0:
			v.enterBlockFocus()
			return true
		case key == ui.KeyEnter && m == 0:
			first := v.codeBlockFirst(v.doc.blockFocus)
			v.doc.blockFocus = 0
			if first >= 0 {
				v.addParagraphAroundCode(first, true)
			}
			return true
		case (key == ui.KeyDelete || key == ui.KeyBackspace) && m == 0:
			v.doc.DeleteCodeBlockAt(v.caret)
			v.doc.blockFocus = 0
			v.reveal = true
			return true
		case key == ui.KeyEscape:
			v.doc.blockFocus, v.reveal = 0, true
			return true
		default:
			v.doc.blockFocus = 0
		}
	}

	switch key {
	case ui.KeyLeft, ui.KeyRight:
		left := key == ui.KeyLeft
		if a == z && m == 0 {
			p := v.shown.paraAt(v.caret)
			st := v.para(p).style
			if st.CodeBlock {
				ps := v.shown.paraStart(p)
				pe := ps + v.para(p).n
				if left && v.caret == ps && p > 0 && v.para(p-1).style.CodeBlock && !v.sameCodeGroup(p-1, p) {
					return true
				}
				if !left && v.caret == pe && p+1 < len(v.paras) && v.para(p+1).style.CodeBlock && !v.sameCodeGroup(p, p+1) {
					return true
				}
			}
		}
		switch {
		case m == ui.Alt && !mac:
			return false // back and forward
		case mac && m == ui.Super:
			s, e := v.lineEdges(v.caret)
			if left {
				v.moveTo(s, ui.TextDownstream, shift)
			} else {
				v.moveTo(e, ui.TextUpstream, shift)
			}
		case word && left:
			v.moveTo(v.prevWord(v.caret), ui.TextDownstream, shift)
		case word:
			v.moveTo(v.nextWord(v.caret), ui.TextDownstream, shift)
		case a != z && !shift && left:
			v.moveTo(a, ui.TextDownstream, false)
		case a != z && !shift:
			v.moveTo(z, ui.TextDownstream, false)
		case left:
			i, aff := v.visual(-1)
			v.moveTo(i, aff, shift)
		default:
			i, aff := v.visual(1)
			v.moveTo(i, aff, shift)
		}
		v.hasDesired = false
		return true
	case ui.KeyUp, ui.KeyDown:
		if mac && m == ui.Super {
			if key == ui.KeyUp {
				v.moveTo(0, ui.TextDownstream, shift)
			} else {
				v.moveTo(n, ui.TextDownstream, shift)
			}
			return true
		}
		if mac && m == ui.Alt || !mac && m == ui.Ctrl {
			// By paragraphs: up to the start of this one or the one
			// before; down to the end of this one or the next on macOS,
			// as NSTextView, to the start of the next elsewhere, as
			// WordPad and GTK.
			a, z := v.shown.ParagraphRange(v.caret)
			switch {
			case key == ui.KeyUp && v.caret == a && a > 0:
				a, _ = v.shown.ParagraphRange(a - 1)
				v.moveTo(a, ui.TextDownstream, shift)
			case key == ui.KeyUp:
				v.moveTo(a, ui.TextDownstream, shift)
			case mac && v.caret == z && z < n:
				_, z = v.shown.ParagraphRange(z + 1)
				v.moveTo(z, ui.TextUpstream, shift)
			case mac:
				v.moveTo(z, ui.TextUpstream, shift)
			default:
				v.moveTo(min(z+1, n), ui.TextDownstream, shift)
			}
			v.hasDesired = false
			return true
		}
		if m != 0 {
			return false
		}
		d := 1
		if key == ui.KeyUp {
			d = -1
		}
		if v.escapeCodeBlock(key, shift) {
			return true
		}
		if key == ui.KeyUp {
			p := v.shown.paraAt(v.caret)
			ps := v.shown.paraStart(p)
			if s, _ := v.lineEdges(v.caret); s == ps {
				// On a paragraph's first line, go to the paragraph above,
				// whatever the heading's room above would make a line up
				// land inside this one.
				if p > 0 {
					v.moveTo(ps-1, ui.TextUpstream, shift)
					return true
				}
				if !shift && v.opts.OnTop != nil {
					v.opts.OnTop()
					return true
				}
			}
		}
		v.vertical(d, shift)
		return true
	case ui.KeyPageUp, ui.KeyPageDown:
		lines := max(int(v.viewH/(v.look.size*1.3))-1, 1)
		if key == ui.KeyPageUp {
			lines = -lines
		}
		v.vertical(lines, shift)
		return true
	case ui.KeyHome, ui.KeyEnd:
		if m == ui.Ctrl {
			if key == ui.KeyHome {
				v.moveTo(0, ui.TextDownstream, shift)
			} else {
				v.moveTo(n, ui.TextDownstream, shift)
			}
			return true
		}
		s, e := v.lineEdges(v.caret)
		if key == ui.KeyHome {
			v.moveTo(s, ui.TextDownstream, shift)
		} else {
			v.moveTo(e, ui.TextUpstream, shift)
		}
		return true
	case ui.KeyA:
		if m == ui.Cmd {
			v.selectAll()
			return true
		}
	case ui.KeyC, ui.KeyX, ui.KeyV, ui.KeyZ, ui.KeyY:
		if m != ui.Cmd {
			break
		}
		switch key {
		case ui.KeyC:
			return v.editCommand("copy")
		case ui.KeyX:
			return v.editCommand("cut")
		case ui.KeyV:
			return v.editCommand("paste")
		case ui.KeyZ:
			if shift {
				return v.editCommand("redo")
			}
			return v.editCommand("undo")
		case ui.KeyY:
			return v.editCommand("redo")
		}
	}
	if readOnly {
		return false
	}
	p := v.shown.paraAt(v.caret)
	st := v.para(p).style
	ps := v.shown.paraStart(p)
	if st.CodeBlock && (key == ui.KeyDelete || key == ui.KeyBackspace) &&
		((mac && m == ui.Cmd) || (!mac && m == ui.Ctrl)) {
		v.doc.DeleteCodeBlockAt(v.caret)
		v.reveal = true
		return true
	}
	switch key {
	case ui.KeyBackspace:
		if m == 0 && a == z && v.caret == ps && st.CodeBlock && v.deleteEmptyCodeBlock(p) {
			return true
		}
		switch {
		case a != z:
			v.deleteRange(a, z, false)
		case m == 0 && v.caret == ps && !st.CodeBlock && v.para(p).n == 0 && p > 0 && v.para(p-1).style.CodeBlock:
			// An empty paragraph below a block of code: remove that
			// paragraph, but keep the code block's last (possibly empty)
			// line and its code style.
			prev := v.para(p - 1).style
			start := v.shown.paraStart(p - 1)
			v.edit(func(e *Edit) {
				e.ReplaceString(ps-1, ps, "", Attributes{})
				e.FormatParagraphs(start, start, func(s *ParagraphStyle) { *s = prev })
				e.Select(ps-1, ps-1)
			})
		case m == 0 && v.caret == ps && st.CodeBlock:
			// Backspace at the start of a code line joins it to the code
			// line above. At the block's edge it does nothing, so the
			// block never turns into ordinary text by accident.
			if p > 0 && v.sameCodeGroup(p-1, p) {
				v.deleteRange(v.caret-1, v.caret, true)
			}
		case m == 0 && v.caret == ps && special(st):
			// At the start of a list's item, a heading or a quotation,
			// Backspace takes the style away before joining.
			v.command(func(d *Document) { d.FormatParagraphs(unstyle) })
		case mac && m == ui.Super:
			s, _ := v.lineEdges(v.caret)
			v.deleteRange(s, v.caret, false)
		case word:
			v.deleteRange(v.prevWord(v.caret), v.caret, false)
		case v.caret > 0:
			v.deleteRange(v.grapheme(v.caret, -1), v.caret, true)
		}
		return true
	case ui.KeyDelete:
		if m == 0 && a == z && v.caret == ps && st.CodeBlock && v.deleteEmptyCodeBlock(p) {
			return true
		}
		pe := ps + v.para(p).n
		if a == z && st.CodeBlock && v.caret == pe {
			// Delete at the end of a code line joins the next code line.
			// At the block's edge it does nothing.
			if p+1 < len(v.paras) && v.sameCodeGroup(p, p+1) {
				v.deleteRange(v.caret, v.grapheme(v.caret, 1), false)
			}
			return true
		}
		switch {
		case a != z:
			v.deleteRange(a, z, false)
		case word:
			v.deleteRange(v.caret, v.nextWord(v.caret), false)
		case v.caret < n:
			v.deleteRange(v.caret, v.grapheme(v.caret, 1), false)
		}
		return true
	case ui.KeyEnter:
		if m == ui.Super || m == ui.Ctrl {
			v.exitCodeBlock()
			return true
		}
		if m != 0 {
			return false
		}
		v.enter()
		return true
	case ui.KeyTab:
		switch {
		case m != 0:
			return false
		case st.List != NoList:
			if shift {
				return v.run("outdent")
			}
			return v.run("indent")
		case st.CodeBlock && !shift:
			v.insert("\t")
			return true
		}
		return false
	}
	return false
}

// shortcut returns the command a key with mods runs: ⌘B, ⌘I, ⌘U, ⇧⌘X, ⌘E,
// ⌘K, ⇧⌘7, ⇧⌘8, ⇧⌘L, ⌘], ⌘[, ⌘{, ⌘|, ⌘} and ⌥⌘0–6 on macOS, as Pages,
// Notes and Google Docs have them, with Control in place of Command
// elsewhere, where Ctrl+L, Ctrl+E, Ctrl+R align, as in Word.
func shortcut(key ui.Key, mods ui.Modifiers) (string, bool) {
	mac := runtime.GOOS == "darwin"
	switch mods {
	case ui.Cmd:
		switch key {
		case ui.KeyB:
			return "bold", true
		case ui.KeyI:
			return "italic", true
		case ui.KeyU:
			return "underline", true
		case ui.KeyK:
			return "link", true
		case ui.KeyBracketRight:
			return "indent", true
		case ui.KeyBracketLeft:
			return "outdent", true
		case ui.KeyE:
			if mac {
				return "code", true
			}
			return "alignCenter", true
		case ui.KeyL:
			if !mac {
				return "alignLeft", true
			}
		case ui.KeyR:
			if !mac {
				return "alignRight", true
			}
		}
	case ui.Cmd | ui.Shift:
		switch key {
		case ui.KeyX:
			return "strikethrough", true
		case ui.Key7:
			return "numbers", true
		case ui.Key8:
			return "bullets", true
		case ui.KeyL:
			return "tasks", true
		case ui.KeyBracketLeft:
			if mac {
				return "alignLeft", true
			}
		case ui.KeyBracketRight:
			if mac {
				return "alignRight", true
			}
		case ui.KeyBackslash:
			if mac {
				return "alignCenter", true
			}
		case ui.KeyE:
			if !mac {
				return "code", true
			}
		}
	case ui.Cmd | ui.Alt:
		if key >= ui.Key0 && key <= ui.Key6 {
			return "heading" + string(rune('0'+int(key-ui.Key0))), true
		}
	}
	return "", false
}

// editCommand performs an edit command of a menu or a shortcut.
func (v *view) editCommand(name string) bool {
	a, z := v.selection()
	switch name {
	case "copy":
		if a != z {
			v.copySelection()
		}
		return true
	case "cut":
		if a != z && !v.opts.ReadOnly {
			v.copySelection()
			v.deleteRange(a, z, false)
		}
		return true
	case "paste", "pasteAndMatchStyle":
		if !v.opts.ReadOnly {
			v.paste(name == "pasteAndMatchStyle")
		}
		return true
	case "selectAll":
		v.selectAll()
		return true
	case "delete":
		if a != z && !v.opts.ReadOnly {
			v.deleteRange(a, z, false)
		}
		return true
	case "undo", "redo":
		if !v.opts.ReadOnly {
			v.commitCompose()
			v.takeBack(name == "redo")
		}
		return true
	}
	return v.run(name)
}

// press handles a press of the pointer.
func (v *view) press(ev ui.InputEvent) bool {
	if ev.Button != 0 {
		return false
	}
	for _, b := range v.copyButtons {
		if b.rect.Contains(ev.X, ev.Y) {
			if v.opts.OnCopyCode != nil {
				v.opts.OnCopyCode(b.code)
			}
			return true
		}
	}
	v.doc.blockFocus = 0
	v.pressMods = ev.Mods
	v.pressLink = ""
	if v.overScrollbar(ev.X) {
		v.thumb = true
		top, size := v.thumbBox()
		if ev.Y >= top && ev.Y < top+size {
			v.thumbGrab = ev.Y - top
		} else {
			v.thumbGrab = size / 2
			v.dragThumb(ev.Y)
		}
		return true
	}
	v.commitCompose()
	x, y := v.contentPoint(ev.X, ev.Y)
	if p, ok := v.taskBox(x, y); ok && ev.Clicks == 1 && !v.opts.ReadOnly {
		start := v.shown.paraStart(p)
		v.command(func(d *Document) { d.ToggleChecked(start) })
		return true
	}
	i, aff := v.positionAt(x, y)
	if ev.Clicks == 1 && ev.Mods&ui.Shift == 0 && i < v.shown.Len() {
		v.pressLink = v.shown.AttributesAt(i).Link
	}
	v.dragging = true
	v.hasDesired = false
	v.dragUnit = min(max(ev.Clicks, 1), 3)
	switch v.dragUnit {
	case 1:
		v.moveTo(i, aff, ev.Mods&ui.Shift != 0)
	case 2:
		v.selAnchor, v.caret = v.wordAt(i)
	default:
		a, z := v.shown.ParagraphRange(i)
		v.selAnchor, v.caret = a, min(z+1, v.shown.Len())
	}
	v.dragStart = [2]int{v.selAnchor, v.caret}
	v.coalesce = false
	return true
}

// move handles the pointer moving: a drag extends the selection.
func (v *view) move(ev ui.InputEvent) bool {
	if v.thumb {
		v.dragThumb(ev.Y)
		return true
	}
	x, y := v.contentPoint(ev.X, ev.Y)
	if !v.dragging {
		cursor := ui.CursorText
		onCopy := false
		for _, b := range v.copyButtons {
			if b.rect.Contains(ev.X, ev.Y) {
				onCopy = true
				break
			}
		}
		if onCopy {
			cursor = ui.CursorPointer
		} else if _, box := v.taskBox(x, y); box || v.overScrollbar(ev.X) {
			cursor = ui.CursorDefault
		} else if i, _ := v.positionAt(x, y); i < v.shown.Len() && v.shown.AttributesAt(i).Link != "" {
			cursor = ui.CursorPointer
		}
		if cursor != v.cursor {
			v.cursor = cursor
			return true
		}
		return false
	}
	// Near the edges, a drag scrolls.
	if ev.Y < v.padY {
		v.setScroll(max(0, v.scroll-float64(v.padY-ev.Y)))
	} else if ev.Y > v.padY+v.viewH {
		v.setScroll(v.scroll + float64(ev.Y-v.padY-v.viewH))
	}
	i, aff := v.positionAt(x, y)
	switch v.dragUnit {
	case 2:
		s, e := v.wordAt(i)
		if i < v.dragStart[0] {
			v.selAnchor, v.caret = v.dragStart[1], s
		} else {
			v.selAnchor, v.caret = v.dragStart[0], max(e, v.dragStart[1])
		}
	case 3:
		a, z := v.shown.ParagraphRange(i)
		if i < v.dragStart[0] {
			v.selAnchor, v.caret = v.dragStart[1], a
		} else {
			v.selAnchor, v.caret = v.dragStart[0], min(z+1, v.shown.Len())
		}
	default:
		v.caret, v.affinity = i, aff
	}
	if i != v.dragStart[0] || v.dragUnit != 1 {
		v.pressLink = ""
	}
	v.reveal = true
	return true
}

// release ends a drag, and opens the link a click began on.
func (v *view) release(ev ui.InputEvent) bool {
	if v.thumb {
		v.thumb = false
		return true
	}
	if !v.dragging {
		return false
	}
	v.dragging = false
	link := v.pressLink
	v.pressLink = ""
	if link != "" && v.caret == v.selAnchor {
		if v.opts.OnLink != nil {
			v.opts.OnLink(link)
		} else if ui.SafeToOpen(link) {
			v.services.OpenURL(link)
		}
	}
	return true
}

// taskBox reports whether (x, y), in the content, is on the check box of
// a task, and which paragraph's.
func (v *view) taskBox(x float32, y float64) (int, bool) {
	if len(v.paras) == 0 {
		return 0, false
	}
	p := v.hs.at(y)
	st := v.para(p).style
	if st.List != Tasks {
		return 0, false
	}
	left, _ := v.insets(st)
	if x < left-v.look.indent || x >= left-2 {
		return p, false
	}
	// Beside the first line, where the box is.
	_, top, h := v.caretAt(v.shown.paraStart(p), 0, ui.TextDownstream)
	return p, y >= top && y < top+float64(h)
}

// overScrollbar reports whether x of the element is on the scroll bar.
func (v *view) overScrollbar(x float32) bool {
	if v.opts.NoScrollbar {
		return false
	}
	return v.hs.top(len(v.paras)) > float64(v.viewH) && x >= v.rect.W-scrollbarWidth
}

// bar returns the scroll bar, in the element.
func (v *view) bar() ui.Scrollbar {
	return ui.Scrollbar{Box: ui.Rect{W: v.rect.W, H: v.rect.H}, View: v.viewH, Content: float32(v.hs.top(len(v.paras))) + v.opts.BottomPadding, Offset: float32(v.scroll)}
}

// thumbBox returns the top and the height of the scroll bar's thumb, in
// the element.
func (v *view) thumbBox() (top, size float32) { return v.bar().Thumb() }

// dragThumb scrolls as the thumb is dragged to y.
func (v *view) dragThumb(y float32) {
	v.setScroll(float64(v.bar().OffsetAt(y - v.thumbGrab)))
}

// menu builds the editor's context menu.
func (v *view) menu(m *ui.Menu) {
	m.EditItems()
}
