package app

import (
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

const (
	dashboardWeeks = 52
)

type dashboardData struct {
	generated time.Time
	kbs       int
	articles  int
	words     int
	activity  map[time.Time]int
}

type dashboardDay struct {
	date   time.Time
	count  int
	future bool
}

// dashboard returns the cached statistics for the current day, scanning
// every document in the tree when the cache is empty or stale.
func (a *app) dashboard(now time.Time) dashboardData {
	today := dayOf(now)
	if a.dashboardCache != nil && a.dashboardCache.generated.Equal(today) {
		return *a.dashboardCache
	}
	data := a.scanDashboard(now)
	data.generated = today
	a.dashboardCache = &data
	return data
}

func (a *app) scanDashboard(now time.Time) dashboardData {
	data := dashboardData{
		kbs:      len(a.tree),
		activity: map[time.Time]int{},
	}
	if a.vault == nil {
		return data
	}
	var walk func([]*Node)
	walk = func(nodes []*Node) {
		for _, n := range nodes {
			if n.IsDir {
				walk(n.Children)
				continue
			}
			data.articles++
			md, mtime, err := a.vault.ReadDoc(n.Rel)
			if err != nil {
				continue
			}
			data.words += countWords(richtext.ParseMarkdown(md).PlainText())
			data.activity[dayOf(mtime.In(now.Location()))]++
		}
	}
	walk(a.tree)
	return data
}

func dashboardActivityWeeks(now time.Time, activity map[time.Time]int) [][]dashboardDay {
	today := dayOf(now)
	currentWeek := today.AddDate(0, 0, -int(today.Weekday()))
	start := currentWeek.AddDate(0, 0, -7*(dashboardWeeks-1))
	weeks := make([][]dashboardDay, dashboardWeeks)
	for week := range weeks {
		weeks[week] = make([]dashboardDay, 7)
		for weekday := range weeks[week] {
			date := start.AddDate(0, 0, week*7+weekday)
			weeks[week][weekday] = dashboardDay{
				date:   date,
				count:  activity[date],
				future: date.After(today),
			}
		}
	}
	return weeks
}

func dayOf(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}

func dashboardMetric(c *ui.Context, value, label string) {
	t := c.Theme()
	ui.Column(c).Grow(1).Gap(2).Children(func() {
		ui.Text(c, value).FontSize(28).Bold()
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
	})
}

func dashboardTileColor(level int) ui.Color {
	switch level {
	case 1:
		return ui.Hex("#b7ebd2")
	case 2:
		return ui.Hex("#75d6aa")
	case 3:
		return ui.Hex("#2fc780")
	case 4:
		return ui.Hex("#00a65f")
	default:
		return ui.Hex("#ebedf0")
	}
}

func dashboardLevel(count int) int {
	switch {
	case count == 0:
		return 0
	case count == 1:
		return 1
	case count <= 3:
		return 2
	case count <= 5:
		return 3
	default:
		return 4
	}
}

func dashboardNumber(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func (a *app) dashboardView(c *ui.Context) {
	t := c.Theme()
	now := time.Now()
	data := a.dashboard(now)
	weeks := dashboardActivityWeeks(now, data.activity)
	ui.Column(c).Gap(12).Children(func() {
		ui.Column(c).Padding(18).Gap(15).Radius(10).Background(t.Surface).Border(1, t.Border).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				dashboardMetric(c, dashboardNumber(data.kbs), "知识库")
				dashboardMetric(c, dashboardNumber(data.articles), "文章")
				dashboardMetric(c, dashboardNumber(data.words), "总字数")
			})

			ui.Box(c).Height(1).Background(t.Border)
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "近 52 周活跃度").FontSize(14).Bold()
				ui.Box(c).Grow(1)
				ui.Textf(c, "%s 至 %s", weeks[0][0].date.Format("2006.01.02"), dayOf(now).Format("2006.01.02")).
					FontSize(11).TextColor(t.TextMuted)
			})

			wdayLabels := []string{"", "一", "", "三", "", "五", ""}
			ui.ScrollHorizontal(c).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					for weekday := 0; weekday < 7; weekday++ {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							if wdayLabels[weekday] == "" {
								ui.Box(c).Width(12).Height(7)
							} else {
								ui.Text(c, wdayLabels[weekday]).Width(12).FontSize(9).
									TextColor(t.TextMuted).TextAlign(ui.End)
							}
							ui.Row(c).Gap(2).Children(func() {
								for _, week := range weeks {
									day := week[weekday]
									tile := ui.Box(c).Width(7).Height(7).Radius(2)
									if !day.future {
										tile.Background(dashboardTileColor(dashboardLevel(day.count))).
											Tooltip(fmt.Sprintf("%s · %d 篇文章更新", day.date.Format("2006年01月02日"), day.count))
									}
								}
							})
						})
					}
				})
			})

			ui.Row(c).Gap(4).AlignItems(ui.Center).Justify(ui.End).Children(func() {
				ui.Text(c, "少").FontSize(10).TextColor(t.TextMuted)
				for level := 0; level <= 4; level++ {
					ui.Box(c).Width(8).Height(8).Radius(2).Background(dashboardTileColor(level))
				}
				ui.Text(c, "多").FontSize(10).TextColor(t.TextMuted)
			})
		})
	})
}
