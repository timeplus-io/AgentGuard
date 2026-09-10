package tpapp

import (
	"bytes"
	"embed"
	"encoding/base64"
	"fmt"
	"io/fs"
	"path"
	"text/template"

	"github.com/timeplus/agentguard/internal/timeplus"
)

const (
	AppID      = "io.timeplus.agentguard"
	AppName    = "AgentGuard"
	AppVersion = "0.1.1" // bump with releases; the drift test pins the output (0.1.1: rules installed running, no SYSTEM PAUSE)
	AppDBName  = "ag"
)

//go:embed manifest.yaml.tmpl static
var assets embed.FS

// File is one output file, Path relative to the app directory ("/"-separated).
type File struct {
	Path    string
	Content []byte
}

// App is the fully rendered package: ordered resources and every file.
type App struct {
	Resources []Resource
	Files     []File
}

// Build renders the app entirely in memory. It needs no Timeplus connection.
func Build() (*App, error) {
	base := timeplus.AppResources()
	names := make([]string, 0, len(base)+len(CoreRuleIDs))
	for _, r := range base {
		names = append(names, r.Name)
	}
	for _, id := range CoreRuleIDs {
		names = append(names, timeplus.MVName(id))
	}

	var resources []Resource
	for _, r := range base {
		ddl := r.DDL
		var err error
		if r.Name == "agentguard_otel_input" {
			if ddl, err = TemplateOTelInput(ddl); err != nil {
				return nil, err
			}
		}
		sql, err := Transform(ddl, names)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r.Name, err)
		}
		resources = append(resources, Resource{Kind: string(r.Kind), Name: r.Name, SQL: sql})
	}
	ruleRes, err := RuleArtifacts(names)
	if err != nil {
		return nil, err
	}
	resources = append(resources, ruleRes...)

	var files []File
	for i := range resources {
		resources[i].File = fmt.Sprintf("ddl/%03d_%s.sql", i+1, resources[i].Name)
		files = append(files, File{Path: resources[i].File, Content: []byte(resources[i].SQL + "\n")})
	}

	icon, err := assets.ReadFile("static/icon.svg")
	if err != nil {
		return nil, err
	}
	manifest, err := renderManifest(resources, "data:image/svg+xml;base64,"+base64.StdEncoding.EncodeToString(bytes.TrimSpace(icon)))
	if err != nil {
		return nil, err
	}
	files = append([]File{{Path: "manifest.yaml", Content: manifest}}, files...)

	for _, p := range []string{"dashboards/main.json", "README.md", "Makefile"} {
		b, err := assets.ReadFile(path.Join("static", p))
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: p, Content: b})
	}
	return &App{Resources: resources, Files: files}, nil
}

func renderManifest(resources []Resource, icon string) ([]byte, error) {
	src, err := fs.ReadFile(assets, "manifest.yaml.tmpl")
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("manifest").Parse(string(src))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]any{
		"ID": AppID, "Name": AppName, "Version": AppVersion, "DBName": AppDBName,
		"Icon": icon, "Resources": resources,
	})
	if err != nil {
		return nil, fmt.Errorf("render manifest: %w", err)
	}
	return buf.Bytes(), nil
}
