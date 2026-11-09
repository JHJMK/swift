package app

import (
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type dragItem struct {
	Rel string
}

func (a *app) view(c *ui.Context) {
	a.finishLakebookImport()
	if a.mermaidRefresh && !a.isWebDoc() {
		a.mermaidRefresh = false
		a.reloadForMermaid()
	}
	if a.doc != nil && !a.isWebDoc() {
		a.syncMermaidVisibility()
	}
	a.applyTheme(c)
	if result := a.syncNote.takeResult(); result != nil {
		a.syncResult = result
	}
	if a.deferred != nil {
		fn := a.deferred
		a.deferred = nil
		fn()
	}
	if a.vault == nil {
		a.onboarding(c)
	} else {
		a.maybeSave()
		a.syncKBWatch()
		a.syncWebIdentity()
		a.mainView(c)
		// Drop and context-menu actions wait until the row that reported
		// them is built; the tree rows are built after the sidebar, so
		// run them once the whole main view exists.
		a.runPending()
	}
	if a.doc == nil {
		a.setEditorFocused(false)
	}
	a.toastView(c)
	if a.insertSlash && a.insertMenuOpen && a.doc != nil {
		anchor := ui.Box(c).Absolute().Left(a.caretX).Top(a.caretY).Width(0).Height(0)
		a.insertPalette(c, anchor, a.doc)
	}
	a.promptModal(c)
	a.firstDocModal(c)
	a.confirmModal(c)
	a.settingsModal(c)
	a.pageSettingsModal(c)
	a.trashModal(c)
	a.searchModal(c)
	a.pickerModal(c)
	a.syncResultModal(c)
	a.syncTitle()
}

// toastView shows a small floating confirmation above the page.
func (a *app) toastView(c *ui.Context) {
	if a.toast == "" {
		return
	}
	t := c.Theme()
	ui.Row(c).Absolute().Left(0).Right(0).Top(96).Justify(ui.Center).Children(func() {
		ui.Row(c).Padding(7, 12).Gap(8).Radius(8).AlignItems(ui.Center).
			Background(t.Surface).Border(1, t.Border).
			Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, 0.12)).Children(func() {
			ui.Box(c).Width(20).Height(20).Radius(10).Background(ui.Hex("#00b96b")).
				Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconCheck).FontSize(12).TextColor(ui.Hex("#ffffff"))
			})
			ui.Text(c, a.toast).FontSize(14).TextColor(t.Text)
		})
	})
}

func (a *app) syncTitle() {
	title := "语燕"
	if a.doc != nil {
		title = a.doc.Title + " — 语燕"
	}
	if title != a.lastTitle {
		a.lastTitle = title
		if a.win != nil {
			a.win.SetTitle(title)
		}
	}
}

func (a *app) onboarding(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		ui.Box(c).Absolute().Left(0).Top(0).Right(0).Height(c.TitleBar().Height).DragWindow()
		ui.Column(c).Grow(1).Center().Gap(16).Padding(40).Children(func() {
			brandLogo(c, 56, 14)
			ui.Text(c, "语燕").FontSize(36).Bold().Padding(6, 0, 0, 0)
			ui.Text(c, "本地优先的个人知识库").FontSize(16).TextColor(t.TextMuted)
			ui.Text(c, "选择一个文件夹作为文档目录。知识库、目录和 Markdown 文档都会保存在这里，之后可以在设置中迁移。").
				MaxWidth(480).TextAlign(ui.Center).TextColor(t.TextMuted)
			if ui.PrimaryButton(c, "选择文档目录").Padding(9, 20).Clicked() {
				a.chooseWorkspace()
			}
			if a.errMsg != "" {
				ui.Text(c, a.errMsg).TextColor(t.Danger).MaxWidth(480).TextAlign(ui.Center)
			}
		})
	})
}

func (a *app) mainView(c *ui.Context) {
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		if !a.treeCollapsed {
			if a.sidebarCollapsed {
				a.sidebarRail(c)
			} else {
				a.globalSidebar(c)
			}
			if kb := a.contextKB(); kb != nil {
				a.kbPanel(c, kb)
			}
		}
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			a.errorBanner(c)
			switch {
			case a.doc != nil:
				a.editorView(c)
			case a.selected != "" && a.selectedIsDir:
				a.folderView(c)
			default:
				a.homeView(c)
			}
		})
		if a.treeCollapsed {
			a.treeExpandButton(c)
		}
	})
}

// contextKB is the knowledge base of the selected item, if any.
func (a *app) contextKB() *Node {
	if a.selected == "" {
		return nil
	}
	top := strings.Split(a.selected, "/")[0]
	for _, n := range a.tree {
		if n.Rel == top {
			return n
		}
	}
	return nil
}

func (a *app) goHome() {
	a.flushTitle()
	a.flushDoc()
	a.doc = nil
	a.selected = ""
	a.selectedIsDir = false
	a.update()
}

// openKB shows a knowledge base: the document open in it stays, and the
// first document opens otherwise.
func (a *app) openKB(n *Node) {
	a.flushTitle()
	a.flushDoc()
	*a.openState(n.Rel) = true
	if a.doc != nil && strings.HasPrefix(a.doc.Rel, n.Rel+"/") {
		a.selected = a.doc.Rel
		a.selectedIsDir = false
		a.refreshTree()
		a.update()
		return
	}
	a.doc = nil
	a.selected = n.Rel
	a.selectedIsDir = true
	a.refreshTree()
	if rel := firstDocRel(n); rel != "" {
		a.openDoc(rel)
	}
	a.update()
}

// openKBHome shows a knowledge base's home rather than a document.
func (a *app) openKBHome(n *Node) {
	a.flushTitle()
	a.flushDoc()
	a.doc = nil
	a.selected = n.Rel
	a.selectedIsDir = true
	*a.openState(n.Rel) = true
	a.refreshTree()
	a.update()
}

// firstDocRel is the first document in a node's tree, folders before the
// documents after them.
func firstDocRel(n *Node) string {
	for _, child := range n.Children {
		if !child.IsDir {
			return child.Rel
		}
		if rel := firstDocRel(child); rel != "" {
			return rel
		}
	}
	return ""
}

func (a *app) errorBanner(c *ui.Context) {
	if a.errMsg == "" {
		return
	}
	t := c.Theme()
	ui.Row(c).Padding(8, 14).Gap(10).AlignItems(ui.Center).Background(t.Danger.Alpha(0.10)).Children(func() {
		ui.Text(c, a.errMsg).Grow(1).TextColor(t.Danger).FontSize(13)
		if ui.Button(c, "关闭").Clicked() {
			a.errMsg = ""
		}
	})
}

func (a *app) globalSidebar(c *ui.Context) {
	t := c.Theme()
	bar := c.TitleBar()
	ui.Column(c).Width(200).Shrink(0).Background(ui.Hex(sidebarBg)).
		BorderWidth(0, 1, 0, 0).BorderColor(ui.Hex("#eff0f0")).Children(func() {
		// The traffic lights sit over the top of the sidebar, and the band
		// under them moves the window when dragged.
		ui.Row(c).Height(bar.Height).DragWindow()
		ui.Row(c).Padding(2, 16, 8, 16).Gap(9).AlignItems(ui.Center).DragWindow().Children(func() {
			brandLogo(c, 30, 8)
			ui.Text(c, "语燕").FontSize(18).Bold().Grow(1)
			collapse := a.chromeIconButton(c, iconChevronLeft, "收起侧栏", false)
			if collapse.Clicked() {
				a.sidebarCollapsed = true
				a.kbRailOpen = false
				a.update()
			}
		})

		ui.Row(c).Padding(0, 12).Children(func() {
			search := ui.Row(c).Fill().Padding(8, 10).Gap(7).Radius(8).AlignItems(ui.Center).
				MinHeight(34).Background(t.Surface).Border(1, t.Border).OnClick(a.openSearch)
			if search.Hovered() {
				search.Border(1, ui.Hex("#d9dbdd"))
			}
			search.Children(func() {
				ui.Icon(c, iconSearch).FontSize(15).TextColor(t.TextMuted)
				ui.Text(c, "搜索").FontSize(14).TextColor(t.TextMuted).Grow(1)
				ui.Text(c, "⌘K").FontSize(12).TextColor(t.TextMuted)
			})
		})

		ui.Column(c).Padding(8, 10).Gap(2).Children(func() {
			navRow(c, iconHome, "工作台", a.selected == "" && a.doc == nil, a.goHome)
			navRow(c, iconPencil, "小记", a.doc != nil && a.doc.Note, a.openNote)
			navRow(c, iconTrash, "回收站", false, func() {
				a.refreshTrash()
				a.trashOpen = true
				a.update()
			})
			navRow(c, iconGear, "设置", false, func() {
				a.settingsOpen = true
				a.update()
			})
		})

		ui.Row(c).Padding(16, 10, 2, 14).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "知识库").FontSize(12).Bold().TextColor(t.TextMuted).Grow(1)
			add := ui.ButtonBase(c).Label("新建或导入知识库").Tooltip("新建或导入知识库").
				Width(22).Height(22).Radius(6).Justify(ui.Center).AlignItems(ui.Center)
			if a.kbAddMenuOpen || a.kbImportMenuOpen || add.Hovered() {
				add.Background(ui.Hex(hoverBg))
			}
			add.Children(func() {
				ui.Icon(c, iconPlus).FontSize(15).TextColor(t.TextMuted)
			})
			if add.Clicked() {
				if a.kbAddMenuOpen || a.kbImportMenuOpen {
					a.kbAddMenuOpen = false
					a.kbImportMenuOpen = false
				} else {
					a.kbAddMenuOpen = true
				}
				a.update()
			}
			dropdownRight(c, add, &a.kbAddMenuOpen, func() {
				menuItem(c, iconBook, "新建知识库", func() {
					a.kbAddMenuOpen = false
					a.askNewKnowledgeBase()
					a.update()
				})
				menuSeparator(c)
				menuItem(c, iconDownload, "导入", func() {
					a.kbAddMenuOpen = false
					a.kbImportMenuOpen = true
					a.update()
				})
			})
			dropdownRight(c, add, &a.kbImportMenuOpen, func() {
				a.importMenuItems(c)
			})
		})

		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Padding(0, 10).Gap(2).Children(func() {
				if a.lakebookImportRunning() {
					a.lakebookImportRow(c)
				}
				if len(a.tree) == 0 {
					ui.Column(c).Padding(6, 10).Gap(6).Children(func() {
						ui.Text(c, "还没有知识库").FontSize(12).TextColor(t.TextMuted)
						if ui.Button(c, "创建知识库").Clicked() {
							a.askNewKnowledgeBase()
						}
					})
					return
				}
				for _, n := range a.tree {
					n := n
					current := false
					if kb := a.contextKB(); kb != nil && kb.Rel == n.Rel {
						current = true
					}
					row := ui.Row(c).Padding(5, 10).Gap(9).Radius(6).AlignItems(ui.Center).
						Height(32).OnClick(func() { a.openKB(n) })
					if row.RightClicked() {
						rel := n.Rel
						a.pending = func() {
							a.rowMenu = rel
							a.rowPlus = ""
							a.update()
						}
					}
					if current {
						row.Background(ui.Hex(sidebarSel))
					} else if row.Hovered() {
						row.Background(ui.Hex(hoverBg))
					}
					// Knowledge bases reorder among themselves by dragging:
					// the half of the row tells whether one goes above or
					// below the row's knowledge base.
					row.Drag(dragItem{Rel: n.Rel})
					if src, over := ui.DragOver[dragItem](row); over && !strings.Contains(src.Rel, "/") && src.Rel != n.Rel {
						insertMark(c, row, kbDragEdge(row))
					}
					if src, ok := ui.Drop[dragItem](row); ok {
						src, target, edge := src.Rel, n, kbDragEdge(row)
						a.pending = func() { a.dropKB(src, target, edge) }
					}
					row.Children(func() {
						ui.Icon(c, iconBook).FontSize(16).TextColor(ui.Hex(bookColor))
						ui.Text(c, n.Title()).FontSize(14).SingleLine().Grow(1)
						if row.Hovered() || current || a.rowMenu == n.Rel {
							more := ui.ButtonBase(c).Label("知识库操作").Tooltip("知识库操作").
								Width(22).Height(22).Radius(5).Justify(ui.Center).AlignItems(ui.Center)
							if more.Hovered() || a.rowMenu == n.Rel {
								more.Background(ui.Hex(hoverBg))
							}
							more.Children(func() {
								ui.Icon(c, iconMore).FontSize(14).TextColor(t.TextMuted)
							})
							if more.Clicked() {
								if a.rowMenu == n.Rel {
									a.rowMenu = ""
								} else {
									a.rowMenu = n.Rel
								}
								a.update()
							}
							open := a.rowMenu == n.Rel
							dropdownRight(c, more, &open, func() {
								if a.vault.IsExternal(n.Rel) {
									menuItem(c, iconDownload, "导出", func() {
										a.rowMenu = ""
										a.exportKnowledgeBase(n)
										a.update()
									})
									menuItem(c, iconFolder, "在 Finder 中显示", func() {
										a.rowMenu = ""
										if p, err := a.vault.Abs(n.Rel); err == nil {
											mygo.Shell.ShowItemInFolder(p)
										}
										a.update()
									})
									menuSeparator(c)
									menuItemDanger(c, iconTrash, "移除外部知识库", func() {
										a.rowMenu = ""
										a.askRemoveExternal(n.Rel)
										a.update()
									})
									return
								}
								menuItem(c, iconPencil, "重命名", func() {
									a.rowMenu = ""
									a.askRename(n.Rel)
									a.update()
								})
								menuItem(c, iconDownload, "导出", func() {
									a.rowMenu = ""
									a.exportKnowledgeBase(n)
									a.update()
								})
								menuSeparator(c)
								menuItemDanger(c, iconTrash, "删除", func() {
									a.rowMenu = ""
									a.askDelete(n.Rel)
									a.update()
								})
							})
							if a.rowMenu == n.Rel && !open {
								a.rowMenu = ""
							}
						}
					})
				}
			})
		})
	})
}

// runPending carries out the action a row reported during this frame,
// after the rows around it have been built.
func (a *app) runPending() {
	if a.pending == nil {
		return
	}
	fn := a.pending
	a.pending = nil
	fn()
}

func (a *app) lakebookImportRow(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().Height(32).Padding(3, 9).Gap(2).Radius(6).
		Background(t.Accent.Alpha(0.08)).Border(1, t.Accent.Alpha(0.20)).
		Children(func() {
			ui.Text(c, "正在导入语雀 Lakebook…").FontSize(12).SingleLine().TextColor(t.TextMuted)
			ui.Progress(c, -1).FillWidth().Height(4).Label("Lakebook 导入进度")
		})
}

// kbPanel is the knowledge base's own column: its tree and home.
func (a *app) kbPanel(c *ui.Context, kb *Node) {
	t := c.Theme()
	ui.Column(c).Width(a.fileTreeWidth()).Shrink(0).Background(t.Surface).
		BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
		ui.Row(c).Padding(topInset+12, 12, 12, 12).Gap(4).AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Text(c, kb.Title()).FontSize(16).Bold().SingleLine().Grow(1)
			collapse := a.chromeIconButton(c, iconChevronLeft, "收起目录", false)
			if collapse.Clicked() {
				a.treeCollapsed = true
				a.kbRailOpen = false
				a.update()
			}
			add := ui.ButtonBase(c).Label("新建").Tooltip("新建").Width(28).Height(28).
				Radius(6).Justify(ui.Center).AlignItems(ui.Center)
			if a.kbMenuOpen || add.Hovered() {
				add.Background(ui.Hex(hoverBg))
			}
			add.Children(func() {
				ui.Icon(c, iconPlus).FontSize(16).TextColor(ui.Hex("#262626"))
			})
			if add.Clicked() {
				a.kbMenuOpen = !a.kbMenuOpen
			}
			dropdownRight(c, add, &a.kbMenuOpen, func() {
				menuItem(c, iconFile, "新建文档", func() {
					a.kbMenuOpen = false
					a.askNewDocIn(kb.Rel)
					a.update()
				})
				menuItem(c, iconFolder, "新建目录", func() {
					a.kbMenuOpen = false
					a.askNewFolderIn(kb.Rel)
					a.update()
				})
				menuItem(c, iconDownload, "导入", func() {
					a.kbMenuOpen = false
					a.importDocument(kb)
					a.update()
				})
				if a.clipboard != nil {
					menuItem(c, iconPaste, "粘贴", func() {
						a.kbMenuOpen = false
						a.pasteAt(kb.Rel)
						a.update()
					})
				}
				if !a.vault.IsExternal(kb.Rel) {
					menuSeparator(c)
					menuItem(c, iconPencil, "重命名知识库", func() {
						a.kbMenuOpen = false
						a.askRename(kb.Rel)
						a.update()
					})
				}
			})
		})

		ui.Column(c).Padding(0, 8).Gap(1).Children(func() {
			home := ui.Row(c).Padding(6, 10).Gap(9).Radius(6).AlignItems(ui.Center).
				MinHeight(32).OnClick(func() { a.openKBHome(kb) })
			if a.selected == kb.Rel && a.doc == nil {
				home.Background(ui.Hex(sidebarSel))
			} else if home.Hovered() {
				home.Background(ui.Hex(hoverBg))
			}
			home.Children(func() {
				ui.Icon(c, iconHome).FontSize(15).TextColor(t.TextMuted)
				ui.Text(c, "首页").FontSize(14)
			})
		})

		ui.Row(c).Padding(14, 16, 2, 16).Children(func() {
			ui.Text(c, "目录").FontSize(12).Bold().TextColor(t.TextMuted)
		})

		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Role(ui.RoleTree).Padding(0, 8).Gap(1).Children(func() {
				if len(kb.Children) == 0 {
					ui.Text(c, "还没有文档").FontSize(13).TextColor(t.TextMuted).Padding(4, 10)
					return
				}
				a.treeRows(c, kb.Children, 0)
			})
		})

		a.fileTreeResizer(c)
	})
}

// fileTreeResizer is the draggable edge between the knowledge base tree
// and the document area.
func (a *app) fileTreeResizer(c *ui.Context) {
	t := c.Theme()
	edge := ui.Box(c).Absolute().Top(0).Bottom(0).Right(-3).Width(7).
		Role(ui.RoleSplitter).Label("调整文件树宽度").Cursor(ui.CursorResizeEW)
	if edge.Hovered() || edge.Pressed() {
		edge.Background(t.Accent.Alpha(0.28))
	}
	if dx, _, held := edge.Dragged(); held && dx != 0 {
		width := clampPanelWidth(a.fileTreeWidth()+dx, defaultFileTreeWidth, minFileTreeWidth, maxFileTreeWidth)
		if width != a.fileTreeWidth() {
			a.cfg.FileTreeWidth = width
			a.update()
		}
	}
	if _, _, done := edge.DragEnded(); done {
		if err := saveConfig(a.cfg); err != nil {
			a.setError(err)
		}
	}
}

// treeRows builds the rows of a knowledge base's tree, and the rows of the
// folders open among them.
func (a *app) treeRows(c *ui.Context, nodes []*Node, depth int) {
	for _, n := range nodes {
		a.treeRow(c, n, depth)
		if len(n.Children) > 0 && depth+1 < maxTreeLevel && *a.openState(n.ChildrenRel()) {
			a.treeRows(c, n.Children, depth+1)
		}
	}
}

// treeRow is a row of the tree: its arrow, icon and title, and the
// actions of the node while it is under the pointer, chosen or opened.
func (a *app) treeRow(c *ui.Context, n *Node, depth int) {
	t := c.Theme()
	branch := n.IsDir || len(n.Children) > 0
	open := *a.openState(n.ChildrenRel())
	row := ui.Row(c.Key(n.Rel)).Role(ui.RoleTreeItem).Level(depth + 1).
		FillWidth().Height(32).Radius(6).Gap(6).
		AlignItems(ui.Center).PaddingStart(4 + float32(depth)*16).PaddingEnd(4)
	if branch {
		row.Expanded(open)
	}
	row.OnClick(func() { a.chooseTree(n) })
	switch {
	case a.selected == n.Rel:
		row.Background(t.Accent.Alpha(0.12))
	case row.Hovered():
		row.Background(ui.Hex(hoverBg))
	}
	row.Children(func() {
		if branch {
			arrow := ui.ButtonBase(c).Label("展开").Tooltip("展开或收起").
				Width(16).Height(16).Radius(4).Justify(ui.Center).AlignItems(ui.Center)
			if arrow.Hovered() {
				arrow.Background(ui.Hex(hoverBg))
			}
			arrow.Children(func() {
				icon := iconChevronRight
				if open {
					icon = iconChevron
				}
				ui.Icon(c, icon).FontSize(12).TextColor(t.TextMuted)
			})
			if arrow.Clicked() {
				*a.openState(n.ChildrenRel()) = !open
				a.update()
			}
		} else {
			ui.Box(c).Width(16).Height(16)
		}
		icon := iconFile
		if n.IsDir {
			icon = iconFolder
		}
		ui.Icon(c, icon).FontSize(15).TextColor(t.TextMuted)
		ui.Text(c, n.Title()).FontSize(15).SingleLine().Grow(1)
		if row.Hovered() || a.rowMenu == n.Rel || a.rowPlus == n.Rel || a.selected == n.Rel {
			if a.canNest(n) {
				plus := ui.ButtonBase(c).Label("添加").Tooltip("添加").
					Width(22).Height(22).Radius(5).Justify(ui.Center).AlignItems(ui.Center)
				if plus.Hovered() || a.rowPlus == n.Rel {
					plus.Background(ui.Hex(hoverBg))
				}
				plus.Children(func() {
					ui.Icon(c, iconPlus).FontSize(14).TextColor(t.TextMuted)
				})
				if plus.Clicked() {
					if a.rowPlus == n.Rel {
						a.rowPlus = ""
					} else {
						a.rowPlus = n.Rel
						a.rowMenu = ""
					}
					a.update()
				}
				openPlus := a.rowPlus == n.Rel
				dropdownRight(c, plus, &openPlus, func() {
					menuItem(c, iconFile, "文档", func() {
						a.rowPlus = ""
						a.newChildDoc(n)
						a.update()
					})
					menuItem(c, iconFolder, "分组", func() {
						a.rowPlus = ""
						a.newChildGroup(n)
						a.update()
					})
					menuItem(c, iconDownload, "导入", func() {
						a.rowPlus = ""
						a.importDocument(n)
						a.update()
					})
				})
				if a.rowPlus == n.Rel && !openPlus {
					a.rowPlus = ""
				}
			}
			more := ui.ButtonBase(c).Label("更多操作").Tooltip("更多操作").
				Width(22).Height(22).Radius(5).Justify(ui.Center).AlignItems(ui.Center)
			if more.Hovered() || a.rowMenu == n.Rel {
				more.Background(ui.Hex(hoverBg))
			}
			more.Children(func() {
				ui.Icon(c, iconMore).FontSize(14).TextColor(t.TextMuted)
			})
			if more.Clicked() {
				if a.rowMenu == n.Rel {
					a.rowMenu = ""
				} else {
					a.rowMenu = n.Rel
					a.rowPlus = ""
				}
				a.update()
			}
			open := a.rowMenu == n.Rel
			dropdownRight(c, more, &open, func() {
				for _, act := range a.nodeActions(n) {
					act := act
					if act.label == "" {
						menuSeparator(c)
						continue
					}
					run := func() {
						a.rowMenu = ""
						act.run()
						a.update()
					}
					if act.danger {
						menuItemDanger(c, act.icon, act.label, run)
					} else {
						menuItem(c, act.icon, act.label, run)
					}
				}
			})
			if a.rowMenu == n.Rel && !open {
				a.rowMenu = ""
			}
		}
	})

	row.Drag(dragItem{Rel: n.Rel})
	if _, over := ui.DragOver[dragItem](row); over {
		if edge := dragEdge(row); edge != "" {
			insertMark(c, row, edge)
		} else {
			row.Background(t.Accent.Alpha(0.14))
		}
	}
	if src, ok := ui.Drop[dragItem](row); ok {
		src, n, edge := src.Rel, n, dragEdge(row)
		a.pending = func() { a.dropTreeItem(src, n, edge) }
	}

	// A right click opens the same menu as the row's More button, rather
	// than the system's own.
	if row.RightClicked() {
		rel := n.Rel
		a.pending = func() {
			a.rowMenu = rel
			a.rowPlus = ""
			a.update()
		}
	}
}

// dragEdge tells which strip of a dragged-over row the pointer is in: the
// top or bottom one inserts the dragged node there, the middle nests it
// into the row's node.
func dragEdge(e ui.Element) string {
	_, y, _ := e.PointerPosition()
	h := e.Bounds().H
	if h <= 0 || y < 0 {
		return ""
	}
	if y < h*0.3 {
		return "top"
	}
	if y > h*0.7 {
		return "bottom"
	}
	return ""
}

// kbDragEdge tells which half of a knowledge base row the pointer is in:
// a knowledge base dropped there goes above or below the row's, as
// knowledge bases only reorder among themselves.
func kbDragEdge(e ui.Element) string {
	_, y, _ := e.PointerPosition()
	if y >= e.Bounds().H/2 {
		return "bottom"
	}
	return "top"
}

// insertMark draws the line where a dragged node would land.
func insertMark(c *ui.Context, e ui.Element, edge string) {
	accent := c.Theme().Accent
	e.DrawOver(func(p *ui.Painter, r ui.Rect) {
		y := r.Y
		if edge == "bottom" {
			y = r.Y + r.H - 2
		}
		p.Fill(ui.Rect{X: r.X + 2, Y: y, W: r.W - 4, H: 2}, accent, 1)
	})
}

// nodeAction is an action a node's row menu offers.
type nodeAction struct {
	label  string
	icon   *ui.SVG
	danger bool
	run    func()
}

// nodeActions are the actions of a node, which differ for a document and a
// folder.
func (a *app) nodeActions(n *Node) []nodeAction {
	rel := n.Rel
	finder := func() {
		if p, err := a.vault.Abs(rel); err == nil {
			mygo.Shell.ShowItemInFolder(p)
		}
	}
	var acts []nodeAction

	acts = append(acts, nodeAction{label: "重命名", icon: iconPencil, run: func() { a.askRename(rel) }})
	acts = append(acts,
		nodeAction{label: "仅复制本资源", icon: iconCopy, run: func() {
			if d := a.findNode(rel); d != nil {
				a.copyNode(d, false)
			}
		}},
		nodeAction{label: "复制全部子资源", icon: iconCopy, run: func() {
			if d := a.findNode(rel); d != nil {
				a.copyNode(d, true)
			}
		}},
	)
	if a.clipboard != nil && a.canNest(n) {
		acts = append(acts, nodeAction{label: "粘贴", icon: iconPaste, run: func() {
			if d := a.findNode(rel); d != nil {
				a.pasteInto(d)
			}
		}})
	}
	if !n.IsDir {
		acts = append(acts,
			nodeAction{label: "复制链接", icon: iconLink, run: func() { a.copyNodeLink(rel) }},
			nodeAction{label: "置顶文档", icon: iconPin, run: func() {
				if d := a.findNode(rel); d != nil {
					a.pinDoc(d)
				}
			}},
		)
	}
	if a.canMoveOut(n) {
		acts = append(acts, nodeAction{label: "移出目录", icon: iconMoveOut, run: func() {
			if d := a.findNode(rel); d != nil {
				a.moveOut(d)
			}
		}})
	}
	if !n.IsDir {
		acts = append(acts,
			nodeAction{label: "导出 Markdown（含图片）", icon: iconDownload, run: func() {
				a.openDoc(rel)
				a.exportDoc("md")
			}},
			nodeAction{label: "导出为 PDF", icon: iconDownload, run: func() {
				a.openDoc(rel)
				a.exportDoc("pdf")
			}},
		)
	}
	acts = append(acts,
		nodeAction{label: "在 Finder 中显示", icon: iconFolder, run: finder},
		nodeAction{},
		nodeAction{label: "删除", icon: iconTrash, danger: true, run: func() { a.askDelete(rel) }},
	)
	return acts
}

func (a *app) homeView(c *ui.Context) {
	t := c.Theme()
	recent := a.vault.Recent()
	kbs, docs := a.counts()
	ui.Column(c).Fill().Children(func() {
		ui.Box(c).Absolute().Left(0).Top(0).Right(0).Height(c.TitleBar().Height).DragWindow()
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Padding(40, 44).Gap(22).MaxWidth(900).Children(func() {
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, "工作台").FontSize(28).Bold()
					ui.Textf(c, "%d 个知识库 · %d 篇文档", kbs, docs).FontSize(14).TextColor(t.TextMuted)
				})

				ui.Row(c).Gap(12).Children(func() {
					actionCard(c, iconFile, "新建文档", "写一篇 Markdown 文档", a.askNewDocAt)
					actionCard(c, iconBook, "新建知识库", "用知识库整理内容", a.askNewKnowledgeBase)
					actionCard(c, iconSearch, "搜索文档", "在标题和正文中查找", a.openSearch)
					importCard := actionCard(c, iconDownload, "导入知识库", "复制、引用、Lakebook", func() {
						a.importMenuOpen = !a.importMenuOpen
						a.update()
					})
					dropdownRight(c, importCard, &a.importMenuOpen, func() {
						a.importMenuItems(c)
					})
				})

				ui.Text(c, "最近打开").FontSize(16).Bold().Padding(6, 0, 0, 0)
				ui.Column(c).Padding(6).Radius(10).Background(t.Surface).Border(1, t.Border).Children(func() {
					if len(recent) == 0 {
						ui.Column(c).Padding(20).Gap(4).Children(func() {
							ui.Text(c, "还没有打开过的文档").FontSize(14)
							ui.Text(c, "从左侧选择知识库，或新建一篇文档开始。").FontSize(13).TextColor(t.TextMuted)
						})
						return
					}
					for _, r := range recent {
						rel := r.Rel
						opened := r.OpenedAt.Local()
						row := ui.Row(c).Key(rel).Padding(9, 10).Gap(10).Radius(6).AlignItems(ui.Center).
							OnClick(func() {
								a.openDoc(rel)
								a.update()
							})
						if row.Hovered() {
							row.Background(ui.Hex("#f7f8fa"))
						}
						row.Children(func() {
							ui.Icon(c, iconFile).FontSize(16).TextColor(t.TextMuted)
							ui.Text(c, titleOf(rel)).Grow(1).SingleLine()
							ui.Text(c, parentLabel(rel)).FontSize(13).TextColor(t.TextMuted).SingleLine()
							ui.Text(c, opened.Format("01-02 15:04")).FontSize(13).TextColor(t.TextMuted).
								Width(96).TextAlign(ui.End)
						})
					}
				})
			})
		})
	})
}

func parentLabel(rel string) string {
	parent := parentRel(rel)
	if parent == "" {
		return ""
	}
	return parent
}

func (a *app) counts() (kbs, docs int) {
	var walk func([]*Node)
	walk = func(nodes []*Node) {
		for _, n := range nodes {
			if !n.IsDir {
				docs++
			}
			walk(n.Children)
		}
	}
	walk(a.tree)
	return len(a.tree), docs
}

func (a *app) folderView(c *ui.Context) {
	t := c.Theme()
	n := a.findNode(a.selected)
	if n == nil {
		a.selected = ""
		return
	}
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Padding(28, 40, 18, 40).Gap(8).AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Column(c).Gap(4).Grow(1).MinWidth(0).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					icon := iconFolder
					color := t.TextMuted
					if !strings.Contains(n.Rel, "/") {
						icon, color = iconBook, ui.Hex(bookColor)
					}
					ui.Icon(c, icon).FontSize(22).TextColor(color)
					ui.Text(c, n.Title()).FontSize(24).Bold().SingleLine()
				})
				ui.Text(c, n.Rel).FontSize(13).TextColor(t.TextMuted).SingleLine()
			})
			if ui.PrimaryButton(c, "新建文档").Clicked() {
				a.askNewDoc()
			}
			if ui.Button(c, "新建目录").Clicked() {
				a.askNewFolder()
			}
			externalKB := a.vault.IsExternal(n.Rel) && !strings.Contains(n.Rel, "/")
			more := menuButton(c, "更多", &a.moreMenuOpen)
			dropdown(c, more, &a.moreMenuOpen, func() {
				if !externalKB {
					menuItem(c, iconPencil, "重命名", func() {
						a.moreMenuOpen = false
						a.askRename(n.Rel)
						a.update()
					})
				}
				menuItem(c, iconFolder, "在 Finder 中显示", func() {
					a.moreMenuOpen = false
					if p, err := a.vault.Abs(n.Rel); err == nil {
						mygo.Shell.ShowItemInFolder(p)
					}
					a.update()
				})
				if externalKB {
					menuSeparator(c)
					menuItemDanger(c, iconTrash, "移除外部知识库", func() {
						a.moreMenuOpen = false
						a.askRemoveExternal(n.Rel)
						a.update()
					})
				} else {
					menuSeparator(c)
					menuItemDanger(c, iconTrash, "删除", func() {
						a.moreMenuOpen = false
						a.askDelete(n.Rel)
						a.update()
					})
				}
			})
		})
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Padding(8, 40).MaxWidth(900).Gap(2).Children(func() {
				if len(n.Children) == 0 {
					ui.Column(c).Padding(30, 10).Gap(4).Children(func() {
						ui.Text(c, "这个目录还是空的").FontSize(15)
						ui.Text(c, "新建一篇文档，或者把文档拖进来。").FontSize(13).TextColor(t.TextMuted)
					})
					return
				}
				for _, child := range n.Children {
					child := child
					row := ui.Row(c).Key(child.Rel).Padding(10, 10).Gap(10).Radius(6).
						AlignItems(ui.Center).OnClick(func() {
						a.selected = child.Rel
						a.selectedIsDir = child.IsDir
						if child.IsDir {
							*a.openState(child.Rel) = true
							a.doc = nil
							a.refreshTree()
						} else {
							a.openDoc(child.Rel)
						}
						a.update()
					})
					if row.Hovered() {
						row.Background(ui.Hex("#f7f8fa"))
					}
					row.Children(func() {
						icon := iconFile
						if child.IsDir {
							icon = iconFolder
						}
						ui.Icon(c, icon).FontSize(16).TextColor(t.TextMuted)
						ui.Text(c, child.Title()).Grow(1).SingleLine()
						if child.IsDir {
							ui.Textf(c, "%d 项", len(child.Children)).FontSize(13).TextColor(t.TextMuted)
						} else {
							ui.Text(c, "Markdown").FontSize(13).TextColor(t.TextMuted)
						}
					})
				}
			})
		})
	})
}

func (a *app) openSearch() {
	a.searchOpen = true
	a.searchFocus = true
	a.searchQuery = ""
	a.searchScope = ""
	a.searchResults = nil
	a.searchNote = ""
	a.update()
}

func (a *app) openEditorTarget(rel string) {
	a.searchOpen = false
	a.openDoc(rel)
	a.update()
}
