package app

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Run starts 语燕 and stays until it quits.
func Run() {
	// 语燕 is light throughout: on a dark desktop its window chrome stays
	// light too, as its pages are.
	mygo.Theme.SetSource(mygo.ThemeLight)
	a := newApp()
	mygo.App.WhenReady(func() {
		a.pasteItem = &mygo.MenuItem{
			Label:       "粘贴",
			Accelerator: "CmdOrCtrl+V",
			Disabled:    true,
			Click:       func(*mygo.MenuItem, *mygo.Window) { a.pasteFromMenu() },
		}
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Role: mygo.RoleAppMenu},
			{Label: "文件", Submenu: []*mygo.MenuItem{
				{Label: "新建文档", Accelerator: "CmdOrCtrl+N", Click: func(*mygo.MenuItem, *mygo.Window) {
					if a.vault != nil {
						a.askNewDoc()
						a.update()
					}
				}},
				{Label: "新建知识库", Click: func(*mygo.MenuItem, *mygo.Window) {
					if a.vault != nil {
						a.askNewKnowledgeBase()
						a.update()
					}
				}},
				{Label: "复制导入知识库…", Click: func(*mygo.MenuItem, *mygo.Window) {
					if a.vault != nil {
						a.askImportKnowledgeBaseCopy()
					}
				}},
				{Label: "引用外部知识库…", Click: func(*mygo.MenuItem, *mygo.Window) {
					if a.vault != nil {
						a.askImportExternalKnowledgeBase()
					}
				}},
				{Label: "新建目录", Click: func(*mygo.MenuItem, *mygo.Window) {
					if a.vault != nil {
						a.askNewFolder()
						a.update()
					}
				}},
				mygo.Separator(),
				{Label: "搜索", Accelerator: "CmdOrCtrl+K", Click: func(*mygo.MenuItem, *mygo.Window) {
					if a.vault != nil {
						a.openSearch()
						a.update()
					}
				}},
				mygo.Separator(),
				{Label: "设置…", Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) {
					if a.vault != nil {
						a.settingsOpen = true
						a.update()
					}
				}},
			}},
			{Label: "编辑", Submenu: []*mygo.MenuItem{
				{Role: mygo.RoleUndo},
				{Role: mygo.RoleRedo},
				mygo.Separator(),
				{Role: mygo.RoleCut},
				{Role: mygo.RoleCopy},
				a.pasteItem,
				{Role: mygo.RolePasteAndMatchStyle},
				mygo.Separator(),
				{Role: mygo.RoleSelectAll},
				mygo.Separator(),
				{Label: "查找", Accelerator: "CmdOrCtrl+F", Click: func(*mygo.MenuItem, *mygo.Window) {
					a.openFind()
				}},
			}},
			{Label: "插入", Submenu: []*mygo.MenuItem{
				{Label: "附件…", Accelerator: "CmdOrCtrl+Shift+A", Click: func(*mygo.MenuItem, *mygo.Window) {
					a.insertAttachment()
				}},
				{Label: "图片…", Click: func(*mygo.MenuItem, *mygo.Window) {
					a.insertImage()
				}},
			}},
			{Role: mygo.RoleFormatMenu},
			{Role: mygo.RoleWindowMenu},
		}))

		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:    "语燕",
			Width:    1180,
			Height:   760,
			MinWidth: 860, MinHeight: 520,
			StateKey: "main",
			// The view draws the whole window, the sidebar up to its top
			// edge, under the traffic lights.
			TitleBarStyle: mygo.TitleBarHidden,
			// The window is white under its chrome, so no dark flash shows
			// while it opens.
			BackgroundColor: "#ffffff",
			Content:         ui.View(a.view),
		})
		a.win.OnClose(func(*mygo.CloseEvent) {
			a.quitting = true
			a.flushTitle()
			a.flushDoc()
			a.flushSync()
		})
		// Start after the window exists: the mirror's status repaints it.
		a.startSync()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
