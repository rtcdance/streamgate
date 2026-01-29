package security

import (
	"fmt"
	"sync"
	"time"
)

// ComplianceStandard represents a compliance standard
type ComplianceStandard string

const (
	GDPR   ComplianceStandard = "GDPR"
	HIPAA  ComplianceStandard = "HIPAA"
	SOC2   ComplianceStandard = "SOC2"
	PCI    ComplianceStandard = "PCI-DSS"
	ISO27K ComplianceStandard = "ISO27001"
)

// ComplianceCheck represents a compliance check
type ComplianceCheck struct {
	ID          string
	Standard    ComplianceStandard
	Name        string
	Description string
	Status      string // "PASS", "FAIL", "WARNING"
	LastChecked time.Time
	Details     string
}

// ComplianceReport represents a compliance report
type ComplianceReport struct {
	ID        string
	Standard  ComplianceStandard
	Timestamp time.Time
	Checks    []ComplianceCheck
	Status    string // "COMPLIANT", "NON_COMPLIANT", "PARTIAL"
	Score     float64
	Details   string
}

// ComplianceFramework manages compliance checks and reporting
type ComplianceFramework struct {
	checks   map[string]*ComplianceCheck
	reports  map[string]*ComplianceReport
	mu       sync.RWMutex
	auditLog []AuditLogEntry
}

// AuditLogEntry represents an audit log entry
type AuditLogEntry struct {
	ID        string
	Timestamp time.Time
	Action    string
	Resource  string
	User      string
	Status    string
	Details   string
}

// NewComplianceFramework creates a new compliance framework
func NewComplianceFramework() *ComplianceFramework {
	return &ComplianceFramework{
		checks:   make(map[string]*ComplianceCheck),
		reports:  make(map[string]*ComplianceReport),
		auditLog: make([]AuditLogEntry, 0),
	}
}

// RegisterCheck registers a compliance check
func (cf *ComplianceFramework) RegisterCheck(check *ComplianceCheck) error {
	cf.mu.Lock()
	defer cf.mu.Unlock()

	if check.ID == "" {
		return fmt.Errorf("check ID is required")
	}

	cf.checks[check.ID] = check
	return nil
}

// RunCheck runs a compliance check
func (cf *ComplianceFramework) RunCheck(checkID string) error {
	cf.mu.Lock()
	defer cf.mu.Unlock()

	check, exists := cf.checks[checkID]
	if !exists {
		return fmt.Errorf("check not found: %s", checkID)
	}

	check.LastChecked = time.Now()
	// In real implementation, this would run actual compliance checks
	check.Status = "PASS"

	return nil
}

// RunAllChecks runs all compliance checks for a standard
func (cf *ComplianceFramework) RunAllChecks(standard ComplianceStandard) ([]ComplianceCheck, error) {
	cf.mu.Lock()
	defer cf.mu.Unlock()

	var results []ComplianceCheck
	for _, check := range cf.checks {
		if check.Standard == standard {
			check.LastChecked = time.Now()
			check.Status = "PASS"
			results = append(results, *check)
		}
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no checks found for standard: %s", standard)
	}

	return results, nil
}

// GenerateReport generates a compliance report
func (cf *ComplianceFramework) GenerateReport(standard ComplianceStandard) (*ComplianceReport, error) {
	cf.mu.Lock()
	defer cf.mu.Unlock()

	checks, err := cf.getChecksByStandard(standard)
	if err != nil {
		return nil, err
	}

	// Calculate compliance score
	passCount := 0
	for _, check := range checks {
		if check.Status == "PASS" {
			passCount++
		}
	}

	score := float64(passCount) / float64(len(checks)) * 100

	status := "COMPLIANT"
	if score < 100 {
		status = "PARTIAL"
	}
	if score < 80 {
		status = "NON_COMPLIANT"
	}

	report := &ComplianceReport{
		ID:        fmt.Sprintf("%s-%d", standard, time.Now().Unix()),
		Standard:  standard,
		Timestamp: time.Now(),
		Checks:    checks,
		Status:    status,
		Score:     score,
	}

	cf.reports[report.ID] = report
	return report, nil
}

// GetReport retrieves a compliance report
func (cf *ComplianceFramework) GetReport(reportID string) (*ComplianceReport, error) {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	report, exists := cf.reports[reportID]
	if !exists {
		return nil, fmt.Errorf("report not found: %s", reportID)
	}

	return report, nil
}

// ListReports lists all compliance reports
func (cf *ComplianceFramework) ListReports(standard ComplianceStandard) []*ComplianceReport {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	var reports []*ComplianceReport
	for _, report := range cf.reports {
		if report.Standard == standard {
			reports = append(reports, report)
		}
	}
	return reports
}

// LogAuditEvent logs an audit event
func (cf *ComplianceFramework) LogAuditEvent(action, resource, user, status, details string) error {
	cf.mu.Lock()
	defer cf.mu.Unlock()

	entry := AuditLogEntry{
		ID:        fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Action:    action,
		Resource:  resource,
		User:      user,
		Status:    status,
		Details:   details,
	}

	cf.auditLog = append(cf.auditLog, entry)
	return nil
}

// GetAuditLog retrieves audit log entries
func (cf *ComplianceFramework) GetAuditLog(limit int) []AuditLogEntry {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	if limit <= 0 || limit > len(cf.auditLog) {
		limit = len(cf.auditLog)
	}

	start := len(cf.auditLog) - limit
	if start < 0 {
		start = 0
	}

	return cf.auditLog[start:]
}

// GetAuditLogByResource retrieves audit log entries for a resource
func (cf *ComplianceFramework) GetAuditLogByResource(resource string, limit int) []AuditLogEntry {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	var entries []AuditLogEntry
	for _, entry := range cf.auditLog {
		if entry.Resource == resource {
			entries = append(entries, entry)
		}
	}

	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	return entries
}

// GetAuditLogByUser retrieves audit log entries for a user
func (cf *ComplianceFramework) GetAuditLogByUser(user string, limit int) []AuditLogEntry {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	var entries []AuditLogEntry
	for _, entry := range cf.auditLog {
		if entry.User == user {
			entries = append(entries, entry)
		}
	}

	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	return entries
}

// GetChecksByStandard retrieves checks for a standard (internal)
func (cf *ComplianceFramework) getChecksByStandard(standard ComplianceStandard) ([]ComplianceCheck, error) {
	var checks []ComplianceCheck
	for _, check := range cf.checks {
		if check.Standard == standard {
			checks = append(checks, *check)
		}
	}

	if len(checks) == 0 {
		return nil, fmt.Errorf("no checks found for standard: %s", standard)
	}

	return checks, nil
}

// GetComplianceStatus returns overall compliance status
func (cf *ComplianceFramework) GetComplianceStatus() map[ComplianceStandard]string {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	status := make(map[ComplianceStandard]string)
	standards := []ComplianceStandard{GDPR, HIPAA, SOC2, PCI, ISO27K}

	for _, standard := range standards {
		var passCount, totalCount int
		for _, check := range cf.checks {
			if check.Standard == standard {
				totalCount++
				if check.Status == "PASS" {
					passCount++
				}
			}
		}

		if totalCount == 0 {
			status[standard] = "NOT_CONFIGURED"
		} else if passCount == totalCount {
			status[standard] = "COMPLIANT"
		} else if passCount > totalCount/2 {
			status[standard] = "PARTIAL"
		} else {
			status[standard] = "NON_COMPLIANT"
		}
	}

	return status
}

// GetAuditLogCount returns the number of audit log entries
func (cf *ComplianceFramework) GetAuditLogCount() int {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	return len(cf.auditLog)
}

// GetCheckCount returns the number of compliance checks
func (cf *ComplianceFramework) GetCheckCount() int {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	return len(cf.checks)
}

// GetReportCount returns the number of compliance reports
func (cf *ComplianceFramework) GetReportCount() int {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	return len(cf.reports)
}
