// Package output renders diagnostics in the supported report formats.
package output

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/source"
	"github.com/ppreeper/rusq/internal/version"
)

// FileResult is the diagnostics for one file.
type FileResult struct {
	Path        string
	Locator     *source.Locator
	Diagnostics []diagnostic.Diagnostic
}

// Reporter renders results.
type Reporter struct {
	Format string
	Writer io.Writer
}

// Formats lists the supported output formats.
var Formats = []string{"concise", "full", "json", "json-lines", "github", "gitlab", "junit", "sarif", "grouped", "azure"}

// Report writes results in the configured format.
func (r *Reporter) Report(results []FileResult) error {
	if r.Writer == nil {
		return nil
	}
	switch strings.ToLower(r.Format) {
	case "", "concise":
		return r.concise(results, false)
	case "full":
		return r.concise(results, true)
	case "json":
		return r.jsonReport(results)
	case "json-lines":
		return r.jsonLines(results)
	case "github":
		return r.github(results)
	case "gitlab":
		return r.gitlab(results)
	case "junit":
		return r.junit(results)
	case "sarif":
		return r.sarif(results)
	case "grouped":
		return r.grouped(results)
	case "azure":
		return r.azure(results)
	default:
		return fmt.Errorf("unknown output format %q", r.Format)
	}
}

func (r *Reporter) concise(results []FileResult, full bool) error {
	var b strings.Builder
	for _, f := range results {
		loc := f.Locator
		for _, d := range f.Diagnostics {
			p := loc.Position(d.Span.Start)
			if !full {
				fmt.Fprintf(&b, "%s:%d:%d: %s %s\n", f.Path, p.Line, p.Col, d.Code, d.Message)
				continue
			}
			fmt.Fprintf(&b, "%s:%d:%d: %s %s\n", f.Path, p.Line, p.Col, d.Code, d.Message)
			fmt.Fprintf(&b, "  %s (%s)\n", d.Name, d.Severity)
			if d.Help != "" {
				fmt.Fprintf(&b, "  help: %s\n", d.Help)
			}
			if d.Fix != nil {
				fmt.Fprintf(&b, "  fix: %s\n", d.Fixability)
			}
		}
	}
	_, err := io.WriteString(r.Writer, b.String())
	return err
}

type jsonDiagnostic struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Severity string `json:"severity"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	EndLine  int    `json:"end_line"`
	EndCol   int    `json:"end_column"`
	Message  string `json:"message"`
	Help     string `json:"help,omitempty"`
	Fixable  string `json:"fixable,omitempty"`
}

type jsonFile struct {
	Path        string           `json:"path"`
	Diagnostics []jsonDiagnostic `json:"diagnostics"`
}

type jsonSummary struct {
	Files    int `json:"files"`
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Hints    int `json:"hints"`
}

type jsonReport struct {
	Files   []jsonFile  `json:"files"`
	Summary jsonSummary `json:"summary"`
}

func (r *Reporter) jsonReport(results []FileResult) error {
	rep := jsonReport{Files: []jsonFile{}}
	rep.Summary.Files = len(results)
	for _, f := range results {
		jf := jsonFile{Path: f.Path, Diagnostics: []jsonDiagnostic{}}
		for _, d := range f.Diagnostics {
			jd := toJSONDiagnostic(f.Locator, d)
			jf.Diagnostics = append(jf.Diagnostics, jd)
			rep.Summary.add(d.Severity)
		}
		rep.Files = append(rep.Files, jf)
	}
	enc := json.NewEncoder(r.Writer)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

func (s *jsonSummary) add(sev diagnostic.Severity) {
	switch sev {
	case diagnostic.Error:
		s.Errors++
	case diagnostic.Warning:
		s.Warnings++
	default:
		s.Hints++
	}
}

func toJSONDiagnostic(loc *source.Locator, d diagnostic.Diagnostic) jsonDiagnostic {
	p := loc.Position(d.Span.Start)
	e := loc.Position(d.Span.End)
	return jsonDiagnostic{
		Code: d.Code, Name: d.Name, Severity: d.Severity.String(),
		Line: p.Line, Column: p.Col, EndLine: e.Line, EndCol: e.Col,
		Message: d.Message, Help: d.Help, Fixable: d.Fixability.String(),
	}
}

type jsonLine struct {
	Path     string `json:"path"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Severity string `json:"severity"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Message  string `json:"message"`
}

func (r *Reporter) jsonLines(results []FileResult) error {
	enc := json.NewEncoder(r.Writer)
	for _, f := range results {
		for _, d := range f.Diagnostics {
			p := f.Locator.Position(d.Span.Start)
			if err := enc.Encode(jsonLine{
				Path: f.Path, Code: d.Code, Name: d.Name, Severity: d.Severity.String(),
				Line: p.Line, Column: p.Col, Message: d.Message,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Reporter) github(results []FileResult) error {
	for _, f := range results {
		for _, d := range f.Diagnostics {
			p := f.Locator.Position(d.Span.Start)
			kind := "warning"
			switch d.Severity {
			case diagnostic.Error:
				kind = "error"
			case diagnostic.Hint:
				kind = "notice"
			}
			fmt.Fprintf(r.Writer, "::%s file=%s,line=%d,col=%d::%s %s\n",
				kind, f.Path, p.Line, p.Col, d.Code, d.Message)
		}
	}
	return nil
}

func (r *Reporter) azure(results []FileResult) error {
	for _, f := range results {
		for _, d := range f.Diagnostics {
			p := f.Locator.Position(d.Span.Start)
			kind := "warning"
			if d.Severity == diagnostic.Error {
				kind = "error"
			}
			fmt.Fprintf(r.Writer, "##vso[task.logissue type=%s;sourcepath=%s;linenumber=%d;columnnumber=%d;code=%s;]%s\n",
				kind, f.Path, p.Line, p.Col, d.Code, d.Message)
		}
	}
	return nil
}

func (r *Reporter) grouped(results []FileResult) error {
	for _, f := range results {
		if len(f.Diagnostics) == 0 {
			continue
		}
		fmt.Fprintf(r.Writer, "%s:\n", f.Path)
		for _, d := range f.Diagnostics {
			p := f.Locator.Position(d.Span.Start)
			fmt.Fprintf(r.Writer, "  %d:%d  %s  %s\n", p.Line, p.Col, d.Code, d.Message)
		}
		fmt.Fprintln(r.Writer)
	}
	return nil
}

type gitlabIssue struct {
	Description string         `json:"description"`
	CheckName   string         `json:"check_name"`
	Fingerprint string         `json:"fingerprint"`
	Severity    string         `json:"severity"`
	Location    gitlabLocation `json:"location"`
}

type gitlabLocation struct {
	Path  string      `json:"path"`
	Lines gitlabLines `json:"lines"`
}

type gitlabLines struct {
	Begin int `json:"begin"`
}

func (r *Reporter) gitlab(results []FileResult) error {
	issues := []gitlabIssue{}
	for _, f := range results {
		for _, d := range f.Diagnostics {
			p := f.Locator.Position(d.Span.Start)
			sev := "minor"
			switch d.Severity {
			case diagnostic.Error:
				sev = "major"
			case diagnostic.Warning:
				sev = "minor"
			default:
				sev = "info"
			}
			issues = append(issues, gitlabIssue{
				Description: d.Message,
				CheckName:   d.Code,
				Fingerprint: f.Path + ":" + fmt.Sprint(d.Span.Start) + ":" + d.Code,
				Severity:    sev,
				Location:    gitlabLocation{Path: f.Path, Lines: gitlabLines{Begin: p.Line}},
			})
		}
	}
	enc := json.NewEncoder(r.Writer)
	enc.SetIndent("", "  ")
	return enc.Encode(issues)
}

func (r *Reporter) junit(results []FileResult) error {
	total, failures := 0, 0
	for _, f := range results {
		total += len(f.Diagnostics)
		failures += len(f.Diagnostics)
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	fmt.Fprintf(&b, `<testsuites tests="%d" failures="%d">`+"\n", total, failures)
	for _, f := range results {
		fmt.Fprintf(&b, `  <testsuite name="%s" tests="%d" failures="%d">`+"\n", xmlEscape(f.Path), len(f.Diagnostics), len(f.Diagnostics))
		for _, d := range f.Diagnostics {
			p := f.Locator.Position(d.Span.Start)
			fmt.Fprintf(&b, `    <testcase classname="%s" name="%s %d:%d">`+"\n", xmlEscape(f.Path), d.Code, p.Line, p.Col)
			fmt.Fprintf(&b, `      <failure message="%s">%s</failure>`+"\n", xmlEscape(d.Message), xmlEscape(d.Name))
			b.WriteString("    </testcase>\n")
		}
		b.WriteString("  </testsuite>\n")
	}
	b.WriteString("</testsuites>\n")
	_, err := io.WriteString(r.Writer, b.String())
	return err
}

type sarifLog struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version"`
	InformationURI string      `json:"informationUri,omitempty"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ShortDescription struct {
		Text string `json:"text"`
	} `json:"shortDescription"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           sarifRegion   `json:"region"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn"`
}

func (r *Reporter) sarif(results []FileResult) error {
	seen := map[string]bool{}
	var ruleList []sarifRule
	var out []sarifResult
	for _, f := range results {
		for _, d := range f.Diagnostics {
			if !seen[d.Code] {
				seen[d.Code] = true
				sr := sarifRule{ID: d.Code, Name: d.Name}
				sr.ShortDescription.Text = d.Name
				ruleList = append(ruleList, sr)
			}
			p := f.Locator.Position(d.Span.Start)
			level := "warning"
			if d.Severity == diagnostic.Error {
				level = "error"
			} else if d.Severity == diagnostic.Hint {
				level = "note"
			}
			out = append(out, sarifResult{
				RuleID:  d.Code,
				Level:   level,
				Message: sarifMessage{Text: d.Message},
				Locations: []sarifLocation{{
					PhysicalLocation: sarifPhysical{
						ArtifactLocation: sarifArtifact{URI: f.Path},
						Region:           sarifRegion{StartLine: p.Line, StartColumn: p.Col},
					},
				}},
			})
		}
	}
	sort.Slice(ruleList, func(i, j int) bool { return ruleList[i].ID < ruleList[j].ID })
	log := sarifLog{
		Version: "2.1.0",
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Runs: []sarifRun{{
			Tool:    sarifTool{Driver: sarifDriver{Name: "rusq", Version: version.Version, Rules: ruleList}},
			Results: out,
		}},
	}
	enc := json.NewEncoder(r.Writer)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// Counts returns error/warning/hint totals.
func Counts(results []FileResult) (errors, warnings, hints int) {
	for _, f := range results {
		for _, d := range f.Diagnostics {
			switch d.Severity {
			case diagnostic.Error:
				errors++
			case diagnostic.Warning:
				warnings++
			default:
				hints++
			}
		}
	}
	return
}

// Statistics returns per-rule counts.
func Statistics(results []FileResult) map[string]int {
	m := map[string]int{}
	for _, f := range results {
		for _, d := range f.Diagnostics {
			m[d.Code]++
		}
	}
	return m
}
