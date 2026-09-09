package db

import "testing"

func TestIncidentCRUD(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer d.Close()
	defer func() { _, _ = d.Exec(`DELETE FROM incidents WHERE title='incident-test'`) }()

	id, err := d.CreateIncident(&Incident{
		Title: "incident-test", Severity: "high", Source: "siem",
		AlertInfo: "beaconing on db-01", Assets: "db-01", IOCs: "1.2.3.4",
	})
	if err != nil {
		t.Fatal(err)
	}
	inc, err := d.GetIncident(id)
	if err != nil || inc == nil || inc.Status != "new" || inc.IOCs != "1.2.3.4" {
		t.Fatalf("get incident wrong: %+v err=%v", inc, err)
	}

	if err := d.SetIncidentStatus(id, "triaging"); err != nil {
		t.Fatal(err)
	}
	if err := d.AttachIncidentTask(id, "task-123"); err != nil {
		t.Fatal(err)
	}
	inc, _ = d.GetIncident(id)
	if inc.Status != "triaging" || inc.TaskID == nil || *inc.TaskID != "task-123" {
		t.Fatalf("status/task update wrong: %+v", inc)
	}

	list, err := d.ListIncidents("", 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range list {
		if v.ID == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("incident not listed: %+v", list)
	}
	t.Log("incident CRUD OK")
}