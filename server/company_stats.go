package server

import (
	"net/http"
)

// GET /api/dashboard/companies — 每个企业一份汇总卡片数据：
// 资产数、任务数、发现数、高危数。供仪表盘「企业概览」卡片区 + 选择器联动。
func (s *Server) dashboardCompanies(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	companies, err := pg.Companies().ListCompanies()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// 资产计数：按 assets.company_id 分组（company_id=0 表示未归属）。
	assetByCompany := map[int64]int{}
	rows, err := pg.Query(`SELECT COALESCE(company_id,0), COUNT(*) FROM assets GROUP BY company_id`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var cid int64
			var n int
			if rows.Scan(&cid, &n) == nil {
				assetByCompany[cid] = n
			}
		}
	}
	// 发现计数：findings → 任务企业 + 资产企业（并集）。
	fs, _ := pg.ListFindings(5000, 0)
	findingsByCompany := map[int64]int{}
	highByCompany := map[int64]int{}
	for _, f := range fs {
		for _, cid := range f.CompanyIDs {
			findingsByCompany[cid]++
			if f.Severity == "high" {
				highByCompany[cid]++
			}
		}
	}
	// 每企业的资产 host 清单（域名 + IP），用于前端过滤流量状态。
	hostsByCompany := pg.CompanyAssetHosts()

	type companyStat struct {
		ID       int64    `json:"id"`
		Name     string   `json:"name"`
		Assets   int      `json:"assets"`
		Tasks    int      `json:"tasks"`
		Findings int      `json:"findings"`
		High     int      `json:"high"`
		Hosts    []string `json:"hosts,omitempty"` // 资产域名+IP，供流量/资产维度过滤
	}
	out := make([]companyStat, 0, len(companies)+1)
	// 「未归属」桶：assets.company_id 为空的资产 + 无企业关联的任务/发现。
	unassigned := companyStat{Name: "未归属"}
	if n := assetByCompany[0]; n > 0 {
		unassigned.Assets = n
	}
	if n := findingsByCompany[0]; n > 0 {
		unassigned.Findings = n
		unassigned.High = highByCompany[0]
	}
	if tasks, _ := pg.CountTasksWithoutCompany(); tasks > 0 {
		unassigned.Tasks = int(tasks)
	}
	if unassigned.Assets+unassigned.Tasks+unassigned.Findings > 0 {
		out = append(out, unassigned)
	}
	for _, c := range companies {
		tasks, _ := pg.CountCompanyTasks(c.ID)
		out = append(out, companyStat{
			ID:       c.ID,
			Name:     c.Name,
			Assets:   assetByCompany[c.ID],
			Tasks:    int(tasks),
			Findings: findingsByCompany[c.ID],
			High:     highByCompany[c.ID],
			Hosts:    hostsByCompany[c.ID],
		})
	}
	writeJSON(w, 200, map[string]any{"companies": out})
}
