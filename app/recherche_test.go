package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const registreExemple = `{"servers":[
 {"server":{"name":"io.github.acme/weather","title":"Weather","description":"Forecasts","version":"1.0.0",
   "packages":[{"registryType":"pypi","identifier":"weather-mcp","version":"0.5.0","runtimeHint":"uvx","transport":{"type":"stdio"},
     "environmentVariables":[{"name":"WEATHER_API_KEY","isRequired":true,"isSecret":true},{"name":"UNITS","default":"celsius"}]}]},
  "_meta":{"io.modelcontextprotocol.registry/official":{"status":"active","isLatest":false}}},
 {"server":{"name":"io.github.acme/weather","title":"Weather","description":"Forecasts v2","version":"2.0.0",
   "packages":[{"registryType":"pypi","identifier":"weather-mcp","version":"0.6.0","transport":{"type":"stdio"},
     "environmentVariables":[{"name":"WEATHER_API_KEY","isRequired":true,"isSecret":true},{"name":"UNITS","default":"celsius"}]}]},
  "_meta":{"io.modelcontextprotocol.registry/official":{"status":"active","isLatest":true}}},
 {"server":{"name":"io.modelcontextprotocol/filesystem","description":"Files","version":"1.0.2",
   "packages":[{"registryType":"npm","identifier":"@modelcontextprotocol/server-filesystem","version":"1.0.2","transport":{"type":"stdio"},
     "packageArguments":[{"type":"positional","valueHint":"target_dir","isRequired":true}]}]},
  "_meta":{"io.modelcontextprotocol.registry/official":{"status":"active","isLatest":true}}},
 {"server":{"name":"com.example/remote","title":"Remote one","description":"Hosted","version":"1",
   "remotes":[{"type":"streamable-http","url":"https://mcp.example.com/mcp","headers":[{"name":"X-API-Key","isRequired":true,"isSecret":true},{"name":"X-Region","default":"eu"}]}]},
  "_meta":{"io.modelcontextprotocol.registry/official":{"status":"active","isLatest":true}}},
 {"server":{"name":"com.example/tenant","description":"Templated","version":"1",
   "remotes":[{"type":"streamable-http","url":"https://x.example.com/mcp/{tenant_id}"}],
   "packages":[{"registryType":"oci","identifier":"docker.io/x/y:1","transport":{"type":"stdio"}}]},
  "_meta":{"io.modelcontextprotocol.registry/official":{"status":"active","isLatest":true}}},
 {"server":{"name":"com.example/old","description":"Gone","version":"1","remotes":[{"type":"sse","url":"https://old.example.com/sse"}]},
  "_meta":{"io.modelcontextprotocol.registry/official":{"status":"deleted","isLatest":true}}}
]}`

func TestRegistreMCP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0.1/servers" || r.URL.Query().Get("search") == "" {
			w.WriteHeader(404)
			return
		}
		w.Write([]byte(registreExemple))
	}))
	defer srv.Close()
	avant := registreMCP
	registreMCP = srv.URL
	defer func() { registreMCP = avant }()
	l, err := chercherMCP("x")
	if err != nil || len(l) != 4 {
		t.Fatalf("%d servers, %v", len(l), err)
	}
	if l[0].Description != "Forecasts v2" {
		t.Fatalf("latest version not kept: %q", l[0].Description)
	}
	c, opt, genre, err := configDepuisRegistre(l[0])
	if err != nil || genre != "pypi" || c.Command != "uvx" || c.Args[0] != "weather-mcp==0.6.0" || c.Env["WEATHER_API_KEY"] != "${WEATHER_API_KEY}" || c.Env["UNITS"] != "celsius" || len(opt) != 0 {
		t.Fatalf("pypi: %+v %v %v", c, opt, err)
	}
	c, _, _, err = configDepuisRegistre(l[1])
	if err != nil || c.Command != "npx" || strings.Join(c.Args, " ") != "-y @modelcontextprotocol/server-filesystem@1.0.2 {PROJECT}" {
		t.Fatalf("npm with folder: %+v %v", c, err)
	}
	c, _, genre, err = configDepuisRegistre(l[2])
	if err != nil || genre != "remote" || c.URL != "https://mcp.example.com/mcp" || c.Headers["X-API-Key"] != "${X_API_KEY}" || c.Headers["X-Region"] != "eu" {
		t.Fatalf("remote: %+v %v", c, err)
	}
	if _, _, _, err = configDepuisRegistre(l[3]); err == nil {
		t.Fatal("templated URL and Docker only should not install automatically")
	}
}

func TestSkillsMP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "limite" {
			w.WriteHeader(429)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"skills": []map[string]any{
			{"name": "frontend-design", "author": "anthropics", "description": "UI", "githubUrl": "https://github.com/anthropics/skills/tree/main/skills/frontend-design", "stars": 10},
			{"name": "elsewhere", "author": "x", "githubUrl": "https://gitlab.com/x/y"},
		}}})
	}))
	defer srv.Close()
	avant := skillsmpBase
	skillsmpBase = srv.URL
	defer func() { skillsmpBase = avant }()
	l, err := chercherSkills("front")
	if err != nil || len(l) != 1 || l[0].Nom != "frontend-design" || l[0].Etoiles != 10 {
		t.Fatalf("%+v %v", l, err)
	}
	if _, err := chercherSkills("limite"); err == nil || !strings.Contains(err.Error(), "50") {
		t.Fatalf("rate limit: %v", err)
	}
}
