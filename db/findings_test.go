package db

import (
	"fmt"
	"testing"
	"time"
)

func TestListFindingsFiltersJSONBAssetIDsByCompany(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer d.Close()

	suffix := time.Now().UnixNano()
	var companyID, explorationID, taskID, assetID, findingID int64
	if err := d.QueryRow(`INSERT INTO companies(name,nkey) VALUES ($1,$2) RETURNING id`,
		fmt.Sprintf("Finding Test %d", suffix), fmt.Sprintf("finding-test-%d", suffix)).Scan(&companyID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM findings WHERE id=$1`, findingID)
		_, _ = d.Exec(`DELETE FROM task_companies WHERE task_id=$1`, taskID)
		_, _ = d.Exec(`DELETE FROM tasks WHERE id=$1`, taskID)
		_, _ = d.Exec(`DELETE FROM explorations WHERE id=$1`, explorationID)
		_, _ = d.Exec(`DELETE FROM assets WHERE id=$1`, assetID)
		_, _ = d.Exec(`DELETE FROM companies WHERE id=$1`, companyID)
	})

	if err := d.QueryRow(`INSERT INTO explorations(description,goal) VALUES ('finding filter test','test') RETURNING id`).Scan(&explorationID); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(`INSERT INTO tasks(description,goal,exploration_id) VALUES ('finding filter test','test',$1) RETURNING id`, explorationID).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(`INSERT INTO assets(type,company_id,domain,root_domain) VALUES ('root_domain',$1,$2,$2) RETURNING id`,
		companyID, fmt.Sprintf("finding-test-%d.invalid", suffix)).Scan(&assetID); err != nil {
		t.Fatal(err)
	}
	findingID, err = d.AddFinding(taskID, 0, "Test Finding", "medium", "JSONB asset filter", "sanitized", "test", []int64{assetID})
	if err != nil {
		t.Fatal(err)
	}

	findings, err := d.ListFindings(10, companyID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range findings {
		if finding.ID == findingID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("finding %d not returned for company %d", findingID, companyID)
	}

	page, total, err := d.ListFindingsPage(FindingFilter{
		CompanyID: companyID,
		Severity:  "medium",
		VulnClass: "Test Finding",
		TaskID:    fmt.Sprint(taskID),
		Sort:      "severity",
	}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(page) != 1 || page[0].ID != findingID {
		t.Fatalf("paginated findings = total %d, rows %+v", total, page)
	}
	if len(page[0].CompanyIDs) != 1 || page[0].CompanyIDs[0] != companyID {
		t.Fatalf("company ids = %v, want [%d]", page[0].CompanyIDs, companyID)
	}

	if affected, err := d.SetFindingStatus(findingID, FindingInProgress); err != nil || affected != 1 {
		t.Fatalf("SetFindingStatus affected=%d err=%v", affected, err)
	}
	if affected, err := d.SetFindingReport(findingID, "# Test report"); err != nil || affected != 1 {
		t.Fatalf("SetFindingReport affected=%d err=%v", affected, err)
	}
	detail, err := d.GetFinding(findingID)
	if err != nil {
		t.Fatal(err)
	}
	if detail == nil || detail.Status != FindingInProgress || detail.Report != "# Test report" {
		t.Fatalf("finding detail = %+v", detail)
	}
}

func TestValidFindingStatus(t *testing.T) {
	valid := []string{
		FindingPending, FindingInProgress, FindingConfirmed, FindingResolved,
		FindingFalsePositive, FindingIgnored, FindingDuplicate, FindingRiskAccepted,
	}
	for _, status := range valid {
		if !ValidFindingStatus(status) {
			t.Errorf("status %q should be valid", status)
		}
	}
	for _, status := range []string{"", "open", "deleted", "CONFIRMED"} {
		if ValidFindingStatus(status) {
			t.Errorf("status %q should be invalid", status)
		}
	}
}
