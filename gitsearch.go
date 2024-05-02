package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/gobs/httpclient"
	"github.com/gobs/sortedmap"
	"github.com/google/go-github/github"
)

var templates = map[string]string{
	"html": `<html><head><title>Search results</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="https://unpkg.com/purecss@2.0.3/build/pure-min.css"
      integrity="sha384-cg6SkqEOCV1NbJoCu11+bm0NvBRc8IYLRGXkmNrqUBfTjmMYwNKPWBTIKyw9mHNJ" crossorigin="anonymous">
<style>
  body {
    margin: 8px;
    background-color: #f1f8ff;
  }

  h1, h2, h3, h4, h5, h6 {
    color: #333;
    margin-top: 10px;
    margin-bottom: 10px;
  }

  h1 {
    font-size: 1.5em;
    margin-left: 0px;
  }

  h2 {
    font-size: 1.2em;
    margin-left: 20px;
  }

  h3 {
    margin-left: 0px;
  }

  h4 {
    margin-left: 20px;
  }

  a {
    text-decoration: none;
    color: inherit;
  }

  .results {
    margin-left: 20px;
  }

  .file {
    background-color: #e0e0e0;
    padding: 1px;
    margin-left: 30px;
  }

  pre, code {
    background-color: #d0f0ff;
    margin: 4px;
  }
</style></head><body>
<h1>Query</h1><h2>{{.Query}}</h2><h1>Results</h1>
<div class="results">
{{- range .Repos}}
{{- with repovalue .}}
<div class="repo">
  <h3><a href="{{.Href}}">{{.Name}}</a></h3>
  {{- range .Files}}
    <h4><a href="{{.Href}}">{{.Path}}</a></h4>
    <div class="file">
      {{- range .Matches}}
      <pre style="background-color: #d0f0ff; margin: 4px">{{html .}}</pre>
      {{- end}}
    </div>
  {{- end}}
</div>
{{- end}}
{{- end}}
<hr/>
<h2>Repositories</h2>
<pre>
{{- range .Repos}}
{{- with repovalue .}}
  {{.Href}}
{{- end}}
{{- end}}
</pre>
</div></body></html>
`,

	"text": `
query: {{.Query}}

{{- range .Repos}}
{{- with repovalue .}}
--------------------------------------------------------------------------------
{{.Name}}
{{- range .Files}}
  {{.Path}}
    {{- range $i, $m := .Matches}}
      {{- if gt $i 0}}
      ...
      {{- end}}
      {{- range splitlines $m}}
  |   {{.}}
      {{- end}}
    {{- end}}
{{- end}}
{{- end}}
{{- end}}

================================================================================
Repositories:

{{- range .Repos}}
{{- with repovalue .}}
  {{.Href}}
{{- end}}
{{- end}}
`,
}

func retryAfter(resp *github.Response) time.Duration {
	if resp != nil && resp.Response.StatusCode == 403 {
		if val := resp.Response.Header.Get("Retry-After"); val != "" {
			after, _ := strconv.Atoi(val)
			return time.Duration(after) * time.Second
		}
	}

	return 0
}

type matchFile struct {
	Path    string
	Href    string
	Matches []string
}

type matchRepo struct {
	Name  string
	Href  string
	Files []matchFile
}

func repoValue(kv sortedmap.KeyValuePair[string, *matchRepo]) *matchRepo {
	return kv.Value
}

func openbrowser(url string) {
	var err error

	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		log.Fatal(err)
	}

}

func main() {
	server := flag.String("server", os.Getenv("GITHUB_BASE_URL"), "Enterprise server URL (use $GITHUB_BASE_URL if set, or https://api.github.com/ if empty)")
	token := flag.String("token", os.Getenv("GITHUB_TOKEN"), "authentication token (use $GITHUB_TOKEN if set)")

	filters := flag.String("filter", "", "apply these filters")
	all := flag.Bool("all", false, "print all results")
	highlight := flag.Bool("highlight", false, "highlight match in source code")
	debug := flag.Bool("debug", false, "enable request tracing")
	verbose := flag.Bool("verbose", false, "enable logging of results")
	format := flag.String("format", "text", "output format (text or html)")
	browse := flag.Bool("browse", false, "if format=html, open browser")
	listOrgs := flag.Bool("orgs", false, "list all organizations")
	ignoreCase := flag.Bool("ignore-case", false, "case insensitive search")
	flag.Parse()

	ttemplate := templates[*format]
	if ttemplate == "" {
		log.Fatalf("invalid format %q", *format)
	}

	q := strings.Join(flag.Args(), " ")
	if strings.Contains(q, " ") {
		q = `"` + q + `"`
	}

	// query if the full query (with filters)
	// q is the textual query (without filters)
	query := q

	if *filters != "" {
		query += " " + *filters
	}

	// some browsers support searching for text in the page
	textMatch := ""
	if *highlight {
		textMatch = fmt.Sprintf("#:~:text=%v", url.QueryEscape(q))
	}

	var httpClient *http.Client

	if *debug {
		httpClient = &http.Client{
			Transport: httpclient.LoggedTransport(httpclient.DefaultTransport, true, true, false),
		}
	}

	client := github.NewClient(httpClient).WithAuthToken(*token)

	if *server != "" {
		// GitHub enterprise
		eclient, err := client.WithEnterpriseURLs(*server, "")
		if err != nil {
			log.Fatal("Client: ", err)
		}

		client = eclient
	}

	ctx := context.Background()

	if *listOrgs {
		var opts github.OrganizationsListOptions

		for {
		retry_orgs:
			orgs, resp, err := client.Organizations.ListAll(ctx, &opts)
			if err != nil {
				if after := retryAfter(resp); after > 0 {
					if *verbose {
						log.Println(">>> Retry-After ", after)
					}
					time.Sleep(after)
					goto retry_orgs
				}

				log.Fatal("List Organizations: ", err)
			}

			if *verbose {
				log.Println(">>> Results", len(orgs))
				log.Println(">>> FirstPage", resp.FirstPage, "LastPage", resp.LastPage,
					"NextPage", resp.NextPage, "PrevPage", resp.PrevPage)
				log.Println(">>> NextPageToken", resp.NextPageToken, "Cursor", resp.Cursor)
				log.Println(">>> Rate", resp.Rate.String())
			}

			for _, org := range orgs {
				fmt.Println(org.GetLogin())
				opts.Since = org.GetID() // use last org.ID to fetch the next page
			}

			if len(orgs) == 0 {
				break
			}
		}

		return
	}

	opts := &github.SearchOptions{TextMatch: true, Sort: "indexed", Order: "desc"}
	opts.ListOptions.PerPage = 100 // this seems to be the maximum

	repos := map[string]*matchRepo{}

	stringContains := func(s, q string) bool {
		if *ignoreCase {
			s = strings.ToLower(s)
			q = strings.ToLower(q)
		}

		return strings.Contains(s, q)
	}

	for {
		if *verbose {
			log.Println(">>> Page", opts.Page)
		}

	retry_search:
		res, resp, err := client.Search.Code(ctx, query, opts)
		if err != nil {
			if after := retryAfter(resp); after > 0 {
				if *verbose {
					log.Println(">>> Retry-After ", after)
				}
				time.Sleep(after)
				goto retry_search
			}

			log.Fatal("Search: ", err)
		}

		if *verbose {
			log.Println(">>> Results", len(res.CodeResults), "Total", res.GetTotal())
			log.Println(">>> FirstPage", resp.FirstPage, "LastPage", resp.LastPage,
				"NextPage", resp.NextPage, "PrevPage", resp.PrevPage)
			log.Println(">>> NextPageToken", resp.NextPageToken, "Cursor", resp.Cursor)
			log.Println(">>> Rate", resp.Rate.String())
		}

		for _, r := range res.CodeResults {
			rname := r.GetRepository().GetFullName()

			repo := repos[rname]
			if repo == nil {
				repo = &matchRepo{Name: rname, Href: r.Repository.GetHTMLURL()}
				repos[rname] = repo
			}

			file := matchFile{Path: r.GetPath(), Href: r.GetHTMLURL() + textMatch}

			for _, m := range r.TextMatches {
				frag := m.GetFragment()
				if *all || stringContains(frag, q) {
					file.Matches = append(file.Matches, frag)
				}
			}

			if len(file.Matches) > 0 {
				repo.Files = append(repo.Files, file)
			}
		}

		if resp.NextPage > 0 {
			opts.Page = resp.NextPage
		} else {
			break
		}
	}

	for name, repo := range repos {
		if len(repo.Files) == 0 {
			delete(repos, name)
		}
	}

	functions := template.FuncMap{
		"repovalue":  repoValue,
		"splitlines": func(s string) []string { return strings.Split(s, "\n") },
	}

	values := struct {
		Query string
		Repos sortedmap.SortedMap[string, *matchRepo]
	}{
		Query: query,
		Repos: sortedmap.AsSortedMap(repos),
	}

	tmpl, err := template.New("results").Funcs(functions).Parse(ttemplate)
	if err != nil {
		log.Fatal("Parse template: ", err)
	}

	if *format == "html" && *browse {
		var b bytes.Buffer

		if err := tmpl.Execute(&b, values); err != nil {
			log.Fatal("Run template: ", err)
		}

		// note that by default data: URLs don't "open" in MacOS
		// and you need to add a mapping scheme -> app
		// (see for example SwiftDefaultApps)
		durl := fmt.Sprintf("data:text/html;base64,%v", base64.StdEncoding.EncodeToString(b.Bytes()))
		openbrowser(durl)
	} else {
		if err := tmpl.Execute(os.Stdout, values); err != nil {
			log.Fatal("Run template: ", err)
		}
	}
}
