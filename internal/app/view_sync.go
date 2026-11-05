package app

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"swift/internal/remote"
)

// syncSettingsSection is the remote sync part of the settings window.
func (a *app) syncSettingsSection(c *ui.Context) {
	t := c.Theme()
	s := &a.cfg.Sync
	if s.Enabled {
		switch s.Provider {
		case remote.ProviderS3, remote.ProviderNFS, remote.ProviderSMB:
		default:
			s.Provider = remote.ProviderS3
		}
	}
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, "远端同步").FontSize(13).Bold()
		ui.Text(c, "把文档目录单向镜像到远端：本地改动会自动上传，从回收站彻底删除的文档也会从远端删除。外部引用的知识库不参与同步。").
			FontSize(12).TextColor(t.TextMuted)

		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "启用").FontSize(13)
			sw := ui.Switch(c, &s.Enabled)
			if sw.Changed() {
				if s.Enabled && s.Provider == "" {
					s.Provider = remote.ProviderS3
				}
				a.update()
			}
			ui.Box(c).Width(10)
			ui.Text(c, "类型").FontSize(13)
			label := providerLabel(s.Provider)
			if label == "" {
				label = "S3"
			}
			sel := ui.Select(c, &label, []string{"S3", "NFS", "SMB"}).Label("远端类型").Width(110)
			if sel.Changed() {
				s.Provider = providerFromLabel(label)
				a.update()
			}
			ui.Box(c).Grow(1)
			ui.Text(c, "自动同步").FontSize(13)
			interval := syncIntervalLabel(*s)
			isel := ui.Select(c, &interval, []string{"关闭（仅手动）", "15 秒", "30 秒", "1 分钟", "5 分钟"}).
				Label("自动同步").Width(140)
			if isel.Changed() {
				if interval == "关闭（仅手动）" {
					s.Manual = true
				} else {
					s.Manual = false
					s.IntervalSeconds = intervalSeconds(interval)
				}
				a.update()
			}
		})

		if s.Enabled && s.Manual {
			ui.Text(c, "自动同步已关闭：只有点“立即同步”时才会上传或删除。").
				FontSize(12).TextColor(t.TextMuted)
		}

		if !s.Enabled {
			ui.Text(c, "打开后，语燕会把知识库里的 Markdown、图片和附件镜像到远端。").
				FontSize(12).TextColor(t.TextMuted)
			return
		}

		switch s.Provider {
		case remote.ProviderS3:
			a.s3SyncFields(c)
		case remote.ProviderNFS, remote.ProviderSMB:
			a.dirSyncFields(c)
		default:
			ui.Text(c, "请选择 S3、NFS 或 SMB。").FontSize(12).TextColor(t.TextMuted)
		}
		a.syncStatus(c)
	})
}

func (a *app) syncInput(c *ui.Context, label, placeholder string, value *string, password bool) {
	t := c.Theme()
	ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
	input := ui.TextInput(c, value).Label(label).Placeholder(placeholder).FillWidth()
	if password {
		input = input.Password()
	}
}

func (a *app) s3SyncFields(c *ui.Context) {
	t := c.Theme()
	s := &a.cfg.Sync
	ui.Column(c).Gap(6).Children(func() {
		ui.Column(c).Gap(3).Children(func() {
			a.syncInput(c, "服务地址", "留空使用 Amazon S3；自建存储填 https://minio.example.com", &s.Endpoint, false)
		})
		ui.Row(c).Gap(12).Children(func() {
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				a.syncInput(c, "区域", "us-east-1", &s.Region, false)
			})
			ui.Column(c).Grow(2).Gap(3).Children(func() {
				a.syncInput(c, "存储桶", "my-notes", &s.Bucket, false)
			})
		})
		ui.Row(c).Gap(12).Children(func() {
			ui.Column(c).Grow(2).Gap(3).Children(func() {
				a.syncInput(c, "前缀", "可选，例如 notes", &s.Prefix, false)
			})
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				a.syncInput(c, "Access Key", "访问密钥 ID", &s.AccessKey, false)
			})
		})
		ui.Column(c).Gap(3).Children(func() {
			a.syncInput(c, "Secret Key", "访问密钥", &s.SecretKey, true)
		})
		ui.Column(c).Gap(3).Children(func() {
			a.syncInput(c, "Session Token（可选）", "临时凭证的会话令牌", &s.SessionToken, true)
		})
		ui.Checkbox(c, &s.PathStyle, "路径式地址（MinIO、Ceph 等自建存储通常需要）")
		ui.Text(c, "凭据保存在本机的设置文件中；区域留空时使用 us-east-1。").
			FontSize(12).TextColor(t.TextMuted)
	})
}

func (a *app) dirSyncFields(c *ui.Context) {
	t := c.Theme()
	s := &a.cfg.Sync
	label := "挂载点"
	placeholder := "/Volumes/notes"
	hint := "先在本机挂载 NFS 共享，再选择挂载后的文件夹。"
	if s.Provider == remote.ProviderSMB {
		label = "共享目录"
		hint = "先在 Finder 中连接服务器（⌘K）挂载 SMB 共享，再选择挂载后的文件夹。"
	}
	ui.Column(c).Gap(4).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
		ui.Row(c).Gap(8).Children(func() {
			ui.TextInput(c, &s.MountPath).Label(label).Placeholder(placeholder).Grow(1)
			if ui.Button(c, "选择…").Clicked() {
				paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
					Parent:      a.win,
					Title:       "选择" + label,
					ButtonLabel: "选择",
					Directory:   true,
				})
				if err != nil {
					a.syncNote.set(err.Error(), true)
				} else if len(paths) > 0 {
					s.MountPath = paths[0]
				}
				a.update()
			}
		})
		ui.Text(c, hint).FontSize(12).TextColor(t.TextMuted)
	})
}

func (a *app) syncStatus(c *ui.Context) {
	t := c.Theme()
	ui.Box(c).Height(1).Background(t.Border)
	job := a.syncJob
	if job == nil {
		ui.Text(c, "同步尚未启动，点“完成”后开始。").FontSize(12).TextColor(t.TextMuted)
	} else {
		st := job.engine.Status()
		text := "等待首次同步"
		if job.manual {
			text = "仅手动同步，尚未同步"
		}
		switch {
		case job.busy.Load() || st.Syncing:
			text = "正在同步…"
		case !st.LastSync.IsZero():
			text = fmt.Sprintf("上次同步 %s · 上传 %d · 删除 %d · 远端 %d 个文件",
				st.LastSync.Local().Format("01-02 15:04:05"), st.Uploaded, st.Deleted, st.Files)
			if job.manual {
				text += "（仅手动）"
			}
		}
		description := st.Description
		if job.manual {
			description += " · 仅手动"
		}
		ui.Text(c, text).FontSize(12).TextColor(t.TextMuted)
		ui.Text(c, description).FontSize(11).TextColor(t.TextMuted).SingleLine()
	}
}

// syncFooterButtons are the sync actions that stay visible below the
// settings, outside the scrolling part.
func (a *app) syncFooterButtons(c *ui.Context) {
	if !a.cfg.Sync.Enabled {
		return
	}
	if ui.Button(c, "测试连接").Padding(3, 10).Clicked() {
		a.testSync()
		a.update()
	}
	if ui.PrimaryButton(c, "立即同步").Padding(3, 10).Clicked() {
		if err := a.applySyncSettings(); err != nil {
			a.syncNote.set(err.Error(), true)
			a.syncNote.setResult(&syncResultState{
				Title:   "远端同步失败",
				Message: "同步设置没有通过检查。",
				Detail:  err.Error(),
				Failed:  true,
			})
		} else if !a.syncNow() {
			msg, _ := a.syncNote.get()
			if msg == "" {
				msg = "同步尚未启动。"
			}
			a.syncNote.setResult(&syncResultState{Title: "远端同步失败", Message: msg, Failed: true})
		}
		a.update()
	}
}

// syncResultModal reports what 立即同步 did: a quiet popup with the
// number of files or the error, over the settings it was pressed in.
func (a *app) syncResultModal(c *ui.Context) {
	r := a.syncResult
	if r == nil {
		return
	}
	t := c.Theme()
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(480).Padding(22).Gap(12).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				color, icon := t.Accent, iconCheck
				if r.Failed {
					color, icon = t.Danger, iconAlert
				}
				ui.Box(c).Width(28).Height(28).Radius(14).Background(color.Alpha(0.12)).
					Justify(ui.Center).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, icon).FontSize(15).TextColor(color)
				})
				ui.Text(c, r.Title).FontSize(17).Bold()
			})
			ui.Text(c, r.Message).FontSize(14)
			if r.Detail != "" {
				ui.Scroll(c).MaxHeight(180).Children(func() {
					ui.Text(c, r.Detail).FontSize(12).TextColor(t.TextMuted).Selectable()
				})
			}
			ui.Row(c).Justify(ui.End).Children(func() {
				if ui.PrimaryButton(c, "好").Clicked() {
					a.syncResult = nil
					open = false
				}
			})
		})
	})
	if !open {
		a.syncResult = nil
	}
}
