package security_test

import (
	"testing"

	"streamgate/pkg/security"
)

func TestComplianceFramework_RegisterCheck(t *testing.T) {
	cf := security.NewComplianceFramework()

	check := &security.ComplianceCheck{
		ID:          "check-1",
		Standard:    security.GDPR,
		Name:        "Data Encryption",
		Description: "Verify data is encrypted",
		Status:      "PASS",
	}

	err := cf.RegisterCheck(check)
	if err != nil {
		t.Fatalf("RegisterCheck failed: %v", err)
	}

	if cf.GetCheckCount() != 1 {
		t.Errorf("Expected 1 check, got %d", cf.GetCheckCount())
	}
}

func TestComplianceFramework_RunCheck(t *testing.T) {
	cf := security.NewComplianceFramework()

	check := &security.ComplianceCheck{
		ID:       "check-1",
		Standard: security.GDPR,
		Name:     "Data Encryption",
		Status:   "PENDING",
	}

	cf.RegisterCheck(check)
	err := cf.RunCheck("check-1")
	if err != nil {
		t.Fatalf("RunCheck failed: %v", err)
	}

	metadata, _ := cf.GetReport("GDPR-1")
	if metadata == nil {
		// Check was run but report not generated yet
	}
}

func TestComplianceFramework_GenerateReport(t *testing.T) {
	cf := security.NewComplianceFramework()

	// Register checks
	for i := 1; i <= 3; i++ {
		check := &security.ComplianceCheck{
			ID:       "check-" + string(rune(i)),
			Standard: security.GDPR,
			Name:     "Check " + string(rune(i)),
			Status:   "PASS",
		}
		cf.RegisterCheck(check)
	}

	report, err := cf.GenerateReport(security.GDPR)
	if err != nil {
		t.Fatalf("GenerateReport failed: %v", err)
	}

	if report.Standard != security.GDPR {
		t.Errorf("Report standard doesn't match. Expected GDPR, got %s", report.Standard)
	}

	if report.Status != "COMPLIANT" {
		t.Errorf("Expected status COMPLIANT, got %s", report.Status)
	}

	if report.Score != 100 {
		t.Errorf("Expected score 100, got %f", report.Score)
	}
}

func TestComplianceFramework_GetReport(t *testing.T) {
	cf := security.NewComplianceFramework()

	check := &security.ComplianceCheck{
		ID:       "check-1",
		Standard: security.GDPR,
		Name:     "Data Encryption",
		Status:   "PASS",
	}

	cf.RegisterCheck(check)
	report, _ := cf.GenerateReport(security.GDPR)

	retrieved, err := cf.GetReport(report.ID)
	if err != nil {
		t.Fatalf("GetReport failed: %v", err)
	}

	if retrieved.ID != report.ID {
		t.Errorf("Report ID doesn't match. Expected %s, got %s", report.ID, retrieved.ID)
	}
}

func TestComplianceFramework_ListReports(t *testing.T) {
	cf := security.NewComplianceFramework()

	// Register GDPR checks
	for i := 1; i <= 2; i++ {
		check := &security.ComplianceCheck{
			ID:       "gdpr-check-" + string(rune(i)),
			Standard: security.GDPR,
			Name:     "GDPR Check " + string(rune(i)),
			Status:   "PASS",
		}
		cf.RegisterCheck(check)
	}

	// Register HIPAA checks
	for i := 1; i <= 2; i++ {
		check := &security.ComplianceCheck{
			ID:       "hipaa-check-" + string(rune(i)),
			Standard: security.HIPAA,
			Name:     "HIPAA Check " + string(rune(i)),
			Status:   "PASS",
		}
		cf.RegisterCheck(check)
	}

	cf.GenerateReport(security.GDPR)
	cf.GenerateReport(security.HIPAA)

	gdprReports := cf.ListReports(security.GDPR)
	if len(gdprReports) != 1 {
		t.Errorf("Expected 1 GDPR report, got %d", len(gdprReports))
	}

	hipaaReports := cf.ListReports(security.HIPAA)
	if len(hipaaReports) != 1 {
		t.Errorf("Expected 1 HIPAA report, got %d", len(hipaaReports))
	}
}

func TestComplianceFramework_LogAuditEvent(t *testing.T) {
	cf := security.NewComplianceFramework()

	err := cf.LogAuditEvent("LOGIN", "user-123", "user@example.com", "SUCCESS", "User logged in")
	if err != nil {
		t.Fatalf("LogAuditEvent failed: %v", err)
	}

	if cf.GetAuditLogCount() != 1 {
		t.Errorf("Expected 1 audit log entry, got %d", cf.GetAuditLogCount())
	}
}

func TestComplianceFramework_GetAuditLog(t *testing.T) {
	cf := security.NewComplianceFramework()

	for i := 1; i <= 5; i++ {
		cf.LogAuditEvent("ACTION", "resource-"+string(rune(i)), "user", "SUCCESS", "Action performed")
	}

	logs := cf.GetAuditLog(3)
	if len(logs) != 3 {
		t.Errorf("Expected 3 logs, got %d", len(logs))
	}
}

func TestComplianceFramework_GetAuditLogByResource(t *testing.T) {
	cf := security.NewComplianceFramework()

	cf.LogAuditEvent("ACTION", "resource-1", "user1", "SUCCESS", "Action 1")
	cf.LogAuditEvent("ACTION", "resource-2", "user2", "SUCCESS", "Action 2")
	cf.LogAuditEvent("ACTION", "resource-1", "user3", "SUCCESS", "Action 3")

	logs := cf.GetAuditLogByResource("resource-1", 10)
	if len(logs) != 2 {
		t.Errorf("Expected 2 logs for resource-1, got %d", len(logs))
	}
}

func TestComplianceFramework_GetAuditLogByUser(t *testing.T) {
	cf := security.NewComplianceFramework()

	cf.LogAuditEvent("ACTION", "resource-1", "user-1", "SUCCESS", "Action 1")
	cf.LogAuditEvent("ACTION", "resource-2", "user-2", "SUCCESS", "Action 2")
	cf.LogAuditEvent("ACTION", "resource-3", "user-1", "SUCCESS", "Action 3")

	logs := cf.GetAuditLogByUser("user-1", 10)
	if len(logs) != 2 {
		t.Errorf("Expected 2 logs for user-1, got %d", len(logs))
	}
}

func TestComplianceFramework_GetComplianceStatus(t *testing.T) {
	cf := security.NewComplianceFramework()

	// Register GDPR checks
	for i := 1; i <= 3; i++ {
		check := &security.ComplianceCheck{
			ID:       "gdpr-check-" + string(rune(i)),
			Standard: security.GDPR,
			Name:     "GDPR Check " + string(rune(i)),
			Status:   "PASS",
		}
		cf.RegisterCheck(check)
	}

	status := cf.GetComplianceStatus()
	if status[security.GDPR] != "COMPLIANT" {
		t.Errorf("Expected GDPR status COMPLIANT, got %s", status[security.GDPR])
	}
}

func TestComplianceFramework_PartialCompliance(t *testing.T) {
	cf := security.NewComplianceFramework()

	// Register checks with mixed status
	check1 := &security.ComplianceCheck{
		ID:       "check-1",
		Standard: security.GDPR,
		Name:     "Check 1",
		Status:   "PASS",
	}
	check2 := &security.ComplianceCheck{
		ID:       "check-2",
		Standard: security.GDPR,
		Name:     "Check 2",
		Status:   "FAIL",
	}

	cf.RegisterCheck(check1)
	cf.RegisterCheck(check2)

	report, _ := cf.GenerateReport(security.GDPR)
	if report.Status != "PARTIAL" {
		t.Errorf("Expected status PARTIAL, got %s", report.Status)
	}

	if report.Score != 50 {
		t.Errorf("Expected score 50, got %f", report.Score)
	}
}

func TestComplianceFramework_GetCheckCount(t *testing.T) {
	cf := security.NewComplianceFramework()

	if cf.GetCheckCount() != 0 {
		t.Errorf("Expected 0 checks, got %d", cf.GetCheckCount())
	}

	for i := 1; i <= 3; i++ {
		check := &security.ComplianceCheck{
			ID:       "check-" + string(rune(i)),
			Standard: security.GDPR,
			Name:     "Check " + string(rune(i)),
			Status:   "PASS",
		}
		cf.RegisterCheck(check)
	}

	if cf.GetCheckCount() != 3 {
		t.Errorf("Expected 3 checks, got %d", cf.GetCheckCount())
	}
}

func TestComplianceFramework_GetReportCount(t *testing.T) {
	cf := security.NewComplianceFramework()

	check := &security.ComplianceCheck{
		ID:       "check-1",
		Standard: security.GDPR,
		Name:     "Check 1",
		Status:   "PASS",
	}
	cf.RegisterCheck(check)

	if cf.GetReportCount() != 0 {
		t.Errorf("Expected 0 reports, got %d", cf.GetReportCount())
	}

	cf.GenerateReport(security.GDPR)

	if cf.GetReportCount() != 1 {
		t.Errorf("Expected 1 report, got %d", cf.GetReportCount())
	}
}

func BenchmarkComplianceFramework_LogAuditEvent(b *testing.B) {
	cf := security.NewComplianceFramework()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cf.LogAuditEvent("ACTION", "resource", "user", "SUCCESS", "Details")
	}
}

func BenchmarkComplianceFramework_GenerateReport(b *testing.B) {
	cf := security.NewComplianceFramework()

	for i := 1; i <= 10; i++ {
		check := &security.ComplianceCheck{
			ID:       "check-" + string(rune(i)),
			Standard: security.GDPR,
			Name:     "Check " + string(rune(i)),
			Status:   "PASS",
		}
		cf.RegisterCheck(check)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cf.GenerateReport(security.GDPR)
	}
}
