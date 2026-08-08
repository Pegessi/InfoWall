package feishusync

import (
	"crypto/sha256"
	"encoding/hex"
	"html"
	"sort"
	"strings"
	"time"
)

const (
	ManagedHeading  = "InfoWall 自动同步"
	EndMarkerPrefix = "INFOWALL_SYNC_END:"
)

type ProjectView struct {
	ID   string
	Name string
}

type DemandView struct {
	ID             string
	Title          string
	Description    string
	Status         string
	Priority       string
	ProjectID      string
	ProjectHint    string
	NextAction     string
	BlockedReason  string
	LatestProgress string
	SourceURL      string
	UpdatedAt      time.Time
	CompletedAt    *time.Time
}

type Snapshot struct {
	Projects  []ProjectView
	Demands   []DemandView
	Generated time.Time
}

type Rendered struct {
	ManagedXML string
	Hash       string
}

func Render(snapshot Snapshot) Rendered {
	generated := snapshot.Generated
	if generated.IsZero() {
		generated = time.Now()
	}

	projects := append([]ProjectView(nil), snapshot.Projects...)
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
	demands := append([]DemandView(nil), snapshot.Demands...)
	sort.SliceStable(demands, func(i, j int) bool {
		if priorityRank(demands[i].Priority) != priorityRank(demands[j].Priority) {
			return priorityRank(demands[i].Priority) < priorityRank(demands[j].Priority)
		}
		return demands[i].UpdatedAt.After(demands[j].UpdatedAt)
	})

	var body strings.Builder
	body.WriteString("<h1>" + ManagedHeading + "</h1>")
	body.WriteString("<p>更新时间：" + escape(generated.In(time.Local).Format("2006-01-02 15:04")) + "</p>")
	body.WriteString(renderSummary(demands))

	pending := filterDemands(demands, func(d DemandView) bool { return d.Status == "pending" })
	body.WriteString("<h2>待确认需求</h2>")
	body.WriteString(renderTable(pending, nil))

	body.WriteString("<h2>按项目</h2>")
	projectNames := make(map[string]string, len(projects))
	for _, project := range projects {
		projectNames[project.ID] = project.Name
		openForProject := filterDemands(demands, func(d DemandView) bool {
			return d.ProjectID == project.ID && isOpen(d.Status) && d.Status != "pending"
		})
		if len(openForProject) == 0 {
			continue
		}
		body.WriteString("<h3>" + escape(project.Name) + "（" + itoa(len(openForProject)) + "）</h3>")
		body.WriteString(renderTable(openForProject, projectNames))
	}

	unclassified := filterDemands(demands, func(d DemandView) bool {
		return d.ProjectID == "" && isOpen(d.Status) && d.Status != "pending"
	})
	body.WriteString("<h2>未分类需求</h2>")
	body.WriteString(renderTable(unclassified, projectNames))

	cutoff := generated.AddDate(0, 0, -30)
	recentDone := filterDemands(demands, func(d DemandView) bool {
		if d.Status != "done" {
			return false
		}
		when := d.UpdatedAt
		if d.CompletedAt != nil {
			when = *d.CompletedAt
		}
		return !when.Before(cutoff)
	})
	body.WriteString("<h2>最近 30 天完成</h2>")
	body.WriteString(renderTable(recentDone, projectNames))

	hashBytes := sha256.Sum256([]byte(body.String()))
	hash := hex.EncodeToString(hashBytes[:])[:16]
	body.WriteString(`<p><span text-color="gray">同步校验：` + EndMarkerPrefix + hash + `</span></p>`)
	return Rendered{ManagedXML: body.String(), Hash: hash}
}

func InitialDocument(rendered Rendered) string {
	return `<title>个人需求工作台</title>` +
		`<callout emoji="ℹ️" background-color="light-blue" border-color="blue"><p>此文档由 InfoWall 单向同步。请在 InfoWall 页面或 CLI 中更新需求。</p></callout>` +
		rendered.ManagedXML +
		`<h1>同步说明</h1><p>InfoWall 只维护上方自动同步区域；飞书同步失败不会回滚本地数据。</p>`
}

func renderSummary(demands []DemandView) string {
	statuses := []string{"pending", "planned", "active", "waiting", "done"}
	labels := map[string]string{"pending": "待确认", "planned": "已规划", "active": "进行中", "waiting": "等待中", "done": "已完成"}
	counts := map[string]int{}
	for _, demand := range demands {
		counts[demand.Status]++
	}
	var b strings.Builder
	b.WriteString(`<table><thead><tr>`)
	for _, status := range statuses {
		b.WriteString(`<th background-color="light-gray">` + labels[status] + `</th>`)
	}
	b.WriteString(`</tr></thead><tbody><tr>`)
	for _, status := range statuses {
		b.WriteString(`<td>` + itoa(counts[status]) + `</td>`)
	}
	b.WriteString(`</tr></tbody></table>`)
	return b.String()
}

func renderTable(demands []DemandView, projects map[string]string) string {
	if len(demands) == 0 {
		return `<p><span text-color="gray">暂无</span></p>`
	}
	var b strings.Builder
	b.WriteString(`<table><thead><tr><th background-color="light-gray">优先级</th><th background-color="light-gray">状态</th><th background-color="light-gray">需求</th><th background-color="light-gray">当前进展 / 下一步</th><th background-color="light-gray">更新时间</th><th background-color="light-gray">来源</th></tr></thead><tbody>`)
	for _, demand := range demands {
		b.WriteString(`<tr>`)
		b.WriteString(`<td>` + escape(strings.ToUpper(defaultString(demand.Priority, "none"))) + `</td>`)
		b.WriteString(`<td>` + escape(statusLabel(demand.Status)) + `</td>`)
		title := escape(demand.Title)
		if projects != nil && demand.ProjectID != "" && projects[demand.ProjectID] != "" {
			title += `<br/><span text-color="gray">` + escape(projects[demand.ProjectID]) + `</span>`
		} else if demand.ProjectHint != "" {
			title += `<br/><span text-color="gray">建议项目：` + escape(demand.ProjectHint) + `</span>`
		}
		b.WriteString(`<td>` + title + `</td>`)
		progress := demand.LatestProgress
		if progress == "" {
			progress = demand.NextAction
		} else if demand.NextAction != "" {
			progress += " / 下一步：" + demand.NextAction
		}
		if demand.Status == "waiting" && demand.BlockedReason != "" {
			progress += " / 等待：" + demand.BlockedReason
		}
		b.WriteString(`<td>` + escape(defaultString(progress, "—")) + `</td>`)
		updated := "—"
		if !demand.UpdatedAt.IsZero() {
			updated = demand.UpdatedAt.In(time.Local).Format("01-02 15:04")
		}
		b.WriteString(`<td>` + updated + `</td>`)
		if demand.SourceURL != "" {
			b.WriteString(`<td><a href="` + escapeAttr(demand.SourceURL) + `">查看来源</a></td>`)
		} else {
			b.WriteString(`<td>—</td>`)
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table>`)
	return b.String()
}

func filterDemands(demands []DemandView, keep func(DemandView) bool) []DemandView {
	out := make([]DemandView, 0, len(demands))
	for _, demand := range demands {
		if keep(demand) {
			out = append(out, demand)
		}
	}
	return out
}

func isOpen(status string) bool {
	return status != "done" && status != "dismissed"
}

func priorityRank(priority string) int {
	switch priority {
	case "p0":
		return 0
	case "p1":
		return 1
	case "p2":
		return 2
	case "p3":
		return 3
	default:
		return 4
	}
}

func statusLabel(status string) string {
	switch status {
	case "pending":
		return "待确认"
	case "planned":
		return "已规划"
	case "active":
		return "进行中"
	case "waiting":
		return "等待中"
	case "done":
		return "已完成"
	case "dismissed":
		return "已忽略"
	default:
		return status
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func escape(value string) string { return html.EscapeString(value) }

func escapeAttr(value string) string { return html.EscapeString(value) }

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for value > 0 {
		pos--
		buf[pos] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[pos:])
}
