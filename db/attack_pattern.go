package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AttackPattern is one attack-pattern library entry (ported from
// Pentest-RestXtra). verification: draft | validated | reference.
type AttackPattern struct {
	ID                   string    `json:"id"`
	Title                string    `json:"title"`
	Summary              string    `json:"summary"`
	AttackTechniqueID    string    `json:"attack_technique_id"`
	CveID                string    `json:"cve_id"`
	Tags                 string    `json:"tags"`
	Verification         string    `json:"verification"`
	EnvironmentSignature string    `json:"environment_signature"`
	ExecutionSteps       string    `json:"execution_steps"`
	ValidationNotes      string    `json:"validation_notes"`
	Source               string    `json:"source"`
	OriginProjectID      string    `json:"origin_project_id"`
	OriginSessionID      string    `json:"origin_session_id"`
	EvidenceRefs         string    `json:"evidence_refs"`
	Confidence           int       `json:"confidence"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

const attackPatternCols = `id, title, summary, attack_technique_id, cve_id, tags,
	verification, environment_signature, execution_steps, validation_notes, source,
	origin_project_id, origin_session_id, evidence_refs, confidence, created_at, updated_at`

func scanAttackPattern(row interface{ Scan(...any) error }, p *AttackPattern) error {
	return row.Scan(
		&p.ID, &p.Title, &p.Summary, &p.AttackTechniqueID, &p.CveID, &p.Tags,
		&p.Verification, &p.EnvironmentSignature, &p.ExecutionSteps, &p.ValidationNotes, &p.Source,
		&p.OriginProjectID, &p.OriginSessionID, &p.EvidenceRefs, &p.Confidence, &p.CreatedAt, &p.UpdatedAt,
	)
}

// CreateAttackPattern inserts a pattern (idempotent by id).
func (d *DB) CreateAttackPattern(p *AttackPattern) (*AttackPattern, error) {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	now := time.Now()
	if p.Verification == "" {
		p.Verification = "draft"
	}
	if p.EnvironmentSignature == "" {
		p.EnvironmentSignature = "{}"
	}
	if p.Source == "" {
		p.Source = "reference"
	}
	if p.EvidenceRefs == "" {
		p.EvidenceRefs = "[]"
	}
	_, err := d.Exec(`
INSERT INTO attack_patterns (`+attackPatternCols+`)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
ON CONFLICT (id) DO UPDATE SET title=EXCLUDED.title, summary=EXCLUDED.summary,
  attack_technique_id=EXCLUDED.attack_technique_id, cve_id=EXCLUDED.cve_id,
  tags=EXCLUDED.tags, verification=EXCLUDED.verification,
  environment_signature=EXCLUDED.environment_signature, execution_steps=EXCLUDED.execution_steps,
  validation_notes=EXCLUDED.validation_notes, source=EXCLUDED.source,
  confidence=EXCLUDED.confidence`,
		p.ID, strings.TrimSpace(p.Title), p.Summary, strings.TrimSpace(p.AttackTechniqueID),
		strings.TrimSpace(p.CveID), p.Tags, p.Verification, p.EnvironmentSignature,
		p.ExecutionSteps, p.ValidationNotes, p.Source,
		p.OriginProjectID, p.OriginSessionID, p.EvidenceRefs, p.Confidence, now, now)
	if err != nil {
		return nil, fmt.Errorf("创建攻击模式失败: %w", err)
	}
	p.CreatedAt, p.UpdatedAt = now, now
	return p, nil
}

// GetAttackPattern returns one pattern by id.
func (d *DB) GetAttackPattern(id string) (*AttackPattern, error) {
	p := &AttackPattern{}
	err := scanAttackPattern(d.QueryRow(`SELECT `+attackPatternCols+` FROM attack_patterns WHERE id=$1`, id), p)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// ListAttackPatterns returns patterns with optional filters + the match total.
func (d *DB) ListAttackPatterns(limit, offset int, techniqueID, cveID, verification, tag string) ([]*AttackPattern, int, error) {
	where := ""
	args := []any{}
	add := func(cond string, val any) {
		if where == "" {
			where = " WHERE "
		} else {
			where += " AND "
		}
		where += cond
		args = append(args, val)
	}
	if strings.TrimSpace(techniqueID) != "" {
		add("attack_technique_id ILIKE $"+itoaN(len(args)+1), "%"+strings.TrimSpace(techniqueID)+"%")
	}
	if strings.TrimSpace(cveID) != "" {
		add("cve_id ILIKE $"+itoaN(len(args)+1), "%"+strings.TrimSpace(cveID)+"%")
	}
	if strings.TrimSpace(verification) != "" {
		add("verification = $"+itoaN(len(args)+1), strings.TrimSpace(verification))
	}
	if strings.TrimSpace(tag) != "" {
		add("tags ILIKE $"+itoaN(len(args)+1), "%"+strings.TrimSpace(tag)+"%")
	}
	if limit <= 0 {
		limit = 100
	}
	total := 0
	_ = d.QueryRow(`SELECT count(*) FROM attack_patterns`+where, args...).Scan(&total)
	q := `SELECT ` + attackPatternCols + ` FROM attack_patterns` + where +
		` ORDER BY created_at DESC LIMIT $` + itoaN(len(args)+1) + ` OFFSET $` + itoaN(len(args)+2)
	args = append(args, limit, offset)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*AttackPattern{}
	for rows.Next() {
		p := &AttackPattern{}
		if err := scanAttackPattern(rows, p); err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}

// DeleteAttackPattern removes a pattern; returns the deleted count.
func (d *DB) DeleteAttackPattern(id string) (int64, error) {
	res, err := d.Exec(`DELETE FROM attack_patterns WHERE id=$1`, id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func itoaN(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// ---------------------------- playbook search ----------------------------

// PlaybookQuery describes a target environment for dual-path retrieval.
type PlaybookQuery struct {
	CVE        string   `json:"cve"`
	Technique  string   `json:"technique"`
	Components []string `json:"components"`
	Keywords   string   `json:"keywords"`
}

// PlaybookSearchResult is one scored match.
type PlaybookSearchResult struct {
	Pattern  *AttackPattern `json:"pattern"`
	Score    int            `json:"score"`
	TagHits  int            `json:"tag_hits"`
	TextHits int            `json:"text_hits"`
}

// SearchPlaybook runs the dual-path (structured + text) retrieval and returns
// matches ranked by score (confidence added). Ported from RestXtra playbook.go.
func (d *DB) SearchPlaybook(q PlaybookQuery, limit int) ([]*PlaybookSearchResult, error) {
	if limit <= 0 {
		limit = 10
	}
	patterns, _, err := d.ListAttackPatterns(2000, 0, "", "", "", "")
	if err != nil {
		return nil, err
	}
	queryTokens := tokenizePlaybookQuery(q)
	results := []*PlaybookSearchResult{}
	for _, p := range patterns {
		tagHits, textHits := scorePlaybookPattern(p, q, queryTokens)
		total := tagHits*3 + textHits
		if total <= 0 {
			continue
		}
		total += p.Confidence
		results = append(results, &PlaybookSearchResult{Pattern: p, Score: total, TagHits: tagHits, TextHits: textHits})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Pattern.Confidence > results[j].Pattern.Confidence
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func tokenizePlaybookQuery(q PlaybookQuery) []string {
	tokens := map[string]bool{}
	add := func(t string) {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" {
			tokens[t] = true
		}
	}
	if q.CVE != "" {
		add(q.CVE)
	}
	if q.Technique != "" {
		add(q.Technique)
	}
	for _, c := range q.Components {
		add(c)
	}
	for _, kw := range strings.FieldsFunc(q.Keywords, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	}) {
		add(kw)
	}
	out := make([]string, 0, len(tokens))
	for t := range tokens {
		out = append(out, t)
	}
	return out
}

func scorePlaybookPattern(p *AttackPattern, q PlaybookQuery, queryTokens []string) (tagHits, textHits int) {
	if q.CVE != "" && strings.EqualFold(strings.TrimSpace(p.CveID), strings.TrimSpace(q.CVE)) {
		tagHits += 3
	}
	if q.Technique != "" && strings.EqualFold(strings.TrimSpace(p.AttackTechniqueID), strings.TrimSpace(q.Technique)) {
		tagHits += 3
	}
	pTags := strings.ToLower(p.Tags)
	for _, c := range q.Components {
		cLower := strings.ToLower(strings.TrimSpace(c))
		if cLower != "" && strings.Contains(pTags, cLower) {
			tagHits++
		}
	}
	var envSig struct {
		Components []string `json:"components"`
	}
	if err := json.Unmarshal([]byte(p.EnvironmentSignature), &envSig); err == nil {
		envLower := strings.ToLower(strings.Join(envSig.Components, " "))
		for _, c := range q.Components {
			if strings.Contains(envLower, strings.ToLower(c)) {
				tagHits++
			}
		}
	}
	haystack := strings.ToLower(p.Title + " " + p.Summary + " " + p.ExecutionSteps + " " + p.ValidationNotes)
	for _, tok := range queryTokens {
		if strings.Contains(haystack, tok) {
			textHits++
		}
	}
	return
}
